package main

// HAProxy'den stats okuma.
// Bu dosya HAProxy'ye YALNIZCA allowedCommands içindeki sabit, salt okunur
// komutları gönderir. Başka bir komut gönderecek kod yolu yoktur.

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

var allowedCommands = map[string]bool{
	"show info": true,
	"show stat": true,
}

func query(socket, cmd string) (string, error) {
	if !allowedCommands[cmd] {
		return "", fmt.Errorf("izin verilmeyen komut: %q", cmd)
	}
	network, addr := socketAddr(socket)
	conn, err := net.DialTimeout(network, addr, 2*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte(cmd + "\n")); err != nil {
		return "", err
	}
	b, err := io.ReadAll(io.LimitReader(conn, 32<<20))
	return string(b), err
}

// "unix:/yol", "/yol", "unix:@soyut" ve "tcp:127.0.0.1:9999" biçimlerini anlar.
func socketAddr(s string) (string, string) {
	switch {
	case strings.HasPrefix(s, "tcp:"):
		return "tcp", strings.TrimPrefix(s, "tcp:")
	case strings.HasPrefix(s, "unix:"):
		return "unix", strings.TrimPrefix(s, "unix:")
	}
	return "unix", s
}

func parseInfo(s string) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if ok && k != "" {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

func parseStat(s string) ([]map[string]string, error) {
	var header []string
	var rows []map[string]string
	sc := bufio.NewScanner(strings.NewReader(s))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			header = strings.Split(strings.TrimSpace(strings.TrimPrefix(line, "#")), ",")
			continue
		}
		if header == nil {
			continue
		}
		vals := strings.Split(line, ",")
		row := make(map[string]string, len(header))
		for i, h := range header {
			if h == "" {
				continue
			}
			if i < len(vals) {
				row[h] = vals[i]
			} else {
				row[h] = ""
			}
		}
		if row["pxname"] != "" && row["svname"] != "" {
			rows = append(rows, row)
		}
	}
	if header == nil {
		return nil, fmt.Errorf("show stat çıktısında başlık satırı yok")
	}
	return rows, nil
}

type Snapshot struct {
	At     int64               `json:"at"` // unix ms
	Info   map[string]string   `json:"info"`
	Rows   []map[string]string `json:"rows"`
	Header []string            `json:"header"`
}

// Grafik için saniyelik özet noktası (tüm frontend'lerin toplamı)
type Point struct {
	T    int64   `json:"t"`
	C2   float64 `json:"c2"`
	C3   float64 `json:"c3"`
	C4   float64 `json:"c4"`
	C5   float64 `json:"c5"`
	In   float64 `json:"in"`  // byte/sn
	Out  float64 `json:"out"` // byte/sn
	Reqs float64 `json:"reqs"`
}

type StatsPoller struct {
	sockMu    sync.Mutex
	failSince time.Time
	socket    string
	interval  time.Duration
	maxHist   int

	mu      sync.RWMutex
	cur     *Snapshot
	prev    *Snapshot
	hist    []Point
	samples []counterSample // 10 saniyede bir, satır bazında sayaç örnekleri (son 1 saat)
	lastErr string
	errAt   int64
}

// Zaman aralığı hesapları için saklanan sayaçlar (sıra önemli, windowRow bu sırayı kullanır)
var windowFields = []string{"req_tot", "stot", "hrsp_1xx", "hrsp_2xx", "hrsp_3xx", "hrsp_4xx", "hrsp_5xx", "hrsp_other", "econ", "eresp"}

const (
	sampleEveryMs = 10_000
	maxSamples    = 3600*1000/sampleEveryMs + 2
	maxWindowMin  = 60
	maxChartPts   = 360
)

type counterSample struct {
	At   int64
	Vals map[string][]float64
}

func rowCounters(r map[string]string) []float64 {
	v := make([]float64, len(windowFields))
	for i, f := range windowFields {
		v[i] = num(r[f])
	}
	return v
}

func (p *StatsPoller) addSample(s *Snapshot) {
	if n := len(p.samples); n > 0 && s.At-p.samples[n-1].At < sampleEveryMs {
		return
	}
	vals := make(map[string][]float64, len(s.Rows))
	for _, r := range s.Rows {
		vals[r["pxname"]+"|"+r["svname"]] = rowCounters(r)
	}
	p.samples = append(p.samples, counterSample{At: s.At, Vals: vals})
	if len(p.samples) > maxSamples {
		p.samples = p.samples[len(p.samples)-maxSamples:]
	}
}

func NewStatsPoller(socket string, interval time.Duration, maxHist int) *StatsPoller {
	return &StatsPoller{socket: socket, interval: interval, maxHist: maxHist}
}

func (p *StatsPoller) Run() {
	p.tick()
	t := time.NewTicker(p.interval)
	defer t.Stop()
	for range t.C {
		p.tick()
	}
}

// Socket çalışmazsa gözlemci config'teki başka bir socket'e geçebilir.
func (p *StatsPoller) SetSocket(s string) {
	p.sockMu.Lock()
	p.socket = s
	p.sockMu.Unlock()
}

func (p *StatsPoller) Socket() string {
	p.sockMu.Lock()
	defer p.sockMu.Unlock()
	return p.socket
}

// Stats kaç süredir okunamıyor (0: sorun yok)
func (p *StatsPoller) FailingFor() time.Duration {
	p.sockMu.Lock()
	defer p.sockMu.Unlock()
	if p.failSince.IsZero() {
		return 0
	}
	return time.Since(p.failSince)
}

// Son N dakikada backend'lerin aldığı toplam istek (log gözlemiyle karşılaştırmak için)
func (p *StatsPoller) BackendRequests(minutes int) float64 {
	st := p.State(minutes)
	if st.Window == nil || st.Cur == nil {
		return 0
	}
	var n float64
	for _, r := range st.Cur.Rows {
		if r["type"] == "1" {
			n += st.Window.Rows[r["pxname"]+"|"+r["svname"]].N
		}
	}
	return n
}

func (p *StatsPoller) tick() {
	sock := p.Socket()
	infoRaw, err := query(sock, "show info")
	if err == nil {
		var statRaw string
		statRaw, err = query(sock, "show stat")
		if err == nil {
			var rows []map[string]string
			rows, err = parseStat(statRaw)
			if err == nil {
				p.store(&Snapshot{At: time.Now().UnixMilli(), Info: parseInfo(infoRaw), Rows: rows, Header: headerOf(statRaw)})
				p.sockMu.Lock()
				p.failSince = time.Time{}
				p.sockMu.Unlock()
				return
			}
		}
	}
	p.mu.Lock()
	p.lastErr = err.Error()
	p.errAt = time.Now().UnixMilli()
	p.mu.Unlock()
	p.sockMu.Lock()
	if p.failSince.IsZero() {
		p.failSince = time.Now()
	}
	p.sockMu.Unlock()
}

func headerOf(statRaw string) []string {
	for _, l := range strings.Split(statRaw, "\n") {
		if strings.HasPrefix(l, "#") {
			return strings.Split(strings.TrimSpace(strings.TrimPrefix(l, "#")), ",")
		}
	}
	return nil
}

func (p *StatsPoller) store(s *Snapshot) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cur != nil && num(s.Info["Uptime_sec"]) < num(p.cur.Info["Uptime_sec"]) {
		p.samples = nil // HAProxy yeniden başladı, sayaçlar sıfırlandı; aralık hesabı baştan
	}
	p.addSample(s)
	p.prev, p.cur = p.cur, s
	p.lastErr = ""
	if p.prev == nil {
		return
	}
	// HAProxy yeniden başlatıldıysa sayaçlar sıfırlanır; o aralığı atla
	if num(s.Info["Uptime_sec"]) < num(p.prev.Info["Uptime_sec"]) {
		return
	}
	dt := float64(s.At-p.prev.At) / 1000
	if dt <= 0 {
		return
	}
	prevByKey := map[string]map[string]string{}
	for _, r := range p.prev.Rows {
		prevByKey[r["pxname"]+"|"+r["svname"]] = r
	}
	pt := Point{T: s.At}
	d := func(a, b map[string]string, f string) float64 {
		v := num(a[f]) - num(b[f])
		if v < 0 {
			return 0
		}
		return v / dt
	}
	for _, r := range s.Rows {
		if r["type"] != "0" {
			continue
		}
		pr, ok := prevByKey[r["pxname"]+"|"+r["svname"]]
		if !ok {
			continue
		}
		pt.C2 += d(r, pr, "hrsp_2xx")
		pt.C3 += d(r, pr, "hrsp_3xx")
		pt.C4 += d(r, pr, "hrsp_4xx")
		pt.C5 += d(r, pr, "hrsp_5xx")
		pt.In += d(r, pr, "bin")
		pt.Out += d(r, pr, "bout")
		pt.Reqs += d(r, pr, "req_tot")
	}
	p.hist = append(p.hist, pt)
	if len(p.hist) > p.maxHist {
		p.hist = p.hist[len(p.hist)-p.maxHist:]
	}
}

type StateResponse struct {
	OK      bool      `json:"ok"`
	Error   string    `json:"error,omitempty"`
	ErrorAt int64     `json:"errorAt,omitempty"`
	Cur     *Snapshot `json:"cur"`
	Prev    *Snapshot `json:"prev"`
	History []Point   `json:"history"`
	Window  *Window   `json:"window"`
}

// Seçilen aralıkta her satırın sayaç farkı: kaç istek, kaçı hangi yanıt sınıfı, kaç bağlantı hatası.
type WindowRow struct {
	N     float64    `json:"n"`
	Codes [6]float64 `json:"codes"` // 1xx, 2xx, 3xx, 4xx, 5xx, diğer
	Econ  float64    `json:"econ"`
	Eresp float64    `json:"eresp"`
}

type Window struct {
	Minutes int                  `json:"minutes"`
	Seconds float64              `json:"seconds"` // gerçekte kapsanan süre (ajan yeni başladıysa daha kısa)
	Rows    map[string]WindowRow `json:"rows"`
}

func clampMinutes(m int) int {
	if m < 1 {
		return 1
	}
	if m > maxWindowMin {
		return maxWindowMin
	}
	return m
}

func (p *StatsPoller) State(minutes int) StateResponse {
	minutes = clampMinutes(minutes)
	p.mu.RLock()
	defer p.mu.RUnlock()
	resp := StateResponse{OK: p.lastErr == "" && p.cur != nil, Error: p.lastErr, ErrorAt: p.errAt, Cur: p.cur, Prev: p.prev}
	if p.cur == nil {
		return resp
	}
	from := p.cur.At - int64(minutes)*60_000
	var pts []Point
	for _, pt := range p.hist {
		if pt.T >= from {
			pts = append(pts, pt)
		}
	}
	resp.History = downsample(pts, maxChartPts)
	resp.Window = p.window(from, minutes)
	return resp
}

// Aralığın başına denk gelen (ya da ondan hemen önceki) örnekle şimdiki değerlerin farkı
func (p *StatsPoller) window(from int64, minutes int) *Window {
	if len(p.samples) == 0 {
		return nil
	}
	base := p.samples[0]
	for _, s := range p.samples {
		if s.At > from {
			break
		}
		base = s
	}
	w := &Window{Minutes: minutes, Seconds: float64(p.cur.At-base.At) / 1000, Rows: map[string]WindowRow{}}
	for _, r := range p.cur.Rows {
		k := r["pxname"] + "|" + r["svname"]
		b, ok := base.Vals[k]
		if !ok {
			continue
		}
		c := rowCounters(r)
		d := func(i int) float64 {
			if v := c[i] - b[i]; v > 0 {
				return v
			}
			return 0
		}
		n := d(0)
		if r["req_tot"] == "" {
			n = d(1) // eski sürümlerde sunucu satırında req_tot yok
		}
		w.Rows[k] = WindowRow{N: n, Codes: [6]float64{d(2), d(3), d(4), d(5), d(6), d(7)}, Econ: d(8), Eresp: d(9)}
	}
	return w
}

// Grafik için en fazla max nokta: ardışık noktaların ortalaması
func downsample(pts []Point, max int) []Point {
	if len(pts) <= max {
		return pts
	}
	size := (len(pts) + max - 1) / max
	var out []Point
	for i := 0; i < len(pts); i += size {
		end := i + size
		if end > len(pts) {
			end = len(pts)
		}
		var a Point
		for _, p := range pts[i:end] {
			a.C2 += p.C2
			a.C3 += p.C3
			a.C4 += p.C4
			a.C5 += p.C5
			a.In += p.In
			a.Out += p.Out
			a.Reqs += p.Reqs
		}
		n := float64(end - i)
		out = append(out, Point{T: pts[end-1].T, C2: a.C2 / n, C3: a.C3 / n, C4: a.C4 / n, C5: a.C5 / n, In: a.In / n, Out: a.Out / n, Reqs: a.Reqs / n})
	}
	return out
}

func num(s string) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}
