package main

// Olay incelemesi: "dün 14:00 ile 14:40 arasında ne oldu?" sorusunun cevabı.
//
// Paneldeki veriler bellekte ve saklama süresiyle sınırlı (adres ayrıntısı 1 saat, listeler
// 6 saat). İnceleme bunlara değil doğrudan log dosyalarına bakar; log ne kadar geriye
// gidiyorsa oraya kadar çalışır. Seçilen aralıktaki her istek okunur ve şunlar çıkarılır:
// dakika dakika istek ve yanıt kodu dağılımı, yanıt süresi, backend ve sunucu dökümü,
// HAProxy'nin isteği neden kestiği (sonlandırma kodu), sunucu düşme/kalkma olayları,
// en çok hata veren adresler, en çok istek atan IP'ler ve alan adları.
//
// Hız: aralık genelde dosyanın küçük bir parçasıdır. Düz dosyada aralığın başlangıcı ikili
// aramayla bulunur, dosyanın geri kalanı hiç okunmaz. Sıkıştırılmış dosyada geriye atlamak
// mümkün olmadığı için baştan okunur ama aralığın öncesindeki satırlar ayrıştırılmadan,
// yüzerlik gruplar halinde geçilir. Arama ile aynı kilit ve süre sınırı kullanılır.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// İnceleme arka planda çalışır (tarayıcı ilerlemeyi sorar), bu yüzden süresi log aramasının
	// 1 dakikasından uzun olabilir. Ajanın işlemci tavanı düşük; yoğun bir LB'nin bütün bir günü
	// bu sürede okunabilsin.
	olaySureSiniri = 3 * time.Minute
	olayEnUzun     = 7 * 24 * time.Hour // en uzun inceleme aralığı
	olayPay        = 5 * time.Minute    // log satırları kabul zamanına göre tam sıralı değildir; sınırlarda pay
	olayYolSin     = 20000              // en fazla bu kadar farklı adres izlenir, gerisi "(diğer)"
	olayIPSin      = 50000
	olayHostSin    = 2000
	olayListe      = 15 // listelerde gösterilen satır
	olayOlaySin    = 500
	olayGrup       = 256 // sıkıştırılmış dosyada aralık öncesi satırlar bu büyüklükte gruplarla geçilir
)

// Yanıt süresi dağılımı için kova sınırları (ms). Yüzdelik değeri kovanın içinde doğrusal
// varsayılarak hesaplanır; gösterilen değer yaklaşık ama kaba değildir (kovalar dar).
var olaySureSinir = []int{1, 2, 3, 5, 7, 10, 15, 20, 30, 50, 70, 100, 150, 200, 300, 500, 700, 1000,
	1500, 2000, 3000, 5000, 7000, 10000, 15000, 20000, 30000, 60000, 120000}

type sureDagilim struct {
	Kova [30]int64
	Top  int64
	N    int64
	Max  int
}

func (d *sureDagilim) ekle(ms int) {
	if ms < 0 {
		return
	}
	i := sort.SearchInts(olaySureSinir, ms+1) // ms < sınır olan ilk kova
	d.Kova[i]++
	d.Top += int64(ms)
	d.N++
	if ms > d.Max {
		d.Max = ms
	}
}

func (d *sureDagilim) birlestir(o *sureDagilim) {
	for i := range d.Kova {
		d.Kova[i] += o.Kova[i]
	}
	d.Top += o.Top
	d.N += o.N
	if o.Max > d.Max {
		d.Max = o.Max
	}
}

func (d *sureDagilim) ort() int {
	if d.N == 0 {
		return -1
	}
	return int(d.Top / d.N)
}

// p: 0-1 arası yüzdelik (0.95)
func (d *sureDagilim) yuzdelik(p float64) int {
	if d.N == 0 {
		return -1
	}
	hedef := p * float64(d.N)
	var birikmis int64
	for i, n := range d.Kova {
		if n == 0 {
			continue
		}
		if float64(birikmis+n) >= hedef {
			alt := 0
			if i > 0 {
				alt = olaySureSinir[i-1]
			}
			ust := d.Max + 1
			if i < len(olaySureSinir) && olaySureSinir[i] < ust {
				ust = olaySureSinir[i]
			}
			oran := (hedef - float64(birikmis)) / float64(n)
			v := alt + int(oran*float64(ust-alt))
			if v > d.Max {
				v = d.Max
			}
			return v
		}
		birikmis += n
	}
	return d.Max
}

// ---------- Sonuç ----------

type OlayDilim struct {
	T     int64    `json:"t"`  // dilimin başlangıcı (ms)
	N     int64    `json:"n"`  // istek
	C     [5]int64 `json:"c"`  // 2xx, 3xx, 4xx, 5xx, diğer
	NoSrv int64    `json:"ns"` // çalışan sunucu olmadığı için 503
	Ort   int      `json:"avg"`
	P95   int      `json:"p95"`
	// O dilimde dönen yanıt kodları (200, 304, 502...); grafiğin üzerine gelince gösterilir
	Codes map[int]int64 `json:"codes"`
}

type OlaySunucu struct {
	Ad  string   `json:"name"`
	N   int64    `json:"n"`
	C   [5]int64 `json:"c"`
	Ort int      `json:"avg"`
	P95 int      `json:"p95"`
	Max int      `json:"max"`
}

type OlayBackend struct {
	Ad      string           `json:"name"`
	N       int64            `json:"n"`
	C       [5]int64         `json:"c"`
	Kinds   map[string]int64 `json:"kinds"`
	Ort     int              `json:"avg"`
	P95     int              `json:"p95"`
	Max     int              `json:"max"`
	Ilk5xx  int64            `json:"first5xx,omitempty"`
	Son5xx  int64            `json:"last5xx,omitempty"`
	Sunucu  []OlaySunucu     `json:"servers"`
	Kesinti []OlayTerm       `json:"terms"` // bu backend'de en sık sonlandırma kodları (normal olanlar hariç)
}

type OlayFrontend struct {
	Ad    string           `json:"name"`
	N     int64            `json:"n"`
	C     [5]int64         `json:"c"`
	Kinds map[string]int64 `json:"kinds"`
}

// HAProxy'nin isteği neden ve hangi aşamada sonlandırdığı (log'daki iki harfli kod)
type OlayTerm struct {
	Kod     string `json:"code"`
	N       int64  `json:"n"`
	Hata    int64  `json:"errors"`            // bunlardan 5xx alan ya da yanıtsız kalanlar
	Backend string `json:"backend,omitempty"` // en çok görüldüğü backend
}

type OlayYol struct {
	Yontem  string   `json:"method"`
	Yol     string   `json:"path"`
	Backend string   `json:"backend"`
	OnYuz   bool     `json:"frontend,omitempty"` // backend'e gitmedi, HAProxy yanıtladı; Backend frontend'in adı
	N       int64    `json:"n"`
	C       [5]int64 `json:"c"`
}

type OlayIP struct {
	IP         string   `json:"ip"`
	N          int64    `json:"n"`
	C          [5]int64 `json:"c"`
	Cloudflare bool     `json:"cloudflare"`
}

type OlayHost struct {
	Ad string   `json:"name"`
	N  int64    `json:"n"`
	C  [5]int64 `json:"c"`
}

// Log'daki olay satırları: sunucu düştü/kalktı, backend'de sunucu kalmadı, HAProxy başladı
type OlayKaydi struct {
	At      int64  `json:"at"`
	Tur     string `json:"type"` // down, up, maint, drain, noserver, start, stop
	Backend string `json:"backend,omitempty"`
	Sunucu  string `json:"server,omitempty"`
	Sebep   string `json:"reason,omitempty"`
	Kalan   int    `json:"left"` // düşme sonrası çalışan sunucu sayısı (-1: bilinmiyor)
}

type OlayRapor struct {
	From     int64            `json:"from"`
	To       int64            `json:"to"`
	Step     int              `json:"step"` // dilim uzunluğu, dakika
	N        int64            `json:"n"`
	C        [5]int64         `json:"c"`
	Kinds    map[string]int64 `json:"kinds"`
	Codes    []CodeCount      `json:"codes"`
	Ort      int              `json:"avg"`
	P95      int              `json:"p95"`
	Seri     []OlayDilim      `json:"series"`
	Backends []OlayBackend    `json:"backends"`
	// Hiçbir backend'e gitmeyen, HAProxy'nin kendisinin yanıtladığı istekler, frontend başına
	Frontends  []OlayFrontend `json:"frontends"`
	Terms      []OlayTerm     `json:"terms"`
	Hatali     []OlayYol      `json:"errorPaths"` // en çok 5xx/4xx alan adresler
	Yogun      []OlayYol      `json:"topPaths"`   // en çok istenen adresler
	IPs        []OlayIP       `json:"ips"`
	Hosts      []OlayHost     `json:"hosts"`
	HostLines  int64          `json:"hostLines"`
	Olaylar    []OlayKaydi    `json:"events"`
	OlayYok    bool           `json:"eventsUnknown"`
	OlayKesik  bool           `json:"eventsCut"` // olay sayısı sınırı aşıldı; ilk olaylar gösteriliyor // olay satırlarının zamanı okunamadı (zaman damgasız log)
	TCP        int64          `json:"tcp"`       // http olmayan (tcp modu) kayıtlar; sayılmaz
	Okunamayan int64          `json:"unparsed"`  // aralıktaki okunamayan trafik satırları
	FirstAt    int64          `json:"firstAt,omitempty"`
	LastAt     int64          `json:"lastAt,omitempty"`
	Scanned    int64          `json:"scanned"`
	Files      []string       `json:"files"`
	Skipped    int            `json:"skipped"`
	Truncated  bool           `json:"truncated"`
	// Aralığın nereye kadar okunduğu (ms). Süre sınırına takılırsa ya da durdurulursa bitişten
	// önce kalır; arayüz bu noktadan sonrasını "okunmadı" diye gösterir, "istek yok" diye değil.
	KapsamSon  int64  `json:"coveredTo"`
	Durduruldu bool   `json:"stopped,omitempty"`
	Took       int64  `json:"took"`
	Note       string `json:"note,omitempty"`
}

// ---------- Toplayıcı ----------

type olayYolAnahtar struct {
	m, p, b string
	fe      bool // HAProxy'nin kendisi yanıtladı (b frontend'in adı)
}

type olayFE struct {
	n     int64
	c     [5]int64
	kinds map[string]int64
}

type olayBE struct {
	n          int64
	c          [5]int64
	kinds      map[string]int64
	sure       sureDagilim
	ilk5, son5 int64
	sunucu     map[string]*olaySrv
	term       map[string]int64
}

type olaySrv struct {
	n    int64
	c    [5]int64
	sure sureDagilim
}

type olayTermAgg struct {
	n, hata int64
	be      map[string]int64
}

type olayDilimAgg struct {
	n     int64
	c     [5]int64
	noSrv int64
	sure  sureDagilim
	kod   map[int]int64
}

type olayToplayici struct {
	from, to time.Time
	step     time.Duration
	parser   *LogParser
	cf       func(string) bool
	r        OlayRapor
	dilim    []olayDilimAgg
	sure     sureDagilim
	kodlar   map[int]int64
	be       map[string]*olayBE
	fe       map[string]*olayFE
	term     map[string]*olayTermAgg
	yol      map[olayYolAnahtar]*[5]int64
	ip       map[string]*[5]int64
	host     map[string]*[5]int64
	olaySon  map[string]int64 // aynı olayın tekrarını ayıklamak için (ör. her proxy için "started")
	bitis    time.Time
	sonZaman time.Time     // okunan satırların en geç zamanı: nereye kadar okunduğu
	ilerleme *atomic.Int64 // arka plandaki iş için: nereye kadar okundu (ms)
	iptal    *atomic.Bool  // kullanıcı "durdur" dedi
	bayt     int64
	satir    int64
	olayZmn  int64 // zamanı okunabilen olay satırı
	olayHam  int64 // olay gibi görünen satır
}

func sinifIdx(st int) int {
	if st >= 200 && st < 600 {
		return st/100 - 2
	}
	return 4
}

// Seçilen aralığa göre dilim uzunluğu: grafikte en fazla ~360 nokta olsun
func olayAdim(d time.Duration) int {
	for _, m := range []int{1, 2, 5, 10, 15, 30, 60} {
		if d <= time.Duration(m)*360*time.Minute {
			return m
		}
	}
	return 60
}

func yeniOlayToplayici(from, to time.Time, parser *LogParser, cf func(string) bool) *olayToplayici {
	adim := olayAdim(to.Sub(from))
	t := &olayToplayici{from: from, to: to, step: time.Duration(adim) * time.Minute, parser: parser, cf: cf,
		kodlar: map[int]int64{}, be: map[string]*olayBE{}, fe: map[string]*olayFE{}, term: map[string]*olayTermAgg{},
		yol: map[olayYolAnahtar]*[5]int64{}, ip: map[string]*[5]int64{}, host: map[string]*[5]int64{},
		olaySon: map[string]int64{}}
	t.r.From, t.r.To, t.r.Step = from.UnixMilli(), to.UnixMilli(), adim
	t.r.Kinds = map[string]int64{}
	// Dilim sayısı yukarı yuvarlanır; tam bitiş anındaki kayıt son dilime girer. (Eskiden
	// yalnızca bitiş anını kapsayan boş bir son dilim oluşuyor, "trafik düştü" gibi görünüyordu.)
	n := int((to.Sub(from) + t.step - 1) / t.step)
	if n < 1 {
		n = 1
	}
	t.dilim = make([]olayDilimAgg, n)
	return t
}

func (t *olayToplayici) kayit(r logRecord) {
	if r.At.Before(t.from) || r.At.After(t.to) {
		return
	}
	if r.Kind == KindTCP {
		t.r.TCP++
		return
	}
	ms := r.At.UnixMilli()
	if t.r.FirstAt == 0 || ms < t.r.FirstAt {
		t.r.FirstAt = ms
	}
	if ms > t.r.LastAt {
		t.r.LastAt = ms
	}
	k := sinifIdx(r.Status)
	t.r.N++
	t.r.C[k]++
	t.r.Kinds[r.Kind]++
	t.kodlar[r.Status]++
	t.sure.ekle(r.Ta)

	i := int(r.At.Sub(t.from) / t.step)
	if i >= len(t.dilim) {
		i = len(t.dilim) - 1
	}
	d := &t.dilim[i]
	d.n++
	d.c[k]++
	if d.kod == nil {
		d.kod = map[int]int64{}
	}
	d.kod[r.Status]++
	if r.Kind == KindNoServer {
		d.noSrv++
	}
	d.sure.ekle(r.Ta)

	// Hiçbir backend'e gönderilmeyip HAProxy'nin kendisinin yanıtladığı istekler (yönlendirme,
	// engelleme, eşleşmeyen adres): log'da backend yerine frontend'in adı yazar. Bunlar
	// backend listesine girmez, ayrı gösterilir; eskiden "http_front" bir backend gibi görünüyordu.
	onYuz := r.Server == "<NOSRV>" && r.Backend == r.Frontend
	if onYuz {
		f := t.fe[r.Frontend]
		if f == nil {
			f = &olayFE{kinds: map[string]int64{}}
			t.fe[r.Frontend] = f
		}
		f.n++
		f.c[k]++
		f.kinds[r.Kind]++
	}
	b := t.be[r.Backend]
	if onYuz {
		b = &olayBE{kinds: map[string]int64{}, sunucu: map[string]*olaySrv{}, term: map[string]int64{}} // atılır
	} else if b == nil {
		b = &olayBE{kinds: map[string]int64{}, sunucu: map[string]*olaySrv{}, term: map[string]int64{}}
		t.be[r.Backend] = b
	}
	b.n++
	b.c[k]++
	b.kinds[r.Kind]++
	b.sure.ekle(r.Ta)
	if k == 3 {
		if b.ilk5 == 0 || ms < b.ilk5 {
			b.ilk5 = ms
		}
		if ms > b.son5 {
			b.son5 = ms
		}
	}
	if r.Server != "" && r.Server != "<NOSRV>" {
		s := b.sunucu[r.Server]
		if s == nil {
			s = &olaySrv{}
			b.sunucu[r.Server] = s
		}
		s.n++
		s.c[k]++
		s.sure.ekle(r.Ta)
	}

	// Sonlandırma kodu: ilk iki harf (dört harfli biçimde kalanlar çerez durumu)
	if len(r.Term) >= 2 {
		kod := r.Term[:2]
		if kod != "--" {
			ta := t.term[kod]
			if ta == nil {
				ta = &olayTermAgg{be: map[string]int64{}}
				t.term[kod] = ta
			}
			ta.n++
			if k == 3 || k == 4 {
				ta.hata++
			}
			if !onYuz {
				ta.be[r.Backend]++
			}
			b.term[kod]++
		}
	}

	yk := olayYolAnahtar{r.Method, r.Path, r.Backend, onYuz}
	y := t.yol[yk]
	if y == nil {
		if len(t.yol) >= olayYolSin {
			yk = olayYolAnahtar{"", otherKey, "", false}
			y = t.yol[yk]
		}
		if y == nil {
			y = &[5]int64{}
			t.yol[yk] = y
		}
	}
	y[k]++

	ipk := r.Client
	c := t.ip[ipk]
	if c == nil {
		if len(t.ip) >= olayIPSin {
			ipk = otherKey
			c = t.ip[ipk]
		}
		if c == nil {
			c = &[5]int64{}
			t.ip[ipk] = c
		}
	}
	c[k]++

	if r.Host != "" {
		t.r.HostLines++
		hk := r.Host
		h := t.host[hk]
		if h == nil {
			if len(t.host) >= olayHostSin {
				hk = otherKey
				h = t.host[hk]
			}
			if h == nil {
				h = &[5]int64{}
				t.host[hk] = h
			}
		}
		h[k]++
	}
}

var (
	reOlaySunucu = regexp.MustCompile(`Server ([^/\s]+)/(\S+) (is DOWN|is UP|is going DOWN for maintenance|was DOWN and now enters maintenance|enters drain state|is UP/READY)`)
	reOlaySebep  = regexp.MustCompile(`reason: (.+?)(?:, info: "[^"]*")?(?:, check duration: [^.]*)?\.`)
	reOlayKod    = regexp.MustCompile(`code: (\d+)`)
	reOlayKalan  = regexp.MustCompile(`(\d+) active and (\d+) backup servers? (?:left|online)`)
	reOlayBos    = regexp.MustCompile(`backend (\S+) has no server available`)
	reOlayBasla  = regexp.MustCompile(`Proxy (\S+) started\.`)
	reOlayDur    = regexp.MustCompile(`(?:Stopping proxy|Proxy) (\S+) (?:in \d+ ms|stopped)`)
)

// Satırın başındaki syslog zaman damgası. Olay satırlarında HAProxy'nin kabul zamanı
// olmadığı için zaman buradan okunur. İki biçim: "Oct  8 14:00:01" (yılsız) ve
// "2026-10-08T14:00:01.123+03:00" (rsyslog yüksek çözünürlük, journalctl short-iso).
func syslogZamani(line string, simdi time.Time) (time.Time, bool) {
	if len(line) >= 19 && line[4] == '-' && line[10] == 'T' {
		alan := line
		if i := strings.IndexByte(line, ' '); i > 0 {
			alan = line[:i]
		}
		for _, l := range []string{time.RFC3339Nano, "2006-01-02T15:04:05-0700", "2006-01-02T15:04:05.999999999-0700"} {
			if t, err := time.Parse(l, alan); err == nil {
				return t, true
			}
		}
		return time.Time{}, false
	}
	if len(line) >= 15 && line[3] == ' ' && line[9] == ':' {
		t, err := time.ParseInLocation("Jan _2 15:04:05", line[:15], time.Local)
		if err != nil {
			return time.Time{}, false
		}
		t = t.AddDate(simdi.Year(), 0, 0)
		if t.After(simdi.Add(48 * time.Hour)) { // yıl dönümü: aralık geçen yıldan
			t = t.AddDate(-1, 0, 0)
		}
		return t, true
	}
	return time.Time{}, false
}

func (t *olayToplayici) olay(line, msg string) bool {
	var o OlayKaydi
	o.Kalan = -1
	if m := reOlaySunucu.FindStringSubmatch(msg); m != nil {
		o.Backend, o.Sunucu = m[1], m[2]
		switch m[3] {
		case "is DOWN":
			o.Tur = "down"
		case "is UP", "is UP/READY":
			o.Tur = "up"
		case "enters drain state":
			o.Tur = "drain"
		default:
			o.Tur = "maint"
		}
		if s := reOlaySebep.FindStringSubmatch(msg); s != nil {
			o.Sebep = s[1]
		}
		if k := reOlayKalan.FindStringSubmatch(msg); k != nil {
			o.Kalan, _ = strconv.Atoi(k[1])
		}
	} else if m := reOlayBos.FindStringSubmatch(msg); m != nil {
		o.Tur, o.Backend = "noserver", m[1]
	} else if reOlayBasla.MatchString(msg) {
		o.Tur = "start"
	} else if reOlayDur.MatchString(msg) {
		o.Tur = "stop"
	} else {
		return false
	}
	t.olayHam++
	zmn, ok := syslogZamani(line, time.Now())
	if !ok {
		return true
	}
	t.olayZmn++
	if zmn.Before(t.from.Add(-olayPay)) || zmn.After(t.to) {
		return true
	}
	o.At = zmn.UnixMilli()
	// "start"/"stop" her proxy için ayrı satır yazılır; bir yeniden yüklemeyi tek olay say
	if o.Tur == "start" || o.Tur == "stop" {
		if son, vardi := t.olaySon[o.Tur]; vardi && o.At-son < 60_000 && o.At >= son {
			return true
		}
		t.olaySon[o.Tur] = o.At
	}
	if len(t.r.Olaylar) < olayOlaySin {
		t.r.Olaylar = append(t.r.Olaylar, o)
	} else {
		t.r.OlayKesik = true
	}
	return true
}

// Bir satırı işler; satırın zamanını (biliniyorsa) döndürür
func (t *olayToplayici) satirIsle(line string) (time.Time, bool) {
	t.satir++
	t.bayt += int64(len(line)) + 1
	if rec, ok := t.parser.Parse(line); ok {
		t.kayit(rec)
		if rec.At.After(t.sonZaman) {
			t.sonZaman = rec.At
		}
		return rec.At, true
	}
	msg := syslogMessage(line)
	if t.olay(line, msg) {
		zmn, ok := syslogZamani(line, time.Now())
		return zmn, ok
	}
	if reTrafficLike.MatchString(msg) {
		if zmn, ok := syslogZamani(line, time.Now()); !ok || (!zmn.Before(t.from) && !zmn.After(t.to)) {
			t.r.Okunamayan++
		}
	}
	return syslogZamani(line, time.Now())
}

func (t *olayToplayici) sureDoldu() bool {
	if t.ilerleme != nil {
		t.ilerleme.Store(t.sonZaman.UnixMilli())
	}
	if t.iptal != nil && t.iptal.Load() {
		t.r.Truncated, t.r.Durduruldu = true, true
		return true
	}
	if time.Now().After(t.bitis) || t.bayt > aramaBaytSiniri {
		t.r.Truncated = true
		return true
	}
	return false
}

// Satırın kaydedilen zamanı (önce HAProxy'nin kabul zamanı, yoksa syslog zamanı)
func (t *olayToplayici) satirZamani(line string) (time.Time, bool) {
	if rec, ok := t.parser.Parse(line); ok {
		return rec.At, true
	}
	return syslogZamani(line, time.Now())
}

// Düz dosyada ikili arama: lo konumundaki satırların zamanı hedeften önce, hi konumundan
// sonrakilerin zamanı hedef ya da sonrası. lo'dan ileri okuyan hedeften sonraki hiçbir
// satırı atlamaz; hi'den geriye okuyan da hedeften önceki hiçbir satırı atlamaz.
// Satırlar tam sıralı olmadığı için çağıranlar hedefe bir pay ekler.
func zamanOfseti(f *os.File, boyut int64, hedef time.Time, zaman func(string) (time.Time, bool)) (lo, hi int64) {
	lo, hi = 0, boyut
	for hi-lo > 64<<10 {
		mid := lo + (hi-lo)/2
		zmn, ok := ofsetZamani(f, mid, hi, zaman)
		if !ok || !zmn.Before(hedef) {
			hi = mid
		} else {
			lo = mid
		}
	}
	return lo, hi
}

// Konumdan sonraki ilk tam satırlardan zamanı okunabilen ilkinin zamanı (en fazla 256 KB okunur)
func ofsetZamani(f *os.File, ofset, sinir int64, zaman func(string) (time.Time, bool)) (time.Time, bool) {
	if _, err := f.Seek(ofset, io.SeekStart); err != nil {
		return time.Time{}, false
	}
	okuyucu := bufio.NewReaderSize(io.LimitReader(f, 256<<10), 64<<10)
	ilk := ofset > 0
	konum := ofset
	for konum < sinir {
		satir, err := okuyucu.ReadString('\n')
		konum += int64(len(satir))
		if !ilk {
			if zmn, ok := zaman(strings.TrimRight(satir, "\r\n")); ok {
				return zmn, true
			}
		}
		ilk = false
		if err != nil {
			break
		}
	}
	return time.Time{}, false
}

// Satırların zamanı aralığın sonunu (payıyla) üst üste geçince okuma durur
type sonDenetci struct {
	sinir time.Time
	ust   int
}

func (s *sonDenetci) bitti(zmn time.Time, ok bool) bool {
	if !ok {
		return false
	}
	if zmn.After(s.sinir) {
		s.ust++
		return s.ust >= 3
	}
	s.ust = 0
	return false
}

func (t *olayToplayici) duzDosya(yol string) {
	f, err := os.Open(yol)
	if err != nil {
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return
	}
	bas, _ := zamanOfseti(f, fi.Size(), t.from.Add(-olayPay), t.satirZamani)
	if _, err := f.Seek(bas, io.SeekStart); err != nil {
		return
	}
	t.r.Files = append(t.r.Files, filepath.Base(yol))
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	if bas > 0 {
		sc.Scan() // yarım satır
	}
	son := sonDenetci{sinir: t.to.Add(olayPay)}
	for n := 0; sc.Scan(); n++ {
		if n%2000 == 0 && t.sureDoldu() {
			return
		}
		if son.bitti(t.satirIsle(sc.Text())) {
			return
		}
	}
}

// Sıkıştırılmış dosya baştan okunur. Aralığın öncesindeki satırlar ayrıştırılmadan geçilir:
// her grubun son satırının zamanına bakılır, aralığın başına (payıyla) varılmadıysa grup
// bütünüyle atlanır. Varıldıysa o grup baştan tam işlenir, böylece sınırdaki satırlar kaçmaz.
func (t *olayToplayici) gzDosya(yol string) {
	sc, kapat, err := satirOkuyucu(yol)
	if err != nil {
		return
	}
	defer kapat()
	t.r.Files = append(t.r.Files, filepath.Base(yol))
	t.akis(sc)
}

func (t *olayToplayici) akis(sc *bufio.Scanner) {
	basSinir := t.from.Add(-olayPay)
	son := sonDenetci{sinir: t.to.Add(olayPay)}
	grup := make([]string, 0, olayGrup)
	icerde := false
	n := 0
	for sc.Scan() {
		if n++; n%2000 == 0 && t.sureDoldu() {
			return
		}
		if icerde {
			if son.bitti(t.satirIsle(sc.Text())) {
				return
			}
			continue
		}
		t.bayt += int64(len(sc.Bytes())) + 1
		grup = append(grup, sc.Text())
		if len(grup) < olayGrup {
			continue
		}
		if t.grupOncesi(grup, basSinir) {
			t.satir += int64(len(grup))
			grup = grup[:0]
			continue
		}
		icerde = true
		for _, s := range grup {
			t.bayt -= int64(len(s)) + 1 // satirIsle yeniden sayacak
			if son.bitti(t.satirIsle(s)) {
				return
			}
		}
		grup = grup[:0]
	}
	if !icerde {
		for _, s := range grup {
			t.bayt -= int64(len(s)) + 1
			t.satirIsle(s)
		}
	}
}

// Grubun tamamı aralığın başından önce mi? Sondan başa, zamanı okunabilen ilk satıra bakılır.
func (t *olayToplayici) grupOncesi(grup []string, sinir time.Time) bool {
	for i := len(grup) - 1; i >= 0 && i >= len(grup)-16; i-- {
		if zmn, ok := t.satirZamani(grup[i]); ok {
			return zmn.Before(sinir)
		}
	}
	return false
}

func (t *olayToplayici) journal(tag string) {
	ctx, cancel := context.WithDeadline(context.Background(), t.bitis)
	defer cancel()
	// short-iso: olay satırlarının da zamanı olsun ("-o cat" zaman damgası vermez)
	cmd := exec.CommandContext(ctx, "journalctl", "--no-pager", "-q", "-o", "short-iso", "-t", tag,
		"--since", t.from.Add(-olayPay).Format("2006-01-02 15:04:05"),
		"--until", t.to.Add(olayPay).Format("2006-01-02 15:04:05"))
	out, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		return
	}
	t.r.Files = append(t.r.Files, "journald:"+tag)
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for n := 0; sc.Scan(); n++ {
		if n%2000 == 0 && t.sureDoldu() {
			break
		}
		t.satirIsle(sc.Text())
	}
	cancel()
	_ = cmd.Wait()
}

func dagilimBas(d *sureDagilim) (int, int) { return d.ort(), d.yuzdelik(0.95) }

func (t *olayToplayici) bitir() {
	r := &t.r
	r.Ort, r.P95 = dagilimBas(&t.sure)
	for k, n := range t.kodlar {
		r.Codes = append(r.Codes, CodeCount{Code: k, N: n})
	}
	sort.Slice(r.Codes, func(i, j int) bool {
		a, b := r.Codes[i], r.Codes[j]
		return a.N > b.N || a.N == b.N && a.Code < b.Code
	})
	for i := range t.dilim {
		d := &t.dilim[i]
		o := OlayDilim{T: t.from.Add(time.Duration(i) * t.step).UnixMilli(), N: d.n, C: d.c, NoSrv: d.noSrv, Codes: d.kod}
		o.Ort, o.P95 = dagilimBas(&d.sure)
		r.Seri = append(r.Seri, o)
	}
	for ad, b := range t.be {
		ob := OlayBackend{Ad: ad, N: b.n, C: b.c, Kinds: b.kinds, Max: b.sure.Max, Ilk5xx: b.ilk5, Son5xx: b.son5}
		ob.Ort, ob.P95 = dagilimBas(&b.sure)
		for sa, s := range b.sunucu {
			sn := OlaySunucu{Ad: sa, N: s.n, C: s.c, Max: s.sure.Max}
			sn.Ort, sn.P95 = dagilimBas(&s.sure)
			ob.Sunucu = append(ob.Sunucu, sn)
		}
		sort.Slice(ob.Sunucu, func(i, j int) bool {
			a, c := ob.Sunucu[i], ob.Sunucu[j]
			if a.C[3] != c.C[3] {
				return a.C[3] > c.C[3]
			}
			if a.N != c.N {
				return a.N > c.N
			}
			return a.Ad < c.Ad
		})
		for kod, n := range b.term {
			ob.Kesinti = append(ob.Kesinti, OlayTerm{Kod: kod, N: n})
		}
		sort.Slice(ob.Kesinti, func(i, j int) bool {
			return ob.Kesinti[i].N > ob.Kesinti[j].N || ob.Kesinti[i].N == ob.Kesinti[j].N && ob.Kesinti[i].Kod < ob.Kesinti[j].Kod
		})
		if len(ob.Kesinti) > 5 {
			ob.Kesinti = ob.Kesinti[:5]
		}
		r.Backends = append(r.Backends, ob)
	}
	// Sorunlu olanlar önce: 5xx sayısı, sonra istek sayısı
	sort.Slice(r.Backends, func(i, j int) bool {
		a, b := r.Backends[i], r.Backends[j]
		if a.C[3] != b.C[3] {
			return a.C[3] > b.C[3]
		}
		if a.N != b.N {
			return a.N > b.N
		}
		return a.Ad < b.Ad
	})
	for ad, f := range t.fe {
		r.Frontends = append(r.Frontends, OlayFrontend{Ad: ad, N: f.n, C: f.c, Kinds: f.kinds})
	}
	sort.Slice(r.Frontends, func(i, j int) bool {
		a, b := r.Frontends[i], r.Frontends[j]
		return a.N > b.N || a.N == b.N && a.Ad < b.Ad
	})
	for kod, ta := range t.term {
		ot := OlayTerm{Kod: kod, N: ta.n, Hata: ta.hata}
		var en int64
		for be, n := range ta.be {
			if n > en || n == en && be < ot.Backend {
				en, ot.Backend = n, be
			}
		}
		r.Terms = append(r.Terms, ot)
	}
	sort.Slice(r.Terms, func(i, j int) bool {
		a, b := r.Terms[i], r.Terms[j]
		if a.Hata != b.Hata {
			return a.Hata > b.Hata
		}
		if a.N != b.N {
			return a.N > b.N
		}
		return a.Kod < b.Kod
	})

	var yollar []OlayYol
	for k, c := range t.yol {
		var n int64
		for _, v := range c {
			n += v
		}
		yollar = append(yollar, OlayYol{Yontem: k.m, Yol: k.p, Backend: k.b, OnYuz: k.fe, N: n, C: *c})
	}
	sirala := func(l []OlayYol, once func(a, b OlayYol) int) []OlayYol {
		out := append([]OlayYol(nil), l...)
		sort.Slice(out, func(i, j int) bool {
			a, b := out[i], out[j]
			if (a.Yol == otherKey) != (b.Yol == otherKey) {
				return b.Yol == otherKey
			}
			if f := once(a, b); f != 0 {
				return f > 0
			}
			if a.N != b.N {
				return a.N > b.N
			}
			return a.Yontem+a.Yol+a.Backend < b.Yontem+b.Yol+b.Backend
		})
		if len(out) > olayListe {
			out = out[:olayListe]
		}
		return out
	}
	r.Yogun = sirala(yollar, func(a, b OlayYol) int { return 0 })
	var hatali []OlayYol
	for _, y := range yollar {
		if y.C[3]+y.C[2]+y.C[4] > 0 {
			hatali = append(hatali, y)
		}
	}
	r.Hatali = sirala(hatali, func(a, b OlayYol) int {
		switch {
		case a.C[3] != b.C[3]:
			if a.C[3] > b.C[3] {
				return 1
			}
			return -1
		case a.C[2] != b.C[2]:
			if a.C[2] > b.C[2] {
				return 1
			}
			return -1
		}
		return 0
	})

	for ip, c := range t.ip {
		var n int64
		for _, v := range c {
			n += v
		}
		r.IPs = append(r.IPs, OlayIP{IP: ip, N: n, C: *c, Cloudflare: ip != otherKey && t.cf != nil && t.cf(ip)})
	}
	sort.Slice(r.IPs, func(i, j int) bool {
		a, b := r.IPs[i], r.IPs[j]
		if (a.IP == otherKey) != (b.IP == otherKey) {
			return b.IP == otherKey
		}
		return a.N > b.N || a.N == b.N && a.IP < b.IP
	})
	if len(r.IPs) > olayListe {
		r.IPs = r.IPs[:olayListe]
	}
	for h, c := range t.host {
		var n int64
		for _, v := range c {
			n += v
		}
		r.Hosts = append(r.Hosts, OlayHost{Ad: h, N: n, C: *c})
	}
	sort.Slice(r.Hosts, func(i, j int) bool {
		a, b := r.Hosts[i], r.Hosts[j]
		if a.C[3] != b.C[3] {
			return a.C[3] > b.C[3]
		}
		return a.N > b.N || a.N == b.N && a.Ad < b.Ad
	})
	if len(r.Hosts) > olayListe {
		r.Hosts = r.Hosts[:olayListe]
	}
	sort.SliceStable(r.Olaylar, func(i, j int) bool { return r.Olaylar[i].At < r.Olaylar[j].At })
	r.OlayYok = t.olayHam > 0 && t.olayZmn == 0
	r.Scanned = t.satir
}

// Seçilen aralığı log dosyalarından inceler (beklemeden; testler ve tek seferlik kullanım için)
func (a *LogAnalyzer) Incident(from, to time.Time) (OlayRapor, error) {
	if !aramaKilidi.TryLock() {
		return OlayRapor{}, ErrAramaSuruyor // arama ile aynı kilit: ikisi aynı anda çalışmaz
	}
	defer aramaKilidi.Unlock()
	return a.incele(from, to, nil, nil), nil
}

// Kilit çağıranda. ilerleme ve iptal arka plandaki iş içindir (nil olabilir).
func (a *LogAnalyzer) incele(from, to time.Time, ilerleme *atomic.Int64, iptal *atomic.Bool) OlayRapor {
	basla := time.Now()
	t := yeniOlayToplayici(from, to, a.parser.Load(), a.isCloudflare)
	t.bitis = basla.Add(olaySureSiniri)
	t.ilerleme, t.iptal = ilerleme, iptal
	kaynak := a.Source()
	switch {
	case kaynak == "":
		t.r.Note = "Bu sunucuda okunabilir bir HAProxy log'u bulunamadı."
	case strings.HasPrefix(kaynak, "journal:"):
		t.journal(strings.TrimPrefix(kaynak, "journal:"))
	default:
		dosyalar := rotasyonlar(strings.TrimPrefix(kaynak, "file:"))
		// Eskiden yeniye: süre sınırına takılırsa aralığın başı (olayın başladığı yer) eksiksiz olsun
		for i := len(dosyalar) - 1; i >= 0 && !t.r.Truncated; i-- {
			f := dosyalar[i]
			if f.Mod.Before(from) { // son yazma aralıktan önce: içinde aralığa giren kayıt yok
				t.r.Skipped++
				continue
			}
			if strings.HasSuffix(f.Yol, ".gz") {
				t.gzDosya(f.Yol)
			} else {
				t.duzDosya(f.Yol)
			}
		}
	}
	t.bitir()
	t.r.Took = time.Since(basla).Milliseconds()
	t.r.KapsamSon = to.UnixMilli()
	if t.r.Truncated {
		// Okuma eskiden yeniye gittiği için okunan kısım aralığın başından bu noktaya kadar
		kapsam := t.sonZaman
		if kapsam.Before(from) {
			kapsam = from
		}
		if kapsam.Before(to) {
			t.r.KapsamSon = kapsam.UnixMilli()
		}
		if t.r.Note == "" {
			t.r.Note = "Süre sınırına ulaşıldı; aralığın tamamı okunamadı. Okunan kısım aralığın başından " +
				"itibaren eksiksiz. Daha kısa bir aralık seçerek tamamını görebilirsin."
			if t.r.Durduruldu {
				t.r.Note = "İnceleme durduruldu. Okunan kısım aralığın başından itibaren eksiksiz."
			}
		}
	}
	return t.r
}

// ---------- Arka planda çalışan inceleme ----------
//
// Uzun bir aralığı okumak, ajanın düşük işlemci tavanıyla dakikalar sürebilir. Tarayıcı bu
// sürede bir HTTP isteğini açık tutmaz: inceleme başlatılır, tarayıcı saniyede bir nereye
// kadar okunduğunu sorar ve bittiğinde sonucu alır. Aynı anda tek inceleme çalışır; yeni bir
// aralık istenirse eskisi durdurulur.

type olayIsi struct {
	id        string
	from, to  time.Time
	basla     time.Time
	ilerleme  atomic.Int64
	iptal     atomic.Bool
	calisiyor atomic.Bool
	bekliyor  atomic.Bool // başka bir arama ya da inceleme kilidi tutuyor
	mu        sync.Mutex
	rapor     *OlayRapor
}

type OlayDurum struct {
	Job    string     `json:"job"`
	Durum  string     `json:"state"` // bekliyor, okunuyor, bitti
	From   int64      `json:"from"`
	To     int64      `json:"to"`
	Okunan int64      `json:"readTo"` // nereye kadar okundu (ms)
	Gecen  int64      `json:"elapsed"`
	Rapor  *OlayRapor `json:"report,omitempty"`
}

var (
	olayIsMu  sync.Mutex
	olayIsSon *olayIsi
)

func (j *olayIsi) durum() OlayDurum {
	d := OlayDurum{Job: j.id, From: j.from.UnixMilli(), To: j.to.UnixMilli(), Okunan: j.ilerleme.Load(),
		Gecen: time.Since(j.basla).Milliseconds()}
	j.mu.Lock()
	d.Rapor = j.rapor
	j.mu.Unlock()
	switch {
	case d.Rapor != nil:
		d.Durum = "bitti"
	case j.bekliyor.Load():
		d.Durum = "bekliyor" // başka bir arama ya da inceleme bitmeyi bekliyor
	default:
		d.Durum = "okunuyor"
	}
	if d.Okunan < d.From {
		d.Okunan = d.From
	}
	return d
}

// İncelemeyi başlatır; aynı aralık zaten çalışıyorsa onu döndürür
func (a *LogAnalyzer) IncidentStart(from, to time.Time) OlayDurum {
	olayIsMu.Lock()
	defer olayIsMu.Unlock()
	if j := olayIsSon; j != nil && j.from.Equal(from) && j.to.Equal(to) && !j.iptal.Load() {
		j.mu.Lock()
		bitmedi := j.rapor == nil
		j.mu.Unlock()
		if bitmedi {
			return j.durum()
		}
	}
	if j := olayIsSon; j != nil {
		j.iptal.Store(true) // yeni aralık istendi: eskisi bıraksın
	}
	j := &olayIsi{id: strconv.FormatInt(time.Now().UnixNano(), 36), from: from, to: to, basla: time.Now()}
	olayIsSon = j
	go func() {
		// "bekliyor" yalnızca kilit gerçekten başkasındaysa gösterilir (eskiden iş başlarken bir
		// an "başka bir arama sürüyor" yazıyordu)
		if !aramaKilidi.TryLock() {
			j.bekliyor.Store(true)
			aramaKilidi.Lock()
			j.bekliyor.Store(false)
		}
		defer aramaKilidi.Unlock()
		if j.iptal.Load() {
			r := OlayRapor{From: from.UnixMilli(), To: to.UnixMilli(), KapsamSon: from.UnixMilli(), Durduruldu: true, Truncated: true}
			j.mu.Lock()
			j.rapor = &r
			j.mu.Unlock()
			return
		}
		j.calisiyor.Store(true)
		r := a.incele(from, to, &j.ilerleme, &j.iptal)
		j.mu.Lock()
		j.rapor = &r
		j.mu.Unlock()
		j.calisiyor.Store(false)
	}()
	return j.durum()
}

// Çalışan ya da biten incelemenin durumu; durdur true ise okumayı keser (okunan kısım raporlanır)
func IncidentStatus(id string, durdur bool) (OlayDurum, bool) {
	olayIsMu.Lock()
	j := olayIsSon
	olayIsMu.Unlock()
	if j == nil || j.id != id {
		return OlayDurum{}, false
	}
	if durdur {
		j.iptal.Store(true)
	}
	return j.durum(), true
}

// HTTP parametrelerinden aralık: from, to (unix ms)
func incidentRange(get func(string) string, simdi time.Time) (time.Time, time.Time, error) {
	f, err1 := strconv.ParseInt(get("from"), 10, 64)
	g, err2 := strconv.ParseInt(get("to"), 10, 64)
	if err1 != nil || err2 != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("başlangıç ve bitiş zamanı gerekli")
	}
	from, to := time.UnixMilli(f), time.UnixMilli(g)
	if !to.After(from) {
		return from, to, fmt.Errorf("bitiş, başlangıçtan sonra olmalı")
	}
	if to.Sub(from) > olayEnUzun {
		return from, to, fmt.Errorf("aralık en fazla 7 gün olabilir")
	}
	if from.After(simdi) {
		return from, to, fmt.Errorf("başlangıç gelecekte")
	}
	if to.After(simdi) {
		to = simdi
	}
	return from, to, nil
}
