package main

import (
	"fmt"
	"math/rand"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// Olay incelemesi için gerçekçi log üretici: satırlar YAZILMA zamanına göre sıralı, ama
// içlerindeki kabul zamanı (HAProxy'nin [..] alanı) istek süresi kadar geride; yani kayıtlar
// tam sıralı değil. Araya sunucu olay satırları ("Server x/y is DOWN") karışır.
type olayUretici struct {
	r *rand.Rand
}

var olayBackendler = []string{"be_web", "be_api", "be_statik"}
var olayTerimler = []string{"----", "----", "----", "----", "SC--", "sC--", "SH--", "sH--", "CD--", "cD--", "PR--", "LR--"}

func (u *olayUretici) satir(yazma time.Time) (string, time.Time) {
	r := u.r
	ta := r.Intn(400)
	if r.Intn(50) == 0 {
		ta = 20000 + r.Intn(200000) // ara sıra uzun istek: kabul zamanı birkaç dakika geride
	}
	kabul := yazma.Add(-time.Duration(ta) * time.Millisecond)
	be := olayBackendler[r.Intn(len(olayBackendler))]
	srv := fmt.Sprintf("srv%d", 1+r.Intn(3))
	kod := []int{200, 200, 200, 200, 304, 301, 404, 403, 500, 502, 503, 504}[r.Intn(12)]
	term := olayTerimler[r.Intn(len(olayTerimler))]
	if kod == 503 && r.Intn(2) == 0 {
		srv = "<NOSRV>"
	}
	// Ara sıra hiçbir backend'e gitmeyen istek: HAProxy yönlendirdi, engelledi ya da eşleşen backend yok.
	// Log'da backend yerine frontend'in adı yazar.
	if r.Intn(25) == 0 {
		be, srv = "http_front", "<NOSRV>"
		kod, term = []int{301, 403, 503}[r.Intn(3)], []string{"LR--", "PR--", "SC--"}[r.Intn(3)]
	}
	ip := fmt.Sprintf("198.51.100.%d", r.Intn(40))
	yol := fmt.Sprintf("/urun/%d/yorum", r.Intn(5))
	if r.Intn(3) == 0 {
		yol = "/api/sepet"
	}
	return fmt.Sprintf("%s lb haproxy[1]: %s:5555 [%s] http_front~ %s/%s 0/0/1/40/%d %d 216 - - %s 1/1/1/1/0 0/0 {} \"GET %s HTTP/1.1\"",
		yazma.Format("Jan _2 15:04:05"), ip, kabul.Format("02/Jan/2006:15:04:05.000"), be, srv, ta, kod, term, yol), kabul
}

func (u *olayUretici) olaySatiri(yazma time.Time) string {
	be := olayBackendler[u.r.Intn(len(olayBackendler))]
	if u.r.Intn(2) == 0 {
		return fmt.Sprintf("%s lb haproxy[1]: Server %s/srv2 is DOWN, reason: Layer4 timeout, check duration: 2001ms. 1 active and 0 backup servers left. 0 sessions active, 0 requeued, 0 remaining in queue.",
			yazma.Format("Jan _2 15:04:05"), be)
	}
	return fmt.Sprintf("%s lb haproxy[1]: Server %s/srv2 is UP, reason: Layer7 check passed, code: 200, check duration: 3ms. 3 active and 0 backup servers online. 0 sessions requeued, 0 total in queue.",
		yazma.Format("Jan _2 15:04:05"), be)
}

type uretilenLog struct {
	kayit    []logRecord // ayrıştırılabilen satırlar, bir kez ayrıştırılır
	olayZ    []time.Time // olay satırlarının syslog zamanı
	dir      string
	satirlar []string // tüm dosyalardaki satırlar
	olaylar  []time.Time
}

// Dört dosya: .3.gz, .2.gz, .1 (düz), güncel. Toplam ~gun gün, her biri bir dilim.
func olayOrtami(t *testing.T, tohum int64, gun float64, sikMs int) (*LogAnalyzer, uretilenLog) {
	t.Helper()
	// Kısa kipte (CI'daki yarış denetimi) daha seyrek log: yarış denetimi 10 kat yavaşlatıyor
	if testing.Short() {
		sikMs *= 8
	}
	u := &olayUretici{r: rand.New(rand.NewSource(tohum))}
	dir := t.TempDir()
	// Saniyenin altı atılır: syslog zamanı saniye hassasiyetinde; testte karşılaştırma kolay olsun
	simdi := time.Now().Truncate(time.Second)
	bas := simdi.Add(-time.Duration(gun * 24 * float64(time.Hour)))
	adlar := []string{"haproxy.log.3.gz", "haproxy.log.2.gz", "haproxy.log.1", "haproxy.log"}
	dilim := simdi.Sub(bas) / time.Duration(len(adlar))
	ul := uretilenLog{dir: dir}
	yazma := bas
	for i, ad := range adlar {
		son := bas.Add(dilim * time.Duration(i+1))
		var satirlar []string
		for yazma.Before(son) {
			if u.r.Intn(400) == 0 {
				satirlar = append(satirlar, u.olaySatiri(yazma))
				ul.olaylar = append(ul.olaylar, yazma)
			} else {
				s, _ := u.satir(yazma)
				satirlar = append(satirlar, s)
			}
			yazma = yazma.Add(time.Duration(1+u.r.Intn(sikMs)) * time.Millisecond)
		}
		yaz(t, filepath.Join(dir, ad), satirlar, strings.HasSuffix(ad, ".gz"))
		eskit(t, filepath.Join(dir, ad), son)
		ul.satirlar = append(ul.satirlar, satirlar...)
	}
	for _, s := range ul.satirlar {
		if rec, ok := defaultParser.Parse(s); ok {
			ul.kayit = append(ul.kayit, rec)
		} else if zmn, ok := syslogZamani(s, time.Now()); ok {
			ul.olayZ = append(ul.olayZ, zmn)
		}
	}
	return NewLogAnalyzer("file:"+filepath.Join(dir, "haproxy.log"), ""), ul
}

// Kaba kuvvet: her satırı ayrıştırıp aralığa girenleri say. İnceleme bununla aynı sonucu vermeli.
type kabaSonuc struct {
	n     int64
	c     [5]int64
	be    map[string][5]int64
	fe    map[string][5]int64
	term  map[string]int64
	dilim map[int64]int64
	olay  int
}

func kabaKuvvet(ul uretilenLog, from, to time.Time, adim time.Duration) kabaSonuc {
	k := kabaSonuc{be: map[string][5]int64{}, fe: map[string][5]int64{}, term: map[string]int64{}, dilim: map[int64]int64{}}
	for _, zmn := range ul.olayZ {
		if !zmn.Before(from.Add(-olayPay)) && !zmn.After(to) {
			k.olay++
		}
	}
	for _, rec := range ul.kayit {
		if rec.At.Before(from) || rec.At.After(to) {
			continue
		}
		i := sinifIdx(rec.Status)
		k.n++
		k.c[i]++
		hedef := k.be
		if rec.Server == "<NOSRV>" && rec.Backend == rec.Frontend {
			hedef = k.fe
		}
		b := hedef[rec.Backend]
		b[i]++
		hedef[rec.Backend] = b
		if rec.Term[:2] != "--" {
			k.term[rec.Term[:2]]++
		}
		d := int64(rec.At.Sub(from) / adim)
		if son := int64((to.Sub(from)+adim-1)/adim) - 1; d > son {
			d = son
		}
		k.dilim[d]++
	}
	return k
}

func karsilastir(t *testing.T, ad string, r OlayRapor, k kabaSonuc) {
	t.Helper()
	if r.N != k.n || r.C != k.c {
		t.Fatalf("%s: toplam %d %v, beklenen %d %v", ad, r.N, r.C, k.n, k.c)
	}
	for _, b := range r.Backends {
		if b.C != k.be[b.Ad] {
			t.Fatalf("%s: backend %s %v, beklenen %v", ad, b.Ad, b.C, k.be[b.Ad])
		}
	}
	for _, f := range r.Frontends {
		if f.C != k.fe[f.Ad] {
			t.Fatalf("%s: HAProxy'nin yanıtladığı %s %v, beklenen %v", ad, f.Ad, f.C, k.fe[f.Ad])
		}
	}
	if len(r.Frontends) != len(k.fe) {
		t.Fatalf("%s: %d frontend, beklenen %d", ad, len(r.Frontends), len(k.fe))
	}
	for _, b := range r.Backends {
		if b.Ad == "http_front" {
			t.Fatalf("%s: frontend backend listesinde görünüyor", ad)
		}
	}
	if len(r.Backends) != len(k.be) {
		t.Fatalf("%s: %d backend, beklenen %d", ad, len(r.Backends), len(k.be))
	}
	for _, tm := range r.Terms {
		if tm.N != k.term[tm.Kod] {
			t.Fatalf("%s: sonlandırma %s %d, beklenen %d", ad, tm.Kod, tm.N, k.term[tm.Kod])
		}
	}
	if len(r.Terms) != len(k.term) {
		t.Fatalf("%s: %d sonlandırma kodu, beklenen %d", ad, len(r.Terms), len(k.term))
	}
	if beklenenDilim := int((r.To - r.From + int64(r.Step)*60000 - 1) / (int64(r.Step) * 60000)); len(r.Seri) != beklenenDilim {
		t.Fatalf("%s: %d dilim, beklenen %d", ad, len(r.Seri), beklenenDilim)
	}
	for i, d := range r.Seri {
		// Dilimdeki kod dökümü sınıflarla tutarlı olmalı
		var kc [5]int64
		for kod, n := range d.Codes {
			kc[sinifIdx(kod)] += n
		}
		if kc != d.C {
			t.Fatalf("%s: %d. dilimin kodları %v, sınıflar %v", ad, i, kc, d.C)
		}
		if d.N != k.dilim[int64(i)] {
			t.Fatalf("%s: %d. dilim %d, beklenen %d", ad, i, d.N, k.dilim[int64(i)])
		}
	}
	beklenen := k.olay
	if beklenen > olayOlaySin {
		beklenen = olayOlaySin
	}
	if len(r.Olaylar) != beklenen || r.OlayKesik != (k.olay > olayOlaySin) {
		t.Fatalf("%s: %d olay (kesik %v), beklenen %d", ad, len(r.Olaylar), r.OlayKesik, k.olay)
	}
}

// Rastgele aralıklarla (dosya sınırlarını geçenler, bir dosyanın ortasına düşenler,
// sıkıştırılmış dosyaya düşenler, çok kısa ve çok uzun olanlar) inceleme, bütün satırları
// tek tek sayan kaba kuvvetle aynı sonucu vermeli. İkili arama ve sıkıştırılmış dosyadaki
// grup atlama hiçbir satırı kaçırmamalı ya da iki kez saymamalı.
func TestOlayIncelemesiKabaKuvvetleAyni(t *testing.T) {
	a, ul := olayOrtami(t, 7, 2, 900) // 2 gün, ~2 satır/sn
	r := rand.New(rand.NewSource(11))
	simdi := time.Now()
	ilk := simdi.Add(-48 * time.Hour)
	adet := 60
	if testing.Short() {
		adet = 15
	}
	for i := 0; i < adet; i++ {
		uzun := []time.Duration{2 * time.Minute, 17 * time.Minute, time.Hour, 5 * time.Hour, 13 * time.Hour, 30 * time.Hour}[r.Intn(6)]
		from := ilk.Add(time.Duration(r.Int63n(int64(50 * time.Hour)))).Add(-time.Hour)
		to := from.Add(uzun)
		if to.After(simdi) {
			to = simdi
		}
		if !to.After(from) {
			continue
		}
		rap, err := a.Incident(from, to)
		if err != nil {
			t.Fatal(err)
		}
		if rap.Truncated {
			t.Fatalf("süre sınırına takıldı: %s", rap.Note)
		}
		k := kabaKuvvet(ul, from, to, time.Duration(rap.Step)*time.Minute)
		karsilastir(t, fmt.Sprintf("%d. aralık %s +%s", i, from.Format("02 15:04"), uzun), rap, k)
	}
}

// Büyük bir düz dosyada kısa bir aralık: dosyanın çoğu okunmamalı (ikili arama).
func TestOlayIncelemesiDosyanınCogunuOkumaz(t *testing.T) {
	a, ul := olayOrtami(t, 3, 1, 240) // 1 gün, ~8 satır/sn, ~700 bin satır
	toplam := int64(len(ul.satirlar))
	from := time.Now().Add(-9 * time.Hour)
	to := from.Add(20 * time.Minute)
	rap, err := a.Incident(from, to)
	if err != nil {
		t.Fatal(err)
	}
	karsilastir(t, "kısa aralık", rap, kabaKuvvet(ul, from, to, time.Minute))
	// 20 dk aralık + iki yanda 5'er dk pay ≈ 30 dk; iki .gz dosya baştan okunmak zorunda
	// ama aralık bunların dışında kalıyor (değiştirilme zamanları aralıktan önce).
	if rap.Scanned > toplam/10 {
		t.Fatalf("%d satırın %d tanesi okundu; aralığa atlamak yerine dosya baştan okunuyor", toplam, rap.Scanned)
	}
	t.Logf("%d satırdan %d okundu, %d ms", toplam, rap.Scanned, rap.Took)
}

// Sıkıştırılmış dosyadaki aralık: dosya baştan okunur ama aralığın öncesi ayrıştırılmadan geçilir
func TestOlayIncelemesiSikistirilmisDosya(t *testing.T) {
	a, ul := olayOrtami(t, 5, 2, 400)
	from := time.Now().Add(-40 * time.Hour) // .3.gz dosyasının ortası
	to := from.Add(45 * time.Minute)
	rap, err := a.Incident(from, to)
	if err != nil {
		t.Fatal(err)
	}
	karsilastir(t, "gz aralık", rap, kabaKuvvet(ul, from, to, time.Minute))
	var gz bool
	for _, f := range rap.Files {
		gz = gz || strings.HasSuffix(f, ".gz")
	}
	if !gz {
		t.Fatalf("sıkıştırılmış dosya okunmadı: %v", rap.Files)
	}
}

// Sunucu olayları: zaman, tür, sebep ve kalan sunucu sayısı doğru okunmalı
func TestOlaySatirlari(t *testing.T) {
	simdi := time.Now()
	from := simdi.Add(-time.Hour)
	tp := yeniOlayToplayici(from, simdi, defaultParser, nil)
	zmn := simdi.Add(-30 * time.Minute).Truncate(time.Second)
	satirlar := []string{
		zmn.Format("Jan _2 15:04:05") + " lb haproxy[1]: Server be_api/srv2 is DOWN, reason: Layer7 wrong status, code: 503, info: \"Service Unavailable\", check duration: 3ms. 0 active and 0 backup servers left. 2 sessions active, 0 requeued, 0 remaining in queue.",
		zmn.Format("Jan _2 15:04:05") + " lb haproxy[1]: backend be_api has no server available!",
		zmn.Add(time.Minute).Format(time.RFC3339Nano) + " lb haproxy[1]: Server be_api/srv2 is UP, reason: Layer4 check passed, check duration: 0ms. 1 active and 0 backup servers online.",
		zmn.Add(2*time.Minute).Format("2006-01-02T15:04:05-0700") + " lb haproxy[9]: Proxy http_front started.",
		zmn.Add(2*time.Minute).Format("2006-01-02T15:04:05-0700") + " lb haproxy[9]: Proxy be_api started.",
		zmn.Add(3*time.Minute).Format("Jan _2 15:04:05") + " lb haproxy[1]: Server be_web/srv1 is going DOWN for maintenance. 2 active and 0 backup servers left.",
		simdi.Add(-3*time.Hour).Format("Jan _2 15:04:05") + " lb haproxy[1]: Server be_web/srv1 is DOWN, reason: Layer4 timeout. 0 active and 0 backup servers left.",
	}
	for _, s := range satirlar {
		tp.satirIsle(s)
	}
	tp.bitir()
	o := tp.r.Olaylar
	if len(o) != 5 {
		t.Fatalf("%d olay, beklenen 5 (yeniden yükleme tek olay, aralık dışındaki sayılmaz): %+v", len(o), o)
	}
	if o[0].Tur != "down" || o[0].Backend != "be_api" || o[0].Sunucu != "srv2" || o[0].Sebep != "Layer7 wrong status, code: 503" || o[0].Kalan != 0 || o[0].At != zmn.UnixMilli() {
		t.Fatalf("düşme olayı yanlış: %+v", o[0])
	}
	if o[1].Tur != "noserver" || o[2].Tur != "up" || o[2].Kalan != 1 || o[3].Tur != "start" || o[4].Tur != "maint" {
		t.Fatalf("olaylar yanlış: %+v", o)
	}
}

// Yüzdelik hesabı: kovalar yaklaşık değer verir ama gerçek değerden çok uzak olmamalı
func TestSureYuzdelik(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	var d sureDagilim
	var hepsi []int
	for i := 0; i < 20000; i++ {
		v := int(r.ExpFloat64() * 120)
		if r.Intn(20) == 0 {
			v = 2000 + r.Intn(8000)
		}
		d.ekle(v)
		hepsi = append(hepsi, v)
	}
	sort.Ints(hepsi)
	for _, p := range []float64{0.5, 0.95, 0.99} {
		gercek := hepsi[int(p*float64(len(hepsi)))]
		yaklasik := d.yuzdelik(p)
		if fark := float64(yaklasik-gercek) / float64(gercek); fark > 0.35 || fark < -0.35 {
			t.Fatalf("p%.0f: yaklaşık %d, gerçek %d", p*100, yaklasik, gercek)
		}
	}
	var bos sureDagilim
	if bos.yuzdelik(0.95) != -1 || bos.ort() != -1 {
		t.Fatal("boş dağılım -1 vermeli")
	}
}

// Aralık parametreleri doğrulanmalı
func TestOlayAraligi(t *testing.T) {
	simdi := time.Now()
	get := func(f, g int64) func(string) string {
		return func(k string) string {
			switch k {
			case "from":
				return fmt.Sprint(f)
			case "to":
				return fmt.Sprint(g)
			}
			return ""
		}
	}
	ms := func(d time.Duration) int64 { return simdi.Add(d).UnixMilli() }
	if _, _, err := incidentRange(get(ms(-time.Hour), ms(-2*time.Hour)), simdi); err == nil {
		t.Fatal("bitiş başlangıçtan önce: hata vermeli")
	}
	if _, _, err := incidentRange(get(ms(-9*24*time.Hour), ms(-time.Hour)), simdi); err == nil {
		t.Fatal("7 günden uzun: hata vermeli")
	}
	if _, _, err := incidentRange(func(string) string { return "" }, simdi); err == nil {
		t.Fatal("boş: hata vermeli")
	}
	_, to, err := incidentRange(get(ms(-time.Hour), ms(time.Hour)), simdi)
	if err != nil || to.After(simdi) {
		t.Fatalf("gelecekteki bitiş şimdiye çekilmeli: %v %v", to, err)
	}
}

// Log'da arama özel aralıkla: sonuçlar kaba kuvvetle aynı, bitişten sonraki satırlar okunmaz
func TestAramaOzelAralik(t *testing.T) {
	a, ul := olayOrtami(t, 9, 1, 150)
	r := rand.New(rand.NewSource(4))
	adet := 25
	if testing.Short() {
		adet = 8
	}
	for i := 0; i < adet; i++ {
		from := time.Now().Add(-time.Duration(1+r.Intn(23)) * time.Hour).Add(-time.Duration(r.Intn(60)) * time.Minute)
		to := from.Add(time.Duration(5+r.Intn(90)) * time.Minute)
		durum := []string{"", "5xx", "404", "503"}[r.Intn(4)]
		q, err := searchQueryFrom(func(k string) string {
			switch k {
			case "from":
				return fmt.Sprint(from.UnixMilli())
			case "to":
				return fmt.Sprint(to.UnixMilli())
			case "status":
				return durum
			}
			return ""
		})
		if err != nil {
			t.Fatal(err)
		}
		res, err := a.Search(q)
		if err != nil {
			t.Fatal(err)
		}
		var beklenen int64
		for _, rec := range ul.kayit {
			if q.uyuyor(rec) {
				beklenen++
			}
		}
		if res.Matches != beklenen {
			t.Fatalf("%d. arama (%s, %s +%s): %d eşleşme, beklenen %d", i, durum, from.Format("15:04"), to.Sub(from), res.Matches, beklenen)
		}
		// Sıkıştırılmış dosyalar baştan okunmak zorunda; ölçü yalnızca düz dosyalardaki aralıklar için
		if from.After(time.Now().Add(-11*time.Hour)) && res.Scanned > int64(len(ul.satirlar))/3 {
			t.Fatalf("%d. arama: %d satırın %d tanesi okundu; bitiş noktasına atlanmıyor", i, len(ul.satirlar), res.Scanned)
		}
	}
	// Alan boş ve aralık yoksa hâlâ hata vermeli
	if _, err := searchQueryFrom(func(string) string { return "" }); err == nil {
		t.Fatal("boş arama hata vermeli")
	}
}

// Arka plandaki inceleme: aynı aralık tekrar istenince aynı iş döner, yeni aralık eskisini
// durdurur; durdurulan incelemede "nereye kadar okundu" bilgisi doğrudur ve o noktaya kadarki
// dakikalar eksiksizdir (okunmayan kısım "istek yok" sanılmamalı).
func TestOlayIncelemesiArkaPlanVeKapsam(t *testing.T) {
	a, ul := olayOrtami(t, 21, 1, 120)
	from := time.Now().Add(-23 * time.Hour).Truncate(time.Minute)
	to := time.Now().Add(-1 * time.Hour).Truncate(time.Minute)
	d1 := a.IncidentStart(from, to)
	d2 := a.IncidentStart(from, to)
	if d1.Job != d2.Job {
		t.Fatalf("aynı aralık için ikinci iş açıldı: %s, %s", d1.Job, d2.Job)
	}
	// Biraz okusun, sonra durdur
	for i := 0; i < 200; i++ {
		d, _ := IncidentStatus(d1.Job, false)
		if d.Okunan > from.Add(time.Hour).UnixMilli() || d.Durum == "bitti" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	IncidentStatus(d1.Job, true)
	var d OlayDurum
	for i := 0; i < 500; i++ {
		d, _ = IncidentStatus(d1.Job, false)
		if d.Durum == "bitti" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if d.Durum != "bitti" || d.Rapor == nil {
		t.Fatalf("durdurulan inceleme bitmedi: %+v", d)
	}
	r := d.Rapor
	if r.KapsamSon >= r.To {
		t.Logf("inceleme durdurulmadan bitti (makine hızlı); kapsam bütün aralık")
	} else {
		if !r.Durduruldu || !r.Truncated || r.KapsamSon <= r.From {
			t.Fatalf("durdurma bilgisi yanlış: durduruldu=%v kesik=%v kapsam=%d", r.Durduruldu, r.Truncated, r.KapsamSon)
		}
	}
	// Kapsamın pay kadar öncesine kadarki her dilim kaba kuvvetle aynı olmalı
	k := kabaKuvvet(ul, from, to, time.Duration(r.Step)*time.Minute)
	sinir := time.UnixMilli(r.KapsamSon).Add(-olayPay)
	for i, dl := range r.Seri {
		if time.UnixMilli(dl.T).Add(time.Duration(r.Step) * time.Minute).After(sinir) {
			break
		}
		if dl.N != k.dilim[int64(i)] {
			t.Fatalf("okunan kısımdaki %d. dilim %d, beklenen %d", i, dl.N, k.dilim[int64(i)])
		}
	}
	// Başka bir aralık istenince yeni iş açılır, eskisi bulunamaz
	d3 := a.IncidentStart(from.Add(time.Minute), to)
	if d3.Job == d1.Job {
		t.Fatal("yeni aralık için yeni iş açılmadı")
	}
	if _, ok := IncidentStatus(d1.Job, false); ok {
		t.Fatal("eski iş hâlâ bulunuyor")
	}
	for i := 0; i < 3000; i++ {
		if d, _ := IncidentStatus(d3.Job, false); d.Durum == "bitti" {
			if d.Rapor.KapsamSon != d.Rapor.To || d.Rapor.Truncated {
				t.Fatalf("tamamlanan incelemede kapsam bütün aralık olmalı: %+v", d.Rapor.KapsamSon)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("ikinci inceleme bitmedi")
}
