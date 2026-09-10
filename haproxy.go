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
	socket   string
	interval time.Duration
	maxHist  int

	mu      sync.RWMutex
	cur     *Snapshot
	prev    *Snapshot
	hist    []Point
	lastErr string
	errAt   int64
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

func (p *StatsPoller) tick() {
	infoRaw, err := query(p.socket, "show info")
	if err == nil {
		var statRaw string
		statRaw, err = query(p.socket, "show stat")
		if err == nil {
			var rows []map[string]string
			rows, err = parseStat(statRaw)
			if err == nil {
				p.store(&Snapshot{At: time.Now().UnixMilli(), Info: parseInfo(infoRaw), Rows: rows, Header: headerOf(statRaw)})
				return
			}
		}
	}
	p.mu.Lock()
	p.lastErr = err.Error()
	p.errAt = time.Now().UnixMilli()
	p.mu.Unlock()
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
}

func (p *StatsPoller) State() StateResponse {
	p.mu.RLock()
	defer p.mu.RUnlock()
	h := make([]Point, len(p.hist))
	copy(h, p.hist)
	return StateResponse{OK: p.lastErr == "" && p.cur != nil, Error: p.lastErr, ErrorAt: p.errAt, Cur: p.cur, Prev: p.prev, History: h}
}

func num(s string) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}
