package main

// Ortam gözlemcisi: ajan çalışırken HAProxy config'ini, süreçlerini, log kaynağını ve
// stats socket'ini izler. Config değişip HAProxy reload edilince yeni log biçimini,
// yeni Host yakalamasını vb. yeniden kurulum gerekmeden devreye alır.
// Hiçbir şeyi değiştirmez; sadece okur.

import (
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type Env struct {
	stats   *StatsPoller
	logs    *LogAnalyzer // nil: log analizi kapalı
	logAuto bool         // log kaynağını ajan kendisi bulsun
	find    func() ([]int, []string, string)

	mu         sync.Mutex
	cfg        *haConfig
	parser     *LogParser
	fp         string
	checkedAt  time.Time
	changedAt  time.Time
	logPickAt  time.Time
	sockPickAt time.Time
	logNote    string
	pids       []int
}

func NewEnv(stats *StatsPoller, logs *LogAnalyzer, logAuto bool) *Env {
	return &Env{stats: stats, logs: logs, logAuto: logAuto, find: findConfigs}
}

func (e *Env) Run() {
	e.refresh()
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	n := 0
	for range t.C {
		n++
		switch {
		case n%6 == 0: // 30 saniyede bir tam kontrol
			e.refresh()
		case e.logs != nil && e.logAuto && e.logs.Source() == "":
			e.maybePickLog(false) // log kaynağı henüz yok: 5 saniyede bir ara
		}
	}
}

// Config dosyalarının değişim zamanı, boyutu ve HAProxy süreç numaraları.
// Reload olunca süreç numaraları değişir; config düzenlenince zaman/boyut değişir.
func fingerprint(files []string, pids []int) string {
	var b strings.Builder
	for _, f := range files {
		if st, err := os.Stat(f); err == nil {
			fmt.Fprintf(&b, "%s|%d|%d;", f, st.ModTime().UnixNano(), st.Size())
		} else {
			fmt.Fprintf(&b, "%s|yok;", f)
		}
	}
	ps := append([]int(nil), pids...)
	sort.Ints(ps)
	fmt.Fprintf(&b, "%v", ps)
	return b.String()
}

func (e *Env) refresh() {
	pids, files, cfgNote := e.find()
	fp := fingerprint(files, pids)
	now := time.Now()

	e.mu.Lock()
	changed := fp != e.fp
	e.mu.Unlock()
	if changed {
		hc := buildHAConfig(files)
		if cfgNote != "" && len(files) == 0 {
			hc.Errors = append(hc.Errors, cfgNote)
		}
		p := buildLogParser(hc)
		if e.logs != nil {
			e.logs.SetParser(p)
		}
		e.mu.Lock()
		first := e.fp == ""
		e.cfg, e.parser, e.fp, e.changedAt, e.pids = hc, p, fp, now, pids
		e.mu.Unlock()
		if !first {
			log.Printf("HAProxy config'i ya da süreçleri değişti; ayarlar yeniden okundu (%d frontend, %d biçim)", len(hc.Frontends), len(p.Formats()))
		}
	}
	if e.logs != nil && e.logAuto {
		e.maybePickLog(changed)
	}
	e.maybeSwitchSocket(files)
	e.mu.Lock()
	e.checkedAt = now
	e.mu.Unlock()
}

// Log kaynağı yoksa ya da 5 dakikadır susuyorsa adayları yeniden tarar.
func (e *Env) maybePickLog(cfgChanged bool) {
	cur := e.logs.Source()
	idle := time.Since(e.logs.LastLine())
	e.mu.Lock()
	sincePick := time.Since(e.logPickAt)
	parser := e.parser
	e.mu.Unlock()
	need := (cur == "" && sincePick > 4*time.Second) || (idle > 5*time.Minute && sincePick > 2*time.Minute) || (cfgChanged && sincePick > 2*time.Minute && idle > time.Minute)
	if !need {
		return
	}
	cands, best := scanLogSources(parser, "auto")
	if best == nil {
		for i := range cands {
			if cands[i].Readble && (best == nil || cands[i].Traffic > best.Traffic) {
				best = &cands[i]
			}
		}
	}
	e.mu.Lock()
	e.logPickAt = time.Now()
	if best == nil {
		d := &detection{Logs: cands}
		if e.cfg != nil {
			for _, t := range e.cfg.Global {
				d.LogTargets = append(d.LogTargets, t.Addr)
			}
		}
		e.logNote = explainNoLog(d)
	} else {
		e.logNote = ""
	}
	e.mu.Unlock()
	if best != nil {
		src := best.Path
		if !strings.HasPrefix(src, "journal:") {
			src = "file:" + src
		}
		if src != cur {
			log.Printf("Log kaynağı: %s", src)
			e.logs.SetSource(src)
		}
	}
}

// Stats bir dakikadır okunamıyorsa config'teki diğer socket'leri dener.
func (e *Env) maybeSwitchSocket(files []string) {
	if e.stats.FailingFor() < time.Minute {
		return
	}
	e.mu.Lock()
	if time.Since(e.sockPickAt) < time.Minute {
		e.mu.Unlock()
		return
	}
	e.sockPickAt = time.Now()
	e.mu.Unlock()
	cur := e.stats.Socket()
	for _, c := range socketsFromConfig(files) {
		checkSocket(&c)
		if c.Usable && c.Addr != cur {
			log.Printf("Stats socket'i %s çalışmıyor; %s kullanılıyor", cur, c.Addr)
			e.stats.SetSocket(c.Addr)
			return
		}
	}
}

func (e *Env) LogNote() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.logNote
}

// ---------- API ----------

type FEInfo struct {
	Name     string `json:"name"`
	Mode     string `json:"mode"`
	Format   string `json:"format"`
	HostFrom string `json:"hostFrom"`
	Logs     bool   `json:"logs"`
}

type ConfigInfo struct {
	Version   string   `json:"version"`
	Files     []string `json:"files"`
	CheckedAt int64    `json:"checkedAt"`
	ChangedAt int64    `json:"changedAt"`
	Socket    string   `json:"socket"`
	LogSource string   `json:"logSource"`
	Frontends []FEInfo `json:"frontends"`
	Notes     []Note   `json:"notes"`
}

func (e *Env) Info() ConfigInfo {
	e.mu.Lock()
	hc, logNote := e.cfg, e.logNote
	info := ConfigInfo{Version: version, CheckedAt: e.checkedAt.UnixMilli(), ChangedAt: e.changedAt.UnixMilli(), Socket: e.stats.Socket()}
	e.mu.Unlock()
	obs := logObservation{}
	if e.logs != nil {
		obs = e.logs.Observe(15)
		info.LogSource = obs.Source
	}
	if hc != nil {
		info.Files = hc.Files
		for _, fe := range hc.Frontends {
			fi := FEInfo{Name: fe.Name, Mode: fe.Mode, Format: fe.FormatKind, Logs: !fe.NoLog && len(fe.Targets) > 0}
			cf := compileFormat(fe.FormatKind, fe.Format)
			switch {
			case cf != nil && fe.Format != "" && cf.has[roleHost]:
				fi.HostFrom = "log biçimi"
			case cf != nil && fe.Format != "" && cf.has[roleSNI]:
				fi.HostFrom = "SNI"
			case fe.HostSlot >= 0 && fe.Captures[fe.HostSlot].Cond:
				fi.HostFrom = "koşullu yakalama"
			case fe.HostSlot >= 0:
				fi.HostFrom = "Host yakalama"
			}
			info.Frontends = append(info.Frontends, fi)
		}
	}
	info.Notes = runtimeNotes(hc, obs, e.stats.BackendRequests(15), logNote)
	return info
}
