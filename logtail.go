package main

// HAProxy log dosyasını salt okunur izler (tail -f gibi) ve dakikalık özet çıkarır.
// Sorgu parametreleri (?...) hiç saklanmaz: cihaz kimliği, token gibi veriler bellekte tutulmaz.

import (
	"bufio"
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// option httplog biçimi:
// %ci:%cp [%tr] %ft %b/%s %TR/%Tw/%Tc/%Tr/%Ta %ST %B %CC %CS %tsc %ac/%fc/%bc/%sc/%rc %sq/%bq %hr %hs %{+Q}r
// Alan adı gibi görünen değer: harf içeren bir uzantı şart (böylece IP'ler ve User-Agent elenir)
var reHostName = regexp.MustCompile(`^(?i)[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*\.[a-z]{2,63}(:\d{1,5})?$`)
var reHostIP = regexp.MustCompile(`^\d{1,3}(\.\d{1,3}){3}(:\d{1,5})?$`)

const (
	KindServed   = "served"   // bir sunucu yanıtladı
	KindDenied   = "denied"   // http-request deny ile engellendi
	KindNoMatch  = "nomatch"  // hiçbir backend eşleşmedi
	KindNoServer = "noserver" // backend seçildi ama çalışan sunucu yok
	KindRedirect = "redirect" // HAProxy yönlendirdi (ör. http -> https)
	KindProxy    = "proxy"    // HAProxy'nin kendisi yanıtladı (400, 408, kopma...)
	KindTCP      = "tcp"      // TCP modu satırı (HTTP ayrıntısı yok)
)

type logRecord struct {
	At       time.Time
	Client   string
	Frontend string
	Backend  string
	Server   string
	Ta       int
	Status   int
	Term     string
	Method   string
	Path     string // sayılar {id} olarak birleştirilmiş, sorgusuz
	RawPath  string // gerçek yol, sorgusuz (örnek göstermek için)
	Host     string // log'da varsa alan adı, yoksa boş
	TLS      bool   // frontend adı "~" ile bitiyorsa bağlantı SSL
	Kind     string
}

// Log satırında alan adı hangi yoldan geçiyor olabilir, sırayla bakılır:
//  1. İstek satırında tam adres (HTTP/2 ya da proxy isteği): "GET https://alan.com/yol HTTP/2.0"
//  2. Yakalanan başlıklar: capture request header Host / http-request capture req.hdr(host)
//  3. İstek satırından sonraki alanlar: option httpslog'daki SNI ya da log-format'a eklenmiş host
func extractHost(captures, uri, trailing string) string {
	if i := strings.Index(uri, "://"); i >= 0 {
		rest := uri[i+3:]
		if j := strings.IndexAny(rest, "/?"); j >= 0 {
			rest = rest[:j]
		}
		if rest != "" {
			return strings.ToLower(rest)
		}
	}
	var vals []string
	for _, blk := range strings.Split(captures, "} ") {
		blk = strings.Trim(strings.TrimSpace(blk), "{}")
		if blk == "" {
			continue
		}
		vals = append(vals, strings.Split(blk, "|")...)
	}
	for _, v := range vals {
		if v = strings.TrimSpace(v); reHostName.MatchString(v) {
			return strings.ToLower(v)
		}
	}
	// Tek yakalanan değer bir IP ise o büyük ihtimalle Host başlığıdır (istek IP ile yapılmış)
	if len(vals) == 1 && reHostIP.MatchString(strings.TrimSpace(vals[0])) {
		return strings.TrimSpace(vals[0])
	}
	// Sonraki alanlarda tam URL taşıyanlar (Referer, Origin gibi) hedef değildir, atlanır
	for _, field := range strings.Fields(trailing) {
		if strings.Contains(field, "://") {
			continue
		}
		for _, tok := range strings.FieldsFunc(field, func(r rune) bool { return r == '/' || r == '"' || r == ',' || r == '{' || r == '}' || r == '|' }) {
			if reHostName.MatchString(tok) {
				return strings.ToLower(tok)
			}
		}
	}
	return ""
}

func rawPathOf(uri string) string {
	if i := strings.IndexAny(uri, "?#"); i >= 0 {
		uri = uri[:i]
	}
	if i := strings.Index(uri, "://"); i >= 0 {
		rest := uri[i+3:]
		if j := strings.Index(rest, "/"); j >= 0 {
			uri = rest[j:]
		} else {
			uri = "/"
		}
	}
	if len(uri) > 200 {
		uri = uri[:200] + "…"
	}
	return uri
}

// Config bilinmeden, hazır biçimlerle okuma (testler ve kurulum öncesi kontrol için)
func parseLogLine(line string) (logRecord, bool) {
	return defaultParser.Parse(line)
}

func classify(r logRecord) string {
	if r.Server != "<NOSRV>" {
		return KindServed
	}
	switch {
	case r.Status == 403:
		return KindDenied
	case r.Status == 503 && r.Backend == r.Frontend:
		return KindNoMatch
	case r.Status == 503:
		return KindNoServer
	case r.Status >= 300 && r.Status < 400:
		return KindRedirect
	}
	return KindProxy
}

func normPath(uri string) string {
	if i := strings.IndexAny(uri, "?#"); i >= 0 {
		uri = uri[:i]
	}
	if i := strings.Index(uri, "://"); i >= 0 { // absolute-form
		rest := uri[i+3:]
		if j := strings.Index(rest, "/"); j >= 0 {
			uri = rest[j:]
		} else {
			uri = "/"
		}
	}
	segs := strings.Split(uri, "/")
	for i, s := range segs {
		if isID(s) {
			segs[i] = "{id}"
		}
	}
	p := strings.Join(segs, "/")
	if len(p) > 120 {
		p = p[:120] + "…"
	}
	return p
}

func isID(s string) bool {
	if s == "" {
		return false
	}
	digits, hex := 0, 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits++
		}
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') || r == '-' {
			hex++
		}
	}
	return digits == len(s) || (len(s) >= 16 && hex == len(s) && digits > 0)
}

// ---------- Dakikalık özet ----------

const maxKeysPerBucket = 5000

type pathKey struct{ Backend, Method, Path string }
type blockKey struct{ Kind, Method, Path string }

type pathAgg struct {
	N, S2, S3, S4, S5, SumTa, NTa int64
	Codes                         map[int]int64     // spesifik kod (301, 404, 502...) -> sayı; 3xx ve üstü
	Kind                          string            // bu yoldaki kayıtların türü; karışıksa ""
	Err                           *errDetail        // 2xx dışındaki isteklerin ayrıntısı
	IPs                           map[string]*ipAgg // bu adrese en çok istek yapan IP'ler (her yanıt kodu için)
}

func (pa *pathAgg) addKind(k string) {
	if pa.N == 0 {
		pa.Kind = k
	} else if pa.Kind != k {
		pa.Kind = "" // aynı yol hem yanıtlanmış hem engellenmiş olabilir
	}
}

type blockAgg struct {
	N   int64
	Det *errDetail
}

// Hata ve engelleme ayrıntısı: nereye (adres), kimden (IP), hangi gerçek yol.
// Bellek sınırlı kalsın diye her dakikalık kovada en fazla maxDetailKeys satırın,
// her listede en fazla maxDetailValues değerin ayrıntısı tutulur; gerisi "(diğer)" olur.
const (
	maxDetailKeys   = 300
	maxPathIPKeys   = 400 // IP dökümü tutulan yol sayısı (dakika başına)
	maxPathIPs      = 20  // yol başına saklanan farklı IP sayısı
	maxDetailValues = 6
	otherKey        = "(diğer)"
)

type errDetail struct {
	Origins map[string]int64 // "https://alan.com"; alan adı log'da yoksa ""
	IPs     map[string]int64
	Samples map[string]int64 // gerçek yollar (sorgusuz)
}

func newErrDetail() *errDetail {
	return &errDetail{Origins: map[string]int64{}, IPs: map[string]int64{}, Samples: map[string]int64{}}
}

func addCapped(m map[string]int64, k string) {
	if _, ok := m[k]; ok || len(m) < maxDetailValues {
		m[k]++
	} else {
		m[otherKey]++
	}
}

func originOf(r logRecord) string {
	if r.Host == "" {
		return ""
	}
	if r.TLS {
		return "https://" + r.Host
	}
	return "http://" + r.Host
}

func (d *errDetail) add(r logRecord) {
	addCapped(d.Origins, originOf(r))
	addCapped(d.IPs, r.Client)
	addCapped(d.Samples, r.RawPath)
}

func (d *errDetail) merge(o *errDetail) {
	for k, v := range o.Origins {
		d.Origins[k] += v
	}
	for k, v := range o.IPs {
		d.IPs[k] += v
	}
	for k, v := range o.Samples {
		d.Samples[k] += v
	}
}

func (pa *pathAgg) addCodeN(code int, n int64) {
	if pa.Codes == nil {
		pa.Codes = map[int]int64{}
	}
	pa.Codes[code] += n
}

func (pa *pathAgg) addCode(code int) {
	if code < 300 || code == 0 {
		return
	}
	if pa.Codes == nil {
		pa.Codes = map[int]int64{}
	}
	if len(pa.Codes) < 16 || pa.Codes[code] > 0 {
		pa.Codes[code]++
	} else {
		pa.Codes[0]++ // taşma: "diğer"
	}
}

type clientAgg struct {
	N, Blocked int64
	Paths      map[string]int64 // "GET /yol" -> sayı; bellek için sınırlı sayıda IP'de tutulur
}

// Kaç IP için adres dökümü tutulacağı ve IP başına kaç farklı adres saklanacağı.
// Trafik birkaç IP'de yoğunlaştığı için bu sınırlar pratikte listenin başını etkilemez.
const (
	maxClientDetail = 200
	maxClientPaths  = 12
)

// Bir IP'nin bir adrese yaptığı istekler. Sınıf (2xx) yerine gerçek kod (200, 404)
// saklanır ama bellek için sabit boyutlu: en fazla 6 farklı kod tutulur, fazlası
// "diğer" altında toplanır. Tek bir IP'nin tek bir adreste 6'dan fazla farklı kod
// alması nadirdir; toplam sayı her durumda doğru kalır.
const maxIPCodes = 6

type ipCode struct {
	Code int16 `json:"code"`
	N    int32 `json:"n"`
}

type ipAgg struct {
	N     int64
	Codes [maxIPCodes]ipCode
	Diger int32 // listeye sığmayan kodların toplamı
}

func (v *ipAgg) kodEkle(kod int, n int32) {
	if kod <= 0 {
		v.Diger += n
		return
	}
	enAz := -1
	for i := range v.Codes {
		switch {
		case v.Codes[i].Code == int16(kod):
			v.Codes[i].N += n
			return
		case v.Codes[i].Code == 0:
			v.Codes[i] = ipCode{int16(kod), n}
			return
		case enAz < 0 || v.Codes[i].N < v.Codes[enAz].N:
			enAz = i
		}
	}
	// Yuvalar dolu. Yer değiştirme yapılmaz çünkü akış hâlinde gelen kayıtlar birer
	// birer sayıldığı için yeni bir kod tek başına hiçbir zaman baskın görünmez;
	// sayıyı "diğer"e eklemek gerçeği olduğu gibi yansıtır.
	_ = enAz
	v.Diger += n
}

// Görüntüleme için: kodlar çoktan aza, boş yuvalar atılır
func (v *ipAgg) kodListesi() []ipCode {
	out := make([]ipCode, 0, maxIPCodes)
	for _, c := range v.Codes {
		if c.Code != 0 {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].N > out[j].N })
	return out
}

// Sınıra takılınca ilk görülen IP'leri değil EN ÇOK İSTEK YAPANLARI tutar:
// harita iki katına çıkınca sayıya göre budanır, elenenler "(diğer)" altında toplanır.
// Böylece geç başlayan yoğun bir IP de listeye girebilir.
func addIP(m map[string]*ipAgg, ip string, kod int, limit int) {
	v := m[ip]
	if v == nil {
		if len(m) >= limit*2 {
			budaIP(m, limit)
			v = m[ip]
		}
		if v == nil {
			v = &ipAgg{}
			m[ip] = v
		}
	}
	v.N++
	v.kodEkle(kod, 1)
}

// Başka bir kaydın kodlarını bu kayda ekler (N hariç)
func (v *ipAgg) birlestir(o *ipAgg) {
	for _, c := range o.Codes {
		if c.Code != 0 {
			v.kodEkle(int(c.Code), c.N)
		}
	}
	v.Diger += o.Diger
}

func budaIP(m map[string]*ipAgg, limit int) {
	tip := make([]string, 0, len(m))
	for k := range m {
		if k != otherKey {
			tip = append(tip, k)
		}
	}
	sort.Slice(tip, func(i, j int) bool { return m[tip[i]].N > m[tip[j]].N })
	if len(tip) <= limit {
		return
	}
	diger := m[otherKey]
	if diger == nil {
		diger = &ipAgg{}
		m[otherKey] = diger
	}
	for _, k := range tip[limit:] {
		v := m[k]
		diger.N += v.N
		diger.birlestir(v)
		delete(m, k)
	}
}

func addCappedN(m map[string]int64, k string, limit int) {
	if _, ok := m[k]; ok || len(m) < limit {
		m[k]++
	} else {
		m[otherKey]++
	}
}

// Backend başına döküm: "bu backend'e hiç trafik gitmemeli" sorusunu cevaplar
type backendAgg struct {
	N, S2, S3, S4, S5, Blocked int64
	IPs                        map[string]int64
}

const (
	maxBackendKeys = 100 // kaç backend için döküm tutulacağı
	maxBackendIPs  = 30  // backend başına kaç farklı IP saklanacağı
)

type bucket struct {
	kinds      map[string]int64
	paths      map[pathKey]*pathAgg
	blocked    map[blockKey]*blockAgg
	clients    map[string]*clientAgg
	backends   map[string]*backendAgg
	classes    [4]int64 // 2xx, 3xx, 4xx, 5xx; yollar sadeleştirilse de kalır
	level      int      // 0 tam ayrıntı, 1 sadeleşmiş, 2 en sade
	withHost   int64    // alan adı bulunan satır sayısı
	detailKeys int      // ayrıntısı tutulan satır sayısı (sınır: maxDetailKeys)
	pathIPKeys int      // IP dökümü tutulan yol sayısı (sınır: maxPathIPKeys)
	clientKeys int      // adres dökümü tutulan IP sayısı (sınır: maxClientDetail)
}

func (b *bucket) backend(ad string) *backendAgg {
	if ba := b.backends[ad]; ba != nil {
		return ba
	}
	if len(b.backends) >= maxBackendKeys {
		return nil
	}
	ba := &backendAgg{IPs: map[string]int64{}}
	b.backends[ad] = ba
	return ba
}

// Kovada yer varsa yeni bir ayrıntı açar; yoksa nil (sayılar yine tutulur, ayrıntı tutulmaz)
func (b *bucket) detail(cur *errDetail) *errDetail {
	if cur != nil {
		return cur
	}
	if b.detailKeys >= maxDetailKeys {
		return nil
	}
	b.detailKeys++
	return newErrDetail()
}

func newBucket() *bucket {
	return &bucket{kinds: map[string]int64{}, paths: map[pathKey]*pathAgg{}, blocked: map[blockKey]*blockAgg{},
		clients: map[string]*clientAgg{}, backends: map[string]*backendAgg{}}
}

type LogAnalyzer struct {
	source     string // "file:/yol", "journal:haproxy" ya da "" (henüz yok)
	path       string
	cfNets     []*net.IPNet
	parser     atomic.Pointer[LogParser]
	mu         sync.Mutex
	stop       chan struct{} // kaynak değişince eski okuyucuyu durdurur
	changed    chan struct{}
	retention  int          // dakika
	yuklenenAt int64        // diskten yüklenen geçmişin kapsadığı son satırın zamanı (ms); öncesi tekrar sayılmaz
	sonSatirAt int64        // sayılan en yeni satırın zamanı (ms); diske yazılır
	detay      int          // tam ayrıntının (tam adres, IP dökümü) saklandığı dakika
	liste      int          // yol ve IP listelerinin saklandığı dakika
	butce      int64        // ayrıntı için bellek bütçesi (bayt); aşılırsa en eski ayrıntı bırakılır
	tepe       int64        // görülen en yüksek ayrıntı ağırlığı; bellek iadesini tetiklemek için
	sonIadeDk  int64        // en son bellek iadesinin yapıldığı dakika
	simdiDk    func() int64 // şimdiki dakika; testlerde sahte saat verilebilir
	dirty      bool
	buckets    map[int64]*bucket
	lines      int64
	parsed     int64
	tcpLines   int64
	lastAt     time.Time
	lastLine   time.Time // kaynaktan en son satır geldiği an (okunmasa bile)
	lastErr    string
	// okunamayan trafik satırları: dakikalık sayım + son örnekler
	unparsed map[int64]int64
	samples  []string
}

var reTrafficLike = regexp.MustCompile(`^\S+:\d+ |^\d{1,3}(\.\d{1,3}){3}[ :,]|^[\[{]|\d{2}/\w{3}/\d{4}:\d{2}:\d{2}:\d{2}`)
var reQuery = regexp.MustCompile(`\?[^ "]*`)

func normSource(source string) string {
	if source == "" || strings.HasPrefix(source, "journal:") || strings.HasPrefix(source, "file:") {
		return source
	}
	return "file:" + source
}

func NewLogAnalyzer(source, cloudflareList string) *LogAnalyzer {
	source = normSource(source)
	_, path, _ := strings.Cut(source, ":")
	nets := append(builtinCloudflareNets(), loadCIDRs(cloudflareList)...)
	a := &LogAnalyzer{source: source, path: path, cfNets: nets, retention: detayDakika, detay: detayDakika, liste: listeDakika,
		simdiDk: func() int64 { return time.Now().Unix() / 60 }, buckets: map[int64]*bucket{},
		stop: make(chan struct{}), changed: make(chan struct{}, 1), unparsed: map[int64]int64{}}
	a.parser.Store(defaultParser)
	return a
}

// Config değişince yeni ayrıştırıcı; okuma kesilmeden devreye girer. Eski biçime göre
// "okunamadı" sayılan satırlar yeni durumu yansıtmadığı için o sayaç sıfırlanır.
// Saklama süresi (dakika); ajan başlarken ayarlanır
func (a *LogAnalyzer) SetRetention(dk int) {
	if dk < detayDakika {
		dk = detayDakika
	}
	a.mu.Lock()
	a.retention = dk
	a.mu.Unlock()
}

func (a *LogAnalyzer) Retention() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.retention
}

func (a *LogAnalyzer) SetParser(p *LogParser) {
	a.parser.Store(p)
	a.mu.Lock()
	a.unparsed = map[int64]int64{}
	a.samples = nil
	a.mu.Unlock()
}

// Log kaynağını değiştirir; eski okuyucu durur, yenisi başlar.
func (a *LogAnalyzer) SetSource(source string) {
	source = normSource(source)
	a.mu.Lock()
	defer a.mu.Unlock()
	if source == a.source {
		return
	}
	a.source = source
	_, a.path, _ = strings.Cut(source, ":")
	a.lastErr = ""
	close(a.stop)
	a.stop = make(chan struct{})
	select {
	case a.changed <- struct{}{}:
	default:
	}
}

func (a *LogAnalyzer) Source() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.source
}

// Kaynaktan en son ne zaman satır geldi (kaynak sessiz mi?)
func (a *LogAnalyzer) LastLine() time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastLine
}

func (a *LogAnalyzer) handleLine(line string) {
	line = strings.TrimRight(line, "\r\n")
	now := time.Now()
	a.mu.Lock()
	a.lines++
	a.lastLine = now
	a.mu.Unlock()
	rec, ok := a.parser.Load().Parse(line)
	if ok {
		if rec.Kind == KindTCP {
			a.mu.Lock()
			a.tcpLines++
			a.mu.Unlock()
			return
		}
		a.add(rec)
		return
	}
	msg := syslogMessage(line)
	if !reTrafficLike.MatchString(msg) {
		return // "Server x is DOWN" gibi olay satırı; okunamayan sayılmaz
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.unparsed[now.Unix()/60]++
	if len(a.samples) >= 3 {
		a.samples = a.samples[1:]
	}
	smp := reQuery.ReplaceAllString(msg, "?…") // sorgudaki token/kimlikler saklanmaz
	if len(smp) > 400 {
		smp = smp[:400] + "…"
	}
	a.samples = append(a.samples, smp)
}

// journald'dan okuma: "journalctl -f" çıktısını satır satır işler, kapanırsa yeniden başlatır.
func (a *LogAnalyzer) runJournal(tag string, stop chan struct{}) {
	n := "2000"
	for {
		select {
		case <-stop:
			return
		default:
		}
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			select {
			case <-stop:
				cancel()
			case <-ctx.Done():
			}
		}()
		cmd := exec.CommandContext(ctx, "journalctl", "--no-pager", "-q", "-o", "cat", "-f", "-n", n, "-t", tag)
		out, err := cmd.StdoutPipe()
		if err == nil {
			err = cmd.Start()
		}
		if err != nil {
			cancel()
			a.setErr("journalctl başlatılamadı: " + err.Error())
			if sleepOrStop(10*time.Second, stop) {
				return
			}
			continue
		}
		a.setErr("")
		n = "0"
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 256<<10), 1<<20)
		for sc.Scan() {
			a.handleLine(sc.Text())
		}
		_ = cmd.Wait()
		cancel()
		a.setErr("journalctl kapandı, yeniden başlatılıyor")
		if sleepOrStop(5*time.Second, stop) {
			return
		}
	}
}

func sleepOrStop(d time.Duration, stop chan struct{}) bool {
	select {
	case <-stop:
		return true
	case <-time.After(d):
		return false
	}
}

func loadCIDRs(file string) []*net.IPNet {
	if file == "" {
		return nil
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	var out []*net.IPNet
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if !strings.Contains(l, "/") {
			if strings.Contains(l, ":") {
				l += "/128"
			} else {
				l += "/32"
			}
		}
		if _, n, err := net.ParseCIDR(l); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func (a *LogAnalyzer) isCloudflare(ip string) bool {
	p := net.ParseIP(ip)
	if p == nil {
		return false
	}
	for _, n := range a.cfNets {
		if n.Contains(p) {
			return true
		}
	}
	return false
}

// Eski dakikalar iki kademede sadeleşir: 1 saatten sonra ayrıntılar (tam adres, IP dökümü)
// düşer, 6 saatten sonra yol ve IP listeleri de düşer. Sayılar her zaman eksiksiz kalır.
const (
	detayDakika = 60  // varsayılan: bu süreye kadar tam ayrıntı
	listeDakika = 360 // varsayılan: bu süreye kadar yol ve IP listeleri
)

// Not: "zaten sadeleştirildi" diye atlamaz. Geç gelen log satırları (ajan açılışta
// birikmiş log'u okurken ya da log gecikmeliyse) eski bir dakikaya yazılabiliyor;
// o kova sadeleştirilmiş olsa bile yeniden dolabiliyor. Bu yüzden her bakımda
// hedef durum yeniden uygulanır.
func (b *bucket) sadelestir(hedef int) {
	if hedef >= 1 {
		for _, v := range b.paths {
			v.Err = nil // tam adres/gerçek yol ayrıntısı düşer; IP dökümü (küçük) kalır
		}
		for _, v := range b.blocked {
			v.Det = nil
		}
		for _, v := range b.clients {
			v.Paths = nil
		}
		b.enYogun(50, 30)
	}
	if hedef >= 2 {
		if len(b.paths) > 0 {
			b.paths = map[pathKey]*pathAgg{}
		}
		if len(b.blocked) > 0 {
			b.blocked = map[blockKey]*blockAgg{}
		}
		if len(b.clients) > 0 {
			b.clients = map[string]*clientAgg{}
		}
	}
	if hedef > b.level {
		b.level = hedef
	}
}

// Kovada yalnızca en yoğun yolları ve IP'leri bırakır.
// Map'ten anahtar silmek Go'da iç diziyi küçültmediği için map'ler yeniden kurulur.
func (b *bucket) enYogun(yol, ip int) {
	if len(b.paths) > yol {
		tip := make([]pathKey, 0, len(b.paths))
		for k := range b.paths {
			tip = append(tip, k)
		}
		sort.Slice(tip, func(i, j int) bool { return b.paths[tip[i]].N > b.paths[tip[j]].N })
		yeni := make(map[pathKey]*pathAgg, yol)
		for _, k := range tip[:yol] {
			yeni[k] = b.paths[k]
		}
		b.paths = yeni
	}
	if len(b.blocked) > yol {
		tip := make([]blockKey, 0, len(b.blocked))
		for k := range b.blocked {
			tip = append(tip, k)
		}
		sort.Slice(tip, func(i, j int) bool { return b.blocked[tip[i]].N > b.blocked[tip[j]].N })
		yeni := make(map[blockKey]*blockAgg, yol)
		for _, k := range tip[:yol] {
			yeni[k] = b.blocked[k]
		}
		b.blocked = yeni
	}
	if len(b.clients) > ip {
		tip := make([]string, 0, len(b.clients))
		for k := range b.clients {
			tip = append(tip, k)
		}
		sort.Slice(tip, func(i, j int) bool { return b.clients[tip[i]].N > b.clients[tip[j]].N })
		yeni := make(map[string]*clientAgg, ip)
		for _, k := range tip[:ip] {
			yeni[k] = b.clients[k]
		}
		b.clients = yeni
	}
}

// Yaşlanan kovaları sadeleştirir, saklama süresini aşanları siler
// Go, boşalan belleği işletim sistemine kendiliğinden hemen iade etmiyor: ölçümde
// saldırı bittikten sonra ajanın içinde 9 MB kalmasına rağmen RSS 269 MB'da duruyordu.
// systemd'nin bellek tavanı RSS üzerinden uygulandığı için büyük bir sadeleştirmeden
// sonra iade tetikleniyor. Arka planda ve tek seferlik; okuma akışını durdurmaz.
var osIadeBekliyor atomic.Bool
var iadeSayaci atomic.Int64 // yalnızca testler için: iade kaç kez tetiklendi

func osBellekIadesi() {
	if osIadeBekliyor.Swap(true) {
		return
	}
	go func() {
		defer osIadeBekliyor.Store(false)
		debug.FreeOSMemory()
	}()
}

func (a *LogAnalyzer) toplamAgirlik() int64 {
	var t int64
	for _, b := range a.buckets {
		t += b.agirlik()
	}
	return t
}

func (a *LogAnalyzer) bakim() { a.bakimAt(a.simdiDk()) }

func (a *LogAnalyzer) bakimAt(simdi int64) {
	for k, b := range a.buckets {
		yas := simdi - k
		switch {
		case yas > int64(a.retention):
			delete(a.buckets, k)
		case yas > int64(a.liste):
			b.sadelestir(2)
		case yas > int64(a.detay):
			b.sadelestir(1)
		}
	}
	a.butceUygula()
	// Bellek tek seferde değil, dakika dakika azalıyor; bu yüzden anlık fark değil
	// tepe değerden ne kadar gerilendiğine bakılır. Saldırı bittikten sonra
	// iade bir kez tetiklenir, her bakımda değil.
	w := a.toplamAgirlik()
	if w > a.tepe {
		a.tepe = w
	}
	// Büyük bir gerileme olduysa hemen, küçük gerilemelerde en fazla 10 dakikada bir.
	buyuk := a.tepe-w > 64<<20
	periyodik := simdi-a.sonIadeDk >= 10 && a.tepe-w > 8<<20
	if buyuk || periyodik {
		a.tepe, a.sonIadeDk = w, simdi
		iadeSayaci.Add(1)
		osBellekIadesi()
	}
}

// Ayrıntı için bellek bütçesi. Tarama saldırılarında her istek benzersiz bir adres
// olabildiği için süre tabanlı sınır tek başına yetmez; bu bütçe aşılırsa en eski
// ayrıntı bırakılır. Sayılar hiçbir durumda kaybolmaz.
func (a *LogAnalyzer) SetBudget(bayt int64) {
	a.mu.Lock()
	a.butce = bayt
	a.mu.Unlock()
}

// Kovanın kabaca kapladığı bellek. Katsayılar ölçümden geldi (bkz. bellek_test.go):
// tam ayrıntılı bir yol kaydı ~600 bayt, sadeleşmiş ~250 bayt.
func (b *bucket) agirlik() int64 {
	switch b.level {
	case 0:
		return int64(len(b.paths))*1600 + int64(len(b.blocked))*400 + int64(len(b.clients))*250 + int64(len(b.backends))*200
	case 1:
		return int64(len(b.paths))*1200 + int64(len(b.blocked))*150 + int64(len(b.clients))*120 + int64(len(b.backends))*200
	default:
		return int64(len(b.backends))*200 + 500
	}
}

// Bütçe aşılırsa en eskiden başlayarak ayrıntıyı bırakır
func (a *LogAnalyzer) butceUygula() {
	if a.butce <= 0 {
		return
	}
	var toplam int64
	for _, b := range a.buckets {
		toplam += b.agirlik()
	}
	if toplam <= a.butce {
		return
	}
	for _, hedef := range []int{1, 2} {
		var sira []int64
		for k, b := range a.buckets {
			if b.level < hedef {
				sira = append(sira, k)
			}
		}
		sort.Slice(sira, func(i, j int) bool { return sira[i] < sira[j] }) // en eski önce
		for _, k := range sira {
			if toplam <= a.butce {
				return
			}
			b := a.buckets[k]
			once := b.agirlik()
			b.sadelestir(hedef)
			toplam -= once - b.agirlik()
		}
	}
}

// Ayrıntının ve listelerin gerçekte kaç dakikayı kapsadığı (bütçe yüzünden kısalmış olabilir)
func (a *LogAnalyzer) kapsam(simdi int64) (detay, liste int) {
	enEski0, enEski1 := int64(-1), int64(-1)
	for k, b := range a.buckets {
		if b.level == 0 && (enEski0 < 0 || k < enEski0) {
			enEski0 = k
		}
		if b.level <= 1 && (enEski1 < 0 || k < enEski1) {
			enEski1 = k
		}
	}
	if enEski0 >= 0 {
		detay = int(simdi-enEski0) + 1
	}
	if enEski1 >= 0 {
		liste = int(simdi-enEski1) + 1
	}
	return detay, liste
}

// Ayrıntı ve liste pencereleri; bellek kullanımını doğrudan belirler
func (a *LogAnalyzer) SetDetailWindows(detayDk, listeDk int) {
	if detayDk < 5 {
		detayDk = 5
	}
	if listeDk < detayDk {
		listeDk = detayDk
	}
	a.mu.Lock()
	a.detay, a.liste = detayDk, listeDk
	a.mu.Unlock()
}

func (a *LogAnalyzer) add(r logRecord) {
	min := r.At.Unix() / 60
	a.mu.Lock()
	defer a.mu.Unlock()
	// Ajan açılırken geçmişi diskten yükler, ayrıca panel boş başlamasın diye log
	// dosyasının sonunu yeniden okur. Diske yazılmış geçmişin kapsadığı satırlar
	// tekrar sayılmaz. Sınır dakika değil satırın tam zamanıdır: böylece ajan aynı
	// dakika içinde yeniden başlasa bile yeni satırlar kaybolmaz, ajan kapalıyken
	// yazılan satırlar da açılışta sayılır.
	ms := r.At.UnixMilli()
	if ms <= a.yuklenenAt {
		return
	}
	if ms > a.sonSatirAt {
		a.sonSatirAt = ms
	}
	b := a.buckets[min]
	if b == nil {
		b = newBucket()
		a.buckets[min] = b
		a.bakim()
		a.dirty = true
	}
	a.parsed++
	if r.At.After(a.lastAt) {
		a.lastAt = r.At
	}
	b.kinds[r.Kind]++
	if i := r.Status/100 - 2; i >= 0 && i < 4 {
		b.classes[i]++
	}
	if r.Host != "" {
		b.withHost++
	}
	blocked := r.Kind == KindDenied || r.Kind == KindNoMatch || r.Kind == KindNoServer
	{
		// Her tür kayıt yol listesine girer: sunucuya ulaşanlar, yönlendirmeler, engellenenler.
		// Böylece "hangi adres 3xx/4xx/5xx dönüyor" sorusu eksiksiz cevaplanabiliyor.
		k := pathKey{r.Backend, r.Method, r.Path}
		pa := b.paths[k]
		if pa == nil {
			if len(b.paths) >= maxKeysPerBucket {
				k = pathKey{r.Backend, "", "(diğer)"}
				pa = b.paths[k]
			}
			if pa == nil {
				pa = &pathAgg{}
				b.paths[k] = pa
			}
		}
		pa.addKind(r.Kind)
		pa.N++
		switch r.Status / 100 {
		case 2:
			pa.S2++
		case 3:
			pa.S3++
			pa.addCode(r.Status)
		case 4:
			pa.S4++
			pa.addCode(r.Status)
		case 5:
			pa.S5++
			pa.addCode(r.Status)
		}
		if r.Ta >= 0 && r.Kind == KindServed {
			pa.SumTa += int64(r.Ta)
			pa.NTa++
		}
		if r.Status >= 300 {
			if pa.Err = b.detail(pa.Err); pa.Err != nil {
				pa.Err.add(r)
			}
		}
		// Adrese en çok istek yapan IP'ler; yanıt koduna bakılmaksızın
		if pa.IPs == nil && b.pathIPKeys < maxPathIPKeys {
			b.pathIPKeys++
			pa.IPs = map[string]*ipAgg{}
		}
		if pa.IPs != nil {
			addIP(pa.IPs, r.Client, r.Status, maxPathIPs)
		}
	}
	if blocked {
		k := blockKey{r.Kind, r.Method, r.Path}
		ba := b.blocked[k]
		if ba == nil {
			if len(b.blocked) >= maxKeysPerBucket {
				k = blockKey{r.Kind, "", "(diğer)"}
				ba = b.blocked[k]
			}
			if ba == nil {
				ba = &blockAgg{}
				b.blocked[k] = ba
			}
		}
		ba.N++
		if ba.Det = b.detail(ba.Det); ba.Det != nil {
			ba.Det.add(r)
		}
	}
	ck := r.Client
	ca := b.clients[ck]
	if ca == nil {
		if len(b.clients) >= maxKeysPerBucket {
			ck = "(diğer)"
			ca = b.clients[ck]
		}
		if ca == nil {
			ca = &clientAgg{}
			b.clients[ck] = ca
		}
	}
	if ba := b.backend(r.Backend); ba != nil {
		ba.N++
		switch r.Status / 100 {
		case 2:
			ba.S2++
		case 3:
			ba.S3++
		case 4:
			ba.S4++
		case 5:
			ba.S5++
		}
		if blocked {
			ba.Blocked++
		}
		addCappedN(ba.IPs, r.Client, maxBackendIPs)
	}

	ca.N++
	if blocked {
		ca.Blocked++
	}
	if ca.Paths == nil && b.clientKeys < maxClientDetail {
		b.clientKeys++
		ca.Paths = map[string]int64{}
	}
	if ca.Paths != nil {
		yol := r.Path
		if r.Method != "" {
			yol = r.Method + " " + r.Path
		}
		addCappedN(ca.Paths, yol, maxClientPaths)
	}
}

// Kaynak değiştikçe uygun okuyucuyu başlatır.
func (a *LogAnalyzer) Run() {
	for {
		a.mu.Lock()
		src, path, stop := a.source, a.path, a.stop
		a.mu.Unlock()
		switch {
		case src == "":
			<-a.changed
		case strings.HasPrefix(src, "journal:"):
			a.runJournal(path, stop)
		default:
			a.runFile(path, stop)
		}
	}
}

func (a *LogAnalyzer) runFile(path string, stop chan struct{}) {
	var f *os.File
	defer func() {
		if f != nil {
			f.Close()
		}
	}()
	var rd *bufio.Reader
	var info os.FileInfo
	var pending strings.Builder
	first := true
	for {
		if f == nil {
			var err error
			f, err = os.Open(path)
			if err != nil {
				a.setErr(err.Error())
				if sleepOrStop(5*time.Second, stop) {
					return
				}
				continue
			}
			a.setErr("")
			info, _ = f.Stat()
			rd = bufio.NewReaderSize(f, 256<<10)
			pending.Reset()
			if first && info != nil && info.Size() > 4<<20 {
				// İlk açılışta son ~4 MB'ı oku, panel boş başlamasın
				_, _ = f.Seek(-4<<20, io.SeekEnd)
				rd.Reset(f)
				_, _ = rd.ReadString('\n') // yarım satırı at
			}
			first = false
		}
		chunk, err := rd.ReadString('\n')
		if chunk != "" {
			pending.WriteString(chunk)
		}
		if err == nil {
			a.handleLine(pending.String())
			pending.Reset()
			continue
		}
		if pending.Len() > 1<<20 { // satır sonu olmayan tuhaf bir dosyaya karşı
			pending.Reset()
		}
		// Dosya sonu: yeni satır bekle
		if sleepOrStop(500*time.Millisecond, stop) {
			return
		}
		st, serr := os.Stat(path)
		if serr != nil {
			continue // logrotate anı; dosya birazdan yeniden oluşur
		}
		if !os.SameFile(st, info) { // logrotate yeni dosya açtı
			f.Close()
			f = nil
			continue
		}
		if pos, perr := f.Seek(0, io.SeekCurrent); perr == nil && st.Size() < pos { // dosya kesildi (copytruncate)
			_, _ = f.Seek(0, io.SeekStart)
			rd.Reset(f)
			pending.Reset()
		}
	}
}

// Son N dakikanın özeti: notlar için
func (a *LogAnalyzer) Observe(minutes int) logObservation {
	a.mu.Lock()
	defer a.mu.Unlock()
	o := logObservation{Enabled: true, Source: a.source, Err: a.lastErr, Samples: append([]string(nil), a.samples...)}
	from := time.Now().Unix()/60 - int64(minutes) + 1
	for m, b := range a.buckets {
		if m < from {
			continue
		}
		for _, v := range b.kinds {
			o.Parsed += v
		}
		o.Served += b.kinds[KindServed]
		o.HostLines += b.withHost
	}
	for m, n := range a.unparsed {
		if m >= from {
			o.Unparsed += n
		} else if m < from-120 {
			delete(a.unparsed, m)
		}
	}
	return o
}

func (a *LogAnalyzer) setErr(s string) {
	a.mu.Lock()
	a.lastErr = s
	a.mu.Unlock()
}

// ---------- Rapor ----------

type PathRow struct {
	Backend string      `json:"backend"`
	Method  string      `json:"method"`
	Path    string      `json:"path"`
	N       int64       `json:"n"`
	S2      int64       `json:"s2"`
	S3      int64       `json:"s3"`
	S4      int64       `json:"s4"`
	S5      int64       `json:"s5"`
	AvgMs   float64     `json:"avgMs"`
	IPs     []PathIPRow `json:"ips,omitempty"` // bu adrese en çok istek yapan IP'ler
}

// Belirli bir kodu (404, 502...) en çok alan yollar
type CodeCount struct {
	Code int   `json:"code"`
	N    int64 `json:"n"`
}

// Bir backend'e log'da görünen trafiğin dökümü
type BackendLogRow struct {
	Backend string      `json:"backend"`
	N       int64       `json:"n"`
	S2      int64       `json:"s2"`
	S3      int64       `json:"s3"`
	S4      int64       `json:"s4"`
	S5      int64       `json:"s5"`
	Blocked int64       `json:"blocked"`
	Paths   []NameCount `json:"paths"` // en çok istenen adresler
	IPs     []ClientRow `json:"ips"`   // en çok istek atan IP'ler
}

// Bir yolun yanıt sınıflarına göre dökümü; panelde 3xx/4xx/5xx sekmeleriyle süzülür
type CodePathRow struct {
	Backend string      `json:"backend"`
	Method  string      `json:"method"`
	Path    string      `json:"path"`
	Kind    string      `json:"kind"` // served, redirect, denied, nomatch, noserver, proxy ya da "" (karışık)
	N       int64       `json:"n"`    // bu yoldaki toplam istek
	S3      int64       `json:"s3"`
	S4      int64       `json:"s4"`
	S5      int64       `json:"s5"`
	Codes   []CodeCount `json:"codes"` // en çok görülen kodlar, çoktan aza
	Detail  *Detail     `json:"detail,omitempty"`
}
type BlockRow struct {
	Kind   string  `json:"kind"`
	Method string  `json:"method"`
	Path   string  `json:"path"`
	N      int64   `json:"n"`
	Detail *Detail `json:"detail,omitempty"`
}

// Panelde satıra tıklayınca açılan ayrıntı
type NameCount struct {
	Name string `json:"name"`
	N    int64  `json:"n"`
}
type Detail struct {
	Total     int64       `json:"total"`     // ayrıntısı tutulan istek sayısı
	HostKnown int64       `json:"hostKnown"` // bunlardan alan adı log'da olan
	Origins   []NameCount `json:"origins"`   // "https://alan.com"; "" = alan adı log'da yok
	Samples   []NameCount `json:"samples"`   // gerçek yollar (sorgusuz)
	IPs       []ClientRow `json:"ips"`
}

func topPerClass(rows []CodePathRow, per int) []CodePathRow {
	seen := map[int]bool{}
	var out []CodePathRow
	for _, get := range []func(CodePathRow) int64{
		func(r CodePathRow) int64 { return r.S5 },
		func(r CodePathRow) int64 { return r.S4 },
		func(r CodePathRow) int64 { return r.S3 },
	} {
		idx := make([]int, 0, len(rows))
		for i, r := range rows {
			if get(r) > 0 {
				idx = append(idx, i)
			}
		}
		sort.Slice(idx, func(a, b int) bool {
			x, y := rows[idx[a]], rows[idx[b]]
			if get(x) != get(y) {
				return get(x) > get(y)
			}
			return x.Path+x.Method+x.Backend < y.Path+y.Method+y.Backend // eşitlikte sabit sıra
		})
		if len(idx) > per {
			idx = idx[:per]
		}
		for _, i := range idx {
			if !seen[i] {
				seen[i] = true
				out = append(out, rows[i])
			}
		}
	}
	return out
}

func topIPs(m map[string]*ipAgg, n int) []struct {
	ip string
	v  *ipAgg
} {
	out := make([]struct {
		ip string
		v  *ipAgg
	}, 0, len(m))
	for k, v := range m {
		out = append(out, struct {
			ip string
			v  *ipAgg
		}{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].ip == otherKey) != (out[j].ip == otherKey) {
			return out[j].ip == otherKey // "(diğer)" hep sonda
		}
		if out[i].v.N != out[j].v.N {
			return out[i].v.N > out[j].v.N
		}
		return out[i].ip < out[j].ip
	})
	// Listeye sığmayanlar "(diğer)" satırında toplanır; böylece IP'lerin toplamı
	// her zaman yolun toplam isteğine eşit olur.
	if len(out) > n {
		diger := &ipAgg{}
		for _, x := range out[n:] {
			diger.N += x.v.N
			diger.birlestir(x.v)
		}
		out = append(out[:n:n], struct {
			ip string
			v  *ipAgg
		}{otherKey, diger})
	}
	return out
}

func topN(m map[string]int64, n int) []NameCount {
	out := make([]NameCount, 0, len(m))
	for k, v := range m {
		out = append(out, NameCount{Name: k, N: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Name == otherKey) != (out[j].Name == otherKey) {
			return out[j].Name == otherKey // "(diğer)" hep sonda
		}
		if out[i].N != out[j].N {
			return out[i].N > out[j].N
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func (a *LogAnalyzer) buildDetail(d *errDetail) *Detail {
	if d == nil {
		return nil
	}
	out := &Detail{Origins: topN(d.Origins, 8), Samples: topN(d.Samples, 5)}
	for k, v := range d.Origins {
		out.Total += v
		if k != "" {
			out.HostKnown += v
		}
	}
	for _, ip := range topN(d.IPs, 8) {
		out.IPs = append(out.IPs, ClientRow{IP: ip.Name, N: ip.N, Cloudflare: a.isCloudflare(ip.Name)})
	}
	return out
}

// Bir adrese istek yapan IP ve o istekler içindeki yanıt kodu dağılımı
type PathIPRow struct {
	IP         string   `json:"ip"`
	N          int64    `json:"n"`
	Codes      []ipCode `json:"codes"`           // gerçek kodlar (200, 404...), çoktan aza
	Other      int32    `json:"other,omitempty"` // listeye sığmayan kodların toplamı
	Cloudflare bool     `json:"cloudflare"`
}

type ClientRow struct {
	IP         string      `json:"ip"`
	N          int64       `json:"n"`
	Blocked    int64       `json:"blocked"`
	Cloudflare bool        `json:"cloudflare"`
	Paths      []NameCount `json:"paths,omitempty"` // bu IP'nin en çok istediği adresler
}
type LogReport struct {
	Enabled   bool             `json:"enabled"`
	Searching bool             `json:"searching,omitempty"` // log kaynağı henüz bulunamadı, aranıyor
	Source    string           `json:"source,omitempty"`
	Error     string           `json:"error,omitempty"`
	Minutes   int              `json:"minutes"`
	Lines     int64            `json:"lines"`
	Parsed    int64            `json:"parsed"`
	LastAt    int64            `json:"lastAt"`
	Kinds     map[string]int64 `json:"kinds"`
	Paths     []PathRow        `json:"paths"`
	CodePaths []CodePathRow    `json:"codePaths"`
	Backends  []BackendLogRow  `json:"backends"`
	Classes   [4]int64         `json:"classes"` // 2xx, 3xx, 4xx, 5xx toplamları
	Blocked   []BlockRow       `json:"blocked"`
	Clients   []ClientRow      `json:"clients"`
	CFKnown   bool             `json:"cfKnown"`
	HostLines int64            `json:"hostLines"` // aralıkta alan adı bulunan satır sayısı
	// Eski dakikalar sadeleştiği için ayrıntı ve listeler daha kısa bir süreyi kapsar
	DetailMinutes int `json:"detailMinutes"`
	ListMinutes   int `json:"listMinutes"`
	Retention     int `json:"retention"`
}

func (a *LogAnalyzer) Report(minutes int) LogReport {
	if minutes < 1 {
		minutes = 1
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if minutes > a.retention {
		minutes = a.retention
	}
	rep := LogReport{Enabled: true, Source: a.source, Error: a.lastErr, Minutes: minutes, Lines: a.lines, Parsed: a.parsed,
		Kinds: map[string]int64{}, CFKnown: len(a.cfNets) > 0,
		Retention: a.retention}
	rep.DetailMinutes, rep.ListMinutes = a.kapsam(a.simdiDk())
	if !a.lastAt.IsZero() {
		rep.LastAt = a.lastAt.UnixMilli()
	}
	from := a.simdiDk() - int64(minutes) + 1
	paths := map[pathKey]*pathAgg{}
	blocked := map[blockKey]*blockAgg{}
	clients := map[string]*clientAgg{}
	for m, b := range a.buckets {
		if m < from {
			continue
		}
		rep.HostLines += b.withHost
		for i, n := range b.classes {
			rep.Classes[i] += n
		}
		for k, v := range b.kinds {
			rep.Kinds[k] += v
		}
		for k, v := range b.paths {
			t := paths[k]
			if t == nil {
				t = &pathAgg{Kind: v.Kind}
				paths[k] = t
			} else if t.Kind != v.Kind {
				t.Kind = "" // farklı kovalarda farklı tür: karışık
			}
			t.N += v.N
			t.S2 += v.S2
			t.S3 += v.S3
			t.S4 += v.S4
			t.S5 += v.S5
			t.SumTa += v.SumTa
			t.NTa += v.NTa
			if v.Err != nil {
				if t.Err == nil {
					t.Err = newErrDetail()
				}
				t.Err.merge(v.Err)
			}
			if v.IPs != nil {
				if t.IPs == nil {
					t.IPs = map[string]*ipAgg{}
				}
				for ip, n := range v.IPs {
					h := t.IPs[ip]
					if h == nil {
						h = &ipAgg{}
						t.IPs[ip] = h
					}
					h.N += n.N
					h.birlestir(n)
				}
			}
			for code, n := range v.Codes {
				t.addCodeN(code, n)
			}
		}
		for k, v := range b.blocked {
			t := blocked[k]
			if t == nil {
				t = &blockAgg{}
				blocked[k] = t
			}
			t.N += v.N
			if v.Det != nil {
				if t.Det == nil {
					t.Det = newErrDetail()
				}
				t.Det.merge(v.Det)
			}
		}
		for k, v := range b.clients {
			t := clients[k]
			if t == nil {
				t = &clientAgg{}
				clients[k] = t
			}
			t.N += v.N
			t.Blocked += v.Blocked
			if v.Paths != nil {
				if t.Paths == nil {
					t.Paths = map[string]int64{}
				}
				for k, n := range v.Paths {
					t.Paths[k] += n
				}
			}
		}
	}
	for k, v := range paths {
		row := PathRow{Backend: k.Backend, Method: k.Method, Path: k.Path, N: v.N, S2: v.S2, S3: v.S3, S4: v.S4, S5: v.S5}
		if v.NTa > 0 {
			row.AvgMs = float64(v.SumTa) / float64(v.NTa)
		}
		for _, ip := range topIPs(v.IPs, maxPathIPs) {
			row.IPs = append(row.IPs, PathIPRow{IP: ip.ip, N: ip.v.N, Codes: ip.v.kodListesi(),
				Other: ip.v.Diger, Cloudflare: a.isCloudflare(ip.ip)})
		}
		rep.Paths = append(rep.Paths, row)
	}
	sort.Slice(rep.Paths, func(i, j int) bool {
		a, b := rep.Paths[i], rep.Paths[j]
		if a.N != b.N {
			return a.N > b.N
		}
		if a.Path != b.Path {
			return a.Path < b.Path // eşit sayıda: sıra her yenilemede aynı kalsın
		}
		return a.Method+a.Backend < b.Method+b.Backend
	})
	if len(rep.Paths) > 40 {
		rep.Paths = rep.Paths[:40]
	}
	// Backend dökümü: kovaları birleştir
	beler := map[string]*backendAgg{}
	for m, b := range a.buckets {
		if m < from {
			continue
		}
		for ad, v := range b.backends {
			t := beler[ad]
			if t == nil {
				t = &backendAgg{IPs: map[string]int64{}}
				beler[ad] = t
			}
			t.N += v.N
			t.S2 += v.S2
			t.S3 += v.S3
			t.S4 += v.S4
			t.S5 += v.S5
			t.Blocked += v.Blocked
			for ip, n := range v.IPs {
				t.IPs[ip] += n
			}
		}
	}
	// Backend başına en çok istenen adresler, yol listesinden türetilir
	beYollar := map[string]map[string]int64{}
	for k, v := range paths {
		m := beYollar[k.Backend]
		if m == nil {
			m = map[string]int64{}
			beYollar[k.Backend] = m
		}
		yol := k.Path
		if k.Method != "" {
			yol = k.Method + " " + k.Path
		}
		m[yol] += v.N
	}
	for ad, v := range beler {
		row := BackendLogRow{Backend: ad, N: v.N, S2: v.S2, S3: v.S3, S4: v.S4, S5: v.S5, Blocked: v.Blocked,
			Paths: topN(beYollar[ad], 10)}
		for _, ip := range topN(v.IPs, 10) {
			row.IPs = append(row.IPs, ClientRow{IP: ip.Name, N: ip.N, Cloudflare: a.isCloudflare(ip.Name)})
		}
		rep.Backends = append(rep.Backends, row)
	}
	sort.Slice(rep.Backends, func(i, j int) bool {
		if rep.Backends[i].N != rep.Backends[j].N {
			return rep.Backends[i].N > rep.Backends[j].N
		}
		return rep.Backends[i].Backend < rep.Backends[j].Backend
	})

	var codeRows []CodePathRow
	for k, v := range paths {
		if v.S3+v.S4+v.S5 == 0 {
			continue
		}
		row := CodePathRow{Backend: k.Backend, Method: k.Method, Path: k.Path, Kind: v.Kind,
			N: v.N, S3: v.S3, S4: v.S4, S5: v.S5, Detail: a.buildDetail(v.Err)}
		for code, n := range v.Codes {
			row.Codes = append(row.Codes, CodeCount{Code: code, N: n})
		}
		sort.Slice(row.Codes, func(i, j int) bool { return row.Codes[i].N > row.Codes[j].N })
		if len(row.Codes) > 6 {
			row.Codes = row.Codes[:6]
		}
		codeRows = append(codeRows, row)
	}
	// Her sınıfın (3xx, 4xx, 5xx) kendi en yoğun 20 yolu listeye girer; böylece çok sayıda
	// yönlendirme, az sayıdaki sunucu hatasını listeden düşürmez.
	rep.CodePaths = topPerClass(codeRows, 20)
	for k, v := range blocked {
		rep.Blocked = append(rep.Blocked, BlockRow{Kind: k.Kind, Method: k.Method, Path: k.Path, N: v.N, Detail: a.buildDetail(v.Det)})
	}
	sort.Slice(rep.Blocked, func(i, j int) bool {
		a, b := rep.Blocked[i], rep.Blocked[j]
		if a.N != b.N {
			return a.N > b.N
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Method+a.Kind < b.Method+b.Kind
	})
	if len(rep.Blocked) > 40 {
		rep.Blocked = rep.Blocked[:40]
	}
	for k, v := range clients {
		rep.Clients = append(rep.Clients, ClientRow{IP: k, N: v.N, Blocked: v.Blocked,
			Cloudflare: a.isCloudflare(k), Paths: topN(v.Paths, 6)})
	}
	sort.Slice(rep.Clients, func(i, j int) bool {
		if rep.Clients[i].N != rep.Clients[j].N {
			return rep.Clients[i].N > rep.Clients[j].N
		}
		return rep.Clients[i].IP < rep.Clients[j].IP
	})
	if len(rep.Clients) > 30 {
		rep.Clients = rep.Clients[:30]
	}
	return rep
}
