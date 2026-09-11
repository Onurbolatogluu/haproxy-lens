package main

// HAProxy config'ini SADECE OKUR ve her frontend için panelin ihtiyaç duyduğu
// "etkin" ayarları çıkarır: mod, log biçimi, log hedefleri, Host yakalama slotu,
// dontlog-normal gibi log'u kısıtlayan seçenekler, backend'lerdeki sağlık kontrolleri.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type cfgDirective struct {
	File   string
	Line   int
	Fields []string // tırnaklar ve kaçış karakterleri çözülmüş
}

func (d cfgDirective) where() string { return fmt.Sprintf("%s:%d", filepath.Base(d.File), d.Line) }

type cfgSection struct {
	Kind, Name, From string
	File             string
	Line             int
	Dirs             []cfgDirective
}

var sectionKinds = map[string]bool{
	"global": true, "defaults": true, "frontend": true, "backend": true, "listen": true,
	"userlist": true, "peers": true, "resolvers": true, "mailers": true, "program": true,
	"http-errors": true, "ring": true, "cache": true, "log-forward": true, "crt-store": true,
	"traces": true, "acme": true,
}

// HAProxy'nin satır sözdizimi: boşlukla ayrılır; "..." ve '...' tırnakları, \ kaçışı; # yorum.
func splitCfgLine(line string) []string {
	var out []string
	var cur strings.Builder
	inTok := false
	var quote rune
	rs := []rune(line)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case quote != 0:
			if r == '\\' && quote == '"' && i+1 < len(rs) {
				i++
				cur.WriteRune(rs[i])
			} else if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\\' && i+1 < len(rs):
			i++
			cur.WriteRune(rs[i])
			inTok = true
		case r == '"' || r == '\'':
			quote = r
			inTok = true
		case r == '#':
			i = len(rs)
		case r == ' ' || r == '\t':
			if inTok {
				out = append(out, cur.String())
				cur.Reset()
				inTok = false
			}
		default:
			cur.WriteRune(r)
			inTok = true
		}
	}
	if inTok {
		out = append(out, cur.String())
	}
	return out
}

func parseHAConfig(files []string) ([]cfgSection, []string) {
	var secs []cfgSection
	var errs []string
	cur := -1
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s okunamadı: %v", f, err))
			continue
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		n := 0
		for sc.Scan() {
			n++
			fs := splitCfgLine(sc.Text())
			if len(fs) == 0 {
				continue
			}
			if sectionKinds[fs[0]] {
				s := cfgSection{Kind: fs[0], File: f, Line: n}
				if len(fs) > 1 {
					s.Name = fs[1]
				}
				for i := 1; i+1 < len(fs); i++ {
					if fs[i] == "from" {
						s.From = fs[i+1]
					}
				}
				secs = append(secs, s)
				cur = len(secs) - 1
				continue
			}
			if cur >= 0 {
				secs[cur].Dirs = append(secs[cur].Dirs, cfgDirective{File: f, Line: n, Fields: fs})
			}
		}
		fh.Close()
	}
	return secs, errs
}

// ---------- Etkin frontend ayarları ----------

type logTarget struct {
	Addr, Facility, Level string
	Where                 string
}

func (t logTarget) remote() bool {
	a := t.Addr
	for _, p := range []string{"/", "127.", "localhost", "stdout", "stderr", "ring@", "fd@", "unix@", "::1"} {
		if strings.HasPrefix(a, p) {
			return false
		}
	}
	return true
}

var logLevels = map[string]int{"emerg": 0, "alert": 1, "crit": 2, "err": 3, "warning": 4, "notice": 5, "info": 6, "debug": 7}

// Trafik log'ları "info" seviyesinde gönderilir; hedefin üst sınırı info'dan düşükse düşer.
func (t logTarget) dropsTraffic() bool {
	l, ok := logLevels[t.Level]
	return ok && l < logLevels["info"]
}

var facilities = map[string]bool{"kern": true, "user": true, "mail": true, "daemon": true, "auth": true, "syslog": true,
	"lpr": true, "news": true, "uucp": true, "cron": true, "auth2": true, "ftp": true, "ntp": true, "audit": true,
	"alert": true, "cron2": true, "local0": true, "local1": true, "local2": true, "local3": true, "local4": true,
	"local5": true, "local6": true, "local7": true}

func parseLogTarget(d cfgDirective) logTarget {
	fs := d.Fields
	t := logTarget{Where: d.where()}
	if len(fs) > 1 {
		t.Addr = fs[1]
	}
	for i := 2; i < len(fs); i++ {
		if facilities[fs[i]] {
			t.Facility = fs[i]
			if i+1 < len(fs) {
				if _, ok := logLevels[fs[i+1]]; ok {
					t.Level = fs[i+1]
				}
			}
			break
		}
	}
	return t
}

type captureSlot struct {
	Index int
	Name  string // başlık adı ya da örnek ifadesi
	Host  bool
	Cond  bool // if/unless ile koşullu
	Where string
}

type proxyLogCfg struct {
	Mode          string
	FormatKind    string // httplog, httpslog, tcplog, clf, custom, default
	Format        string
	FormatWhere   string
	DontLogNormal bool
	DLNWhere      string
	Targets       []logTarget
	NoLog         bool
}

type feCfg struct {
	Name, Kind string // Kind: frontend ya da listen
	Where      string
	proxyLogCfg
	Captures []captureSlot
	HostSlot int  // Host başlığının yakalandığı slot; yoksa -1
	IsStats  bool // sadece stats sayfası sunan bölüm (notlarda atlanır)
}

type beCfg struct {
	Name    string
	Servers []string
	NoCheck []string // check olmayan sunucular
	Where   string
}

type haConfig struct {
	Files     []string
	Errors    []string
	Global    []logTarget
	Frontends []*feCfg
	Backends  []*beCfg
}

func isHostExpr(s string) bool {
	s = strings.ToLower(strings.ReplaceAll(s, " ", ""))
	for _, p := range []string{"req.hdr(host", "req.fhdr(host", "hdr(host", "req.hdr_ip(host", "req.hdr(:authority", "req.fhdr(:authority"} {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func hasCond(fs []string) bool {
	for _, f := range fs {
		if f == "if" || f == "unless" {
			return true
		}
	}
	return false
}

// Bir bölümdeki log ile ilgili yönergeleri, önceki değerlerin üstüne uygular (defaults mirası için).
func applyLogDirectives(base proxyLogCfg, dirs []cfgDirective, global []logTarget) proxyLogCfg {
	c := base
	c.Targets = append([]logTarget(nil), base.Targets...)
	for _, d := range dirs {
		fs := d.Fields
		switch {
		case fs[0] == "mode" && len(fs) > 1:
			c.Mode = fs[1]
		case fs[0] == "option" && len(fs) > 1 && fs[1] == "httplog":
			if len(fs) > 2 && fs[2] == "clf" {
				c.FormatKind, c.Format = "clf", ""
			} else {
				c.FormatKind, c.Format = "httplog", fmtHTTPLog
			}
			c.FormatWhere = d.where()
		case fs[0] == "option" && len(fs) > 1 && fs[1] == "httpslog":
			c.FormatKind, c.Format, c.FormatWhere = "httpslog", fmtHTTPSLog, d.where()
		case fs[0] == "option" && len(fs) > 1 && fs[1] == "tcplog":
			c.FormatKind, c.Format, c.FormatWhere = "tcplog", fmtTCPLog, d.where()
		case fs[0] == "log-format" && len(fs) > 1:
			c.FormatKind, c.Format, c.FormatWhere = "custom", strings.Join(fs[1:], " "), d.where()
		case fs[0] == "no" && len(fs) > 2 && fs[1] == "option" && (fs[2] == "httplog" || fs[2] == "httpslog" || fs[2] == "tcplog"):
			c.FormatKind, c.Format, c.FormatWhere = "default", "", d.where()
		case fs[0] == "option" && len(fs) > 1 && fs[1] == "dontlog-normal":
			c.DontLogNormal, c.DLNWhere = true, d.where()
		case fs[0] == "no" && len(fs) > 2 && fs[1] == "option" && fs[2] == "dontlog-normal":
			c.DontLogNormal = false
		case fs[0] == "no" && len(fs) > 1 && fs[1] == "log":
			c.NoLog, c.Targets = true, nil
		case fs[0] == "log" && len(fs) > 1:
			c.NoLog = false
			if fs[1] == "global" {
				c.Targets = append(c.Targets, global...)
			} else {
				c.Targets = append(c.Targets, parseLogTarget(d))
			}
		}
	}
	return c
}

func buildHAConfig(files []string) *haConfig {
	secs, errs := parseHAConfig(files)
	hc := &haConfig{Files: files, Errors: errs}
	for _, s := range secs {
		if s.Kind == "global" {
			for _, d := range s.Dirs {
				if d.Fields[0] == "log" && len(d.Fields) > 1 {
					hc.Global = append(hc.Global, parseLogTarget(d))
				}
			}
		}
	}
	defaults := map[string]proxyLogCfg{}
	var last proxyLogCfg
	haveLast := false
	for _, s := range secs {
		switch s.Kind {
		case "defaults":
			base := proxyLogCfg{Mode: "tcp", FormatKind: "default"}
			if s.From != "" {
				base = defaults[s.From]
			}
			c := applyLogDirectives(base, s.Dirs, hc.Global)
			if s.Name != "" {
				defaults[s.Name] = c
			}
			last, haveLast = c, true
		case "frontend", "listen":
			base := proxyLogCfg{Mode: "tcp", FormatKind: "default"}
			if s.From != "" {
				base = defaults[s.From]
			} else if haveLast {
				base = last
			}
			fe := &feCfg{Name: s.Name, Kind: s.Kind, Where: fmt.Sprintf("%s:%d", filepath.Base(s.File), s.Line), HostSlot: -1}
			fe.proxyLogCfg = applyLogDirectives(base, s.Dirs, hc.Global)
			idx := 0
			for _, d := range s.Dirs {
				fs := d.Fields
				if len(fs) >= 2 && fs[0] == "stats" && (fs[1] == "enable" || fs[1] == "uri") {
					fe.IsStats = true
				}
				switch {
				case len(fs) >= 4 && fs[0] == "capture" && fs[1] == "request" && fs[2] == "header":
					fe.Captures = append(fe.Captures, captureSlot{Index: idx, Name: fs[3], Host: strings.EqualFold(fs[3], "host"), Where: d.where()})
					idx++
				case len(fs) >= 3 && fs[0] == "declare" && fs[1] == "capture" && fs[2] == "request":
					fe.Captures = append(fe.Captures, captureSlot{Index: idx, Name: "(declare)", Where: d.where()})
					idx++
				case len(fs) >= 3 && fs[0] == "http-request" && fs[1] == "capture":
					slot := -1
					for i := 2; i+1 < len(fs); i++ {
						if fs[i] == "id" {
							slot, _ = strconv.Atoi(fs[i+1])
						}
					}
					cs := captureSlot{Name: fs[2], Host: isHostExpr(fs[2]), Cond: hasCond(fs), Where: d.where()}
					if slot >= 0 && slot < len(fe.Captures) {
						// önceden declare edilmiş slota yazılıyor
						fe.Captures[slot].Name, fe.Captures[slot].Host = cs.Name, cs.Host || fe.Captures[slot].Host
						fe.Captures[slot].Cond, fe.Captures[slot].Where = cs.Cond, cs.Where
						continue
					}
					cs.Index = idx
					fe.Captures = append(fe.Captures, cs)
					idx++
				}
			}
			for _, c := range fe.Captures {
				if c.Host {
					fe.HostSlot = c.Index
					break
				}
			}
			hc.Frontends = append(hc.Frontends, fe)
			if s.Kind == "listen" {
				hc.Backends = append(hc.Backends, backendFrom(s))
			}
		case "backend":
			hc.Backends = append(hc.Backends, backendFrom(s))
		}
	}
	return hc
}

func backendFrom(s cfgSection) *beCfg {
	b := &beCfg{Name: s.Name, Where: fmt.Sprintf("%s:%d", filepath.Base(s.File), s.Line)}
	defaultCheck := false
	for _, d := range s.Dirs {
		fs := d.Fields
		if len(fs) >= 2 && fs[0] == "default-server" {
			for _, f := range fs[1:] {
				if f == "check" {
					defaultCheck = true
				}
			}
		}
	}
	for _, d := range s.Dirs {
		fs := d.Fields
		if len(fs) >= 3 && fs[0] == "server" {
			b.Servers = append(b.Servers, fs[1])
			check := defaultCheck
			for _, f := range fs[3:] {
				if f == "check" {
					check = true
				}
				if f == "no-check" {
					check = false
				}
			}
			if !check {
				b.NoCheck = append(b.NoCheck, fs[1])
			}
		}
		// server-template da check'i aynı şekilde taşır
		if len(fs) >= 4 && fs[0] == "server-template" {
			b.Servers = append(b.Servers, fs[1]+"*")
		}
	}
	return b
}

func (hc *haConfig) frontend(name string) *feCfg {
	for _, f := range hc.Frontends {
		if f.Name == name {
			return f
		}
	}
	return nil
}
