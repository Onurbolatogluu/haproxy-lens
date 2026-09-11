package main

// Kurulum öncesi otomatik tespit. Hiçbir şeyi değiştirmez:
// çalışan HAProxy'nin komut satırından config dosyalarını bulur, config'i sadece okur,
// bulduğu socket'lere "show info" gönderip gerçekten çalıştığını doğrular,
// log dosyalarından örnek satır okuyup ayrıştırılabildiğini test eder.

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type socketCand struct {
	Addr   string // unix:/yol veya tcp:host:port
	Source string // dosya:satır
	Group  string
	Usable bool
	Note   string
	Info   map[string]string
}

type logCand struct {
	Path    string
	Group   string
	Traffic int // ip:port [tarih] ile başlayan satır
	TCP     int // tcplog satırı
	Parsed  int // httplog olarak okunabilen satır
	Host    int // bunlardan alan adı bulunan
	Usable  bool
	Note    string
}

type detection struct {
	PIDs        []int
	ConfigFiles []string
	ConfigNote  string
	LogTargets  []string
	Sockets     []socketCand
	Socket      *socketCand
	Logs        []logCand
	Log         *logCand
	LogNote     string
	Groups      []string
	Listen      listenChoice
}

var reTCPLogMsg = regexp.MustCompile(`^\S+:\d+ \[[^\]]+\] \S+ \S+/\S+ -?\d+/-?\d+/\+?-?\d+ \+?\d+ \S{2} `)
var reTrafficStart = regexp.MustCompile(`^\S+:\d+ \[\d{2}/\w{3}/\d{4}:`)
var reVarLogPath = regexp.MustCompile(`-?(/var/log/[^\s;"'()]+)`)

func runDetect(forceSocket, forceLog string) *detection {
	d := &detection{}
	d.PIDs, d.ConfigFiles, d.ConfigNote = findConfigs()

	// ---- stats socket ----
	var cands []socketCand
	if forceSocket != "" {
		cands = append(cands, socketCand{Addr: forceSocket, Source: "elle verildi"})
	} else {
		cands = socketsFromConfig(d.ConfigFiles)
	}
	for i := range cands {
		checkSocket(&cands[i])
	}
	d.Sockets = cands
	for i := range d.Sockets {
		if d.Sockets[i].Usable {
			d.Socket = &d.Sockets[i]
			break
		}
	}

	// ---- log ----
	d.LogTargets = logTargetsFromConfig(d.ConfigFiles)
	var paths []string
	if forceLog != "" {
		paths = []string{forceLog}
	} else {
		paths = logCandidates()
	}
	for _, p := range paths {
		d.Logs = append(d.Logs, checkLog(p))
	}
	if forceLog == "" && !anyUsable(d.Logs) {
		if j := checkLog("journal:haproxy"); j.Traffic > 0 {
			d.Logs = append(d.Logs, j)
		}
	}
	for i := range d.Logs {
		if d.Logs[i].Usable && (d.Log == nil || d.Logs[i].Parsed > d.Log.Parsed) {
			d.Log = &d.Logs[i]
		}
	}
	if d.Log == nil {
		d.LogNote = explainNoLog(d)
	}

	// ---- panelin dinleyeceği adres ----
	d.Listen = detectListen()

	// ---- servis kullanıcısının ihtiyaç duyacağı gruplar ----
	seen := map[string]bool{}
	for _, g := range []string{groupOf(d.Socket), groupOfLog(d.Log)} {
		if g != "" && !seen[g] {
			seen[g] = true
			d.Groups = append(d.Groups, g)
		}
	}
	return d
}

func anyUsable(ls []logCand) bool {
	for _, l := range ls {
		if l.Usable {
			return true
		}
	}
	return false
}

func groupOf(s *socketCand) string {
	if s == nil {
		return ""
	}
	return s.Group
}
func groupOfLog(l *logCand) string {
	if l == nil {
		return ""
	}
	return l.Group
}

// Çalışan haproxy süreçlerinin komut satırındaki -f parametrelerini okur.
func findConfigs() ([]int, []string, string) {
	var pids []int
	cfgSet := map[string]bool{}
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		comm, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
		if err != nil || strings.TrimSpace(string(comm)) != "haproxy" {
			continue
		}
		pids = append(pids, pid)
		raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
		if err != nil {
			continue
		}
		args := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
		cwd, _ := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
		for i := 0; i < len(args); i++ {
			if args[i] == "-C" && i+1 < len(args) {
				cwd = args[i+1]
			}
		}
		for i := 0; i < len(args); i++ {
			if args[i] == "-f" && i+1 < len(args) {
				p := args[i+1]
				if !filepath.IsAbs(p) {
					p = filepath.Join(cwd, p)
				}
				for _, f := range expandCfg(p) {
					cfgSet[f] = true
				}
			}
		}
	}
	sort.Ints(pids)
	var files []string
	for f := range cfgSet {
		files = append(files, f)
	}
	sort.Strings(files)
	note := ""
	if len(pids) == 0 {
		note = "Çalışan HAProxy süreci bulunamadı; varsayılan /etc/haproxy/haproxy.cfg okunuyor."
		if _, err := os.Stat("/etc/haproxy/haproxy.cfg"); err == nil {
			files = []string{"/etc/haproxy/haproxy.cfg"}
		}
	} else if len(files) == 0 {
		note = "HAProxy çalışıyor ama komut satırında -f ile verilmiş config bulunamadı."
	}
	return pids, files, note
}

func expandCfg(p string) []string {
	st, err := os.Stat(p)
	if err != nil {
		return nil
	}
	if !st.IsDir() {
		return []string{p}
	}
	m, _ := filepath.Glob(filepath.Join(p, "*.cfg"))
	sort.Strings(m)
	return m
}

type cfgLine struct {
	File   string
	N      int
	Fields []string
}

func readCfgLines(files []string) []cfgLine {
	var out []cfgLine
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		n := 0
		for sc.Scan() {
			n++
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if i := strings.Index(line, " #"); i >= 0 {
				line = line[:i]
			}
			out = append(out, cfgLine{File: f, N: n, Fields: strings.Fields(line)})
		}
		fh.Close()
	}
	return out
}

func socketsFromConfig(files []string) []socketCand {
	var out []socketCand
	for _, l := range readCfgLines(files) {
		if len(l.Fields) < 3 || l.Fields[0] != "stats" || l.Fields[1] != "socket" {
			continue
		}
		a := strings.Trim(l.Fields[2], `"'`)
		src := fmt.Sprintf("%s:%d", filepath.Base(l.File), l.N)
		addr := ""
		switch {
		case strings.Contains(a, "${"):
			out = append(out, socketCand{Addr: a, Source: src, Note: "ortam değişkeni içeriyor, çözülemedi"})
			continue
		case strings.HasPrefix(a, "unix@"):
			addr = "unix:" + strings.TrimPrefix(a, "unix@")
		case strings.HasPrefix(a, "/"):
			addr = "unix:" + a
		case strings.HasPrefix(a, "abns@"):
			addr = "unix:@" + strings.TrimPrefix(a, "abns@")
		case strings.HasPrefix(a, "ipv4@"), strings.HasPrefix(a, "ipv6@"), strings.Contains(a, ":"):
			hp := a
			if i := strings.Index(hp, "@"); i >= 0 {
				hp = hp[i+1:]
			}
			if strings.HasPrefix(hp, ":") || strings.HasPrefix(hp, "*:") || strings.HasPrefix(hp, "0.0.0.0:") {
				hp = "127.0.0.1:" + hp[strings.LastIndex(hp, ":")+1:]
			}
			addr = "tcp:" + hp
		default:
			out = append(out, socketCand{Addr: a, Source: src, Note: "bu adres biçimi desteklenmiyor"})
			continue
		}
		out = append(out, socketCand{Addr: addr, Source: src})
	}
	// Unix socket'leri öne al
	sort.SliceStable(out, func(i, j int) bool {
		return strings.HasPrefix(out[i].Addr, "unix:") && !strings.HasPrefix(out[j].Addr, "unix:")
	})
	return out
}

func checkSocket(c *socketCand) {
	if c.Note != "" {
		return
	}
	network, addr := socketAddr(c.Addr)
	if network == "unix" && !strings.HasPrefix(addr, "@") {
		st, err := os.Stat(addr)
		if err != nil {
			c.Note = "dosya yok (HAProxy çalışmıyor olabilir)"
			return
		}
		sys, _ := st.Sys().(*syscall.Stat_t)
		if sys != nil {
			c.Group = groupName(sys.Gid)
			if sys.Gid == 0 {
				c.Note = "socket root grubuna ait; ajanı root grubuna eklemek güvenli değil"
				return
			}
		}
		if st.Mode().Perm()&0o060 != 0o060 {
			c.Note = fmt.Sprintf("grup okuma/yazma izni yok (%s)", st.Mode().Perm())
			return
		}
	}
	raw, err := query(c.Addr, "show info")
	if err != nil {
		c.Note = "bağlanılamadı: " + err.Error()
		return
	}
	info := parseInfo(raw)
	if info["Name"] == "" && info["Version"] == "" {
		c.Note = "yanıt geldi ama HAProxy bilgisi okunamadı"
		return
	}
	c.Info = info
	c.Usable = true
}

func groupName(gid uint32) string {
	g, err := user.LookupGroupId(strconv.Itoa(int(gid)))
	if err != nil {
		return strconv.Itoa(int(gid))
	}
	return g.Name
}

func logTargetsFromConfig(files []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, l := range readCfgLines(files) {
		if len(l.Fields) >= 2 && l.Fields[0] == "log" && l.Fields[1] != "global" {
			t := strings.Join(l.Fields[1:], " ")
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}

// Log dosyası adayları: syslog ayarlarında "haproxy" geçen yerlerdeki /var/log yolları + bilinen yollar.
func logCandidates() []string {
	set := map[string]bool{}
	var order []string
	add := func(p string) {
		p = strings.TrimSuffix(p, "}")
		if !set[p] {
			set[p] = true
			order = append(order, p)
		}
	}
	var confs []string
	for _, g := range []string{"/etc/rsyslog.conf", "/etc/rsyslog.d/*.conf", "/etc/syslog-ng/syslog-ng.conf", "/etc/syslog-ng/conf.d/*.conf"} {
		m, _ := filepath.Glob(g)
		confs = append(confs, m...)
	}
	for _, c := range confs {
		b, err := os.ReadFile(c)
		if err != nil || !bytes.Contains(bytes.ToLower(b), []byte("haproxy")) {
			continue
		}
		for _, m := range reVarLogPath.FindAllStringSubmatch(string(b), -1) {
			if strings.Contains(strings.ToLower(m[1]), "haproxy") {
				add(m[1])
			}
		}
	}
	add("/var/log/haproxy.log")
	m, _ := filepath.Glob("/var/log/haproxy/*.log")
	for _, p := range m {
		add(p)
	}
	var out []string
	for _, p := range order {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			out = append(out, p)
		}
	}
	return out
}

func checkLog(p string) logCand {
	c := logCand{Path: p}
	var lines []string
	groupOK := true
	var perm os.FileMode
	journal := strings.HasPrefix(p, "journal:")
	if journal {
		out, err := journalSample(strings.TrimPrefix(p, "journal:"))
		if err != nil {
			c.Note = "journald okunamadı: " + err.Error()
			return c
		}
		lines = strings.Split(out, "\n")
		c.Group = "systemd-journal"
		if _, err := user.LookupGroup("systemd-journal"); err != nil {
			groupOK = false
		}
	} else {
		st, err := os.Stat(p)
		if err != nil {
			c.Note = "dosya yok"
			return c
		}
		perm = st.Mode().Perm()
		if sys, _ := st.Sys().(*syscall.Stat_t); sys != nil {
			c.Group = groupName(sys.Gid)
			if sys.Gid == 0 || perm&0o040 == 0 {
				groupOK = false
			}
		}
		f, err := os.Open(p)
		if err != nil {
			c.Note = "okunamadı: " + err.Error()
			return c
		}
		if st.Size() > 1<<20 {
			_, _ = f.Seek(-1<<20, io.SeekEnd)
		}
		b, _ := io.ReadAll(f)
		f.Close()
		lines = strings.Split(string(b), "\n")
		if len(lines) > 1 {
			lines = lines[1:] // yarım olabilecek ilk satır
		}
	}
	for _, line := range lines {
		msg := line
		if i := strings.Index(line, "]: "); i >= 0 && strings.Contains(line[:i], "haproxy[") {
			msg = line[i+3:]
		} else if !journal {
			continue // dosyada başka programların satırları
		}
		if !reTrafficStart.MatchString(msg) {
			continue // "Server x is DOWN" gibi olay satırları
		}
		c.Traffic++
		if rec, ok := parseLogLine(line); ok {
			c.Parsed++
			if rec.Host != "" {
				c.Host++
			}
		} else if reTCPLogMsg.MatchString(msg) {
			c.TCP++
		}
	}
	httpLines := c.Traffic - c.TCP
	switch {
	case c.Traffic == 0:
		c.Note = "son kayıtlarda HAProxy trafik satırı yok"
	case httpLines == 0:
		c.Note = "sadece TCP modu satırları var; HTTP analizi yapılamaz"
	case float64(c.Parsed)/float64(httpLines) < 0.8:
		c.Note = fmt.Sprintf("satırların çoğu okunamadı (%d/%d); özel log-format kullanılıyor olabilir", c.Parsed, httpLines)
	case !groupOK && journal:
		c.Note = "systemd-journal grubu yok, servis kullanıcısı journald'ı okuyamaz"
	case !groupOK:
		c.Note = fmt.Sprintf("servis kullanıcısı okuyamaz (grup %s, izin %s)", c.Group, perm)
	default:
		c.Usable = true
	}
	return c
}

func journalSample(tag string) (string, error) {
	if _, err := exec.LookPath("journalctl"); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "journalctl", "--no-pager", "-q", "-o", "cat", "-n", "500", "-t", tag).Output()
	return string(out), err
}

func explainNoLog(d *detection) string {
	var notes []string
	for _, l := range d.Logs {
		notes = append(notes, l.Path+": "+l.Note)
	}
	if len(notes) > 0 {
		return strings.Join(notes, "; ")
	}
	for _, t := range d.LogTargets {
		f := strings.Fields(t)
		if len(f) == 0 {
			continue
		}
		dst := f[0]
		if !strings.HasPrefix(dst, "/") && !strings.HasPrefix(dst, "127.") && !strings.HasPrefix(dst, "localhost") &&
			!strings.HasPrefix(dst, "stdout") && !strings.HasPrefix(dst, "stderr") && !strings.HasPrefix(dst, "ring@") {
			return "HAProxy log'ları başka bir sunucuya gönderiliyor (" + dst + "); bu sunucuda yerel log yok."
		}
	}
	return "HAProxy log'u bulunamadı (ne dosyada ne journald'da)."
}

// ---------- Çıktı ----------

func (d *detection) printReport(w io.Writer) {
	fmt.Fprintln(w, "haproxy-lens uyumluluk kontrolü (hiçbir şey değiştirilmez)")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "HAProxy")
	if len(d.PIDs) > 0 {
		fmt.Fprintf(w, "  Çalışan süreç(ler): %v\n", d.PIDs)
	}
	for _, f := range d.ConfigFiles {
		fmt.Fprintf(w, "  Config: %s\n", f)
	}
	if d.ConfigNote != "" {
		fmt.Fprintf(w, "  Not: %s\n", d.ConfigNote)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Stats socket")
	if len(d.Sockets) == 0 {
		fmt.Fprintln(w, "  Config'te 'stats socket' satırı yok.")
	}
	for _, s := range d.Sockets {
		if s.Usable {
			fmt.Fprintf(w, "  TAMAM  %s (%s) HAProxy %s yanıt verdi\n", s.Addr, s.Source, s.Info["Version"])
		} else {
			fmt.Fprintf(w, "  SORUN  %s (%s): %s\n", s.Addr, s.Source, s.Note)
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Log")
	if len(d.LogTargets) > 0 {
		fmt.Fprintf(w, "  Config'teki log hedefleri: %s\n", strings.Join(d.LogTargets, " | "))
	}
	for _, l := range d.Logs {
		if l.Usable {
			fmt.Fprintf(w, "  TAMAM  %s: son kayıtlardaki %d HTTP satırının %d tanesi okunabildi\n", l.Path, l.Traffic-l.TCP, l.Parsed)
			switch {
			case l.Host == 0:
				fmt.Fprintln(w, "         Alan adı: log biçiminde yok. Hatalı isteklerin ayrıntısında yol ve IP görünecek.")
			case l.Host*10 >= l.Parsed*9:
				fmt.Fprintln(w, "         Alan adı: log'da var. Hatalı isteklerin tam adresi görünecek.")
			default:
				fmt.Fprintf(w, "         Alan adı: satırların sadece %d/%d tanesinde var (koşullu yakalama olabilir). Olanlarda tam adres görünecek.\n", l.Host, l.Parsed)
			}
		} else {
			fmt.Fprintf(w, "  SORUN  %s: %s\n", l.Path, l.Note)
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Panel adresi")
	if d.Listen.Iface != "" {
		fmt.Fprintf(w, "  Varsayılan rota arayüzü: %s\n", d.Listen.Iface)
	}
	for _, s := range d.Listen.Skipped {
		fmt.Fprintf(w, "  ATLANDI %s\n", s)
	}
	if d.Listen.IP != "" {
		fmt.Fprintf(w, "  TAMAM  %s (%s)\n", d.Listen.IP, d.Listen.Why)
	} else {
		fmt.Fprintf(w, "  NOT    %s. Panel 127.0.0.1'de açılacak (SSH tüneliyle). Elle vermek için: LISTEN=IP ./install.sh\n", d.Listen.Note)
	}
	fmt.Fprintln(w)
	switch {
	case d.Socket == nil:
		fmt.Fprintln(w, "SONUÇ: KURULAMAZ. Çalışan ve erişilebilir bir stats socket bulunamadı; bu sunucuda hiçbir şey kurulmayacak.")
	case d.Log == nil:
		fmt.Fprintln(w, "SONUÇ: UYUMLU (sadece stats). Log analizi kapalı olacak:")
		fmt.Fprintf(w, "  %s\n", d.LogNote)
	default:
		fmt.Fprintln(w, "SONUÇ: UYUMLU (stats + log analizi).")
	}
}

func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func (d *detection) printEnv(w io.Writer) {
	sock, lg := "", ""
	if d.Socket != nil {
		sock = d.Socket.Addr
	}
	if d.Log != nil {
		lg = d.Log.Path
	}
	fmt.Fprintf(w, "DET_SOCKET=%s\n", shq(sock))
	fmt.Fprintf(w, "DET_LOG=%s\n", shq(lg))
	fmt.Fprintf(w, "DET_LOG_NOTE=%s\n", shq(d.LogNote))
	fmt.Fprintf(w, "DET_GROUPS=%s\n", shq(strings.Join(d.Groups, " ")))
	fmt.Fprintf(w, "DET_CONFIG_FILES=%s\n", shq(strings.Join(d.ConfigFiles, " ")))
	fmt.Fprintf(w, "DET_LISTEN_IP=%s\n", shq(d.Listen.IP))
}
