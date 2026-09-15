package main

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func rssMB() int {
	b, _ := os.ReadFile("/proc/self/status")
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "VmRSS:") {
			kb, _ := strconv.Atoi(strings.Fields(l)[1])
			return kb / 1024
		}
	}
	return -1
}

// Saldırı bitince bellek hem ajanın içinde boşalmalı hem de işletim sistemine iade
// edilmeli. İade Go tarafından kendiliğinden hemen yapılmadığı için tetikleniyor;
// systemd'nin bellek tavanı RSS üzerinden uygulandığından bu önemli.
func TestSaldiriSonrasiBellekIadesi(t *testing.T) {
	if testing.Short() {
		t.Skip("uzun süren ölçüm")
	}
	basIade := iadeSayaci.Load()
	a := NewLogAnalyzer("file:/yok", "")
	a.SetRetention(1440)
	a.SetDetailWindows(60, 360)
	a.SetBudget(250 << 20)
	simdi := time.Now()

	// 6 saat ağır tarama
	for dk := 359; dk >= 0; dk-- {
		ts := simdi.Add(-time.Duration(dk) * time.Minute)
		for i := 0; i < 8000; i++ {
			yol := "/vendor/" + strconv.Itoa(dk) + "/" + strconv.Itoa(i) + "/eval.php"
			a.add(logRecord{At: ts, Client: fmt.Sprintf("203.0.113.%d", i%256), Frontend: "fe",
				Backend: "fe", Server: "<NOSRV>", Status: 403, Method: "GET",
				Path: yol, RawPath: yol, Kind: KindDenied})
		}
		a.mu.Lock()
		a.bakimAt(ts.Unix() / 60)
		a.mu.Unlock()
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	tepeHeap := m.HeapAlloc / 1024 / 1024
	t.Logf("saldırı sırasında: heap %d MB, RSS %d MB", tepeHeap, rssMB())

	// Saldırı bitti, 2 saat normal trafik
	for dk := 0; dk < 120; dk++ {
		ts := simdi.Add(time.Duration(dk) * time.Minute)
		for i := 0; i < 500; i++ {
			yol := "/urun/" + strconv.Itoa(i%50)
			a.add(logRecord{At: ts, Client: "198.51.100.5", Frontend: "fe", Backend: "be_web",
				Server: "s1", Status: 200, Method: "GET", Path: yol, RawPath: yol, Kind: KindServed})
		}
		a.mu.Lock()
		a.bakimAt(ts.Unix() / 60)
		a.mu.Unlock()
	}
	runtime.GC()
	runtime.ReadMemStats(&m)
	sonHeap := m.HeapAlloc / 1024 / 1024
	t.Logf("saldırı bittikten sonra: heap %d MB", sonHeap)

	// Ajanın içinde bellek gerçekten boşalmalı
	if sonHeap > tepeHeap/4 {
		t.Fatalf("bellek boşalmadı: tepe %d MB, sonra %d MB", tepeHeap, sonHeap)
	}
	// İşletim sistemine iade tetiklenmiş olmalı
	if iadeSayaci.Load() == basIade {
		t.Fatal("bellek iadesi hiç tetiklenmedi")
	}
	// RSS iadesi arka planda ve kademeli; bilgi olarak yazılır (ortama göre değişir)
	for i := 1; i <= 3; i++ {
		time.Sleep(2 * time.Second)
		t.Logf("  iade sonrası +%d sn: RSS %d MB", i*2, rssMB())
	}
	runtime.KeepAlive(a)
}
