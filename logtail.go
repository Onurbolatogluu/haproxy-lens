package main

// HAProxy log dosyasını salt okunur izler (tail -f gibi) ve dakikalık özet çıkarır.
// Sorgu parametreleri (?...) hiç saklanmaz: cihaz kimliği, token gibi veriler bellekte tutulmaz.

import (
	"bufio"
	"io"
	"net"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// option httplog biçimi:
// %ci:%cp [%tr] %ft %b/%s %TR/%Tw/%Tc/%Tr/%Ta %ST %B %CC %CS %tsc %ac/%fc/%bc/%sc/%rc %sq/%bq %hr %hs %{+Q}r
var reHTTPLog = regexp.MustCompile(`^(\S+):\d+ \[([^\]]+)\] (\S+) (\S+)/(\S+) -?\d+/-?\d+/-?\d+/-?\d+/\+?(-?\d+) (-?\d+) \+?\d+ \S+ \S+ (\S{4}) \d+/\d+/\d+/\d+/\+?\d+ \d+/\d+ (?:\{[^}]*\} )*"(.*)"\s*$`)

const (
	KindServed   = "served"   // bir sunucu yanıtladı
	KindDenied   = "denied"   // http-request deny ile engellendi
	KindNoMatch  = "nomatch"  // hiçbir backend eşleşmedi
	KindNoServer = "noserver" // backend seçildi ama çalışan sunucu yok
	KindRedirect = "redirect" // HAProxy yönlendirdi (ör. http -> https)
	KindProxy    = "proxy"    // HAProxy'nin kendisi yanıtladı (400, 408, kopma...)
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
	Path     string
	Kind     string
}

func parseLogLine(line string) (logRecord, bool) {
	var rec logRecord
	// syslog satırı ("... haproxy[123]: mesaj") ya da journald'dan gelen çıplak mesaj
	msg := line
	if i := strings.Index(line, "]: "); i >= 0 && strings.Contains(line[:i], "haproxy[") {
		msg = line[i+3:]
	}
	m := reHTTPLog.FindStringSubmatch(msg)
	if m == nil {
		return rec, false
	}
	at, err := time.ParseInLocation("02/Jan/2006:15:04:05.000", m[2], time.Local)
	if err != nil {
		return rec, false
	}
	rec.At = at
	rec.Client = m[1]
	rec.Frontend = strings.TrimSuffix(m[3], "~")
	rec.Backend = m[4]
	rec.Server = m[5]
	rec.Ta, _ = strconv.Atoi(m[6])
	rec.Status, _ = strconv.Atoi(m[7])
	rec.Term = m[8]
	parts := strings.SplitN(m[9], " ", 3)
	if len(parts) >= 2 {
		rec.Method = parts[0]
		rec.Path = normPath(parts[1])
	} else {
		rec.Path = m[9] // <BADREQ> gibi
	}
	rec.Kind = classify(rec)
	return rec, true
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
	Codes                         map[int]int64 // spesifik kod (404, 502...) -> sayı; sadece 4xx/5xx
}

func (pa *pathAgg) addCodeN(code int, n int64) {
	if pa.Codes == nil {
		pa.Codes = map[int]int64{}
	}
	pa.Codes[code] += n
}

func (pa *pathAgg) addCode(code int) {
	if code < 400 || code == 0 {
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

type clientAgg struct{ N, Blocked int64 }

type bucket struct {
	kinds   map[string]int64
	paths   map[pathKey]*pathAgg
	blocked map[blockKey]int64
	clients map[string]*clientAgg
}

func newBucket() *bucket {
	return &bucket{kinds: map[string]int64{}, paths: map[pathKey]*pathAgg{}, blocked: map[blockKey]int64{}, clients: map[string]*clientAgg{}}
}

type LogAnalyzer struct {
	source  string // "file:/yol" ya da "journal:haproxy"
	path    string
	cfNets  []*net.IPNet
	mu      sync.Mutex
	buckets map[int64]*bucket
	lines   int64
	parsed  int64
	lastAt  time.Time
	lastErr string
}

func NewLogAnalyzer(source, cloudflareList string) *LogAnalyzer {
	if !strings.HasPrefix(source, "journal:") && !strings.HasPrefix(source, "file:") {
		source = "file:" + source
	}
	_, path, _ := strings.Cut(source, ":")
	nets := append(builtinCloudflareNets(), loadCIDRs(cloudflareList)...)
	return &LogAnalyzer{source: source, path: path, cfNets: nets, buckets: map[int64]*bucket{}}
}

func (a *LogAnalyzer) handleLine(line string) {
	a.mu.Lock()
	a.lines++
	a.mu.Unlock()
	if rec, ok := parseLogLine(strings.TrimRight(line, "\r\n")); ok {
		a.add(rec)
	}
}

// journald'dan okuma: "journalctl -f" çıktısını satır satır işler, kapanırsa yeniden başlatır.
func (a *LogAnalyzer) runJournal() {
	n := "2000"
	for {
		cmd := exec.Command("journalctl", "--no-pager", "-q", "-o", "cat", "-f", "-n", n, "-t", a.path)
		out, err := cmd.StdoutPipe()
		if err == nil {
			err = cmd.Start()
		}
		if err != nil {
			a.setErr("journalctl başlatılamadı: " + err.Error())
			time.Sleep(10 * time.Second)
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
		a.setErr("journalctl kapandı, yeniden başlatılıyor")
		time.Sleep(5 * time.Second)
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

func (a *LogAnalyzer) add(r logRecord) {
	min := r.At.Unix() / 60
	a.mu.Lock()
	defer a.mu.Unlock()
	b := a.buckets[min]
	if b == nil {
		b = newBucket()
		a.buckets[min] = b
		cutoff := time.Now().Unix()/60 - 61
		for k := range a.buckets {
			if k < cutoff {
				delete(a.buckets, k)
			}
		}
	}
	a.parsed++
	if r.At.After(a.lastAt) {
		a.lastAt = r.At
	}
	b.kinds[r.Kind]++
	blocked := r.Kind == KindDenied || r.Kind == KindNoMatch || r.Kind == KindNoServer
	if r.Kind == KindServed || r.Kind == KindNoServer {
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
		pa.N++
		switch r.Status / 100 {
		case 2:
			pa.S2++
		case 3:
			pa.S3++
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
	}
	if blocked {
		k := blockKey{r.Kind, r.Method, r.Path}
		if _, ok := b.blocked[k]; !ok && len(b.blocked) >= maxKeysPerBucket {
			k = blockKey{r.Kind, "", "(diğer)"}
		}
		b.blocked[k]++
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
	ca.N++
	if blocked {
		ca.Blocked++
	}
}

func (a *LogAnalyzer) Run() {
	if strings.HasPrefix(a.source, "journal:") {
		a.runJournal()
		return
	}
	var f *os.File
	var rd *bufio.Reader
	var info os.FileInfo
	var pending strings.Builder
	first := true
	for {
		if f == nil {
			var err error
			f, err = os.Open(a.path)
			if err != nil {
				a.setErr(err.Error())
				time.Sleep(5 * time.Second)
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
		time.Sleep(500 * time.Millisecond)
		st, serr := os.Stat(a.path)
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

func (a *LogAnalyzer) setErr(s string) {
	a.mu.Lock()
	a.lastErr = s
	a.mu.Unlock()
}

// ---------- Rapor ----------

type PathRow struct {
	Backend string  `json:"backend"`
	Method  string  `json:"method"`
	Path    string  `json:"path"`
	N       int64   `json:"n"`
	S2      int64   `json:"s2"`
	S3      int64   `json:"s3"`
	S4      int64   `json:"s4"`
	S5      int64   `json:"s5"`
	AvgMs   float64 `json:"avgMs"`
}

// Belirli bir kodu (404, 502...) en çok alan yollar
type CodeCount struct {
	Code int   `json:"code"`
	N    int64 `json:"n"`
}
type ErrorPathRow struct {
	Backend string      `json:"backend"`
	Method  string      `json:"method"`
	Path    string      `json:"path"`
	N       int64       `json:"n"`     // bu yoldaki toplam istek
	Errs    int64       `json:"errs"`  // 4xx+5xx toplamı
	Class   string      `json:"class"` // "4xx", "5xx" ya da "karışık"
	Codes   []CodeCount `json:"codes"` // en çok görülen kodlar, çoktan aza
}
type BlockRow struct {
	Kind   string `json:"kind"`
	Method string `json:"method"`
	Path   string `json:"path"`
	N      int64  `json:"n"`
}
type ClientRow struct {
	IP         string `json:"ip"`
	N          int64  `json:"n"`
	Blocked    int64  `json:"blocked"`
	Cloudflare bool   `json:"cloudflare"`
}
type LogReport struct {
	Enabled    bool             `json:"enabled"`
	Source     string           `json:"source,omitempty"`
	Error      string           `json:"error,omitempty"`
	Minutes    int              `json:"minutes"`
	Lines      int64            `json:"lines"`
	Parsed     int64            `json:"parsed"`
	LastAt     int64            `json:"lastAt"`
	Kinds      map[string]int64 `json:"kinds"`
	Paths      []PathRow        `json:"paths"`
	ErrorPaths []ErrorPathRow   `json:"errorPaths"`
	Blocked    []BlockRow       `json:"blocked"`
	Clients    []ClientRow      `json:"clients"`
	CFKnown    bool             `json:"cfKnown"`
}

func (a *LogAnalyzer) Report(minutes int) LogReport {
	if minutes < 1 {
		minutes = 1
	}
	if minutes > 60 {
		minutes = 60
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	rep := LogReport{Enabled: true, Source: a.source, Error: a.lastErr, Minutes: minutes, Lines: a.lines, Parsed: a.parsed, Kinds: map[string]int64{}, CFKnown: len(a.cfNets) > 0}
	if !a.lastAt.IsZero() {
		rep.LastAt = a.lastAt.UnixMilli()
	}
	from := time.Now().Unix()/60 - int64(minutes) + 1
	paths := map[pathKey]*pathAgg{}
	blocked := map[blockKey]int64{}
	clients := map[string]*clientAgg{}
	for m, b := range a.buckets {
		if m < from {
			continue
		}
		for k, v := range b.kinds {
			rep.Kinds[k] += v
		}
		for k, v := range b.paths {
			t := paths[k]
			if t == nil {
				t = &pathAgg{}
				paths[k] = t
			}
			t.N += v.N
			t.S2 += v.S2
			t.S3 += v.S3
			t.S4 += v.S4
			t.S5 += v.S5
			t.SumTa += v.SumTa
			t.NTa += v.NTa
			for code, n := range v.Codes {
				t.addCodeN(code, n)
			}
		}
		for k, v := range b.blocked {
			blocked[k] += v
		}
		for k, v := range b.clients {
			t := clients[k]
			if t == nil {
				t = &clientAgg{}
				clients[k] = t
			}
			t.N += v.N
			t.Blocked += v.Blocked
		}
	}
	for k, v := range paths {
		row := PathRow{Backend: k.Backend, Method: k.Method, Path: k.Path, N: v.N, S2: v.S2, S3: v.S3, S4: v.S4, S5: v.S5}
		if v.NTa > 0 {
			row.AvgMs = float64(v.SumTa) / float64(v.NTa)
		}
		rep.Paths = append(rep.Paths, row)
	}
	sort.Slice(rep.Paths, func(i, j int) bool { return rep.Paths[i].N > rep.Paths[j].N })
	if len(rep.Paths) > 40 {
		rep.Paths = rep.Paths[:40]
	}
	for k, v := range paths {
		errs := v.S4 + v.S5
		if errs == 0 {
			continue
		}
		row := ErrorPathRow{Backend: k.Backend, Method: k.Method, Path: k.Path, N: v.N, Errs: errs}
		switch {
		case v.S4 > 0 && v.S5 > 0:
			row.Class = "karışık"
		case v.S5 > 0:
			row.Class = "5xx"
		default:
			row.Class = "4xx"
		}
		for code, n := range v.Codes {
			row.Codes = append(row.Codes, CodeCount{Code: code, N: n})
		}
		sort.Slice(row.Codes, func(i, j int) bool { return row.Codes[i].N > row.Codes[j].N })
		if len(row.Codes) > 5 {
			row.Codes = row.Codes[:5]
		}
		rep.ErrorPaths = append(rep.ErrorPaths, row)
	}
	sort.Slice(rep.ErrorPaths, func(i, j int) bool { return rep.ErrorPaths[i].Errs > rep.ErrorPaths[j].Errs })
	if len(rep.ErrorPaths) > 30 {
		rep.ErrorPaths = rep.ErrorPaths[:30]
	}
	for k, v := range blocked {
		rep.Blocked = append(rep.Blocked, BlockRow{Kind: k.Kind, Method: k.Method, Path: k.Path, N: v})
	}
	sort.Slice(rep.Blocked, func(i, j int) bool { return rep.Blocked[i].N > rep.Blocked[j].N })
	if len(rep.Blocked) > 40 {
		rep.Blocked = rep.Blocked[:40]
	}
	for k, v := range clients {
		rep.Clients = append(rep.Clients, ClientRow{IP: k, N: v.N, Blocked: v.Blocked, Cloudflare: a.isCloudflare(k)})
	}
	sort.Slice(rep.Clients, func(i, j int) bool { return rep.Clients[i].N > rep.Clients[j].N })
	if len(rep.Clients) > 25 {
		rep.Clients = rep.Clients[:25]
	}
	return rep
}
