package main

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"
)

// Hızlı çözücü, düzenli ifadeyle birebir aynı sonucu vermeli: aynı satır eşleşmeli ya da
// eşleşmemeli, eşleşiyorsa her alan aynı çıkmalı. Her biçim için o biçime uyan rastgele
// satırlar üretilir, sonra bunlar rastgele bozulur (karakter eklenir, silinir, değiştirilir;
// alanlara iki nokta, eğik çizgi, tırnak, süslü parantez, boşluk girer) ve iki yöntem
// karşılaştırılır. Hızlı çözücü emin olamadığında "bilmiyorum" diyebilir; o zaman düzenli
// ifade kullanılır, bu da doğrudur. Yanlış bir sonuç ise asla kabul edilmez.
var hizliTestBicimleri = []string{
	fmtHTTPLog,
	fmtHTTPSLog,
	fmtTCPLog,
	`%ci:%cp [%tr] %ft %b/%s %Ta %ST %{+Q}r %[req.hdr(host)]`,
	`%ci %ST %HM %HP %b/%s %[req.hdr(host)]`,
	`{"ip":"%ci","st":%ST,"u":"%HU","b":"%b","s":"%s","t":%Ta}`,
	`%ci:%cp [%trg] %ft %b/%s %TR/%Tw/%Tc/%Tr/%Ta %ST %B %tsc %{+Q}HM %{+Q}HU %{+Q}HV`,
	`%Ts %{+X}Ts %ci %b %ST %hr %HP`,
	`%ci:%cp %hr %hs [%tr] %ft %b/%s %ST %{+Q}r`,
	`%ci %o %zz %ST %b/%s %{+Q}r %[capture.req.hdr(0)] %[ssl_fc_sni]`,
	`%ci:%cp [%t] %ft %b/%s %Tw/%Tc/%Tt %B %ts %ac/%fc/%bc/%sc/%rc %sq/%bq %hr`,
	`%HM %HU %HQ %ci`,
}

// Satırlarda sık geçen ve sınırları zorlayan karakterler
const hizliZorKarakter = ` :/"{}|-+[]0123456789abcXYZ.?&=` + "\t"

func hizliRastgeleDeger(r *rand.Rand, o hizliOge) string {
	kel := func(n int, alfabe string) string {
		var b strings.Builder
		for i := 0; i < n; i++ {
			b.WriteByte(alfabe[r.Intn(len(alfabe))])
		}
		return b.String()
	}
	bosluksuz := "abcdefXYZ0123456789.:/-_~?&=%[]+"
	switch o.tur {
	case hBosluksuz:
		return kel(1+r.Intn(12), bosluksuz)
	case hBosluksuz0:
		return kel(r.Intn(8), bosluksuz)
	case hRakam:
		return kel(1+r.Intn(6), "0123456789")
	case hIsaretRakam:
		return []string{"", "+", "-", "+-"}[r.Intn(4)] + kel(1+r.Intn(5), "0123456789")
	case hEksiRakam:
		return []string{"", "-"}[r.Intn(2)] + kel(1+r.Intn(5), "0123456789")
	case hArtiRakam:
		return []string{"", "+"}[r.Intn(2)] + kel(1+r.Intn(5), "0123456789")
	case hSabitBosluksuz:
		return kel(o.n, "-CDHRSPLcs")
	case hSabitRakam:
		return kel(o.n, "0123456789")
	case hTarihMs:
		return fmt.Sprintf("%02d/%s/20%02d:%02d:%02d:%02d.%03d", r.Intn(32), []string{"Oct", "Feb", "oct", "Xyz"}[r.Intn(4)], r.Intn(100), r.Intn(25), r.Intn(61), r.Intn(61), r.Intn(1000))
	case hTarihTZ:
		return fmt.Sprintf("%02d/Oct/2026:%02d:%02d:%02d %s%04d", r.Intn(32), r.Intn(24), r.Intn(60), r.Intn(60), []string{"+", "-"}[r.Intn(2)], r.Intn(1500))
	case hSuslu:
		return "{" + kel(r.Intn(20), "abc.|/ :-XY0") + "}"
	case hIstek:
		s := kel(1+r.Intn(6), "GETPOSabc") + " " + kel(1+r.Intn(14), bosluksuz)
		if r.Intn(3) > 0 {
			s += " " + kel(1+r.Intn(8), "HTP/1.0")
		}
		return s
	case hHex:
		return kel(1+r.Intn(10), "0123456789abcdefABCDEF")
	case hTirnakli:
		if r.Intn(5) == 0 {
			return "-"
		}
		return `"` + kel(r.Intn(20), "GET /a?b=c HTTP/1.1:{}|") + `"`
	}
	return ""
}

func hizliRastgeleSatir(r *rand.Rand, h *hizliEslestirici) string {
	var b strings.Builder
	atlaBosluk := false
	for _, o := range h.ogeler {
		if o.tur == hLit {
			l := o.lit
			if atlaBosluk && strings.HasPrefix(l, " ") {
				l = l[1:]
			}
			atlaBosluk = false
			b.WriteString(l)
			continue
		}
		atlaBosluk = false
		if o.ops && r.Intn(3) == 0 {
			continue // isteğe bağlı alan yazılmadı (ardındaki boşluk da)
		}
		b.WriteString(hizliRastgeleDeger(r, o))
		if o.ops && o.opsBosluk {
			b.WriteByte(' ')
		}
	}
	switch r.Intn(4) {
	case 0:
		b.WriteString(" www.example.com")
	case 1:
		b.WriteString("  ")
	}
	return b.String()
}

func hizliBoz(r *rand.Rand, s string) string {
	b := []byte(s)
	for k := r.Intn(4); k > 0 && len(b) > 0; k-- {
		i := r.Intn(len(b))
		c := hizliZorKarakter[r.Intn(len(hizliZorKarakter))]
		switch r.Intn(3) {
		case 0:
			b[i] = c
		case 1:
			b = append(b[:i], b[i+1:]...)
		default:
			b = append(b[:i], append([]byte{c}, b[i:]...)...)
		}
	}
	return string(b)
}

func TestHizliCozucuDuzenliIfadeyleAyni(t *testing.T) {
	r := rand.New(rand.NewSource(20261008))
	adet := 40000
	if testing.Short() {
		adet = 8000
	}
	var toplam, eslesen, emin, bozuk int
	for _, bicim := range hizliTestBicimleri {
		cf := compileFormat("test", bicim)
		if cf == nil {
			t.Fatalf("biçim derlenemedi: %s", bicim)
		}
		if cf.hizli == nil {
			t.Fatalf("hızlı yol kurulmadı: %s", bicim)
		}
		for i := 0; i < adet; i++ {
			satir := hizliRastgeleSatir(r, cf.hizli)
			if i%2 == 1 {
				satir = hizliBoz(r, satir)
				bozuk++
			}
			if !hizliUygun(satir) {
				continue
			}
			toplam++
			beklenen := cf.re.FindStringSubmatch(satir)
			m, kesin := cf.hizli.eslestir(satir)
			if !kesin {
				continue
			}
			emin++
			if (m == nil) != (beklenen == nil) {
				t.Fatalf("biçim %q\nsatır %q\ndüzenli ifade eşleşti mi: %v, hızlı: %v", bicim, satir, beklenen != nil, m != nil)
			}
			if m == nil {
				continue
			}
			eslesen++
			for g := range beklenen {
				if m[g] != beklenen[g] {
					t.Fatalf("biçim %q\nsatır %q\n%d. alan: düzenli ifade %q, hızlı %q", bicim, satir, g, beklenen[g], m[g])
				}
			}
		}
	}
	t.Logf("%d satır (%d bozulmuş): %d tanesinde hızlı çözücü kesin sonuç verdi, %d eşleşme; hepsi düzenli ifadeyle aynı", toplam, bozuk, emin, eslesen)
	if emin < toplam*9/10 {
		t.Fatalf("hızlı çözücü satırların yalnızca %d/%d tanesinde kesin sonuç verdi", emin, toplam)
	}
}

// Ayrıştırılmış kayıt da aynı olmalı (hızlı yol açık ve kapalıyken)
func TestHizliCozucuKayitAyni(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for _, bicim := range hizliTestBicimleri {
		hizli := compileFormat("test", bicim)
		yavas := compileFormat("test", bicim)
		yavas.hizli = nil
		for i := 0; i < 3000; i++ {
			satir := hizliRastgeleSatir(r, hizli.hizli)
			if i%3 == 0 {
				satir = hizliBoz(r, satir)
			}
			a, okA := hizli.parse(satir, nil)
			b, okB := yavas.parse(satir, nil)
			// Tarih okunamayan satırda (tarih alanı yok ya da geçersiz) zaman "şimdi" olur; iki
			// çağrı arasında birkaç mikrosaniye geçtiği için bu durumda karşılaştırılmaz
			if a.At.Sub(b.At).Abs() < time.Second && time.Since(a.At) < time.Minute {
				a.At, b.At = time.Time{}, time.Time{}
			}
			// Zamanlar değerce karşılaştırılır (saat dilimi nesneleri her çözümde ayrı oluşur)
			if a.At.Equal(b.At) {
				a.At, b.At = time.Time{}, time.Time{}
			}
			if okA != okB || a != b {
				t.Fatalf("biçim %q\nsatır %q\nhızlı %+v (%v)\nyavaş %+v (%v)", bicim, satir, a, okA, b, okB)
			}
		}
	}
}

// Hızlı tarih çözme, time.ParseInLocation ile aynı sonucu vermeli
func TestHizliTarih(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	aylar := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec", "oct", "OCT", "Xyz"}
	var hizli int
	for i := 0; i < 200000; i++ {
		v := fmt.Sprintf("%02d/%s/%04d:%02d:%02d:%02d.%03d", r.Intn(33), aylar[r.Intn(len(aylar))], 1990+r.Intn(60), r.Intn(25), r.Intn(61), r.Intn(61), r.Intn(1000))
		a, okA := hizliTarihMs(v)
		if !okA {
			continue
		}
		hizli++
		b, err := time.ParseInLocation("02/Jan/2006:15:04:05.000", v, time.Local)
		if err != nil || !a.Equal(b) {
			t.Fatalf("%q: hızlı %v, ParseInLocation %v (%v)", v, a, b, err)
		}
	}
	if hizli < 50000 {
		t.Fatalf("hızlı yol çok az kullanıldı: %d", hizli)
	}
}

// Biçime hiç uymayan uzun satırlarda (başka bir programın satırı) deneme sayısı patlamamalı
func TestHizliCozucuUyumsuzSatirdaYavaslamaz(t *testing.T) {
	cf := compileFormat("httplog", fmtHTTPLog)
	satir := strings.Repeat("a:b/c d:e/f ", 400)
	bas := time.Now()
	for i := 0; i < 200; i++ {
		cf.parse(satir, nil)
	}
	if gecen := time.Since(bas); gecen > 2*time.Second {
		t.Fatalf("uyumsuz satır çok yavaş: 200 satır %v", gecen)
	}
}
