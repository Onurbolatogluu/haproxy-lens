package main

// Log'da arama. Paneldeki veriler bellekte tutulduğu ve saklama süresiyle sınırlı olduğu
// için burada log dosyaları DOĞRUDAN okunur: döndürülmüş (haproxy.log.1, .2.gz) dosyalar
// dahil. Böylece 24 saatlik sınırın ötesine bakılabilir.
//
// Dikkat edilenler:
//   - Kabuk komutu çalıştırılmaz; dosyalar Go içinde okunur, yani enjeksiyon riski yok.
//   - Yalnızca ajanın kullandığı log kaynağı ve onun döndürülmüş kopyaları okunabilir;
//     kullanıcıdan gelen bir dosya yolu kabul edilmez.
//   - Ajanın CPU tavanı düşük olduğu için arama süre, boyut ve sonuç sınırlarıyla korunur;
//     sınıra takılırsa sonuç bunu açıkça söyler.

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	aramaSureSiniri  = 20 * time.Second
	aramaBaytSiniri  = 4 << 30 // en fazla 4 GB tara
	aramaOrnekSiniri = 200     // gösterilecek en fazla satır
	aramaIPSiniri    = 20
	aramaYolSiniri   = 20
)

type SearchQuery struct {
	Path    string // yolun içinde geçen metin
	IP      string // istemci IP'si (tam ya da önek)
	Status  string // "500", "5xx" ya da boş
	Method  string
	Backend string
	Since   time.Time
	Until   time.Time
}

type SearchHit struct {
	At      int64  `json:"at"`
	IP      string `json:"ip"`
	Status  int    `json:"status"`
	Method  string `json:"method"`
	Path    string `json:"path"`
	Host    string `json:"host,omitempty"`
	Backend string `json:"backend"`
	Server  string `json:"server"`
	Ms      int    `json:"ms"`
}

type SearchResult struct {
	Matches   int64       `json:"matches"`
	Scanned   int64       `json:"scanned"` // taranan satır
	Bytes     int64       `json:"bytes"`
	Files     []string    `json:"files"`
	Skipped   int         `json:"skipped"` // aralık dışında kaldığı için hiç açılmayan dosya
	Codes     []CodeCount `json:"codes"`
	IPs       []ClientRow `json:"ips"`
	Paths     []NameCount `json:"paths"`
	Hits      []SearchHit `json:"hits"` // en yeniden eskiye
	FirstAt   int64       `json:"firstAt,omitempty"`
	LastAt    int64       `json:"lastAt,omitempty"`
	Truncated bool        `json:"truncated"` // süre/boyut sınırına takıldı
	Took      int64       `json:"took"`      // ms
	Note      string      `json:"note,omitempty"`
}

// Aynı anda tek arama: ajanın CPU tavanı düşük, paralel aramalar paneli yavaşlatır.
var aramaKilidi sync.Mutex

func (q SearchQuery) bos() bool {
	return q.Path == "" && q.IP == "" && q.Status == "" && q.Method == "" && q.Backend == ""
}

// Satırı ayrıştırmadan önce ucuz bir metin elemesi. Ayrıştırma saniyede ~200 bin
// satır işlerken düz metin araması ~4 milyon satır işliyor; aranan metni içermeyen
// satırları burada elemek aramayı kat kat hızlandırıyor. Eleme yalnızca "bu satır
// kesinlikle eşleşmez" diyebildiğinde atlar; asıl süzgeç yine ayrıştırılmış kayıt
// üzerinde çalıştığı için sonuç değişmez.
func (q SearchQuery) onEleme() []string {
	var gerekli []string
	for _, v := range []string{q.Path, q.IP, q.Backend} {
		if v != "" {
			gerekli = append(gerekli, strings.ToLower(v))
		}
	}
	return gerekli
}

// Büyük/küçük harf ayrımı olmadan, bellek ayırmadan metin arama
func icerirFold(s, aranan string) bool {
	n, m := len(s), len(aranan)
	if m == 0 {
		return true
	}
	if m > n {
		return false
	}
	for i := 0; i+m <= n; i++ {
		k := 0
		for ; k < m; k++ {
			d := s[i+k]
			if d >= 'A' && d <= 'Z' {
				d += 'a' - 'A'
			}
			if d != aranan[k] {
				break
			}
		}
		if k == m {
			return true
		}
	}
	return false
}

func (q SearchQuery) uyuyor(r logRecord) bool {
	if q.Path != "" && !strings.Contains(strings.ToLower(r.RawPath), strings.ToLower(q.Path)) &&
		!strings.Contains(strings.ToLower(r.Path), strings.ToLower(q.Path)) {
		return false
	}
	if q.IP != "" && !strings.HasPrefix(r.Client, q.IP) {
		return false
	}
	if q.Method != "" && !strings.EqualFold(r.Method, q.Method) {
		return false
	}
	if q.Backend != "" && !strings.Contains(strings.ToLower(r.Backend), strings.ToLower(q.Backend)) {
		return false
	}
	if q.Status != "" {
		s := strconv.Itoa(r.Status)
		if strings.HasSuffix(q.Status, "xx") {
			if len(s) == 0 || s[0] != q.Status[0] {
				return false
			}
		} else if s != q.Status {
			return false
		}
	}
	if !q.Since.IsZero() && r.At.Before(q.Since) {
		return false
	}
	if !q.Until.IsZero() && r.At.After(q.Until) {
		return false
	}
	return true
}

// Ajanın okuduğu log dosyası ve onun döndürülmüş kopyaları, yeniden eskiye.
// Başka hiçbir dosya okunmaz.
type logDosyasi struct {
	Yol string
	Mod time.Time
}

func rotasyonlar(path string) []logDosyasi {
	dir, taban := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	girdiler, err := os.ReadDir(dir)
	if err != nil {
		return []logDosyasi{{path, time.Now()}}
	}
	var liste []logDosyasi
	for _, e := range girdiler {
		ad := e.Name()
		if ad != taban && !strings.HasPrefix(ad, taban+".") && !strings.HasPrefix(ad, taban+"-") {
			continue
		}
		fi, err := e.Info()
		if err != nil || fi.IsDir() {
			continue
		}
		liste = append(liste, logDosyasi{filepath.Join(dir, ad), fi.ModTime()})
	}
	sort.Slice(liste, func(i, j int) bool { return liste[i].Mod.After(liste[j].Mod) })
	if len(liste) == 0 {
		liste = append(liste, logDosyasi{path, time.Now()})
	}
	return liste
}

func satirOkuyucu(yol string) (*bufio.Scanner, func(), error) {
	f, err := os.Open(yol)
	if err != nil {
		return nil, nil, err
	}
	var kapat = func() { f.Close() }
	var sc *bufio.Scanner
	if strings.HasSuffix(yol, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			f.Close()
			return nil, nil, err
		}
		kapat = func() { gz.Close(); f.Close() }
		sc = bufio.NewScanner(gz)
	} else {
		sc = bufio.NewScanner(f)
	}
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	return sc, kapat, nil
}

type aramaToplayici struct {
	gerekli []string // satırda mutlaka geçmesi gereken metinler (ucuz ön eleme)
	q       SearchQuery
	res     SearchResult
	kodlar  map[int]int64
	ipler   map[string]int64
	yollar  map[string]int64
	cf      func(string) bool
}

func (t *aramaToplayici) elemedenGecer(satir string) bool {
	for _, g := range t.gerekli {
		if !icerirFold(satir, g) {
			return false
		}
	}
	return true
}

func (t *aramaToplayici) ekle(r logRecord) {
	t.res.Matches++
	t.kodlar[r.Status]++
	t.ipler[r.Client]++
	t.yollar[r.Method+" "+r.Path]++
	ms := r.At.UnixMilli()
	if t.res.FirstAt == 0 || ms < t.res.FirstAt {
		t.res.FirstAt = ms
	}
	if ms > t.res.LastAt {
		t.res.LastAt = ms
	}
	// Gösterilecek satırlar: ilk görülenler değil EN YENİLER. Dosyaların taranma
	// sırasına güvenilmez; liste iki katına çıkınca zamana göre budanır.
	t.res.Hits = append(t.res.Hits, SearchHit{At: ms, IP: r.Client, Status: r.Status, Method: r.Method,
		Path: r.RawPath, Host: r.Host, Backend: r.Backend, Server: r.Server, Ms: r.Ta})
	if len(t.res.Hits) >= aramaOrnekSiniri*2 {
		t.budaHits()
	}
}

func (t *aramaToplayici) budaHits() {
	sort.Slice(t.res.Hits, func(i, j int) bool { return t.res.Hits[i].At > t.res.Hits[j].At })
	if len(t.res.Hits) > aramaOrnekSiniri {
		t.res.Hits = t.res.Hits[:aramaOrnekSiniri]
	}
}

func (t *aramaToplayici) bitir() {
	for k, n := range t.kodlar {
		t.res.Codes = append(t.res.Codes, CodeCount{Code: k, N: n})
	}
	sort.Slice(t.res.Codes, func(i, j int) bool { return t.res.Codes[i].N > t.res.Codes[j].N })
	for _, x := range topN(t.ipler, aramaIPSiniri) {
		t.res.IPs = append(t.res.IPs, ClientRow{IP: x.Name, N: x.N, Cloudflare: t.cf(x.Name)})
	}
	t.res.Paths = topN(t.yollar, aramaYolSiniri)
	t.budaHits()
}

// Aynı anda ikinci bir arama gelirse beklemez; kuyruğa giren her arama 20 saniyeye
// kadar bağlantı tutup paneli yavaşlatıyordu.
var ErrAramaSuruyor = fmt.Errorf("başka bir arama sürüyor; birkaç saniye sonra tekrar deneyin")

// Log'da arama yapar. Panelin belleğine değil, doğrudan dosyalara bakar.
func (a *LogAnalyzer) Search(q SearchQuery) (SearchResult, error) {
	basla := time.Now()
	if !aramaKilidi.TryLock() {
		return SearchResult{}, ErrAramaSuruyor
	}
	defer aramaKilidi.Unlock()

	kaynak := a.Source()
	t := &aramaToplayici{q: q, gerekli: q.onEleme(), kodlar: map[int]int64{}, ipler: map[string]int64{},
		yollar: map[string]int64{}, cf: a.isCloudflare}
	if kaynak == "" {
		t.res.Note = "Bu sunucuda okunabilir bir HAProxy log'u bulunamadı."
		t.bitir()
		return t.res, nil
	}
	parser := a.parser.Load()
	bitis := basla.Add(aramaSureSiniri)

	if strings.HasPrefix(kaynak, "journal:") {
		t.aramaJournal(strings.TrimPrefix(kaynak, "journal:"), parser, bitis)
	} else {
		yol := strings.TrimPrefix(kaynak, "file:")
		for _, f := range rotasyonlar(yol) {
			if t.res.Truncated {
				break
			}
			// Dosyaya son yazma, aranan aralıktan önceyse içinde aralığa giren kayıt
			// olamaz; dosya hiç açılmaz. Büyük döndürülmüş dosyalarda aramayı
			// saniyelerden milisaniyelere indirir.
			if !q.Since.IsZero() && f.Mod.Before(q.Since) {
				t.res.Skipped++
				continue
			}
			// Not: "bu dosya eskiydi, ötekilere bakmayalım" gibi bir kestirme YOK.
			// Öyle bir kural dosyaların değiştirilme zamanına göre doğru sıralanmasına
			// bağlı olurdu; zamanlar birbirine yakınsa sıra karışır ve arama erken
			// durup boş sonuç döndürür. Güvenlik, dosya başına iki sınırla sağlanıyor:
			// aralık dışındaki dosya hiç açılmaz (yukarıda), açılan dosya da sondan
			// başa okunup aralığın öncesine geçilince bırakılır.
			t.aramaDosya(f.Yol, parser, bitis)
		}
	}
	t.res.Took = time.Since(basla).Milliseconds()
	t.bitir()
	if t.res.Truncated && t.res.Note == "" {
		t.res.Note = "Süre sınırına ulaşıldı. Log en yeniden eskiye tarandığı için yukarıdaki sonuçlar en güncel kayıtları kapsar; daha eskiler taranamadı. Daha dar bir zaman aralığı seçerek tamamını tarayabilirsin."
	}
	return t.res, nil
}

func (t *aramaToplayici) aramaDosya(yol string, parser *LogParser, bitis time.Time) {
	t.res.Files = append(t.res.Files, filepath.Base(yol))
	if strings.HasSuffix(yol, ".gz") {
		t.aramaGz(yol, parser, bitis)
		return
	}
	// Ön eleme satırların çoğunu ayrıştırmadan atar. Zaman sınırını bilmek için
	// zamana ihtiyaç olduğundan her 500 satırda bir örnek satır tam ayrıştırılır.
	n, eski := 0, 0
	geriSatirlar(yol, func(satir string) bool {
		t.res.Scanned++
		t.res.Bytes += int64(len(satir)) + 1
		n++
		if n%2000 == 0 && (time.Now().After(bitis) || t.res.Bytes > aramaBaytSiniri) {
			t.res.Truncated = true
			return false
		}
		gecer := t.elemedenGecer(satir)
		zamanBak := !t.q.Since.IsZero() && n%500 == 0
		if !gecer && !zamanBak {
			return true
		}
		rec, ok := parser.Parse(satir)
		if !ok {
			return true
		}
		if !t.q.Since.IsZero() {
			if rec.At.Before(t.q.Since) {
				// Sondan başa okunuyor: aralığın öncesine geçilmiş olabilir. Satırlar
				// tam sıralı olmayabileceği için birkaç örnek üst üste eski olmalı.
				if eski++; eski >= 3 {
					return false
				}
				return true
			}
			eski = 0
		}
		if gecer && t.q.uyuyor(rec) {
			t.ekle(rec)
		}
		return true
	})
}

func (t *aramaToplayici) aramaGz(yol string, parser *LogParser, bitis time.Time) {
	sc, kapat, err := satirOkuyucu(yol)
	if err != nil {
		return
	}
	defer kapat()
	n := 0
	for sc.Scan() {
		satir := sc.Text()
		t.res.Scanned++
		t.res.Bytes += int64(len(satir)) + 1
		if n++; n%2000 == 0 && (time.Now().After(bitis) || t.res.Bytes > aramaBaytSiniri) {
			t.res.Truncated = true
			return
		}
		if !t.elemedenGecer(satir) {
			continue
		}
		rec, ok := parser.Parse(satir)
		if !ok {
			continue
		}
		if t.q.uyuyor(rec) {
			t.ekle(rec)
		}
	}
}

func (t *aramaToplayici) aramaJournal(tag string, parser *LogParser, bitis time.Time) {
	args := []string{"--no-pager", "-q", "-o", "cat", "-t", tag}
	if !t.q.Since.IsZero() {
		args = append(args, "--since", t.q.Since.Format("2006-01-02 15:04:05"))
	} else {
		args = append(args, "-n", "2000000")
	}
	if !t.q.Until.IsZero() {
		args = append(args, "--until", t.q.Until.Format("2006-01-02 15:04:05"))
	}
	ctx, cancel := context.WithDeadline(context.Background(), bitis)
	defer cancel()
	cmd := exec.CommandContext(ctx, "journalctl", args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		return
	}
	t.res.Files = append(t.res.Files, "journald:"+tag)
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	n := 0
	for sc.Scan() {
		t.res.Scanned++
		t.res.Bytes += int64(len(sc.Bytes())) + 1
		if n++; n%2000 == 0 && (time.Now().After(bitis) || t.res.Bytes > aramaBaytSiniri) {
			t.res.Truncated = true
			break
		}
		if rec, ok := parser.Parse(sc.Text()); ok && t.q.uyuyor(rec) {
			t.ekle(rec)
		}
	}
	cancel()
	_ = cmd.Wait()
}

// HTTP parametrelerinden sorgu kurar
func searchQueryFrom(get func(string) string) (SearchQuery, error) {
	q := SearchQuery{
		Path:    strings.TrimSpace(get("path")),
		IP:      strings.TrimSpace(get("ip")),
		Status:  strings.TrimSpace(get("status")),
		Method:  strings.TrimSpace(get("method")),
		Backend: strings.TrimSpace(get("backend")),
	}
	if q.bos() {
		return q, fmt.Errorf("aranacak bir şey yazın: adres, IP, durum kodu, yöntem ya da backend")
	}
	if len(q.Path) > 200 || len(q.IP) > 60 || len(q.Backend) > 80 {
		return q, fmt.Errorf("arama metni çok uzun")
	}
	if q.Status != "" {
		s := q.Status
		gecerli := len(s) == 3 && s[0] >= '1' && s[0] <= '5'
		if gecerli && strings.HasSuffix(s, "xx") {
			// 5xx gibi
		} else if gecerli {
			if _, err := strconv.Atoi(s); err != nil {
				gecerli = false
			}
		}
		if !gecerli {
			return q, fmt.Errorf("durum kodu 200 ya da 5xx biçiminde olmalı")
		}
	}
	if v := get("hours"); v != "" {
		h, err := strconv.Atoi(v)
		if err != nil || h < 0 || h > 24*365 {
			return q, fmt.Errorf("zaman aralığı geçersiz")
		}
		if h > 0 {
			q.Since = time.Now().Add(-time.Duration(h) * time.Hour)
		}
	}
	return q, nil
}

// Dosyayı SONDAN BAŞA okur. Log'lar zaman sıralı olduğu için en yeni kayıtlar dosyanın
// sonundadır; baştan okumak, büyük bir dosyada aranan aralığa hiç ulaşamadan süre
// sınırına takılmak demekti. Geriye okuyunca "son 1 saat" araması dosya ne kadar
// büyük olursa olsun hızlı biter.
func geriSatirlar(yol string, fn func(satir string) bool) error {
	f, err := os.Open(yol)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	const parca = 1 << 20
	kalan := fi.Size()
	var kuyruk []byte // parçanın başındaki yarım satır
	buf := make([]byte, parca)
	for kalan > 0 {
		n := int64(parca)
		if kalan < n {
			n = kalan
		}
		kalan -= n
		if _, err := f.Seek(kalan, 0); err != nil {
			return err
		}
		if _, err := io.ReadFull(f, buf[:n]); err != nil {
			return err
		}
		veri := append(buf[:n:n], kuyruk...)
		// İlk satır yarım olabilir; dosyanın başına geldiysek değildir
		bas := 0
		if kalan > 0 {
			if i := bytes.IndexByte(veri, '\n'); i >= 0 {
				kuyruk = append([]byte(nil), veri[:i]...)
				bas = i + 1
			} else {
				kuyruk = append([]byte(nil), veri...)
				continue
			}
		} else {
			kuyruk = nil
		}
		govde := veri[bas:]
		for son := len(govde); son > 0; {
			i := bytes.LastIndexByte(govde[:son], '\n')
			satir := govde[i+1 : son]
			son = i
			if len(satir) > 0 && !fn(string(satir)) {
				return nil
			}
			if i < 0 {
				break
			}
		}
	}
	if len(kuyruk) > 0 {
		fn(string(kuyruk))
	}
	return nil
}
