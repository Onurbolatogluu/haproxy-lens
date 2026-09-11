package main

// HAProxy log-format tanımlarını düzenli ifadeye çevirir. Böylece ajan, config'te hangi
// biçim tanımlıysa (option httplog, httpslog, tcplog ya da özel log-format) onu okuyabilir.
// Tanımadığı değişkenleri atlar; panelin ihtiyaç duyduğu alanlar yoksa bunu not olarak raporlar.

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// HAProxy'nin hazır biçimleri (belgelerdeki tanımlar)
const (
	fmtHTTPLog  = `%ci:%cp [%tr] %ft %b/%s %TR/%Tw/%Tc/%Tr/%Ta %ST %B %CC %CS %tsc %ac/%fc/%bc/%sc/%rc %sq/%bq %hr %hs %{+Q}r`
	fmtHTTPSLog = fmtHTTPLog + ` %[fc_err]/%[ssl_fc_err,hex]/%[ssl_c_err]/%[ssl_c_ca_err]/%[ssl_fc_is_resumed] %[ssl_fc_sni]/%sslv/%sslc`
	fmtTCPLog   = `%ci:%cp [%t] %ft %b/%s %Tw/%Tc/%Tt %B %ts %ac/%fc/%bc/%sc/%rc %sq/%bq`
)

type fieldRole int

const (
	roleNone fieldRole = iota
	roleClient
	roleDateMs  // 10/Sep/2026:11:36:46.774
	roleDateTZ  // 10/Sep/2026:11:36:46 +0000
	roleDateTs  // unix saniye
	roleDateHex // unix saniye, onaltılık
	roleFrontend
	roleBackend
	roleServer
	roleTa
	roleTt
	roleStatus
	roleTerm
	roleCaptures // %hr: {a|b|c}
	roleRequest  // "GET /yol HTTP/1.1"
	roleMethod
	roleURI
	rolePath
	roleQuery
	roleHost
	roleSNI
	roleCapSlot // %[capture.req.hdr(N)]
	roleTrailing
)

type fmtVar struct {
	pat  string
	role fieldRole
	opt  bool // boş olabilir ve boşsa sonraki boşlukla birlikte hiç yazılmaz (%hr, %hs)
}

var fmtVars = map[string]fmtVar{
	"ci": {`\S+`, roleClient, false}, "cp": {`\d+`, roleNone, false},
	"fi": {`\S+`, roleNone, false}, "fp": {`\d+`, roleNone, false},
	"bi": {`\S+`, roleNone, false}, "bp": {`\d+`, roleNone, false},
	"si": {`\S+`, roleNone, false}, "sp": {`\d+`, roleNone, false},
	"tr":  {`\d{2}/\w{3}/\d{4}:\d{2}:\d{2}:\d{2}\.\d{3}`, roleDateMs, false},
	"t":   {`\d{2}/\w{3}/\d{4}:\d{2}:\d{2}:\d{2}\.\d{3}`, roleDateMs, false},
	"trg": {`\d{2}/\w{3}/\d{4}:\d{2}:\d{2}:\d{2} [+-]\d{4}`, roleDateTZ, false},
	"trl": {`\d{2}/\w{3}/\d{4}:\d{2}:\d{2}:\d{2} [+-]\d{4}`, roleDateTZ, false},
	"T":   {`\d{2}/\w{3}/\d{4}:\d{2}:\d{2}:\d{2} [+-]\d{4}`, roleDateTZ, false},
	"Tl":  {`\d{2}/\w{3}/\d{4}:\d{2}:\d{2}:\d{2} [+-]\d{4}`, roleDateTZ, false},
	"Ts":  {`\d+`, roleDateTs, false}, "ms": {`\d{3}`, roleNone, false},
	"ft": {`\S+`, roleFrontend, false}, "f": {`\S+`, roleFrontend, false},
	"b": {`\S+`, roleBackend, false}, "s": {`\S+`, roleServer, false},
	"TR": {`\+?-?\d+`, roleNone, false}, "Tw": {`\+?-?\d+`, roleNone, false},
	"Tc": {`\+?-?\d+`, roleNone, false}, "Tr": {`\+?-?\d+`, roleNone, false},
	"Th": {`\+?-?\d+`, roleNone, false}, "Ti": {`\+?-?\d+`, roleNone, false},
	"Tq": {`\+?-?\d+`, roleNone, false}, "Tu": {`\+?-?\d+`, roleNone, false},
	"Td": {`\+?-?\d+`, roleNone, false},
	"Ta": {`\+?-?\d+`, roleTa, false}, "Tt": {`\+?-?\d+`, roleTt, false},
	"ST": {`-?\d+`, roleStatus, false},
	"B":  {`\+?\d+`, roleNone, false}, "U": {`\+?\d+`, roleNone, false},
	"CC": {`\S+`, roleNone, false}, "CS": {`\S+`, roleNone, false},
	"tsc": {`\S{4}`, roleTerm, false}, "ts": {`\S{2}`, roleTerm, false},
	"ac": {`\+?\d+`, roleNone, false}, "fc": {`\+?\d+`, roleNone, false},
	"bc": {`\+?\d+`, roleNone, false}, "sc": {`\+?\d+`, roleNone, false},
	"rc": {`\+?\d+`, roleNone, false}, "sq": {`\+?\d+`, roleNone, false},
	"bq": {`\+?\d+`, roleNone, false}, "lc": {`\d+`, roleNone, false},
	"rt": {`\d+`, roleNone, false}, "pid": {`\d+`, roleNone, false},
	"hr": {`\{[^}]*\}`, roleCaptures, true}, "hs": {`\{[^}]*\}`, roleNone, true},
	"hrl": {`\S*`, roleNone, true}, "hsl": {`\S*`, roleNone, true},
	"r":  {`\S+ \S+(?: \S+)?`, roleRequest, false},
	"HM": {`\S+`, roleMethod, false}, "HU": {`\S+`, roleURI, false},
	"HP": {`\S+`, rolePath, false}, "HPO": {`\S+`, rolePath, false},
	"HQ": {`\S*`, roleQuery, false}, "HV": {`\S+`, roleNone, false},
	"H": {`\S+`, roleNone, false}, "ID": {`\S*`, roleNone, false},
	"sslc": {`\S+`, roleNone, false}, "sslv": {`\S+`, roleNone, false},
	"o": {``, roleNone, true},
}

var fmtAliases = func() []string {
	var ks []string
	for k := range fmtVars {
		ks = append(ks, k)
	}
	sort.Slice(ks, func(i, j int) bool { return len(ks[i]) > len(ks[j]) }) // en uzun eşleşme önce
	return ks
}()

type compiledFormat struct {
	Kind   string
	Source string // biçim tanımı
	re     *regexp.Regexp
	roles  []fieldRole // grup sırası
	slots  []int       // roleCapSlot için slot numarası (roles ile aynı sırada)
	has    map[fieldRole]bool
	hits   int64
}

type fmtItem struct {
	lit   string
	v     fmtVar
	isVar bool
	quote bool
	hex   bool
	slot  int
}

func tokenizeFormat(f string) []fmtItem {
	var items []fmtItem
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			items = append(items, fmtItem{lit: lit.String()})
			lit.Reset()
		}
	}
	for i := 0; i < len(f); i++ {
		c := f[i]
		if c != '%' {
			lit.WriteByte(c)
			continue
		}
		if i+1 < len(f) && f[i+1] == '%' {
			lit.WriteByte('%')
			i++
			continue
		}
		j := i + 1
		quote, hex := false, false
		if j < len(f) && f[j] == '{' {
			end := strings.IndexByte(f[j:], '}')
			if end < 0 {
				lit.WriteByte(c)
				continue
			}
			flags := f[j+1 : j+end]
			quote = strings.Contains(flags, "+Q")
			hex = strings.Contains(flags, "+X")
			j += end + 1
		}
		if j < len(f) && f[j] == '[' {
			depth, k := 0, j
			for ; k < len(f); k++ {
				if f[k] == '[' {
					depth++
				} else if f[k] == ']' {
					depth--
					if depth == 0 {
						break
					}
				}
			}
			expr := f[j+1 : min(k, len(f))]
			flush()
			it := fmtItem{isVar: true, quote: quote, v: fmtVar{pat: `\S*`, role: roleNone}}
			le := strings.ToLower(strings.ReplaceAll(expr, " ", ""))
			switch {
			case isHostExpr(le):
				it.v.role = roleHost
			case strings.HasPrefix(le, "ssl_fc_sni"):
				it.v.role = roleSNI
			case strings.HasPrefix(le, "capture.req.hdr("):
				n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(strings.SplitN(le, ",", 2)[0], "capture.req.hdr("), ")"))
				if err == nil {
					it.v.role, it.slot = roleCapSlot, n
				}
			}
			items = append(items, it)
			i = k
			continue
		}
		name := ""
		for _, a := range fmtAliases {
			if strings.HasPrefix(f[j:], a) {
				name = a
				break
			}
		}
		flush()
		if name == "" { // tanınmayan değişken: harfleri al, herhangi bir değer kabul et
			k := j
			for k < len(f) && (f[k] >= 'a' && f[k] <= 'z' || f[k] >= 'A' && f[k] <= 'Z') {
				k++
			}
			items = append(items, fmtItem{isVar: true, quote: quote, v: fmtVar{pat: `\S*`}})
			i = k - 1
			continue
		}
		it := fmtItem{isVar: true, quote: quote, hex: hex, v: fmtVars[name]}
		if name == "Ts" && hex {
			it.v = fmtVar{pat: `[0-9A-Fa-f]+`, role: roleDateHex}
		}
		items = append(items, it)
		i = j + len(name) - 1
	}
	flush()
	return items
}

func compileFormat(kind, format string) *compiledFormat {
	items := tokenizeFormat(format)
	cf := &compiledFormat{Kind: kind, Source: format, has: map[fieldRole]bool{}}
	var b strings.Builder
	b.WriteString(`^`)
	for i := 0; i < len(items); i++ {
		it := items[i]
		if !it.isVar {
			b.WriteString(regexp.QuoteMeta(it.lit))
			continue
		}
		if it.v.pat == "" { // %o gibi çıktısı olmayan
			continue
		}
		pat := it.v.pat
		if it.quote {
			pat = `"[^"]*"|-`
		}
		grp := `(` + pat + `)`
		if it.v.role == roleNone {
			grp = `(?:` + pat + `)`
		} else {
			cf.roles = append(cf.roles, it.v.role)
			cf.slots = append(cf.slots, it.slot)
			cf.has[it.v.role] = true
		}
		if it.v.opt {
			// boşsa kendisi ve ardındaki tek boşluk yazılmaz
			if i+1 < len(items) && !items[i+1].isVar && strings.HasPrefix(items[i+1].lit, " ") {
				b.WriteString(`(?:` + grp + ` )?`)
				items[i+1].lit = items[i+1].lit[1:]
			} else {
				b.WriteString(`(?:` + grp + `)?`)
			}
			continue
		}
		b.WriteString(grp)
	}
	// Fazladan alanlara izin ver (sonuna eklenmiş alanlar); alan adı için taranır
	b.WriteString(`(?:\s+(.*?))?\s*$`)
	cf.roles = append(cf.roles, roleTrailing)
	cf.slots = append(cf.slots, 0)
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil
	}
	cf.re = re
	return cf
}

func unquote(v string) string {
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		return v[1 : len(v)-1]
	}
	if v == "-" {
		return ""
	}
	return v
}

// hostSlot: frontend'in Host yakalama slotu (-1 bilinmiyor/yok, -2 config yok: sezgisel ara)
func (cf *compiledFormat) parse(msg string, hostSlotOf func(fe string) int) (logRecord, bool) {
	var rec logRecord
	m := cf.re.FindStringSubmatch(msg)
	if m == nil {
		return rec, false
	}
	var captures, uri, trailing, host, sni, method, path, query string
	capSlots := map[int]string{}
	haveDate := false
	for gi, role := range cf.roles {
		v := m[gi+1]
		switch role {
		case roleClient:
			rec.Client = v
		case roleDateMs:
			if t, err := time.ParseInLocation("02/Jan/2006:15:04:05.000", v, time.Local); err == nil {
				rec.At, haveDate = t, true
			}
		case roleDateTZ:
			if t, err := time.Parse("02/Jan/2006:15:04:05 -0700", v); err == nil {
				rec.At, haveDate = t, true
			}
		case roleDateTs, roleDateHex:
			base := 10
			if role == roleDateHex {
				base = 16
			}
			if n, err := strconv.ParseInt(v, base, 64); err == nil {
				rec.At, haveDate = time.Unix(n, 0), true
			}
		case roleFrontend:
			rec.TLS = strings.HasSuffix(v, "~")
			rec.Frontend = strings.TrimSuffix(v, "~")
		case roleBackend:
			rec.Backend = v
		case roleServer:
			rec.Server = v
		case roleTa:
			rec.Ta, _ = strconv.Atoi(strings.TrimPrefix(v, "+"))
		case roleTt:
			if !cf.has[roleTa] {
				rec.Ta, _ = strconv.Atoi(strings.TrimPrefix(v, "+"))
			}
		case roleStatus:
			rec.Status, _ = strconv.Atoi(v)
		case roleTerm:
			rec.Term = v
		case roleCaptures:
			captures = v
		case roleRequest:
			parts := strings.SplitN(unquote(v), " ", 3)
			if len(parts) >= 2 {
				method, uri = parts[0], parts[1]
			} else {
				path = unquote(v)
			}
		case roleMethod:
			method = unquote(v)
		case roleURI:
			uri = unquote(v)
		case rolePath:
			path = unquote(v)
		case roleQuery:
			query = unquote(v)
		case roleHost:
			host = unquote(v)
		case roleSNI:
			sni = unquote(v)
		case roleCapSlot:
			capSlots[cf.slots[gi]] = unquote(v)
		case roleTrailing:
			trailing = v
		}
	}
	if !haveDate {
		rec.At = time.Now()
	}
	_ = query
	if uri == "" && path != "" {
		uri = path
	}
	rec.Method = method
	if uri != "" {
		rec.Path = normPath(uri)
		rec.RawPath = rawPathOf(uri)
	} else {
		rec.Path, rec.RawPath = "(yol yok)", "(yol yok)"
	}
	// Alan adı: tam URI > açık host alanı > Host yakalama slotu > SNI > sezgisel
	slot := -2
	if hostSlotOf != nil {
		slot = hostSlotOf(rec.Frontend)
	}
	switch {
	case strings.Contains(uri, "://"):
		rec.Host = extractHost("", uri, "")
	case host != "" && host != "-":
		rec.Host = strings.ToLower(host)
	case slot >= 0 && capSlots[slot] != "":
		rec.Host = strings.ToLower(capSlots[slot])
	case slot >= 0 && captures != "":
		vals := strings.Split(strings.Trim(captures, "{}"), "|")
		if slot < len(vals) {
			rec.Host = strings.ToLower(strings.TrimSpace(vals[slot]))
		}
	case sni != "" && sni != "-":
		rec.Host = strings.ToLower(sni)
	case slot == -2:
		rec.Host = extractHost(captures, uri, trailing)
	default:
		rec.Host = extractHost("", uri, trailing) // yakalamalar Host değil; sadece sonraki alanlara bak
	}
	if !cf.has[roleStatus] && !cf.has[roleRequest] && !cf.has[rolePath] && !cf.has[roleURI] {
		rec.Kind = KindTCP
	} else {
		rec.Kind = classify(rec)
	}
	return rec, true
}

// ---------- Birden fazla biçimi deneyen ayrıştırıcı ----------

type LogParser struct {
	mu       sync.Mutex
	formats  []*compiledFormat
	hostSlot map[string]int // frontend -> Host slotu (-1: Host yakalanmıyor)
	fromCfg  bool
}

func newLogParser(formats []*compiledFormat, hostSlot map[string]int, fromCfg bool) *LogParser {
	return &LogParser{formats: formats, hostSlot: hostSlot, fromCfg: fromCfg}
}

// Config yoksa kullanılan varsayılan: hazır biçimlerin hepsi, alan adı sezgisel aranır
var defaultParser = newLogParser([]*compiledFormat{
	compileFormat("httplog", fmtHTTPLog),
	compileFormat("tcplog", fmtTCPLog),
}, nil, false)

func buildLogParser(hc *haConfig) *LogParser {
	if hc == nil || len(hc.Frontends) == 0 {
		return defaultParser
	}
	seen := map[string]*compiledFormat{}
	var formats []*compiledFormat
	hostSlot := map[string]int{}
	for _, fe := range hc.Frontends {
		hostSlot[fe.Name] = fe.HostSlot
		if fe.Format == "" || seen[fe.Format] != nil {
			continue
		}
		if cf := compileFormat(fe.FormatKind, fe.Format); cf != nil {
			seen[fe.Format] = cf
			formats = append(formats, cf)
		}
	}
	// Config'te olmasa da hazır biçimleri yedek olarak ekle (başka bir config'ten gelen satırlar olabilir)
	for _, k := range []struct{ kind, f string }{{"httplog", fmtHTTPLog}, {"tcplog", fmtTCPLog}} {
		if seen[k.f] == nil {
			formats = append(formats, compileFormat(k.kind, k.f))
		}
	}
	return newLogParser(formats, hostSlot, true)
}

func (p *LogParser) hostSlotOf(fe string) int {
	if !p.fromCfg {
		return -2
	}
	if s, ok := p.hostSlot[fe]; ok {
		return s
	}
	return -2 // bu frontend config'te yok: sezgisel
}

func syslogMessage(line string) string {
	if i := strings.Index(line, "]: "); i >= 0 && strings.Contains(line[:i], "haproxy[") {
		return line[i+3:]
	}
	return line
}

func (p *LogParser) Parse(line string) (logRecord, bool) {
	msg := syslogMessage(line)
	p.mu.Lock()
	fs := p.formats
	p.mu.Unlock()
	for i, cf := range fs {
		if rec, ok := cf.parse(msg, p.hostSlotOf); ok {
			p.mu.Lock()
			cf.hits++
			if i > 0 && cf.hits > fs[i-1].hits { // sık eşleşen biçimi öne al
				fs[i-1], fs[i] = fs[i], fs[i-1]
			}
			p.mu.Unlock()
			return rec, true
		}
	}
	return logRecord{}, false
}

func (p *LogParser) Formats() []*compiledFormat {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*compiledFormat(nil), p.formats...)
}
