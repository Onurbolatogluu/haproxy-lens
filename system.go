package main

// Sunucunun kendi ölçümleri: CPU, bellek, disk doluluğu ve disk G/Ç.
// Veriler doğrudan /proc altından okunur; kabuk komutu çalıştırılmaz, ek yetki
// gerekmez (bu dosyalar herkese okunabilir) ve HAProxy'ye hiç dokunulmaz.
//
// Geçmiş, HAProxy ölçümleriyle aynı mantıkta tutulur: son 1 saat ince (her ölçümde
// bir nokta), ötesi dakikalık ortalama. Dakikalık özet diske de yazılır.

import (
	"bufio"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type SysPoint struct {
	T       int64   `json:"t"`
	CPU     float64 `json:"cpu"`     // %, boşta olmayan
	IOWait  float64 `json:"iowait"`  // %, disk bekleme
	Steal   float64 `json:"steal"`   // %, sanal makinede çalınan zaman
	MemPct  float64 `json:"memPct"`  // %, kullanılan bellek
	SwapPct float64 `json:"swapPct"` // %
	Read    float64 `json:"read"`    // bayt/sn
	Write   float64 `json:"write"`   // bayt/sn
	Load1   float64 `json:"load1"`
}

type DiskUsage struct {
	Path    string  `json:"path"`
	Total   int64   `json:"total"`
	Free    int64   `json:"free"`
	UsedPct float64 `json:"usedPct"`
}

type SystemState struct {
	OK        bool        `json:"ok"`
	Error     string      `json:"error,omitempty"`
	Cur       SysPoint    `json:"cur"`
	MemTotal  int64       `json:"memTotal"`
	MemUsed   int64       `json:"memUsed"`
	SwapTotal int64       `json:"swapTotal"`
	CPUs      int         `json:"cpus"`
	Load      [3]float64  `json:"load"`
	Disks     []DiskUsage `json:"disks"`
	History   []SysPoint  `json:"history"`
}

type cpuSayac struct{ toplam, bosta, iowait, steal float64 }

type SysPoller struct {
	interval  time.Duration
	yollar    []string // doluluğuna bakılacak dizinler
	retention int      // dakika

	mu       sync.RWMutex
	hist     []SysPoint
	dakika   []SysPoint
	curMin   int64
	minTop   SysPoint
	minN     int
	son      SysPoint
	memTotal int64
	memUsed  int64
	swapTot  int64
	cpus     int
	load     [3]float64
	disks    []DiskUsage
	oncekiC  cpuSayac
	oncekiD  map[string][2]float64
	oncekiAt time.Time
	hata     string
	dirty    bool
}

func NewSysPoller(interval time.Duration, retentionMin int, yollar ...string) *SysPoller {
	temiz := []string{"/"}
	for _, y := range yollar {
		if y != "" && !contains(temiz, y) {
			temiz = append(temiz, y)
		}
	}
	return &SysPoller{interval: interval, retention: retentionMin, yollar: temiz, oncekiD: map[string][2]float64{}}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func (s *SysPoller) Run() {
	s.tick()
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for range t.C {
		s.tick()
	}
}

func okuSatirlar(yol string) ([]string, error) {
	f, err := os.Open(yol)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out, sc.Err()
}

// /proc/stat ilk satırı: cpu user nice system idle iowait irq softirq steal ...
func cpuOku() (cpuSayac, int, error) {
	satirlar, err := okuSatirlar("/proc/stat")
	if err != nil {
		return cpuSayac{}, 0, err
	}
	var c cpuSayac
	cpus := 0
	for _, l := range satirlar {
		if strings.HasPrefix(l, "cpu ") {
			f := strings.Fields(l)[1:]
			for i, v := range f {
				n, _ := strconv.ParseFloat(v, 64)
				c.toplam += n
				switch i {
				case 3:
					c.bosta += n
				case 4:
					c.iowait = n
					c.bosta += n // iowait da boşta sayılır
				case 7:
					c.steal = n
				}
			}
		} else if strings.HasPrefix(l, "cpu") {
			cpus++
		}
	}
	return c, cpus, nil
}

func memOku() (total, used, swapTotal, swapUsed int64) {
	satirlar, _ := okuSatirlar("/proc/meminfo")
	var avail, swapFree int64
	for _, l := range satirlar {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		v, _ := strconv.ParseInt(f[1], 10, 64)
		v *= 1024
		switch f[0] {
		case "MemTotal:":
			total = v
		case "MemAvailable:":
			avail = v
		case "SwapTotal:":
			swapTotal = v
		case "SwapFree:":
			swapFree = v
		}
	}
	return total, total - avail, swapTotal, swapTotal - swapFree
}

// Bölüm değil, fiziksel aygıtlar sayılır; aksi hâlde aynı G/Ç iki kez toplanır.
func fizikselAygit(ad string) bool {
	switch {
	case strings.HasPrefix(ad, "loop"), strings.HasPrefix(ad, "ram"), strings.HasPrefix(ad, "dm-"),
		strings.HasPrefix(ad, "md"), strings.HasPrefix(ad, "sr"), strings.HasPrefix(ad, "zram"):
		return false
	case strings.HasPrefix(ad, "nvme"):
		return !strings.Contains(ad, "p") || !son2Rakam(ad) // nvme0n1 evet, nvme0n1p1 hayır
	case strings.HasPrefix(ad, "sd"), strings.HasPrefix(ad, "vd"), strings.HasPrefix(ad, "xvd"), strings.HasPrefix(ad, "hd"):
		return !son2Rakam(ad) // sda evet, sda1 hayır
	}
	return false
}

func son2Rakam(ad string) bool {
	if ad == "" {
		return false
	}
	c := ad[len(ad)-1]
	return c >= '0' && c <= '9'
}

// /proc/diskstats: 3=ad, 6=okunan sektör, 10=yazılan sektör (512 bayt)
func diskOku() map[string][2]float64 {
	out := map[string][2]float64{}
	satirlar, _ := okuSatirlar("/proc/diskstats")
	for _, l := range satirlar {
		f := strings.Fields(l)
		if len(f) < 10 || !fizikselAygit(f[2]) {
			continue
		}
		r, _ := strconv.ParseFloat(f[5], 64)
		w, _ := strconv.ParseFloat(f[9], 64)
		out[f[2]] = [2]float64{r * 512, w * 512}
	}
	return out
}

func loadOku() [3]float64 {
	var l [3]float64
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return l
	}
	f := strings.Fields(string(b))
	for i := 0; i < 3 && i < len(f); i++ {
		l[i], _ = strconv.ParseFloat(f[i], 64)
	}
	return l
}

func diskDoluluk(yollar []string) []DiskUsage {
	gorulen := map[uint64]bool{}
	var out []DiskUsage
	for _, y := range yollar {
		var st syscall.Statfs_t
		if err := syscall.Statfs(y, &st); err != nil {
			continue
		}
		anahtar := uint64(st.Fsid.X__val[0])<<32 | uint64(uint32(st.Fsid.X__val[1]))
		if st.Blocks == 0 || gorulen[anahtar] {
			continue // aynı dosya sistemi iki kez listelenmesin
		}
		gorulen[anahtar] = true
		toplam := int64(st.Blocks) * int64(st.Bsize)
		bos := int64(st.Bavail) * int64(st.Bsize)
		out = append(out, DiskUsage{Path: y, Total: toplam, Free: bos,
			UsedPct: 100 * float64(toplam-bos) / float64(toplam)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func (s *SysPoller) tick() {
	simdi := time.Now()
	c, cpus, err := cpuOku()
	if err != nil {
		s.mu.Lock()
		s.hata = "sistem ölçümleri okunamadı: " + err.Error()
		s.mu.Unlock()
		return
	}
	memT, memU, swapT, swapU := memOku()
	d := diskOku()
	load := loadOku()
	disks := diskDoluluk(s.yollar)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.hata = ""
	s.memTotal, s.memUsed, s.swapTot, s.cpus, s.load, s.disks = memT, memU, swapT, cpus, load, disks

	p := SysPoint{T: simdi.UnixMilli(), Load1: load[0]}
	if memT > 0 {
		p.MemPct = 100 * float64(memU) / float64(memT)
	}
	if swapT > 0 {
		p.SwapPct = 100 * float64(swapU) / float64(swapT)
	}
	if s.oncekiC.toplam > 0 && c.toplam > s.oncekiC.toplam {
		dt := c.toplam - s.oncekiC.toplam
		p.CPU = 100 * (dt - (c.bosta - s.oncekiC.bosta)) / dt
		p.IOWait = 100 * (c.iowait - s.oncekiC.iowait) / dt
		p.Steal = 100 * (c.steal - s.oncekiC.steal) / dt
	}
	if !s.oncekiAt.IsZero() {
		sn := simdi.Sub(s.oncekiAt).Seconds()
		if sn > 0 {
			for ad, v := range d {
				o, ok := s.oncekiD[ad]
				if !ok {
					continue
				}
				if v[0] >= o[0] {
					p.Read += (v[0] - o[0]) / sn
				}
				if v[1] >= o[1] {
					p.Write += (v[1] - o[1]) / sn
				}
			}
		}
	}
	s.oncekiC, s.oncekiD, s.oncekiAt = c, d, simdi
	s.son = p

	s.hist = append(s.hist, p)
	if n := int(time.Hour / s.interval); len(s.hist) > n {
		s.hist = s.hist[len(s.hist)-n:]
	}
	s.dakikaEkle(p, simdi)
}

func (s *SysPoller) dakikaEkle(p SysPoint, simdi time.Time) {
	dk := simdi.Unix() / 60
	if s.curMin == 0 {
		s.curMin = dk
	}
	if dk != s.curMin {
		if s.minN > 0 {
			n := float64(s.minN)
			s.dakika = append(s.dakika, SysPoint{T: s.curMin * 60_000,
				CPU: s.minTop.CPU / n, IOWait: s.minTop.IOWait / n, Steal: s.minTop.Steal / n,
				MemPct: s.minTop.MemPct / n, SwapPct: s.minTop.SwapPct / n,
				Read: s.minTop.Read / n, Write: s.minTop.Write / n, Load1: s.minTop.Load1 / n})
			if len(s.dakika) > s.retention {
				s.dakika = s.dakika[len(s.dakika)-s.retention:]
			}
			s.dirty = true
		}
		s.curMin, s.minTop, s.minN = dk, SysPoint{}, 0
	}
	s.minTop.CPU += p.CPU
	s.minTop.IOWait += p.IOWait
	s.minTop.Steal += p.Steal
	s.minTop.MemPct += p.MemPct
	s.minTop.SwapPct += p.SwapPct
	s.minTop.Read += p.Read
	s.minTop.Write += p.Write
	s.minTop.Load1 += p.Load1
	s.minN++
}

func (s *SysPoller) State(minutes int) *SystemState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.oncekiAt.IsZero() && s.hata == "" {
		return nil // henüz ilk ölçüm alınmadı
	}
	st := &SystemState{OK: s.hata == "", Error: s.hata, Cur: s.son, MemTotal: s.memTotal, MemUsed: s.memUsed,
		SwapTotal: s.swapTot, CPUs: s.cpus, Load: s.load, Disks: s.disks}
	from := time.Now().UnixMilli() - int64(minutes)*60_000
	// Uzun aralıklarda dakikalık ortalama; kısa aralıklarda ince ölçüm. İnce ölçüm
	// aralığın başına yetişmiyorsa (ajan yeni başladıysa) eksik kalan eski kısım
	// diskten gelen dakikalık veriyle doldurulur. Eskiden bu durumda ince veri tamamen
	// bırakılıyordu; taze kurulumda grafik tek noktaya iniyordu.
	var kaynak []SysPoint
	if minutes > fineWindowMin {
		kaynak = s.dakika
	} else {
		ilkInce := int64(1<<62 - 1)
		if len(s.hist) > 0 {
			ilkInce = s.hist[0].T
		}
		for _, p := range s.dakika {
			if p.T < ilkInce-60_000 {
				kaynak = append(kaynak, p)
			}
		}
		kaynak = append(kaynak, s.hist...)
	}
	for _, p := range kaynak {
		if p.T >= from {
			st.History = append(st.History, p)
		}
	}
	st.History = sysDownsample(st.History, maxChartPts)
	return st
}

// Grafik için nokta seyreltme (haproxy tarafındakiyle aynı mantık)
func sysDownsample(pts []SysPoint, max int) []SysPoint {
	if len(pts) <= max || max <= 0 {
		return pts
	}
	grup := (len(pts) + max - 1) / max
	out := make([]SysPoint, 0, max)
	for i := 0; i < len(pts); i += grup {
		son := i + grup
		if son > len(pts) {
			son = len(pts)
		}
		var t SysPoint
		n := float64(son - i)
		for _, p := range pts[i:son] {
			t.CPU += p.CPU
			t.IOWait += p.IOWait
			t.Steal += p.Steal
			t.MemPct += p.MemPct
			t.SwapPct += p.SwapPct
			t.Read += p.Read
			t.Write += p.Write
			t.Load1 += p.Load1
		}
		out = append(out, SysPoint{T: pts[son-1].T, CPU: t.CPU / n, IOWait: t.IOWait / n, Steal: t.Steal / n,
			MemPct: t.MemPct / n, SwapPct: t.SwapPct / n, Read: t.Read / n, Write: t.Write / n, Load1: t.Load1 / n})
	}
	return out
}

func (s *SysPoller) dumpDakika() []SysPoint {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dirty = false
	return append([]SysPoint(nil), s.dakika...)
}

func (s *SysPoller) loadDakika(p []SysPoint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kesim := time.Now().UnixMilli() - int64(s.retention)*60_000
	for _, x := range p {
		if x.T >= kesim {
			s.dakika = append(s.dakika, x)
		}
	}
	if len(s.dakika) > s.retention {
		s.dakika = s.dakika[len(s.dakika)-s.retention:]
	}
}

func (s *SysPoller) isDirty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dirty
}
