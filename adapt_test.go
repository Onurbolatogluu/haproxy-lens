package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeCfg(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, "haproxy.cfg")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const sampleCfg = `global
    log /dev/log local0
    log /dev/log local1 notice
    stats socket /run/haproxy/admin.sock mode 660 level admin

defaults
    log     global
    mode    http
    option  httplog

listen stats
    bind *:8404
    stats enable
    stats uri /

frontend fe_ana
    bind *:443
    http-request capture req.fhdr(Host) len 64 if { src 10.0.0.0/8 }
    use_backend be_web


frontend fe_iki
    bind *:9443
    capture request header User-Agent len 100
    capture request header Host len 64

frontend fe_tcp
    mode tcp
    option tcplog
    bind *:3306

backend be_web
    default-server check
    server w1 10.0.0.1:80
    server w2 10.0.0.2:80 no-check

backend be_eski
    server e1 10.0.0.9:80

defaults ozel
    log global
    mode http
    log-format "%ci:%cp [%tr] %ft %b/%s %Ta %ST %{+Q}r %[req.hdr(host)]"

frontend fe_json from ozel
    bind *:8443
    option dontlog-normal
`

func TestConfigInheritance(t *testing.T) {
	hc := buildHAConfig([]string{writeCfg(t, t.TempDir(), sampleCfg)})
	fe := hc.frontend("fe_ana")
	if fe.Mode != "http" || fe.FormatKind != "httplog" || fe.HostSlot != 0 || !fe.Captures[0].Cond || len(fe.Targets) != 2 {
		t.Fatalf("fe_ana: %+v", fe)
	}
	js := hc.frontend("fe_json")
	if js.FormatKind != "custom" || !strings.Contains(js.Format, "%[req.hdr(host)]") || !js.DontLogNormal {
		t.Fatalf("fe_json: %+v", js)
	}
	if iki := hc.frontend("fe_iki"); iki.HostSlot != 1 {
		t.Fatalf("fe_iki Host slotu: %d", iki.HostSlot)
	}
	if !hc.frontend("stats").IsStats || hc.frontend("fe_tcp").Mode != "tcp" {
		t.Fatal("stats / tcp algılanamadı")
	}
	var eski, web *beCfg
	for _, b := range hc.Backends {
		if b.Name == "be_eski" {
			eski = b
		}
		if b.Name == "be_web" {
			web = b
		}
	}
	if len(eski.NoCheck) != 1 || len(web.NoCheck) != 1 || web.NoCheck[0] != "w2" {
		t.Fatalf("sağlık kontrolü: eski=%v web=%v", eski.NoCheck, web.NoCheck)
	}
}

func TestCustomFormats(t *testing.T) {
	now := `11/Sep/2026:12:00:00.100`
	cases := []struct {
		name, format, line string
		want               logRecord
	}{
		{"sonuna host eklenmiş httplog benzeri",
			`%ci:%cp [%tr] %ft %b/%s %Ta %ST %{+Q}r %[req.hdr(host)]`,
			`203.0.113.7:5000 [` + now + `] fe~ be_api/api1 41 502 "GET /siparis/9?x=1 HTTP/1.1" api.ornek.com`,
			logRecord{Client: "203.0.113.7", Frontend: "fe", TLS: true, Backend: "be_api", Server: "api1", Status: 502, Method: "GET", Path: "/siparis/{id}", Host: "api.ornek.com", Kind: KindServed}},
		{"JSON benzeri biçim",
			`{"ip":"%ci","zaman":"%tr","fe":"%ft","be":"%b","srv":"%s","kod":%ST,"alan":"%[req.hdr(host)]","yontem":"%HM","yol":"%HP"}`,
			`{"ip":"198.51.100.4","zaman":"` + now + `","fe":"fe","be":"fe","srv":"<NOSRV>","kod":403,"alan":"shop.ornek.com","yontem":"POST","yol":"/admin"}`,
			logRecord{Client: "198.51.100.4", Frontend: "fe", Backend: "fe", Server: "<NOSRV>", Status: 403, Method: "POST", Path: "/admin", Host: "shop.ornek.com", Kind: KindDenied}},
		{"unix zamanı ve kısa biçim",
			`%Ts %ci %ST %HM %HU %b/%s`,
			`1789123456 198.51.100.4 404 GET /yok be_web/w1`,
			logRecord{Client: "198.51.100.4", Backend: "be_web", Server: "w1", Status: 404, Method: "GET", Path: "/yok", Kind: KindServed}},
		{"capture.req.hdr ile Host",
			`%ci:%cp [%tr] %ft %b/%s %ST %[capture.req.hdr(0)] %{+Q}r`,
			`203.0.113.7:5000 [` + now + `] fe be/s1 200 www.ornek.com "GET / HTTP/1.1"`,
			logRecord{Client: "203.0.113.7", Frontend: "fe", Backend: "be", Server: "s1", Status: 200, Method: "GET", Path: "/", Host: "www.ornek.com", Kind: KindServed}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cf := compileFormat("custom", c.format)
			if cf == nil {
				t.Fatal("biçim derlenemedi")
			}
			p := newLogParser([]*compiledFormat{cf}, map[string]int{"fe": 0}, true)
			r, ok := p.Parse(`Sep 11 12:00:00 lb haproxy[1]: ` + c.line)
			if !ok {
				t.Fatalf("okunamadı: %s", cf.re)
			}
			w := c.want
			if r.Client != w.Client || r.Frontend != w.Frontend || r.TLS != w.TLS || r.Backend != w.Backend || r.Server != w.Server ||
				r.Status != w.Status || r.Method != w.Method || r.Path != w.Path || r.Host != w.Host || r.Kind != w.Kind {
				t.Fatalf("\n  gelen   %+v\n  beklenen %+v", r, w)
			}
		})
	}
}

func TestHostSlotFromConfig(t *testing.T) {
	hc := buildHAConfig([]string{writeCfg(t, t.TempDir(), sampleCfg+`
frontend fe_xff
    bind *:7443
    capture request header X-Forwarded-For len 64
`)})
	p := buildLogParser(hc)
	base := `Sep 11 12:00:00 lb haproxy[1]: 203.0.113.7:5000 [11/Sep/2026:12:00:00.100] `
	mid := ` 0/0/1/40/41 200 100 - - ---- 1/1/1/1/0 0/0 `
	// fe_iki: slot 1 Host; User-Agent boşluklu olsa da doğru slot okunur
	if r, _ := p.Parse(base + `fe_iki be/s1` + mid + `{Mozilla/5.0 (X11; Linux)|www.ornek.com} "GET / HTTP/1.1"`); r.Host != "www.ornek.com" {
		t.Fatalf("fe_iki host: %q", r.Host)
	}
	// fe_xff: tek yakalama X-Forwarded-For; IP olsa bile Host sayılmaz (config biliyor)
	if r, _ := p.Parse(base + `fe_xff be/s1` + mid + `{198.51.100.2} "GET / HTTP/1.1"`); r.Host != "" {
		t.Fatalf("fe_xff host olmamalı: %q", r.Host)
	}
	// tcplog satırı TCP olarak tanınır, okunamayan sayılmaz
	if r, ok := p.Parse(base + `fe_tcp be_db/db1 1/0/5007 212 -- 1/1/1/1/0 0/0`); !ok || r.Kind != KindTCP {
		t.Fatalf("tcp satırı: ok=%v kind=%q", ok, r.Kind)
	}
}

// Yarın config'e log-format eklenip reload edilirse ajan yeniden kurulum olmadan uyum sağlamalı
func TestRuntimeConfigChange(t *testing.T) {
	dir := t.TempDir()
	cfg := writeCfg(t, dir, "global\n    log /dev/log local0\ndefaults\n    log global\n    mode http\n    option httplog\nfrontend fe\n    bind *:80\n")
	logs := NewLogAnalyzer("file:/yok", "")
	env := NewEnv(NewStatsPoller("/yok.sock", time.Second, 10, 60), logs, false)
	pid := 100
	env.find = func() ([]int, []string, string) { return []int{pid}, []string{cfg}, "" }
	env.refresh()

	custom := `Sep 11 12:00:00 lb haproxy[1]: 203.0.113.7 403 GET /admin fe/<NOSRV> shop.ornek.com`
	logs.handleLine(custom)
	if obs := logs.Observe(15); obs.Parsed != 0 || obs.Unparsed != 1 || len(obs.Samples) != 1 {
		t.Fatalf("değişiklikten önce özel satır okunmamalıydı: %+v", obs)
	}
	// Config'e özel biçim eklendi ve HAProxy reload edildi (süreç numarası değişti)
	writeCfg(t, dir, "global\n    log /dev/log local0\ndefaults\n    log global\n    mode http\n    log-format \"%ci %ST %HM %HP %b/%s %[req.hdr(host)]\"\nfrontend fe\n    bind *:80\n")
	pid = 200
	env.refresh()
	logs.handleLine(custom)
	obs := logs.Observe(15)
	if obs.Parsed != 1 || obs.HostLines != 1 {
		t.Fatalf("değişiklikten sonra okunmalıydı: %+v", obs)
	}
	rep := logs.Report(15)
	if len(rep.Blocked) != 1 || rep.Blocked[0].Detail.Origins[0].Name != "http://shop.ornek.com" {
		t.Fatalf("engellenen: %+v", rep.Blocked)
	}
}

func TestConfigNotes(t *testing.T) {
	hc := buildHAConfig([]string{writeCfg(t, t.TempDir(), sampleCfg+`
frontend fe_sessiz
    mode http
    no log
    bind *:81
`)})
	notes := configNotes(hc)
	has := func(sub string) *Note {
		for i := range notes {
			if strings.Contains(notes[i].Title, sub) {
				return &notes[i]
			}
		}
		return nil
	}
	checks := map[string]string{
		"«fe_json» başarılı istekleri log'a yazmıyor": "no option dontlog-normal",
		"«fe_ana» alan adını sadece bazı isteklerde":  "capture request header Host len 64",
		"«fe_sessiz» log yazmıyor":                    "log global",
		"«fe_tcp» TCP modunda":                        "",
		"sunucuda sağlık kontrolü yok":                "server <ad> <adres>:<port> check",
	}
	for title, fix := range checks {
		n := has(title)
		if n == nil {
			t.Errorf("not yok: %s", title)
			continue
		}
		if n.Fix != fix {
			t.Errorf("%s: öneri %q, beklenen %q", title, n.Fix, fix)
		}
	}
	if has("«stats»") != nil {
		t.Error("stats bölümü için not üretilmemeli")
	}
	if has("«fe_iki» alan adını") != nil {
		t.Error("fe_iki Host yakalıyor, alan adı notu olmamalı")
	}
	if n := has("sunucuda sağlık kontrolü yok"); n != nil && !strings.HasPrefix(n.Title, "2 ") {
		t.Errorf("sağlık kontrolü sayısı: %s", n.Title)
	}
}

func TestLevelFilterNote(t *testing.T) {
	hc := buildHAConfig([]string{writeCfg(t, t.TempDir(), "global\n    log /dev/log local0 notice\ndefaults\n    log global\n    mode http\n    option httplog\nfrontend fe\n    bind *:80\n")})
	for _, n := range configNotes(hc) {
		if strings.Contains(n.Title, "log seviyesine takılıyor") {
			return
		}
	}
	t.Fatal("seviye filtresi notu üretilmedi")
}

func TestUnparsedNote(t *testing.T) {
	logs := NewLogAnalyzer("file:/yok", "")
	for i := 0; i < 30; i++ {
		logs.handleLine(`Sep 11 12:00:00 lb haproxy[1]: 203.0.113.7:5000 garip-bicim 11/Sep/2026:12:00:00 GET /x?token=gizli`)
	}
	logs.handleLine(`Sep 11 12:00:00 lb haproxy[1]: Server be/s1 is DOWN, reason: Layer4 timeout`) // olay satırı sayılmaz
	obs := logs.Observe(15)
	if obs.Unparsed != 30 {
		t.Fatalf("okunamayan: %d", obs.Unparsed)
	}
	if strings.Contains(strings.Join(obs.Samples, " "), "gizli") {
		t.Fatal("örneklerde sorgu değeri görünmemeli")
	}
	notes := runtimeNotes(nil, obs, 0, "")
	if len(notes) != 1 || !strings.Contains(notes[0].Title, "okunamadı") || len(notes[0].Samples) == 0 {
		t.Fatalf("not: %+v", notes)
	}
}
