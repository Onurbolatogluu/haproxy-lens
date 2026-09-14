package main

import (
	"fmt"
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

func TestCodePathsFromLog(t *testing.T) {
	a := NewLogAnalyzer("file:/yok", "")
	mk := func(be, path string, status int, kind string) logRecord {
		srv := "s1"
		if kind != KindServed {
			srv = "<NOSRV>"
		}
		return logRecord{At: time.Now(), Client: "1.2.3.4", Frontend: "fe", Backend: be, Server: srv,
			Status: status, Method: "GET", Path: path, RawPath: path, Kind: kind}
	}
	add := func(n int, r logRecord) {
		for i := 0; i < n; i++ {
			a.add(r)
		}
	}
	add(50, mk("be_api", "/api/orders", 200, KindServed))
	add(12, mk("be_api", "/api/orders", 502, KindServed))
	add(3, mk("be_api", "/api/orders", 503, KindServed))
	add(8, mk("be_web", "/gizli", 404, KindServed))
	add(4, mk("be_web", "/gizli", 403, KindServed))
	// HAProxy'nin kendi ürettiği http->https yönlendirmesi: hiçbir sunucuya gitmez
	add(900, mk("fe", "/", 301, KindRedirect))

	rep := a.Report(60)
	if rep.Classes != [4]int64{50, 900, 12, 15} {
		t.Fatalf("sınıf toplamları: %v", rep.Classes)
	}
	get := func(path string) *CodePathRow {
		for i := range rep.CodePaths {
			if rep.CodePaths[i].Path == path {
				return &rep.CodePaths[i]
			}
		}
		return nil
	}
	// Yönlendirme satırı listede olmalı (eskiden hiçbir yerde görünmüyordu)
	red := get("/")
	if red == nil || red.S3 != 900 || red.Kind != KindRedirect || red.Codes[0].Code != 301 {
		t.Fatalf("yönlendirme satırı: %+v", red)
	}
	if red.Detail == nil || red.Detail.Total != 900 {
		t.Fatalf("yönlendirme ayrıntısı yok: %+v", red.Detail)
	}
	api := get("/api/orders")
	if api == nil || api.S5 != 15 || api.N != 65 || api.Codes[0].Code != 502 || api.Codes[0].N != 12 {
		t.Fatalf("5xx satırı: %+v", api)
	}
	web := get("/gizli")
	if web == nil || web.S4 != 12 {
		t.Fatalf("4xx satırı: %+v", web)
	}
	// "En çok istenen adresler" artık yönlendirmeleri de içerir
	if len(rep.Paths) != 3 {
		t.Fatalf("yol sayısı: %d", len(rep.Paths))
	}
}

func TestTopPerClass(t *testing.T) {
	// Çok sayıda 3xx, az sayıdaki 5xx satırını listeden düşürmemeli
	var rows []CodePathRow
	for i := 0; i < 40; i++ {
		rows = append(rows, CodePathRow{Path: "/y" + strconv.Itoa(i), S3: int64(1000 - i)})
	}
	rows = append(rows, CodePathRow{Path: "/nadir-hata", S5: 2})
	out := topPerClass(rows, 20)
	var bulundu bool
	for _, r := range out {
		if r.Path == "/nadir-hata" {
			bulundu = true
		}
	}
	if !bulundu {
		t.Fatalf("az sayıdaki 5xx satırı listeye girmedi (%d satır)", len(out))
	}
	if len(out) != 21 {
		t.Fatalf("beklenen 21 satır, gelen %d", len(out))
	}
}

func TestHostFromLog(t *testing.T) {
	pre := `Sep 11 12:00:00 lb haproxy[1]: 203.0.113.7:5000 [11/Sep/2026:12:00:00.100] `
	mid := ` 0/0/1/40/41 503 216 - - SC-- 4/2/0/0/0 0/0 `
	cases := []struct {
		name, line, host, raw string
		tls                   bool
	}{
		{"Host başlığı yakalanıyor", pre + `fe~ be/s1` + mid + `{www.ornek.com} "GET /api/auth/GetGuestToken?cihaz=123 HTTP/1.1"`, "www.ornek.com", "/api/auth/GetGuestToken", true},
		{"iki yakalama: User-Agent ve Host", pre + `fe be/s1` + mid + `{Mozilla/5.0 (X11; Linux)|shop.ornek.com.tr:8443} "GET / HTTP/1.1"`, "shop.ornek.com.tr:8443", "/", false},
		{"HTTP/2 tam adres", pre + `fe~ be/s1` + mid + `{} "GET https://api.ornek.com/v1/siparis/42?x=1 HTTP/2.0"`, "api.ornek.com", "/v1/siparis/42", true},
		{"option httpslog SNI", pre + `fe~ be/s1` + mid + `{} "GET /x HTTP/1.1" 0/0/0/0/0 app.ornek.com/TLSv1.3/TLS_AES_256_GCM_SHA384`, "app.ornek.com", "/x", true},
		{"koşullu yakalama bu satırda boş", pre + `fe~ fe/<NOSRV>` + mid + `{} "GET /login HTTP/1.1"`, "", "/login", true},
		{"istek IP ile yapılmış", pre + `fe fe/<NOSRV>` + mid + `{192.168.10.20:8405} "GET / HTTP/1.1"`, "192.168.10.20:8405", "/", false},
		{"iki IP yakalaması (X-Forwarded-For gibi) alan adı sayılmaz", pre + `fe be/s1` + mid + `{198.51.100.1|198.51.100.2} "GET / HTTP/1.1"`, "", "/", false},
		{"sonraki alandaki Referer URL'si hedef sayılmaz", pre + `fe be/s1` + mid + `{} "GET / HTTP/1.1" https://google.com/arama`, "", "/", false},
		{"yakalama bloğu hiç yok", pre + `fe be/s1` + mid + `"GET /eski HTTP/1.1"`, "", "/eski", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, ok := parseLogLine(c.line)
			if !ok {
				t.Fatalf("satır okunamadı")
			}
			if r.Host != c.host || r.RawPath != c.raw || r.TLS != c.tls {
				t.Fatalf("host=%q raw=%q tls=%v", r.Host, r.RawPath, r.TLS)
			}
		})
	}
}

func TestBlockedDetail(t *testing.T) {
	a := NewLogAnalyzer("file:/yok", "")
	rec := func(host, client, raw string) logRecord {
		return logRecord{At: time.Now(), Client: client, Frontend: "fe", Backend: "fe", Server: "<NOSRV>", Status: 503,
			Method: "GET", Path: normPath(raw), RawPath: raw, Host: host, TLS: true, Kind: KindNoMatch}
	}
	for i := 0; i < 30; i++ {
		a.add(rec("app.ornek.com", "162.158.1.1", "/api/auth/GetGuestToken"))
	}
	for i := 0; i < 10; i++ {
		a.add(rec("", "85.105.1.2", "/api/auth/GetGuestToken"))
	}
	rep := a.Report(60)
	if len(rep.Blocked) != 1 || rep.Blocked[0].Detail == nil {
		t.Fatalf("engellenen satır: %+v", rep.Blocked)
	}
	d := rep.Blocked[0].Detail
	if d.Total != 40 || d.HostKnown != 30 || d.Origins[0].Name != "https://app.ornek.com" || d.Origins[0].N != 30 || d.Origins[1].Name != "" {
		t.Fatalf("adresler: %+v", d)
	}
	if !d.IPs[0].Cloudflare || d.IPs[0].N != 30 || d.IPs[1].Cloudflare {
		t.Fatalf("IP'ler: %+v", d.IPs)
	}
	if rep.HostLines != 30 {
		t.Fatalf("alan adlı satır: %d", rep.HostLines)
	}
}

func TestDetailMemoryCap(t *testing.T) {
	a := NewLogAnalyzer("file:/yok", "")
	now := time.Now()
	// Tarayıcı botu gibi binlerce farklı yola 403
	for i := 0; i < 2000; i++ {
		p := "/tarama/" + strconv.Itoa(i) + "x"
		a.add(logRecord{At: now, Client: "198.51.100.9", Frontend: "fe", Backend: "fe", Server: "<NOSRV>", Status: 403, Method: "GET", Path: p, RawPath: p, Kind: KindDenied})
	}
	b := a.buckets[now.Unix()/60]
	if b.detailKeys > maxDetailKeys {
		t.Fatalf("ayrıntı sınırı aşıldı: %d", b.detailKeys)
	}
	if b.kinds[KindDenied] != 2000 {
		t.Fatalf("sayım eksik: %d", b.kinds[KindDenied])
	}
}

func TestClientTopPaths(t *testing.T) {
	a := NewLogAnalyzer("file:/yok", "")
	rec := func(ip, path string, status int, kind string) logRecord {
		srv := "s1"
		if kind != KindServed {
			srv = "<NOSRV>"
		}
		return logRecord{At: time.Now(), Client: ip, Frontend: "fe", Backend: "be", Server: srv,
			Status: status, Method: "GET", Path: path, RawPath: path, Kind: kind}
	}
	add := func(n int, r logRecord) {
		for i := 0; i < n; i++ {
			a.add(r)
		}
	}
	add(300, rec("198.51.100.5", "/api/urun", 200, KindServed))
	add(120, rec("198.51.100.5", "/api/sepet", 200, KindServed))
	add(40, rec("198.51.100.5", "/wp-login.php", 403, KindDenied))
	add(90, rec("203.0.113.9", "/", 200, KindServed))

	rep := a.Report(60)
	if len(rep.Clients) != 2 || rep.Clients[0].IP != "198.51.100.5" || rep.Clients[0].N != 460 {
		t.Fatalf("istemciler: %+v", rep.Clients)
	}
	c := rep.Clients[0]
	if c.Blocked != 40 {
		t.Fatalf("engellenen: %d", c.Blocked)
	}
	if len(c.Paths) != 3 || c.Paths[0].Name != "GET /api/urun" || c.Paths[0].N != 300 || c.Paths[1].Name != "GET /api/sepet" {
		t.Fatalf("adres dökümü: %+v", c.Paths)
	}
}

func TestClientDetailMemoryCap(t *testing.T) {
	a := NewLogAnalyzer("file:/yok", "")
	now := time.Now()
	// Çok sayıda farklı IP ve her birinden çok sayıda farklı yol
	for i := 0; i < 500; i++ {
		for j := 0; j < 30; j++ {
			a.add(logRecord{At: now, Client: fmt.Sprintf("198.51.100.%d", i%256), Frontend: "fe", Backend: "be", Server: "s1",
				Status: 200, Method: "GET", Path: "/y" + strconv.Itoa(j), RawPath: "/y", Kind: KindServed})
		}
	}
	b := a.buckets[now.Unix()/60]
	if b.clientKeys > maxClientDetail {
		t.Fatalf("IP ayrıntı sınırı aşıldı: %d", b.clientKeys)
	}
	for ip, ca := range b.clients {
		if ca.Paths != nil && len(ca.Paths) > maxClientPaths+1 {
			t.Fatalf("%s için %d farklı yol tutulmuş", ip, len(ca.Paths))
		}
		if ca.N == 0 {
			t.Fatalf("%s sayımı boş", ip)
		}
	}
}
