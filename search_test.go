package main

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Arama, panelin belleğine değil doğrudan log dosyalarına bakmalı; döndürülmüş
// (.1) ve sıkıştırılmış (.gz) dosyalar da taranmalı. Böylece saklama süresinin
// ötesindeki kayıtlar da bulunur.
func logSatiri(ts time.Time, ip, yol string, kod int) string {
	a := ts.Format("Jan _2 15:04:05")
	b := ts.Format("02/Jan/2006:15:04:05.000")
	srv := "srv1"
	if kod >= 500 {
		srv = "srv2"
	}
	return fmt.Sprintf("%s lb haproxy[1]: %s:5555 [%s] http_front~ be_web/%s 0/0/1/40/41 %d 216 - - ---- 1/1/1/1/0 0/0 {} \"GET %s HTTP/1.1\"",
		a, ip, b, srv, kod, yol)
}

func yaz(t *testing.T, yol string, satirlar []string, sikistir bool) {
	t.Helper()
	f, err := os.Create(yol)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	metin := strings.Join(satirlar, "\n") + "\n"
	if sikistir {
		gz := gzip.NewWriter(f)
		gz.Write([]byte(metin))
		gz.Close()
		return
	}
	f.WriteString(metin)
}

func aramaOrtami(t *testing.T) *LogAnalyzer {
	t.Helper()
	dir := t.TempDir()
	simdi := time.Now()
	// Güncel dosya: son 1 saat
	var guncel []string
	for i := 0; i < 50; i++ {
		guncel = append(guncel, logSatiri(simdi.Add(-time.Duration(i)*time.Minute), "203.0.113.5", "/api/kayit", 200))
	}
	guncel = append(guncel, logSatiri(simdi.Add(-10*time.Minute), "198.51.100.9", "/api/kayit", 500))
	guncel = append(guncel, logSatiri(simdi.Add(-5*time.Minute), "198.51.100.9", "/baska/yol", 404))
	yaz(t, filepath.Join(dir, "haproxy.log"), guncel, false)

	// Döndürülmüş dosya: 3 gün önce (panelin saklama süresinin çok ötesi)
	eski := []string{
		logSatiri(simdi.Add(-72*time.Hour), "192.0.2.77", "/api/kayit", 200),
		logSatiri(simdi.Add(-72*time.Hour), "192.0.2.77", "/api/kayit", 503),
	}
	yaz(t, filepath.Join(dir, "haproxy.log.1"), eski, false)

	// Sıkıştırılmış dosya: 5 gün önce
	cokEski := []string{logSatiri(simdi.Add(-120*time.Hour), "192.0.2.88", "/api/kayit", 200)}
	yaz(t, filepath.Join(dir, "haproxy.log.2.gz"), cokEski, true)

	// Döndürülmüş dosyaların değiştirilme zamanları gerçekte olduğu gibi eski
	eskit(t, filepath.Join(dir, "haproxy.log.1"), simdi.Add(-30*time.Hour))
	eskit(t, filepath.Join(dir, "haproxy.log.2.gz"), simdi.Add(-5*24*time.Hour))
	return NewLogAnalyzer("file:"+filepath.Join(dir, "haproxy.log"), "")
}

func eskit(t *testing.T, yol string, ts time.Time) {
	t.Helper()
	if err := os.Chtimes(yol, ts, ts); err != nil {
		t.Fatal(err)
	}
}

func TestAramaDondurulmusDosyalar(t *testing.T) {
	a := aramaOrtami(t)
	res := ara(t, a, SearchQuery{Path: "/api/kayit"})
	// 50 + 1 (güncel) + 2 (döndürülmüş) + 1 (gz) = 54
	if res.Matches != 54 {
		t.Fatalf("eşleşme: %d, beklenen 54 (dosyalar: %v)", res.Matches, res.Files)
	}
	if len(res.Files) != 3 {
		t.Fatalf("taranan dosyalar: %v", res.Files)
	}
	// 5 gün öncesi de bulunmalı: saklama süresinin ötesi
	if time.Since(time.UnixMilli(res.FirstAt)) < 100*time.Hour {
		t.Fatalf("en eski kayıt %v, daha eskisi bulunmalıydı", time.UnixMilli(res.FirstAt))
	}
	var kod503 bool
	for _, c := range res.Codes {
		if c.Code == 503 {
			kod503 = true
		}
	}
	if !kod503 {
		t.Fatalf("eski dosyadaki 503 bulunamadı: %+v", res.Codes)
	}
}

func TestAramaSuzgecleri(t *testing.T) {
	a := aramaOrtami(t)
	if res := ara(t, a, SearchQuery{Path: "/api/kayit", Status: "5xx"}); res.Matches != 2 {
		t.Fatalf("5xx: %d, beklenen 2", res.Matches)
	}
	if res := ara(t, a, SearchQuery{Path: "/api/kayit", Status: "503"}); res.Matches != 1 {
		t.Fatalf("503: %d, beklenen 1", res.Matches)
	}
	if res := ara(t, a, SearchQuery{IP: "198.51.100.9"}); res.Matches != 2 {
		t.Fatalf("IP: %d, beklenen 2", res.Matches)
	}
	if res := ara(t, a, SearchQuery{IP: "192.0.2."}); res.Matches != 3 { // önek eşleşmesi
		t.Fatalf("IP öneki: %d, beklenen 3", res.Matches)
	}
	// Zaman aralığı: yalnızca son 2 saat
	res := ara(t, a, SearchQuery{Path: "/api/kayit", Since: time.Now().Add(-2 * time.Hour)})
	if res.Matches != 51 {
		t.Fatalf("son 2 saat: %d, beklenen 51", res.Matches)
	}
	// En yeni satır başta olmalı
	if len(res.Hits) < 2 || res.Hits[0].At < res.Hits[1].At {
		t.Fatal("sonuçlar yeniden eskiye sıralı değil")
	}
}

func TestAramaSorguDogrulama(t *testing.T) {
	al := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	if _, err := searchQueryFrom(al(map[string]string{})); err == nil {
		t.Fatal("boş sorgu kabul edilmemeliydi")
	}
	if _, err := searchQueryFrom(al(map[string]string{"path": "/x", "status": "abc"})); err == nil {
		t.Fatal("geçersiz durum kodu kabul edilmemeliydi")
	}
	if _, err := searchQueryFrom(al(map[string]string{"path": strings.Repeat("a", 500)})); err == nil {
		t.Fatal("çok uzun metin kabul edilmemeliydi")
	}
	q, err := searchQueryFrom(al(map[string]string{"path": "/x", "status": "5xx", "hours": "48"}))
	if err != nil {
		t.Fatal(err)
	}
	if q.Since.IsZero() || time.Since(q.Since) < 47*time.Hour {
		t.Fatalf("zaman aralığı: %v", q.Since)
	}
}

// Ajan yalnızca kendi log kaynağını ve onun döndürülmüş kopyalarını okumalı
func TestAramaBaskaDosyayiOkumaz(t *testing.T) {
	dir := t.TempDir()
	yaz(t, filepath.Join(dir, "haproxy.log"), []string{logSatiri(time.Now(), "203.0.113.5", "/var", 200)}, false)
	yaz(t, filepath.Join(dir, "gizli.log"), []string{logSatiri(time.Now(), "203.0.113.9", "/gizli", 200)}, false)
	a := NewLogAnalyzer("file:"+filepath.Join(dir, "haproxy.log"), "")
	res := ara(t, a, SearchQuery{Path: "/"})
	for _, f := range res.Files {
		if strings.Contains(f, "gizli") {
			t.Fatalf("ilgisiz dosya okundu: %v", res.Files)
		}
	}
	if res.Matches != 1 {
		t.Fatalf("eşleşme: %d", res.Matches)
	}
}

// Büyük dosyada süre sınırı devreye girmeli ve bu sonuçta belirtilmeli
func TestAramaSinirlari(t *testing.T) {
	if testing.Short() {
		t.Skip("uzun süren ölçüm")
	}
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "haproxy.log"))
	if err != nil {
		t.Fatal(err)
	}
	simdi := time.Now()
	for i := 0; i < 400000; i++ {
		fmt.Fprintln(f, logSatiri(simdi, "203.0.113."+strconv.Itoa(i%250), "/api/kayit/"+strconv.Itoa(i), 200))
	}
	f.Close()
	a := NewLogAnalyzer("file:"+filepath.Join(dir, "haproxy.log"), "")
	res := ara(t, a, SearchQuery{Path: "/api/kayit"})
	t.Logf("%d satır tarandı, %d eşleşme, %d ms", res.Scanned, res.Matches, res.Took)
	if len(res.Hits) > aramaOrnekSiniri {
		t.Fatalf("örnek sınırı aşıldı: %d", len(res.Hits))
	}
	if len(res.IPs) > aramaIPSiniri {
		t.Fatalf("IP sınırı aşıldı: %d", len(res.IPs))
	}
}

// Gösterilen satırlar, dosyaların taranma sırasından bağımsız olarak EN YENİLER olmalı;
// ve zaman aralığı verilmişse eski dosyalar boşuna taranmamalı.
func TestAramaEnYeniVeErkenDurma(t *testing.T) {
	dir := t.TempDir()
	simdi := time.Now()
	// Eski dosya çok kalabalık, yeni dosya az satırlı: yanlış sıralama olursa yakalanır
	var eski []string
	for i := 0; i < 600; i++ {
		eski = append(eski, logSatiri(simdi.Add(-72*time.Hour-time.Duration(i)*time.Second), "192.0.2.77", "/api/kayit", 200))
	}
	yaz(t, filepath.Join(dir, "haproxy.log.1"), eski, false)
	var yeni []string
	for i := 0; i < 20; i++ {
		yeni = append(yeni, logSatiri(simdi.Add(-time.Duration(i)*time.Minute), "203.0.113.5", "/api/kayit", 200))
	}
	yaz(t, filepath.Join(dir, "haproxy.log"), yeni, false)
	// Döndürülmüş dosyanın değiştirilme zamanı gerçekte olduğu gibi eski olsun
	eskiZaman := simdi.Add(-72 * time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "haproxy.log.1"), eskiZaman, eskiZaman); err != nil {
		t.Fatal(err)
	}
	a := NewLogAnalyzer("file:"+filepath.Join(dir, "haproxy.log"), "")

	// Tüm log: en yeni satır güncel dosyadan gelmeli
	res := ara(t, a, SearchQuery{Path: "/api/kayit"})
	if res.Matches != 620 {
		t.Fatalf("eşleşme: %d", res.Matches)
	}
	if res.Hits[0].IP != "203.0.113.5" {
		t.Fatalf("en yeni satır yanlış: %+v", res.Hits[0])
	}
	for i := 1; i < len(res.Hits); i++ {
		if res.Hits[i-1].At < res.Hits[i].At {
			t.Fatal("sonuçlar yeniden eskiye sıralı değil")
		}
	}

	// Son 2 saat: eski dosya taranmamalı (erken durma)
	res = ara(t, a, SearchQuery{Path: "/api/kayit", Since: simdi.Add(-2 * time.Hour)})
	if res.Matches != 20 {
		t.Fatalf("son 2 saat eşleşme: %d, beklenen 20", res.Matches)
	}
	if res.Scanned > 100 {
		t.Fatalf("erken durma çalışmadı: %d satır tarandı", res.Scanned)
	}
}

// Büyük bir dosyada "son 1 saat" araması, dosyanın tamamını taramadan hızlıca
// bitmeli: log'lar zaman sıralı olduğu için sondan başa okunur.
func TestAramaBuyukDosyadaSonSaat(t *testing.T) {
	if testing.Short() {
		t.Skip("uzun süren ölçüm")
	}
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "haproxy.log"))
	if err != nil {
		t.Fatal(err)
	}
	w := bufio.NewWriterSize(f, 1<<20)
	simdi := time.Now()
	// 24 saatlik log, saniyede ~10 istek: 860.000 satır. Aranan adres yalnızca
	// son 30 dakikada geçiyor, yani dosyanın en sonunda.
	for i := 860000; i > 0; i-- {
		ts := simdi.Add(-time.Duration(i) * 100 * time.Millisecond)
		yol := "/eski/sayfa"
		if i < 18000 {
			yol = "/api/yeni"
		}
		fmt.Fprintln(w, logSatiri(ts, "203.0.113.5", yol, 200))
	}
	w.Flush()
	f.Close()

	a := NewLogAnalyzer("file:"+filepath.Join(dir, "haproxy.log"), "")
	basla := time.Now()
	res := ara(t, a, SearchQuery{Path: "/api/yeni", Since: simdi.Add(-time.Hour)})
	sure := time.Since(basla)
	t.Logf("%d eşleşme, %d satır tarandı, %v sürdü (dosyada 860.000 satır var)", res.Matches, res.Scanned, sure.Round(time.Millisecond))

	if res.Matches != 17999 {
		t.Fatalf("eşleşme: %d, beklenen 17.999", res.Matches)
	}
	if res.Truncated {
		t.Fatal("süre sınırına takılmamalıydı")
	}
	// Dosyanın tamamı taranmamalı: aralığın dışına çıkınca durulur
	if res.Scanned > 100000 {
		t.Fatalf("gereğinden fazla tarandı: %d satır", res.Scanned)
	}
}

// Dosyaların değiştirilme zamanları birbirine çok yakın olabilir (ör. yeni kurulan
// bir sunucuda ya da kopyalanmış log'larda). Arama bu sıralamaya bağlı olmamalı:
// hangi sırayla taranırsa taransın sonuç aynı çıkmalı.
func TestAramaDosyaSirasinaBagliDegil(t *testing.T) {
	dir := t.TempDir()
	simdi := time.Now()
	yaz(t, filepath.Join(dir, "haproxy.log"), []string{
		logSatiri(simdi.Add(-10*time.Minute), "203.0.113.5", "/api/kayit", 200),
		logSatiri(simdi.Add(-20*time.Minute), "203.0.113.5", "/api/kayit", 200),
	}, false)
	yaz(t, filepath.Join(dir, "haproxy.log.1"), []string{
		logSatiri(simdi.Add(-40*time.Minute), "203.0.113.6", "/api/kayit", 500),
	}, false)
	// Zamanlar birebir aynı: sıralama belirsiz
	ayni := simdi
	eskit(t, filepath.Join(dir, "haproxy.log"), ayni)
	eskit(t, filepath.Join(dir, "haproxy.log.1"), ayni)

	a := NewLogAnalyzer("file:"+filepath.Join(dir, "haproxy.log"), "")
	res := ara(t, a, SearchQuery{Path: "/api/kayit", Since: simdi.Add(-2 * time.Hour)})
	if res.Matches != 3 {
		t.Fatalf("eşleşme: %d, beklenen 3 (dosyalar: %v)", res.Matches, res.Files)
	}
	if len(res.Files) != 2 {
		t.Fatalf("her iki dosya da taranmalıydı: %v", res.Files)
	}
}

// Ön eleme, eşleşmeyen satırları ayrıştırmadan attığı için arama kat kat hızlanır.
// Bu test hem hızı hem de sonucun değişmediğini korur.
func TestAramaOnElemeHizi(t *testing.T) {
	if testing.Short() {
		t.Skip("uzun süren ölçüm")
	}
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "haproxy.log"))
	if err != nil {
		t.Fatal(err)
	}
	w := bufio.NewWriterSize(f, 1<<20)
	simdi := time.Now()
	const toplam = 500000
	for i := toplam; i > 0; i-- {
		yol := "/cmsapi/webanalytics/LogHit"
		if i%1000 == 0 {
			yol = "/api/v1/config"
		}
		fmt.Fprintln(w, logSatiri(simdi.Add(-time.Duration(i)*80*time.Millisecond), "172.69.251.5", yol, 200))
	}
	w.Flush()
	f.Close()

	a := NewLogAnalyzer("file:"+filepath.Join(dir, "haproxy.log"), "")
	basla := time.Now()
	res := ara(t, a, SearchQuery{Path: "/api/v1"})
	hiz := float64(res.Scanned) / time.Since(basla).Seconds()
	t.Logf("%d satır tarandı, %d eşleşme, %.0f satır/sn", res.Scanned, res.Matches, hiz)

	if res.Matches != 500 {
		t.Fatalf("eşleşme: %d, beklenen 500", res.Matches)
	}
	if res.Truncated {
		t.Fatal("süre sınırına takılmamalıydı")
	}
	// Ön eleme olmadan bu hız ~200 bin satır/sn seviyesindeydi
	if hiz < 600000 {
		t.Fatalf("arama beklenenden yavaş: %.0f satır/sn", hiz)
	}
}

// Testler için: aramayı yapar, "başka arama sürüyor" hatasını test hatası sayar
func ara(t *testing.T, a *LogAnalyzer, q SearchQuery) SearchResult {
	t.Helper()
	res, err := a.Search(q)
	if err != nil {
		t.Fatalf("arama: %v", err)
	}
	return res
}

// Aynı anda ikinci bir arama gelirse beklemeden reddedilmeli
func TestAramaAyniAndaTek(t *testing.T) {
	a := aramaOrtami(t)
	aramaKilidi.Lock() // bir arama sürüyormuş gibi
	_, err := a.Search(SearchQuery{Path: "/api"})
	aramaKilidi.Unlock()
	if err != ErrAramaSuruyor {
		t.Fatalf("ikinci arama beklemeden reddedilmeliydi, hata: %v", err)
	}
	if _, err := a.Search(SearchQuery{Path: "/api"}); err != nil {
		t.Fatalf("kilit bırakıldıktan sonra arama çalışmalı: %v", err)
	}
}

// Tam adres araması: "/" yalnızca ana sayfayı bulmalı, içinde "/" geçen her şeyi değil
func TestAramaTamAdres(t *testing.T) {
	a := aramaOrtami(t)
	hepsi := ara(t, a, SearchQuery{Path: "/"})
	tam := ara(t, a, SearchQuery{Path: "/baska/yol", Exact: true})
	if tam.Matches != 1 {
		t.Fatalf("tam adres eşleşmesi: %d, beklenen 1", tam.Matches)
	}
	if kok := ara(t, a, SearchQuery{Path: "/", Exact: true}); kok.Matches != 0 || hepsi.Matches == 0 {
		t.Fatalf("'/' tam aramada hiçbir şey bulmamalı (ortamda ana sayfa isteği yok): %d, içinde geçen: %d", kok.Matches, hepsi.Matches)
	}
	if yarim := ara(t, a, SearchQuery{Path: "/baska", Exact: true}); yarim.Matches != 0 {
		t.Fatalf("yolun bir kısmı tam aramada eşleşmemeli: %d", yarim.Matches)
	}
}

// Yöntemle arama: yalnızca yöntem verilse de çalışmalı, ön eleme sonucu değiştirmemeli
func TestAramaYontem(t *testing.T) {
	dir := t.TempDir()
	simdi := time.Now()
	var satirlar []string
	for i := 0; i < 30; i++ {
		y := "GET"
		if i%3 == 0 {
			y = "POST"
		}
		satirlar = append(satirlar, strings.Replace(logSatiri(simdi.Add(-time.Duration(i)*time.Minute), "203.0.113.5", "/api/kayit", 200), `"GET `, `"`+y+" ", 1))
	}
	// Yolun içinde "post" geçen ama yöntemi GET olan satır: ön eleme yanıltmamalı
	satirlar = append(satirlar, logSatiri(simdi, "203.0.113.6", "/blog/post", 200))
	yaz(t, filepath.Join(dir, "haproxy.log"), satirlar, false)
	a := NewLogAnalyzer("file:"+filepath.Join(dir, "haproxy.log"), "")

	if res := ara(t, a, SearchQuery{Method: "POST"}); res.Matches != 10 {
		t.Fatalf("yalnızca yöntem: %d, beklenen 10", res.Matches)
	}
	if res := ara(t, a, SearchQuery{Method: "GET", Path: "/api"}); res.Matches != 20 {
		t.Fatalf("yöntem + adres: %d, beklenen 20", res.Matches)
	}
	al := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if q, err := searchQueryFrom(al(map[string]string{"method": "post"})); err != nil || q.Method != "POST" {
		t.Fatalf("küçük harfli yöntem kabul edilmeli: %v %v", q.Method, err)
	}
	if _, err := searchQueryFrom(al(map[string]string{"method": "GETX"})); err == nil {
		t.Fatal("bilinmeyen yöntem reddedilmeliydi")
	}
}
