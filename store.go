package main

// Geçmişin diske yazılması. Amaç yalnızca ajan yeniden başladığında (sürüm güncellemesi gibi)
// geçmişin kaybolmaması. Veritabanı kullanmıyoruz: tek bir sıkıştırılmış JSON dosyası,
// dakikada bir, önce geçici dosyaya yazılıp sonra yerine taşınır (yarım dosya kalmaz).

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

const stateFile = "history.json.gz"

type stateDump struct {
	Version int         `json:"version"`
	SavedAt int64       `json:"savedAt"`
	Minutes []MinuteAgg `json:"minutes"`
	Log     []logMinute `json:"log"`
}

// Bir dakikanın sadeleşmiş log özeti (diske yazılabilir hâli)
type logMinute struct {
	T        int64                  `json:"t"` // dakika (unix dakika)
	Level    int                    `json:"lv"`
	Kinds    map[string]int64       `json:"k"`
	Classes  [4]int64               `json:"c"`
	WithHost int64                  `json:"wh"`
	Backends map[string]*backendAgg `json:"be,omitempty"`
	Paths    []pathDump             `json:"p,omitempty"`
	Blocked  []blockDump            `json:"b,omitempty"`
	Clients  []clientDump           `json:"cl,omitempty"`
}

type pathDump struct {
	Backend, Method, Path string
	Kind                  string
	N, S2, S3, S4, S5     int64
	SumTa, NTa            int64
	Codes                 map[int]int64
}
type blockDump struct {
	Kind, Method, Path string
	N                  int64
}
type clientDump struct {
	IP         string
	N, Blocked int64
}

func (p *StatsPoller) dumpMinutes() []MinuteAgg {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]MinuteAgg, len(p.minutes))
	copy(out, p.minutes)
	p.dirty = false
	return out
}

func (p *StatsPoller) loadMinutes(m []MinuteAgg) {
	p.mu.Lock()
	defer p.mu.Unlock()
	kesim := time.Now().UnixMilli() - int64(p.retention)*60_000
	for _, a := range m {
		if a.T >= kesim {
			p.minutes = append(p.minutes, a)
		}
	}
	p.trimMinutes()
}

func (a *LogAnalyzer) dumpBuckets() []logMinute {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.bakim()
	a.dirty = false
	out := make([]logMinute, 0, len(a.buckets))
	for t, b := range a.buckets {
		lm := logMinute{T: t, Level: b.level, Kinds: b.kinds, Classes: b.classes, WithHost: b.withHost, Backends: b.backends}
		for k, v := range b.paths {
			lm.Paths = append(lm.Paths, pathDump{Backend: k.Backend, Method: k.Method, Path: k.Path, Kind: v.Kind,
				N: v.N, S2: v.S2, S3: v.S3, S4: v.S4, S5: v.S5, SumTa: v.SumTa, NTa: v.NTa, Codes: v.Codes})
		}
		for k, v := range b.blocked {
			lm.Blocked = append(lm.Blocked, blockDump{Kind: k.Kind, Method: k.Method, Path: k.Path, N: v.N})
		}
		for ip, v := range b.clients {
			lm.Clients = append(lm.Clients, clientDump{IP: ip, N: v.N, Blocked: v.Blocked})
		}
		out = append(out, lm)
	}
	return out
}

func (a *LogAnalyzer) loadBuckets(ms []logMinute) {
	a.mu.Lock()
	defer a.mu.Unlock()
	kesim := time.Now().Unix()/60 - int64(a.retention)
	for _, lm := range ms {
		if lm.T < kesim || a.buckets[lm.T] != nil {
			continue
		}
		b := newBucket()
		// Diskten gelen dakikalar her zaman sadeleşmiş sayılır: ayrıntılar (tam adres, IP dökümü)
		// geri yüklenmez, sayılar ve listeler yüklenir.
		b.level = lm.Level
		if b.level < 1 {
			b.level = 1
		}
		b.kinds, b.classes, b.withHost = lm.Kinds, lm.Classes, lm.WithHost
		if b.kinds == nil {
			b.kinds = map[string]int64{}
		}
		if lm.Backends != nil {
			b.backends = lm.Backends
		}
		for _, d := range lm.Paths {
			b.paths[pathKey{d.Backend, d.Method, d.Path}] = &pathAgg{Kind: d.Kind, N: d.N, S2: d.S2, S3: d.S3,
				S4: d.S4, S5: d.S5, SumTa: d.SumTa, NTa: d.NTa, Codes: d.Codes}
		}
		for _, d := range lm.Blocked {
			b.blocked[blockKey{d.Kind, d.Method, d.Path}] = &blockAgg{N: d.N}
		}
		for _, d := range lm.Clients {
			b.clients[d.IP] = &clientAgg{N: d.N, Blocked: d.Blocked}
		}
		a.buckets[lm.T] = b
	}
	a.bakim()
}

// ---------- Dosya ----------

type Store struct {
	dir   string
	stats *StatsPoller
	logs  *LogAnalyzer
}

func NewStore(dir string, stats *StatsPoller, logs *LogAnalyzer) *Store {
	return &Store{dir: dir, stats: stats, logs: logs}
}

func (s *Store) path() string { return filepath.Join(s.dir, stateFile) }

func (s *Store) Load() error {
	f, err := os.Open(s.path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil // ilk çalıştırma
		}
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("dosya okunamadı (bozuk olabilir): %w", err)
	}
	defer gz.Close()
	var d stateDump
	if err := json.NewDecoder(gz).Decode(&d); err != nil {
		return fmt.Errorf("dosya çözülemedi (bozuk olabilir): %w", err)
	}
	if s.stats != nil {
		s.stats.loadMinutes(d.Minutes)
	}
	if s.logs != nil {
		s.logs.loadBuckets(d.Log)
	}
	log.Printf("Geçmiş diskten yüklendi: %d dakika istatistik, %d dakika log", len(d.Minutes), len(d.Log))
	return nil
}

func (s *Store) Save() error {
	d := stateDump{Version: 1, SavedAt: time.Now().UnixMilli()}
	if s.stats != nil {
		d.Minutes = s.stats.dumpMinutes()
	}
	if s.logs != nil {
		d.Log = s.logs.dumpBuckets()
	}
	if err := os.MkdirAll(s.dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, "history-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	gz := gzip.NewWriter(tmp)
	if err := json.NewEncoder(gz).Encode(d); err != nil {
		tmp.Close()
		return err
	}
	if err := gz.Close(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o640); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path())
}

// Dakikada bir, yalnızca değişiklik varsa yazar.
func (s *Store) Run() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	var hata int
	for range t.C {
		if s.stats != nil && !s.stats.isDirty() && s.logs != nil && !s.logs.isDirty() {
			continue
		}
		if err := s.Save(); err != nil {
			hata++
			if hata <= 3 || hata%60 == 0 {
				log.Printf("Geçmiş diske yazılamadı: %v", err)
			}
			continue
		}
		hata = 0
	}
}

func (p *StatsPoller) isDirty() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.dirty
}

func (a *LogAnalyzer) isDirty() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.dirty
}
