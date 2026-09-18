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
	p := NewStatsPoller("", 2*time.Second, 1800, 1440)
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
	// HAProxy yeniden başlarsa (uptime küçülür) sayaçları sıfırlanır. Dakikalık birikim
	// ayrı tutulduğu için önceki trafik pencerede görünmeye devam eder; burada önemli
	// olan eksi ya da şişmiş bir değer çıkmaması.
	p.store(snap(1_210_000, "5", "10", "1"))
	w2 := p.State(60).Window.Rows["be|s1"]
	if w2.N < 0 || w2.Codes[4] < 0 {
		t.Fatalf("yeniden başlatma sonrası eksi değer: %+v", w2)
	}
	if w2.N > 12100 || w2.Codes[4] > 610 { // 20 dakikada en fazla 12.000 istek, 600 hata
		t.Fatalf("yeniden başlatma sonrası şişme: %+v", w2)
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

func TestBackendBreakdown(t *testing.T) {
	a := NewLogAnalyzer("file:/yok", "")
	rec := func(be, ip, path string, status int, kind string) logRecord {
		srv := "s1"
		if kind != KindServed {
			srv = "<NOSRV>"
		}
		return logRecord{At: time.Now(), Client: ip, Frontend: "fe", Backend: be, Server: srv,
			Status: status, Method: "GET", Path: path, RawPath: path, Kind: kind}
	}
	add := func(n int, r logRecord) {
		for i := 0; i < n; i++ {
			a.add(r)
		}
	}
	// Yoğun backend
	add(500, rec("be_prod", "203.0.113.10", "/urun", 200, KindServed))
	add(20, rec("be_prod", "203.0.113.10", "/urun", 500, KindServed))
	// "Hiç trafik almaması gereken" backend: az ama var
	add(7, rec("be_dev", "198.51.100.44", "/admin", 200, KindServed))
	add(3, rec("be_dev", "192.0.2.7", "/admin", 200, KindServed))
	add(2, rec("be_dev", "198.51.100.44", "/config.json", 404, KindServed))

	rep := a.Report(60)
	get := func(ad string) *BackendLogRow {
		for i := range rep.Backends {
			if rep.Backends[i].Backend == ad {
				return &rep.Backends[i]
			}
		}
		return nil
	}
	if len(rep.Backends) != 2 || rep.Backends[0].Backend != "be_prod" {
		t.Fatalf("backend listesi: %+v", rep.Backends)
	}
	prod := get("be_prod")
	if prod.N != 520 || prod.S5 != 20 || prod.S2 != 500 {
		t.Fatalf("be_prod: %+v", prod)
	}
	dev := get("be_dev")
	if dev.N != 12 || dev.S4 != 2 {
		t.Fatalf("be_dev sayıları: %+v", dev)
	}
	// Asıl soru: bu backend'e kim, nereye istek atmış
	if len(dev.IPs) != 2 || dev.IPs[0].IP != "198.51.100.44" || dev.IPs[0].N != 9 {
		t.Fatalf("be_dev IP'leri: %+v", dev.IPs)
	}
	if len(dev.Paths) != 2 || dev.Paths[0].Name != "GET /admin" || dev.Paths[0].N != 10 {
		t.Fatalf("be_dev adresleri: %+v", dev.Paths)
	}
}

func TestBackendBreakdownCap(t *testing.T) {
	a := NewLogAnalyzer("file:/yok", "")
	now := time.Now()
	for i := 0; i < 300; i++ {
		for j := 0; j < 60; j++ {
			a.add(logRecord{At: now, Client: fmt.Sprintf("198.51.100.%d", j), Frontend: "fe",
				Backend: "be" + strconv.Itoa(i), Server: "s1", Status: 200, Method: "GET", Path: "/", Kind: KindServed})
		}
	}
	b := a.buckets[now.Unix()/60]
	if len(b.backends) > maxBackendKeys {
		t.Fatalf("backend sınırı aşıldı: %d", len(b.backends))
	}
	for ad, ba := range b.backends {
		if len(ba.IPs) > maxBackendIPs+1 {
			t.Fatalf("%s için %d IP tutulmuş", ad, len(ba.IPs))
		}
	}
	// Sınır aşılsa da genel sayımlar eksiksiz kalmalı
	if rep := a.Report(60); rep.Kinds[KindServed] != 300*60 {
		t.Fatalf("genel sayım bozuldu: %d", rep.Kinds[KindServed])
	}
}

func TestUzunAralikVeDiskeKayit(t *testing.T) {
	dir := t.TempDir()
	p := NewStatsPoller("", 2*time.Second, 1800, 1440)
	row := func(req, e5 string) map[string]string {
		return map[string]string{"pxname": "be", "svname": "BACKEND", "type": "1", "req_tot": req, "hrsp_5xx": e5, "hrsp_2xx": "0"}
	}
	// 3 saat boyunca dakikada 600 istek, 6'sı 5xx
	basla := (time.Now().UnixMilli()/60_000 - 180) * 60_000
	for i := int64(0); i <= 180*6; i++ { // 10 saniyede bir ölçüm
		at := basla + i*10_000
		p.store(&Snapshot{At: at, Info: map[string]string{"Uptime_sec": strconv.FormatInt(1000+i*10, 10)},
			Rows: []map[string]string{row(strconv.FormatInt(i*100, 10), strconv.FormatInt(i, 10))}})
	}
	if len(p.minutes) < 175 {
		t.Fatalf("dakikalık birikim eksik: %d", len(p.minutes))
	}
	// 2 saatlik pencere: dakikalık artışların toplamı
	st := p.State(120)
	if st.Window == nil {
		t.Fatal("2 saatlik pencere boş")
	}
	w := st.Window.Rows["be|BACKEND"]
	if w.N < 60000 || w.N > 78000 {
		t.Fatalf("2 saatte %v istek (beklenen ~72.000)", w.N)
	}
	if oran := w.Codes[4] / w.N; oran < 0.009 || oran > 0.011 {
		t.Fatalf("5xx oranı %v (beklenen ~%%1)", oran)
	}
	if len(st.History) == 0 || len(st.History) > maxChartPts {
		t.Fatalf("grafik noktası: %d", len(st.History))
	}
	if st.Retention != 1440 {
		t.Fatalf("saklama: %d", st.Retention)
	}

	// Log tarafı: 3 saatlik kayıt, eski dakikalar sadeleşmeli ama sayılar kalmalı
	a := NewLogAnalyzer("file:/yok", "")
	a.SetRetention(1440)
	simdi := time.Now()
	for dk := 0; dk < 180; dk++ {
		ts := simdi.Add(-time.Duration(dk) * time.Minute)
		for i := 0; i < 10; i++ {
			a.add(logRecord{At: ts, Client: "198.51.100.5", Frontend: "fe", Backend: "be", Server: "s1",
				Status: 200, Method: "GET", Path: "/y" + strconv.Itoa(i), RawPath: "/y", Kind: KindServed})
		}
		a.add(logRecord{At: ts, Client: "198.51.100.5", Frontend: "fe", Backend: "be", Server: "s1",
			Status: 500, Method: "GET", Path: "/hata", RawPath: "/hata", Kind: KindServed})
	}
	rep := a.Report(185) // pencere bilerek geniş: dakika sınırı aşılabilir
	if rep.Classes[0] != 1800 || rep.Classes[3] != 180 {
		t.Fatalf("3 saatlik sınıf toplamları: %v", rep.Classes)
	}
	// DetailMinutes artık ayarlanan değil, gerçekte kapsanan süre
	if rep.Minutes != 185 || rep.DetailMinutes < 55 || rep.DetailMinutes > 65 {
		t.Fatalf("rapor aralığı: %d, ayrıntı kapsamı: %d (beklenen ~60)", rep.Minutes, rep.DetailMinutes)
	}
	if rep.ListMinutes < 175 {
		t.Fatalf("liste kapsamı: %d (beklenen ~180)", rep.ListMinutes)
	}
	// 1 saati aşan kovalarda ayrıntı düşmüş olmalı, sayılar durmalı
	eski := a.buckets[simdi.Add(-120*time.Minute).Unix()/60]
	if eski == nil || eski.level == 0 {
		t.Fatalf("eski kova sadeleşmedi: %+v", eski)
	}
	if eski.classes[0] != 10 {
		t.Fatalf("eski kovanın sayıları kaybolmuş: %v", eski.classes)
	}

	// Diske yaz, yeni ajanlara yükle
	st1 := NewStore(dir, p, a, nil)
	if err := st1.Save(); err != nil {
		t.Fatal(err)
	}
	p2 := NewStatsPoller("", 2*time.Second, 1800, 1440)
	a2 := NewLogAnalyzer("file:/yok", "")
	a2.SetRetention(1440)
	if err := NewStore(dir, p2, a2, nil).Load(); err != nil {
		t.Fatal(err)
	}
	if len(p2.minutes) != len(p.minutes) {
		t.Fatalf("yüklenen dakika sayısı %d, beklenen %d", len(p2.minutes), len(p.minutes))
	}
	rep2 := a2.Report(185)
	if rep2.Classes != rep.Classes {
		t.Fatalf("yüklenen sınıf toplamları %v, beklenen %v", rep2.Classes, rep.Classes)
	}
	if rep2.Kinds[KindServed] != rep.Kinds[KindServed] {
		t.Fatalf("yüklenen tür sayıları farklı")
	}
	// Dosya makul boyutta mı
	fi, err := os.Stat(filepath.Join(dir, stateFile))
	if err != nil || fi.Size() > 5<<20 {
		t.Fatalf("kayıt dosyası: %v", err)
	}
	t.Logf("3 saatlik geçmiş diskte %d KB", fi.Size()/1024)
}

func TestBozukKayitDosyasi(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, stateFile), []byte("bu gzip değil"), 0o640); err != nil {
		t.Fatal(err)
	}
	p := NewStatsPoller("", 2*time.Second, 1800, 1440)
	if err := NewStore(dir, p, nil, nil).Load(); err == nil {
		t.Fatal("bozuk dosya hata vermeliydi")
	}
	if len(p.minutes) != 0 {
		t.Fatal("bozuk dosyadan veri yüklenmiş")
	}
}

func TestYolBasinaIPDokumu(t *testing.T) {
	a := NewLogAnalyzer("file:/yok", "")
	kayit := func(ip, yol string, kod int) logRecord {
		return logRecord{At: time.Now(), Client: ip, Frontend: "fe", Backend: "be_web", Server: "s1",
			Status: kod, Method: "POST", Path: yol, RawPath: yol, Kind: KindServed}
	}
	ekle := func(n int, r logRecord) {
		for i := 0; i < n; i++ {
			a.add(r)
		}
	}
	// Başarılı (2xx) bir adres: eskiden hiç IP dökümü tutulmuyordu
	ekle(900, kayit("162.158.1.1", "/cmsapi/webanalytics/LogHit", 200))
	ekle(300, kayit("198.51.100.7", "/cmsapi/webanalytics/LogHit", 200))
	ekle(50, kayit("198.51.100.7", "/cmsapi/webanalytics/LogHit", 404))
	ekle(20, kayit("203.0.113.9", "/baska", 200))

	rep := a.Report(60)
	var row *PathRow
	for i := range rep.Paths {
		if rep.Paths[i].Path == "/cmsapi/webanalytics/LogHit" {
			row = &rep.Paths[i]
		}
	}
	if row == nil || row.N != 1250 {
		t.Fatalf("yol satırı: %+v", row)
	}
	if len(row.IPs) != 2 {
		t.Fatalf("IP dökümü: %+v", row.IPs)
	}
	// En çok istek yapan IP başta ve sayı tüm yanıt kodlarını kapsamalı
	if row.IPs[0].IP != "162.158.1.1" || row.IPs[0].N != 900 || !row.IPs[0].Cloudflare {
		t.Fatalf("ilk IP: %+v", row.IPs[0])
	}
	// Sınıf değil gerçek kod saklanmalı
	if len(row.IPs[0].Codes) != 1 || row.IPs[0].Codes[0] != (ipCode{200, 900}) {
		t.Fatalf("ilk IP kod dağılımı: %+v", row.IPs[0].Codes)
	}
	// İkinci IP'nin istekleri hem 2xx hem 4xx
	if row.IPs[1].IP != "198.51.100.7" || row.IPs[1].N != 350 {
		t.Fatalf("ikinci IP: %+v", row.IPs[1])
	}
	// İkinci IP hem 200 hem 404 almış; çoktan aza sıralı
	if len(row.IPs[1].Codes) != 2 || row.IPs[1].Codes[0] != (ipCode{200, 300}) || row.IPs[1].Codes[1] != (ipCode{404, 50}) {
		t.Fatalf("ikinci IP kod dağılımı: %+v", row.IPs[1].Codes)
	}
}

func TestYolIPSiniri(t *testing.T) {
	a := NewLogAnalyzer("file:/yok", "")
	// 30 farklı IP; en çok istek yapan 20'si listelenmeli, gerisi "(diğer)" altında toplanmalı
	for i := 0; i < 30; i++ {
		for j := 0; j <= i; j++ {
			a.add(logRecord{At: time.Now(), Client: fmt.Sprintf("198.51.100.%d", i), Frontend: "fe",
				Backend: "be", Server: "s1", Status: 200, Method: "GET", Path: "/x", RawPath: "/x", Kind: KindServed})
		}
	}
	rep := a.Report(60)
	if len(rep.Paths) != 1 {
		t.Fatalf("yol sayısı: %d", len(rep.Paths))
	}
	ips := rep.Paths[0].IPs
	if len(ips) != maxPathIPs+1 { // 20 IP + "(diğer)"
		t.Fatalf("listelenen IP sayısı: %d, beklenen %d", len(ips), maxPathIPs+1)
	}
	// Listede EN ÇOK istek yapanlar olmalı (ilk görülenler değil)
	if ips[0].IP != "198.51.100.29" || ips[0].N != 30 {
		t.Fatalf("en çok istek yapan IP listede değil: %+v", ips[0])
	}
	if ips[0].N < ips[1].N {
		t.Fatalf("sıralama bozuk: %+v", ips[:2])
	}
	// Toplam sayım eksiksiz olmalı (listede olmayanlar "(diğer)" altında)
	var toplam int64
	for _, c := range ips {
		toplam += c.N
	}
	if toplam != rep.Paths[0].N {
		t.Fatalf("IP toplamı %d, yol toplamı %d", toplam, rep.Paths[0].N)
	}
	if ips[len(ips)-1].IP != otherKey {
		t.Fatalf("son satır '(diğer)' olmalı: %+v", ips[len(ips)-1])
	}
}

// Ajan yeniden başladığında IP dökümü de korunmalı: eskiden yalnızca sayılar
// diske yazılıyordu, bu yüzden yeniden başlatmadan sonra IP'ler eksik görünüyordu.
func TestIPDokumuDiskeYaziliyor(t *testing.T) {
	dir := t.TempDir()
	a := NewLogAnalyzer("file:/yok", "")
	for i := 0; i < 500; i++ {
		a.add(logRecord{At: time.Now(), Client: "203.0.113.5", Frontend: "fe", Backend: "be", Server: "s1",
			Status: 200, Method: "POST", Path: "/api/kayit", RawPath: "/api/kayit", Kind: KindServed})
	}
	for i := 0; i < 120; i++ {
		a.add(logRecord{At: time.Now(), Client: "198.51.100.9", Frontend: "fe", Backend: "be", Server: "s1",
			Status: 404, Method: "POST", Path: "/api/kayit", RawPath: "/api/kayit", Kind: KindServed})
	}
	if err := NewStore(dir, nil, a, nil).Save(); err != nil {
		t.Fatal(err)
	}
	a2 := NewLogAnalyzer("file:/yok", "")
	if err := NewStore(dir, nil, a2, nil).Load(); err != nil {
		t.Fatal(err)
	}
	rep := a2.Report(60)
	if len(rep.Paths) != 1 || rep.Paths[0].N != 620 {
		t.Fatalf("yüklenen yol: %+v", rep.Paths)
	}
	ips := rep.Paths[0].IPs
	if len(ips) != 2 {
		t.Fatalf("yüklenen IP dökümü: %+v", ips)
	}
	var toplam int64
	for _, c := range ips {
		toplam += c.N
	}
	if toplam != rep.Paths[0].N {
		t.Fatalf("IP toplamı %d, yol toplamı %d", toplam, rep.Paths[0].N)
	}
	if len(ips[0].Codes) != 1 || ips[0].Codes[0] != (ipCode{200, 500}) {
		t.Fatalf("kod dağılımı korunmamış: %+v", ips[0].Codes)
	}
	if len(ips[1].Codes) != 1 || ips[1].Codes[0] != (ipCode{404, 120}) {
		t.Fatalf("kod dağılımı korunmamış: %+v", ips[1].Codes)
	}
}

// Diske yazılan dosya, IP dökümüyle birlikte makul boyutta kalmalı
func TestKayitDosyasiBoyutu(t *testing.T) {
	if testing.Short() {
		t.Skip("uzun süren ölçüm")
	}
	dir := t.TempDir()
	a := NewLogAnalyzer("file:/yok", "")
	a.SetRetention(1440)
	simdi := time.Now()
	// 24 saat, dakikada 300 farklı adres ve 50 farklı IP
	for dk := 1439; dk >= 0; dk-- {
		ts := simdi.Add(-time.Duration(dk) * time.Minute)
		for i := 0; i < 300; i++ {
			yol := "/api/bolum" + strconv.Itoa(i%37) + "/kaynak/" + strconv.Itoa(i)
			a.add(logRecord{At: ts, Client: fmt.Sprintf("198.51.100.%d", i%50), Frontend: "fe",
				Backend: "be_web", Server: "s1", Status: 200, Method: "GET", Path: yol, RawPath: yol, Kind: KindServed})
		}
	}
	if err := NewStore(dir, nil, a, nil).Save(); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, stateFile))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("24 saatlik geçmiş diskte: %d KB", fi.Size()/1024)
	if fi.Size() > 20<<20 {
		t.Fatalf("kayıt dosyası çok büyük: %d MB", fi.Size()>>20)
	}
}

// Bir IP'nin tek bir adreste aldığı farklı kodlar: 6'ya kadar gerçek kodla görünür,
// fazlası "diğer"de toplanır ve toplam hiçbir durumda bozulmaz.
func TestIPKodSiniri(t *testing.T) {
	ekle := func(a *LogAnalyzer, kodlar []int, adet int) int64 {
		var toplam int64
		for tur := 0; tur < adet; tur++ {
			for _, kod := range kodlar {
				a.add(logRecord{At: time.Now(), Client: "203.0.113.5", Frontend: "fe", Backend: "be", Server: "s1",
					Status: kod, Method: "GET", Path: "/x", RawPath: "/x", Kind: KindServed})
				toplam++
			}
		}
		return toplam
	}

	// 6 farklı kod: hepsi gerçek koduyla görünmeli
	a := NewLogAnalyzer("file:/yok", "")
	toplam := ekle(a, []int{200, 404, 500, 302, 403, 502}, 10)
	ip := a.Report(60).Paths[0].IPs[0]
	if ip.N != toplam || len(ip.Codes) != 6 || ip.Other != 0 {
		t.Fatalf("6 kod: %+v (toplam %d)", ip, toplam)
	}

	// 9 farklı kod: 6'sı görünür, kalanı "diğer"de; toplam korunur
	b := NewLogAnalyzer("file:/yok", "")
	toplam = ekle(b, []int{200, 404, 500, 302, 403, 502, 401, 429, 301}, 10)
	ip = b.Report(60).Paths[0].IPs[0]
	if ip.N != toplam {
		t.Fatalf("IP toplamı %d, beklenen %d", ip.N, toplam)
	}
	if len(ip.Codes) != maxIPCodes {
		t.Fatalf("kod sayısı: %d", len(ip.Codes))
	}
	var k int64
	for _, c := range ip.Codes {
		k += int64(c.N)
	}
	if k+int64(ip.Other) != ip.N {
		t.Fatalf("kodların toplamı %d + diğer %d, IP toplamı %d", k, ip.Other, ip.N)
	}
	if ip.Other != 30 { // listeye girmeyen 3 kod x 10 istek
		t.Fatalf("diğer: %d", ip.Other)
	}
}

// Ajan yeniden başladığında 10 saniyelik ölçümler sıfırlanır (diske yazılmazlar).
// Kısa aralıklar bu durumda diskten gelen dakikalık veriye düşmeli, yoksa panel
// "5 dakika" seçiliyken "son 1 dk" gibi çok kısa bir aralık gösterir.
func TestKisaAralikDiskVerisineDuser(t *testing.T) {
	dir := t.TempDir()
	row := func(req string) map[string]string {
		return map[string]string{"pxname": "be", "svname": "BACKEND", "type": "1", "req_tot": req, "hrsp_2xx": req}
	}
	// Birinci ajan: 20 dakikalık trafik biriktirip diske yazıyor
	a := NewStatsPoller("", 2*time.Second, 1800, 1440)
	basla := (time.Now().UnixMilli()/60_000 - 20) * 60_000
	for i := int64(0); i <= 20*6; i++ {
		at := basla + i*10_000
		a.store(&Snapshot{At: at, Info: map[string]string{"Uptime_sec": strconv.FormatInt(1000+i*10, 10)},
			Rows: []map[string]string{row(strconv.FormatInt(i*100, 10))}})
	}
	if err := NewStore(dir, a, nil, nil).Save(); err != nil {
		t.Fatal(err)
	}

	// İkinci ajan: diskten yükledi, henüz tek ölçüm aldı
	b := NewStatsPoller("", 2*time.Second, 1800, 1440)
	if err := NewStore(dir, b, nil, nil).Load(); err != nil {
		t.Fatal(err)
	}
	b.store(&Snapshot{At: time.Now().UnixMilli(), Info: map[string]string{"Uptime_sec": "5000"},
		Rows: []map[string]string{row("999999")}})

	st := b.State(15) // 15 dakika: ince ölçümler yok, dakikalık veri var
	if st.Window == nil {
		t.Fatal("15 dakikalık pencere boş")
	}
	if st.Window.Seconds < 600 {
		t.Fatalf("pencere yalnızca %.0f saniyeyi kapsıyor; diskteki dakikalık veriye düşmeliydi", st.Window.Seconds)
	}
	if st.Window.Rows["be|BACKEND"].N == 0 {
		t.Fatal("pencere boş geldi")
	}
	if len(st.History) == 0 {
		t.Fatal("grafik noktası yok")
	}
}

// Aynı veriyle üretilen rapor her seferinde aynı sırada gelmeli. Eşit sayıdaki
// satırlar (özellikle birer kez görülen tarama istekleri) rastgele sıralanırsa
// panel her yenilemede yerinden oynar.
func TestRaporSirasiKararli(t *testing.T) {
	a := NewLogAnalyzer("file:/yok", "")
	simdi := time.Now()
	// Çoğu birer kez görülen, yani eşit sayıda satırlar
	yollar := []string{"/HNAP1", "/evox/about", "/admin/login.jsp", "/Dr0v", "/sdk", "/robots.txt",
		"/favicon.ico", "/api/auth/validate-sso", "/nmaplowercheck", "/nice%20ports"}
	for _, y := range yollar {
		a.add(logRecord{At: simdi, Client: "203.0.113.5", Frontend: "fe", Backend: "fe", Server: "<NOSRV>",
			Status: 403, Method: "GET", Path: y, RawPath: y, Kind: KindDenied})
	}
	for i := 0; i < 5; i++ { // biri daha sık
		a.add(logRecord{At: simdi, Client: "203.0.113.6", Frontend: "fe", Backend: "fe", Server: "<NOSRV>",
			Status: 403, Method: "HEAD", Path: "/", RawPath: "/", Kind: KindDenied})
	}

	sira := func() []string {
		rep := a.Report(60)
		var out []string
		for _, b := range rep.Blocked {
			out = append(out, b.Method+" "+b.Path)
		}
		for _, p := range rep.Paths {
			out = append(out, "y:"+p.Method+" "+p.Path)
		}
		for _, c := range rep.Clients {
			out = append(out, "ip:"+c.IP)
		}
		return out
	}
	ilk := sira()
	if len(ilk) < len(yollar) {
		t.Fatalf("rapor eksik: %d satır", len(ilk))
	}
	for i := 0; i < 20; i++ { // map sırası her turda değişir; sonuç değişmemeli
		if s := sira(); !reflect.DeepEqual(s, ilk) {
			t.Fatalf("sıra %d. denemede değişti:\n  ilk:   %v\n  sonra: %v", i+1, ilk, s)
		}
	}
	// En sık görülen yine başta olmalı
	if ilk[0] != "HEAD /" {
		t.Fatalf("en sık satır başta değil: %v", ilk[0])
	}
}

// Sunucu ölçümleri /proc'tan okunur; değerler makul aralıkta olmalı ve
// geçmiş diske yazılıp geri yüklenebilmeli.
func TestSistemOlcumleri(t *testing.T) {
	s := NewSysPoller(10*time.Millisecond, 1440, "/tmp")
	s.tick()
	time.Sleep(60 * time.Millisecond)
	s.tick()

	st := s.State(60)
	if st == nil || !st.OK {
		t.Fatalf("ölçüm alınamadı: %+v", st)
	}
	if st.CPUs < 1 {
		t.Fatalf("çekirdek sayısı: %d", st.CPUs)
	}
	if st.MemTotal <= 0 || st.MemUsed <= 0 || st.MemUsed > st.MemTotal {
		t.Fatalf("bellek: %d / %d", st.MemUsed, st.MemTotal)
	}
	if st.Cur.CPU < 0 || st.Cur.CPU > 100 || st.Cur.MemPct < 0 || st.Cur.MemPct > 100 {
		t.Fatalf("yüzdeler aralık dışında: %+v", st.Cur)
	}
	if len(st.Disks) == 0 {
		t.Fatal("disk doluluğu okunamadı")
	}
	for _, d := range st.Disks {
		if d.Total <= 0 || d.UsedPct < 0 || d.UsedPct > 100 {
			t.Fatalf("disk değeri hatalı: %+v", d)
		}
	}
	if len(st.History) == 0 {
		t.Fatal("grafik noktası yok")
	}

	// Diske yazıp geri yükleme
	dir := t.TempDir()
	s.dakika = []SysPoint{{T: time.Now().UnixMilli() - 60_000, CPU: 12, MemPct: 40}}
	if err := NewStore(dir, nil, nil, s).Save(); err != nil {
		t.Fatal(err)
	}
	s2 := NewSysPoller(time.Second, 1440)
	if err := NewStore(dir, nil, nil, s2).Load(); err != nil {
		t.Fatal(err)
	}
	if len(s2.dakika) != 1 || s2.dakika[0].CPU != 12 {
		t.Fatalf("diskten yüklenen sistem geçmişi: %+v", s2.dakika)
	}
}

func TestFizikselAygitSecimi(t *testing.T) {
	for ad, bekle := range map[string]bool{
		"sda": true, "sda1": false, "vda": true, "vda2": false, "nvme0n1": true, "nvme0n1p3": false,
		"loop0": false, "dm-0": false, "ram0": false, "xvdb": true, "sr0": false,
	} {
		if fizikselAygit(ad) != bekle {
			t.Errorf("%s: %v, beklenen %v", ad, fizikselAygit(ad), bekle)
		}
	}
}
