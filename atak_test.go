package main

import (
	"fmt"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// Tarama saldırısı: her istek benzersiz bir adrese. Bellek bütçesi devredeyken
// ajan en eski ayrıntıyı bırakmalı, bellek bütçenin yakınında kalmalı.
func TestAtakBellekButcesi(t *testing.T) {
	if testing.Short() {
		t.Skip("uzun süren ölçüm")
	}
	const butceMB = 100
	runtime.GC()
	var once, sonra runtime.MemStats
	runtime.ReadMemStats(&once)

	a := NewLogAnalyzer("file:/yok", "")
	a.SetRetention(1440)
	a.SetDetailWindows(1440, 1440) // tam ayrıntı istendi
	a.SetBudget(butceMB << 20)
	simdi := time.Now()
	istek := 0
	for dk := 179; dk >= 0; dk-- { // 3 saat tarama
		ts := simdi.Add(-time.Duration(dk) * time.Minute)
		for i := 0; i < 3000; i++ {
			yol := "/vendor/" + strconv.Itoa(dk) + "/" + strconv.Itoa(i) + "/eval-stdin.php"
			a.add(logRecord{At: ts, Client: fmt.Sprintf("203.0.113.%d", i%256), Frontend: "fe",
				Backend: "fe", Server: "<NOSRV>", Status: 403, Method: "GET",
				Path: yol, RawPath: yol, Kind: KindDenied})
			istek++
		}
		a.mu.Lock()
		a.bakimAt(ts.Unix() / 60)
		a.mu.Unlock()
	}
	runtime.GC()
	runtime.ReadMemStats(&sonra)
	mb := (sonra.HeapAlloc - once.HeapAlloc) / 1024 / 1024

	rep := a.Report(180)
	t.Logf("3 saat tarama, %d istek: %d MB (bütçe %d MB)", istek, mb, butceMB)
	t.Logf("  ayrıntı kapsamı: %d dk, liste kapsamı: %d dk", rep.DetailMinutes, rep.ListMinutes)
	if mb > butceMB*2 {
		t.Fatalf("bütçe tutmadı: %d MB", mb)
	}
	// Sayılar bütçeden etkilenmemeli
	if rep.Kinds[KindDenied] != int64(istek) {
		t.Fatalf("sayımlar eksik: %d, beklenen %d", rep.Kinds[KindDenied], istek)
	}
	runtime.KeepAlive(a)
}

// Bugünkü kurulumun (0.11.0: ayrıntı 1 saat, liste 6 saat, bütçe yok) saldırı altındaki hâli
func TestAtakDayanikliligi(t *testing.T) {
	if testing.Short() {
		t.Skip("uzun süren ölçüm")
	}
	runtime.GC()
	var once, sonra runtime.MemStats
	runtime.ReadMemStats(&once)
	a := NewLogAnalyzer("file:/yok", "")
	a.SetRetention(1440)
	a.SetDetailWindows(60, 360)
	a.SetBudget(250 << 20)
	simdi := time.Now()
	istek := 0
	for dk := 359; dk >= 0; dk-- {
		ts := simdi.Add(-time.Duration(dk) * time.Minute)
		for i := 0; i < 8000; i++ { // çok ağır: dakikada 8.000 benzersiz adres
			yol := "/vendor/" + strconv.Itoa(dk) + "/" + strconv.Itoa(i) + "/eval-stdin.php"
			a.add(logRecord{At: ts, Client: fmt.Sprintf("203.0.113.%d", i%256), Frontend: "fe",
				Backend: "fe", Server: "<NOSRV>", Status: 403, Method: "GET",
				Path: yol, RawPath: yol, Kind: KindDenied})
			istek++
		}
		a.mu.Lock()
		a.bakimAt(ts.Unix() / 60)
		a.mu.Unlock()
	}
	runtime.GC()
	runtime.ReadMemStats(&sonra)
	mb := (sonra.HeapAlloc - once.HeapAlloc) / 1024 / 1024
	rep := a.Report(360)
	t.Logf("6 saat ağır saldırı (%d istek): %d MB, ayrıntı kapsamı %d dk", istek, mb, rep.DetailMinutes)
	if mb > 400 {
		t.Fatalf("servis tavanına (512 MB) fazla yaklaşıyor: %d MB", mb)
	}
	if rep.Kinds[KindDenied] != int64(istek) {
		t.Fatalf("sayımlar eksik: %d / %d", rep.Kinds[KindDenied], istek)
	}
	runtime.KeepAlive(a)
}
