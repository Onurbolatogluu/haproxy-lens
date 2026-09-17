package main

import (
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

	a := NewLogAnalyzer("file:"+filepath.Join(dir, "haproxy.log"), "")
	return a
}

func TestAramaDondurulmusDosyalar(t *testing.T) {
	a := aramaOrtami(t)
	res := a.Search(SearchQuery{Path: "/api/kayit"})
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
	if res := a.Search(SearchQuery{Path: "/api/kayit", Status: "5xx"}); res.Matches != 2 {
		t.Fatalf("5xx: %d, beklenen 2", res.Matches)
	}
	if res := a.Search(SearchQuery{Path: "/api/kayit", Status: "503"}); res.Matches != 1 {
		t.Fatalf("503: %d, beklenen 1", res.Matches)
	}
	if res := a.Search(SearchQuery{IP: "198.51.100.9"}); res.Matches != 2 {
		t.Fatalf("IP: %d, beklenen 2", res.Matches)
	}
	if res := a.Search(SearchQuery{IP: "192.0.2."}); res.Matches != 3 { // önek eşleşmesi
		t.Fatalf("IP öneki: %d, beklenen 3", res.Matches)
	}
	// Zaman aralığı: yalnızca son 2 saat
	res := a.Search(SearchQuery{Path: "/api/kayit", Since: time.Now().Add(-2 * time.Hour)})
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
	res := a.Search(SearchQuery{Path: "/"})
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
	res := a.Search(SearchQuery{Path: "/api/kayit"})
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
	res := a.Search(SearchQuery{Path: "/api/kayit"})
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
	res = a.Search(SearchQuery{Path: "/api/kayit", Since: simdi.Add(-2 * time.Hour)})
	if res.Matches != 20 {
		t.Fatalf("son 2 saat eşleşme: %d, beklenen 20", res.Matches)
	}
	if res.Scanned > 100 {
		t.Fatalf("erken durma çalışmadı: %d satır tarandı", res.Scanned)
	}
}
