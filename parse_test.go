package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"
)

func TestParseLogLine(t *testing.T) {
	cases := []struct {
		name, line                 string
		kind, fe, be, method, path string
		status                     int
	}{
		{
			name: "sunucuya ulaşan istek, sorgu ve sayı yolu",
			line: `Sep 10 11:36:46 lb01 haproxy[123]: 203.0.113.7:51000 [10/Sep/2026:11:36:46.774] fe_main~ be_web/web1 0/0/1/40/41 200 3456 - - ---- 40/30/2/1/0 0/0 {} "GET /urun/12345?token=gizli HTTP/1.1"`,
			kind: KindServed, fe: "fe_main", be: "be_web", method: "GET", path: "/urun/{id}", status: 200,
		},
		{
			name: "deny kuralına takılan",
			line: `Sep 10 11:36:46 lb01 haproxy[123]: 203.0.113.7:51000 [10/Sep/2026:11:36:46.774] fe_main~ fe_main/<NOSRV> -1/-1/-1/-1/0 403 192 - - PR-- 3/1/0/0/0 0/0 {} "GET /admin HTTP/1.1"`,
			kind: KindDenied, fe: "fe_main", be: "fe_main", method: "GET", path: "/admin", status: 403,
		},
		{
			name: "hiçbir backend eşleşmedi",
			line: `Sep 10 11:36:46 lb01 haproxy[123]: 203.0.113.7:51000 [10/Sep/2026:11:36:46.774] fe_main~ fe_main/<NOSRV> -1/-1/-1/-1/0 503 216 - - SC-- 4/2/0/0/0 0/0 {} "POST /api/v1/orders HTTP/1.1"`,
			kind: KindNoMatch, fe: "fe_main", be: "fe_main", method: "POST", path: "/api/v1/orders", status: 503,
		},
		{
			name: "backend seçildi ama sunucu yok",
			line: `Sep 10 11:36:46 lb01 haproxy[123]: 203.0.113.7:51000 [10/Sep/2026:11:36:46.774] fe_main~ be_api/<NOSRV> 0/-1/-1/-1/0 503 216 - - SC-- 4/2/0/0/0 0/0 {} "GET /api/v1/cart HTTP/1.1"`,
			kind: KindNoServer, fe: "fe_main", be: "be_api", method: "GET", path: "/api/v1/cart", status: 503,
		},
		{
			name: "IPv6 istemci ve http->https yönlendirme",
			line: `Sep 10 11:36:46 lb01 haproxy[123]: 2001:db8::1:51000 [10/Sep/2026:11:36:46.774] fe_main fe_main/<NOSRV> -1/-1/-1/-1/0 301 97 - - LR-- 3/1/0/0/0 0/0 {} "GET / HTTP/1.1"`,
			kind: KindRedirect, fe: "fe_main", be: "fe_main", method: "GET", path: "/", status: 301,
		},
		{
			name: "journald'dan gelen çıplak mesaj",
			line: `203.0.113.7:51000 [10/Sep/2026:11:36:46.774] fe_main~ be_web/web1 0/0/1/40/41 200 3456 - - ---- 40/30/2/1/0 0/0 "GET /a/b HTTP/1.1"`,
			kind: KindServed, fe: "fe_main", be: "be_web", method: "GET", path: "/a/b", status: 200,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, ok := parseLogLine(c.line)
			if !ok {
				t.Fatalf("satır ayrıştırılamadı")
			}
			if r.Kind != c.kind || r.Frontend != c.fe || r.Backend != c.be || r.Method != c.method || r.Path != c.path || r.Status != c.status {
				t.Fatalf("beklenmeyen sonuç: %+v", r)
			}
		})
	}
}

func TestParseLogLineRejects(t *testing.T) {
	for _, l := range []string{
		`Sep 10 11:36:46 lb01 sshd[99]: Accepted publickey for root`,
		`Sep 10 11:36:46 lb01 haproxy[123]: Server be_web/web1 is DOWN, reason: Layer4 timeout`,
		``,
	} {
		if _, ok := parseLogLine(l); ok {
			t.Errorf("reddedilmesi gereken satır kabul edildi: %q", l)
		}
	}
}

func TestNormPath(t *testing.T) {
	cases := map[string]string{
		"/":                 "/",
		"/urun/12345":       "/urun/{id}",
		"/a?x=1&device=abc": "/a",
		"/u/550e8400-e29b-41d4-a716-446655440000": "/u/{id}",
		"https://ornek.com/yol/9":                 "/yol/{id}",
		"/haber/12-mac-sonucu":                    "/haber/12-mac-sonucu",
	}
	for in, want := range cases {
		if got := normPath(in); got != want {
			t.Errorf("normPath(%q) = %q, beklenen %q", in, got, want)
		}
	}
}

func TestSocketsFromConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "haproxy.cfg")
	content := `global
    # stats socket /yorum/satiri.sock
    stats socket ipv4@127.0.0.1:9999 level user
    stats socket /run/haproxy/admin.sock mode 660 level admin expose-fd listeners
    stats socket :9998
    stats socket abns@lens
`
	if err := os.WriteFile(cfg, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range socketsFromConfig([]string{cfg}) {
		got = append(got, s.Addr)
	}
	want := []string{"unix:/run/haproxy/admin.sock", "unix:@lens", "tcp:127.0.0.1:9999", "tcp:127.0.0.1:9998"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSocketAddr(t *testing.T) {
	cases := map[string][2]string{
		"/run/haproxy/admin.sock":      {"unix", "/run/haproxy/admin.sock"},
		"unix:/run/haproxy/admin.sock": {"unix", "/run/haproxy/admin.sock"},
		"tcp:127.0.0.1:9999":           {"tcp", "127.0.0.1:9999"},
	}
	for in, want := range cases {
		n, a := socketAddr(in)
		if n != want[0] || a != want[1] {
			t.Errorf("socketAddr(%q) = %s %s", in, n, a)
		}
	}
}

func TestParseStat(t *testing.T) {
	raw := "# pxname,svname,scur,status,type,\nfe_main,FRONTEND,12,OPEN,0,\nbe_web,web1,3,UP,2,\n\n"
	rows, err := parseStat(raw)
	if err != nil || len(rows) != 2 || rows[1]["status"] != "UP" || rows[0]["scur"] != "12" {
		t.Fatalf("parseStat hatalı: %v %v", rows, err)
	}
	if _, err := parseStat("Unknown command.\n"); err == nil {
		t.Fatal("başlıksız çıktı hata vermeliydi")
	}
}

func TestOnlyReadOnlyCommands(t *testing.T) {
	if _, err := query("/yok.sock", "disable server be_web/web1"); err == nil || err.Error() != `izin verilmeyen komut: "disable server be_web/web1"` {
		t.Fatalf("izin listesi dışındaki komut engellenmedi: %v", err)
	}
}

func TestWindowAndDownsample(t *testing.T) {
	p := NewStatsPoller("", 2*time.Second, 1800)
	row := func(req, e5 string) map[string]string {
		return map[string]string{"pxname": "be", "svname": "s1", "type": "2", "req_tot": req, "hrsp_5xx": e5, "hrsp_2xx": "0"}
	}
	snap := func(at int64, up, req, e5 string) *Snapshot {
		return &Snapshot{At: at, Info: map[string]string{"Uptime_sec": up}, Rows: []map[string]string{row(req, e5)}}
	}
	// 20 dakika boyunca her 10 saniyede 100 istek, 5'i 5xx
	for i := int64(0); i <= 120; i++ {
		p.store(snap(i*10_000, strconv.FormatInt(1000+i*10, 10), strconv.FormatInt(i*100, 10), strconv.FormatInt(i*5, 10)))
	}
	st := p.State(5)
	w := st.Window.Rows["be|s1"]
	if st.Window.Seconds != 300 || w.N != 3000 || w.Codes[4] != 150 {
		t.Fatalf("5 dk penceresi hatalı: %+v %+v", st.Window, w)
	}
	// Ajan 20 dk önce başladı; 60 dk istenince kapsanan süre 20 dk olmalı
	if st := p.State(60); st.Window.Seconds != 1200 {
		t.Fatalf("kapsanan süre %v", st.Window.Seconds)
	}
	// HAProxy yeniden başlarsa (uptime küçülür) pencere sıfırdan başlar, eksi değer çıkmaz
	p.store(snap(1_210_000, "5", "10", "1"))
	if w := p.State(60).Window.Rows["be|s1"]; w.N != 0 || w.Codes[4] != 0 {
		t.Fatalf("yeniden başlatma sonrası: %+v", w)
	}
	pts := make([]Point, 1800)
	for i := range pts {
		pts[i] = Point{T: int64(i), C5: 2}
	}
	ds := downsample(pts, 360)
	if len(ds) != 360 || ds[0].C5 != 2 || ds[len(ds)-1].T != 1799 {
		t.Fatalf("seyreltme: %d nokta, %+v", len(ds), ds[len(ds)-1])
	}
}
