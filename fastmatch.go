package main

import (
	"strings"
	"time"
)

// Log satırlarını düzenli ifade (regexp) kullanmadan çözen hızlı yol.
//
// Bir log biçimi ("%ci:%cp [%tr] %ft %b/%s ...") birkaç alan türünden oluşur: boşluksuz metin,
// sayı, tırnaklı metin, süslü parantezli yakalama, tarih. compileFormat bunlardan bir düzenli
// ifade kurar; Go'nun düzenli ifade motoru her satırda geri izleme (backtracking) yaptığı için
// satır başına ~5 mikrosaniye harcıyordu. Ajanın %10 işlemci tavanıyla bu, olay incelemesinde
// 24 saatlik bir log'u 1 dakikada okumaya yetmiyordu.
//
// Burada aynı biçim, alan türlerini doğrudan tanıyan bir eşleştiriciye çevrilir. Eşleştirici
// düzenli ifadeyle AYNI tercih sırasını izler (açgözlü alanlar önce en uzun hali dener, sonra
// kısaltır; isteğe bağlı parçalar önce "var" sonra "yok" diye denenir). Böylece aynı satırdan
// aynı alanları çıkarır; bu, rastgele üretilen ve bozulan yüz binlerce satırla düzenli ifadeye
// karşı testle doğrulanıyor (fastmatch_test.go).
//
// Güvenli çıkışlar: tanınmayan bir alan türü varsa biçim için hızlı yol kurulmaz; satırda ASCII
// dışı bir bayt ya da satır sonu varsa (düzenli ifade bunları bayt bayt değil harf harf işler)
// ya da bir satırda deneme sayısı sınırı aşılırsa o satır düzenli ifadeyle çözülür.

type hizliTur uint8

const (
	hLit            hizliTur = iota
	hBosluksuz               // \S+
	hBosluksuz0              // \S*
	hRakam                   // \d+
	hIsaretRakam             // \+?-?\d+
	hEksiRakam               // -?\d+
	hArtiRakam               // \+?\d+
	hSabitBosluksuz          // \S{n}
	hSabitRakam              // \d{n}
	hTarihMs                 // 02/Jan/2006:15:04:05.000
	hTarihTZ                 // 02/Jan/2006:15:04:05 -0700
	hSuslu                   // \{[^}]*\}
	hIstek                   // \S+ \S+(?: \S+)?
	hHex                     // [0-9A-Fa-f]+
	hTirnakli                // "[^"]*"|-
)

var hizliDesen = map[string]hizliTur{
	`\S+`: hBosluksuz, `\S*`: hBosluksuz0, `\d+`: hRakam, `\+?-?\d+`: hIsaretRakam,
	`-?\d+`: hEksiRakam, `\+?\d+`: hArtiRakam, `\S{4}`: hSabitBosluksuz, `\S{2}`: hSabitBosluksuz,
	`\d{3}`: hSabitRakam,
	`\d{2}/\w{3}/\d{4}:\d{2}:\d{2}:\d{2}\.\d{3}`:    hTarihMs,
	`\d{2}/\w{3}/\d{4}:\d{2}:\d{2}:\d{2} [+-]\d{4}`: hTarihTZ,
	`\{[^}]*\}`: hSuslu, `\S+ \S+(?: \S+)?`: hIstek, `[0-9A-Fa-f]+`: hHex, `"[^"]*"|-`: hTirnakli,
}

type hizliOge struct {
	tur       hizliTur
	lit       string
	n         int  // sabit uzunluk (\S{n}, \d{n})
	grup      int  // yakalanıyorsa grup numarası (1'den), değilse 0
	ops       bool // isteğe bağlı: (?:(…) )? ya da (?:(…))?
	opsBosluk bool // isteğe bağlıysa ardından tek boşluk gelir ve o da isteğe dahildir
	// Budama: bu öğeden hemen sonra ne gelmeli. Ardından sabit bir metin geliyorsa onun ilk
	// karakteri; biçimin sonuysa boşluk ya da satır sonu. Aday bitişlerin çoğu böylece hiç
	// denenmeden elenir; biçime uymayan satırlarda deneme sayısı patlamaz.
	ardKar byte
	ardSon bool
}

type hizliEslestirici struct {
	ogeler  []hizliOge
	gruplar int // sondaki alan grubu dahil
}

// Bir satır için en fazla bu kadar adım; aşılırsa satır düzenli ifadeyle çözülür
const hizliAdimSiniri = 4000

// Budama bilgisini doldurur (compileFormat öğeleri ekledikten sonra çağırır)
func (h *hizliEslestirici) hazirla() {
	for i := range h.ogeler {
		o := &h.ogeler[i]
		if o.ops && o.opsBosluk {
			o.ardKar = ' '
			continue
		}
		if i+1 == len(h.ogeler) {
			o.ardSon = true
		} else if s := &h.ogeler[i+1]; s.tur == hLit && !s.ops {
			o.ardKar = s.lit[0]
		}
	}
}

// Aday bitiş, ardından gelmesi gerekenle uyuşuyor mu (ucuz ön kontrol)
func (o *hizliOge) uyar(s string, son int) bool {
	if o.ardKar != 0 {
		return son < len(s) && s[son] == o.ardKar
	}
	if o.ardSon {
		return son == len(s) || bosluk(s[son])
	}
	return true
}

// Go'nun düzenli ifadesindeki \s: [\t\n\f\r ]
func bosluk(c byte) bool  { return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r' }
func rakamMi(c byte) bool { return c >= '0' && c <= '9' }
func kelimeMi(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
}
func hexMi(c byte) bool { return rakamMi(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }

// Hızlı yolun güvenle kullanılabileceği satır: yalnızca ASCII, satır sonu yok
func hizliUygun(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 || s[i] == '\n' {
			return false
		}
	}
	return true
}

type hizliCalisma struct {
	s    string
	m    []string
	adim int
}

// Satırı çözer. Dönen dilim FindStringSubmatch ile aynı biçimde: [0] bütün satır, sonra gruplar
// (katılmayan grup boş metin); eşleşme yoksa nil. İkinci değer sonucun kesin olup olmadığı:
// deneme sınırı aşıldıysa false döner ve çağıran düzenli ifadeye başvurur.
func (h *hizliEslestirici) eslestir(s string) ([]string, bool) {
	// Önce düz geçiş: her öğede düzenli ifadenin İLK tercih edeceği bitiş alınır. Satırların
	// neredeyse hepsi böyle tek geçişte çözülür. Düz geçiş sonuna kadar giderse bu, geri izlemenin
	// denediği ilk yoldur ve başarılıdır; yani düzenli ifadenin sonucuyla aynıdır. Bir yerde
	// tıkanırsa (başka bir tercih gerekebilir) ayrıntılı aramaya geçilir.
	if m := h.dogrusal(s); m != nil {
		return m, true
	}
	c := hizliCalisma{s: s, m: make([]string, h.gruplar+1)}
	ok := h.git(&c, 0, 0)
	if c.adim > hizliAdimSiniri {
		return nil, false
	}
	if !ok {
		return nil, true
	}
	c.m[0] = s
	return c.m, true
}

func (h *hizliEslestirici) git(c *hizliCalisma, i, pos int) bool {
	if c.adim++; c.adim > hizliAdimSiniri {
		return false
	}
	if i == len(h.ogeler) {
		return h.son(c, pos)
	}
	o := &h.ogeler[i]
	if o.ops {
		// Açgözlü "?": önce parçayı alarak dene, olmazsa atla
		if h.dene(c, i, pos) {
			return true
		}
		if c.adim > hizliAdimSiniri {
			return false
		}
		if o.grup > 0 {
			c.m[o.grup] = ""
		}
		return h.git(c, i+1, pos)
	}
	return h.dene(c, i, pos)
}

// Aday bitişle devam: yakalanan değeri yazar, sonraki öğeye geçer
func (h *hizliEslestirici) devam(c *hizliCalisma, i, pos, son int) bool {
	o := &h.ogeler[i]
	if !o.uyar(c.s, son) {
		return false
	}
	sonraki := son
	if o.ops && o.opsBosluk {
		sonraki = son + 1
	}
	if o.grup > 0 {
		c.m[o.grup] = c.s[pos:son]
	}
	return h.git(c, i+1, sonraki)
}

// Öğeyi pos'tan başlayarak, düzenli ifadenin tercih sırasıyla aday bitişlerle dener
func (h *hizliEslestirici) dene(c *hizliCalisma, i, pos int) bool {
	o := &h.ogeler[i]
	s := c.s
	n := len(s)
	switch o.tur {
	case hLit:
		if n-pos >= len(o.lit) && s[pos:pos+len(o.lit)] == o.lit {
			return h.devam(c, i, pos, pos+len(o.lit))
		}
		return false
	case hBosluksuz, hBosluksuz0:
		e := pos
		for e < n && !bosluk(s[e]) {
			e++
		}
		alt := pos + 1
		if o.tur == hBosluksuz0 {
			alt = pos
		}
		return h.adaylar(c, i, pos, alt, e, func(b byte) bool { return !bosluk(b) })
	case hRakam:
		return h.rakamlar(c, i, pos, pos)
	case hHex:
		e := pos
		for e < n && hexMi(s[e]) {
			e++
		}
		return h.adaylar(c, i, pos, pos+1, e, hexMi)
	case hIsaretRakam, hEksiRakam, hArtiRakam:
		// \+? ve -? açgözlü: işaret varsa önce onunla dene
		artiOlabilir := o.tur != hEksiRakam
		eksiOlabilir := o.tur != hArtiRakam
		for _, arti := range [2]bool{true, false} {
			p := pos
			if arti {
				if !artiOlabilir || p >= n || s[p] != '+' {
					continue
				}
				p++
			}
			for _, eksi := range [2]bool{true, false} {
				q := p
				if eksi {
					if !eksiOlabilir || q >= n || s[q] != '-' {
						continue
					}
					q++
				}
				if h.rakamlar(c, i, pos, q) {
					return true
				}
			}
		}
		return false
	case hSabitBosluksuz:
		if pos+o.n > n {
			return false
		}
		for k := pos; k < pos+o.n; k++ {
			if bosluk(s[k]) {
				return false
			}
		}
		return h.devam(c, i, pos, pos+o.n)
	case hSabitRakam:
		if pos+o.n > n {
			return false
		}
		for k := pos; k < pos+o.n; k++ {
			if !rakamMi(s[k]) {
				return false
			}
		}
		return h.devam(c, i, pos, pos+o.n)
	case hTarihMs, hTarihTZ:
		// 02/Jan/2006:15:04:05.000 ya da 02/Jan/2006:15:04:05 -0700
		kalip := "dd/www/dddd:dd:dd:dd.ddd"
		if o.tur == hTarihTZ {
			kalip = "dd/www/dddd:dd:dd:dd sdddd"
		}
		if pos+len(kalip) > n {
			return false
		}
		for k := 0; k < len(kalip); k++ {
			ch := s[pos+k]
			switch kalip[k] {
			case 'd':
				if !rakamMi(ch) {
					return false
				}
			case 'w':
				if !kelimeMi(ch) {
					return false
				}
			case 's':
				if ch != '+' && ch != '-' {
					return false
				}
			default:
				if ch != kalip[k] {
					return false
				}
			}
		}
		return h.devam(c, i, pos, pos+len(kalip))
	case hSuslu:
		if pos >= n || s[pos] != '{' {
			return false
		}
		for k := pos + 1; k < n; k++ {
			if s[k] == '}' {
				return h.devam(c, i, pos, k+1)
			}
		}
		return false
	case hTirnakli:
		// "[^"]*" önce, olmazsa "-"
		if pos < n && s[pos] == '"' {
			for k := pos + 1; k < n; k++ {
				if s[k] == '"' {
					if h.devam(c, i, pos, k+1) {
						return true
					}
					break
				}
			}
		}
		if pos < n && s[pos] == '-' {
			return h.devam(c, i, pos, pos+1)
		}
		return false
	case hIstek:
		// \S+ \S+(?: \S+)? : birinci parça boşluksuz ve ardında tek boşluk olmalı
		e1 := pos
		for e1 < n && !bosluk(s[e1]) {
			e1++
		}
		if e1 == pos || e1 >= n || s[e1] != ' ' {
			return false
		}
		p2 := e1 + 1
		e2 := p2
		for e2 < n && !bosluk(s[e2]) {
			e2++
		}
		for b2 := e2; b2 >= p2+1; b2-- {
			// Üçüncü parça (açgözlü ?): önce varsa onunla
			if b2 < n && s[b2] == ' ' {
				p3 := b2 + 1
				e3 := p3
				for e3 < n && !bosluk(s[e3]) {
					e3++
				}
				for b3 := e3; b3 >= p3+1; b3-- {
					if h.devam(c, i, pos, b3) {
						return true
					}
				}
			}
			if h.devam(c, i, pos, b2) {
				return true
			}
		}
		return false
	}
	return false
}

// \d+ : basla'dan itibaren rakamlar, en uzundan kısaya; yakalanan değer pos'tan başlar
// (önündeki işaret dahil)
func (h *hizliEslestirici) rakamlar(c *hizliCalisma, i, pos, basla int) bool {
	e := basla
	for e < len(c.s) && rakamMi(c.s[e]) {
		e++
	}
	return h.adaylar(c, i, pos, basla+1, e, rakamMi)
}

// [alt, e] aralığındaki aday bitişleri en uzundan kısaya dener. Aralıktaki her konumdaki
// karakter sinif'a uyar (e'deki uymaz). Ardından gelmesi gereken karakter biliniyorsa yalnızca
// onun bulunduğu konumlar aday olabilir; çoğu zaman tek aday kalır (en uzunu).
func (h *hizliEslestirici) adaylar(c *hizliCalisma, i, pos, alt, e int, sinif func(byte) bool) bool {
	if e < alt {
		return false
	}
	o := &h.ogeler[i]
	if o.ardSon || o.ardKar != 0 && !sinif(o.ardKar) {
		// Ardından boşluk/satır sonu ya da sınıf dışı bir karakter gelmeli: e'den kısası olamaz
		return h.devam(c, i, pos, e)
	}
	if o.ardKar != 0 {
		for son := e; son >= alt; son-- {
			if (son == len(c.s) || c.s[son] != o.ardKar) && son != e {
				continue
			}
			if h.devam(c, i, pos, son) {
				return true
			}
		}
		return false
	}
	for son := e; son >= alt; son-- {
		if h.devam(c, i, pos, son) {
			return true
		}
	}
	return false
}

// Biçimin sonu: (?:\s+(.*?))?\s*$ — satırın sonuna eklenmiş alanlar (alan adı için taranır)
func (h *hizliEslestirici) son(c *hizliCalisma, pos int) bool {
	s := c.s
	if pos == len(s) {
		c.m[h.gruplar] = ""
		return true
	}
	if !bosluk(s[pos]) {
		return false
	}
	bas := pos
	for bas < len(s) && bosluk(s[bas]) {
		bas++
	}
	bit := len(s)
	for bit > bas && bosluk(s[bit-1]) {
		bit--
	}
	c.m[h.gruplar] = s[bas:bit]
	return true
}

// Düz geçiş. Her öğe için tercih sırasındaki ilk uygun bitiş (ardından gelmesi gerekenle uyuşan)
// seçilir; isteğe bağlı öğe önce "var" diye denenir. Tıkanırsa nil.
func (h *hizliEslestirici) dogrusal(s string) []string {
	m := make([]string, h.gruplar+1)
	pos := 0
	for i := range h.ogeler {
		o := &h.ogeler[i]
		son, ok := h.ilkAday(s, i, pos)
		if !ok {
			if !o.ops {
				return nil
			}
			if o.grup > 0 {
				m[o.grup] = ""
			}
			continue
		}
		if o.grup > 0 {
			m[o.grup] = s[pos:son]
		}
		pos = son
		if o.ops && o.opsBosluk {
			pos++
		}
	}
	c := hizliCalisma{s: s, m: m}
	if !h.son(&c, pos) {
		return nil
	}
	m[0] = s
	return m
}

// Öğenin pos'tan başlayan, tercih sırasındaki ilk uygun bitişi
func (h *hizliEslestirici) ilkAday(s string, i, pos int) (int, bool) {
	o := &h.ogeler[i]
	n := len(s)
	enUzun := func(alt, e int, sinif func(byte) bool) (int, bool) {
		if e < alt {
			return 0, false
		}
		if o.ardSon || o.ardKar != 0 && !sinif(o.ardKar) {
			return e, o.uyar(s, e)
		}
		for son := e; son >= alt; son-- {
			if o.uyar(s, son) {
				return son, true
			}
		}
		return 0, false
	}
	tek := func(son int) (int, bool) { return son, o.uyar(s, son) }
	switch o.tur {
	case hLit:
		if n-pos >= len(o.lit) && s[pos:pos+len(o.lit)] == o.lit {
			return tek(pos + len(o.lit))
		}
	case hBosluksuz, hBosluksuz0:
		e := pos
		for e < n && !bosluk(s[e]) {
			e++
		}
		alt := pos + 1
		if o.tur == hBosluksuz0 {
			alt = pos
		}
		return enUzun(alt, e, func(b byte) bool { return !bosluk(b) })
	case hRakam, hIsaretRakam, hEksiRakam, hArtiRakam:
		q := pos
		if o.tur != hRakam && o.tur != hEksiRakam && q < n && s[q] == '+' {
			q++
		}
		if o.tur != hRakam && o.tur != hArtiRakam && q < n && s[q] == '-' {
			q++
		}
		e := q
		for e < n && rakamMi(s[e]) {
			e++
		}
		// İşaret alındıysa ve ardından rakam yoksa, işaretsiz hali de denenmeliydi: ayrıntılı aramaya bırak
		if son, ok := enUzun(q+1, e, rakamMi); ok {
			return son, true
		}
		return 0, false
	case hHex:
		e := pos
		for e < n && hexMi(s[e]) {
			e++
		}
		return enUzun(pos+1, e, hexMi)
	case hSabitBosluksuz:
		if pos+o.n > n {
			return 0, false
		}
		for k := pos; k < pos+o.n; k++ {
			if bosluk(s[k]) {
				return 0, false
			}
		}
		return tek(pos + o.n)
	case hSabitRakam:
		if pos+o.n > n {
			return 0, false
		}
		for k := pos; k < pos+o.n; k++ {
			if !rakamMi(s[k]) {
				return 0, false
			}
		}
		return tek(pos + o.n)
	case hTarihMs, hTarihTZ:
		kalip := "dd/www/dddd:dd:dd:dd.ddd"
		if o.tur == hTarihTZ {
			kalip = "dd/www/dddd:dd:dd:dd sdddd"
		}
		if pos+len(kalip) > n {
			return 0, false
		}
		for k := 0; k < len(kalip); k++ {
			ch := s[pos+k]
			switch kalip[k] {
			case 'd':
				if !rakamMi(ch) {
					return 0, false
				}
			case 'w':
				if !kelimeMi(ch) {
					return 0, false
				}
			case 's':
				if ch != '+' && ch != '-' {
					return 0, false
				}
			default:
				if ch != kalip[k] {
					return 0, false
				}
			}
		}
		return tek(pos + len(kalip))
	case hSuslu:
		if pos < n && s[pos] == '{' {
			for k := pos + 1; k < n; k++ {
				if s[k] == '}' {
					return tek(k + 1)
				}
			}
		}
	case hTirnakli:
		if pos < n && s[pos] == '"' {
			for k := pos + 1; k < n; k++ {
				if s[k] == '"' {
					if o.uyar(s, k+1) {
						return k + 1, true
					}
					return 0, false // "-" seçeneği bu konumda olamaz (ilk karakter tırnak)
				}
			}
			return 0, false
		}
		if pos < n && s[pos] == '-' {
			return tek(pos + 1)
		}
	case hIstek:
		// Yalnızca en olağan durum: üç parça da en uzun haliyle ve ardından gelmesi gerekenle uyuşuyor
		e1 := pos
		for e1 < n && !bosluk(s[e1]) {
			e1++
		}
		if e1 == pos || e1 >= n || s[e1] != ' ' {
			return 0, false
		}
		e2 := e1 + 1
		for e2 < n && !bosluk(s[e2]) {
			e2++
		}
		if e2 == e1+1 {
			return 0, false
		}
		if e2 < n && s[e2] == ' ' {
			e3 := e2 + 1
			for e3 < n && !bosluk(s[e3]) {
				e3++
			}
			if e3 > e2+1 && o.uyar(s, e3) {
				return e3, true
			}
			return 0, false
		}
		return tek(e2)
	}
	return 0, false
}

var ayKisa = [12]string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"}

// "07/Oct/2026:14:05:01.123" biçimindeki kabul zamanını time.ParseInLocation'dan ~10 kat hızlı
// çözer. Emin olamadığı her durumda (ayın 28'inden sonrası, beklenmeyen karakter) false döner;
// çağıran o zaman time.ParseInLocation kullanır, sonuç aynı kalır.
func hizliTarihMs(v string) (time.Time, bool) {
	if len(v) != 24 || v[2] != '/' || v[6] != '/' || v[11] != ':' || v[14] != ':' || v[17] != ':' || v[20] != '.' {
		return time.Time{}, false
	}
	sayi := func(a, b int) (int, bool) {
		n := 0
		for k := a; k < b; k++ {
			if !rakamMi(v[k]) {
				return 0, false
			}
			n = n*10 + int(v[k]-'0')
		}
		return n, true
	}
	gun, ok1 := sayi(0, 2)
	yil, ok2 := sayi(7, 11)
	sa, ok3 := sayi(12, 14)
	dk, ok4 := sayi(15, 17)
	sn, ok5 := sayi(18, 20)
	ms, ok6 := sayi(21, 24)
	if !(ok1 && ok2 && ok3 && ok4 && ok5 && ok6) || gun < 1 || gun > 28 || sa > 23 || dk > 59 || sn > 59 {
		return time.Time{}, false
	}
	ay := -1
	for k, a := range ayKisa {
		if strings.EqualFold(v[3:6], a) {
			ay = k
			break
		}
	}
	if ay < 0 {
		return time.Time{}, false
	}
	return time.Date(yil, time.Month(ay+1), gun, sa, dk, sn, ms*1e6, time.Local), true
}
