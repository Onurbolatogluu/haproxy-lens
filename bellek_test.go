package main

import (
	"fmt"
	"math/rand"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// Saklama ayarlarının bellek maliyetini ölçer. Yük, yoğun bir LB'ye yakın seçildi:
// saatte ~44.000 istek, dakikada 300 farklı adres, 150 farklı IP, %20 hata.
func bellekOlc(t *testing.T, dakika, detay, liste int) uint64 {
	const (
		istekDk   = 733
		farkliYol = 300
		farkliIP  = 150
		hataOrani = 0.20
	)
	yollar := make([]string, farkliYol)
	for i := range yollar {
		yollar[i] = "/api/v1/bolum" + strconv.Itoa(i%37) + "/kaynak/" + strconv.Itoa(i)
	}
	ipler := make([]string, farkliIP)
	for i := range ipler {
		ipler[i] = fmt.Sprintf("198.51.%d.%d", i/256, i%256)
	}
	beler := []string{"be_web", "be_api", "be_static", "be_auth", "be_admin"}

	runtime.GC()
	var once, sonra runtime.MemStats
	runtime.ReadMemStats(&once)

	a := NewLogAnalyzer("file:/yok", "")
	a.SetRetention(dakika)
	a.SetDetailWindows(dakika+1, dakika+1) // doldururken sadeleştirme yok
	a.SetBudget(0)                         // bütçe ayrı test ediliyor
	rnd := rand.New(rand.NewSource(1))
	simdi := time.Now()
	for dk := dakika - 1; dk >= 0; dk-- {
		ts := simdi.Add(-time.Duration(dk) * time.Minute)
		for i := 0; i < istekDk; i++ {
			kod := 200
			if rnd.Float64() < hataOrani {
				kod = 404
				if rnd.Float64() < 0.5 {
					kod = 502
				}
			}
			yol := yollar[rnd.Intn(farkliYol)]
			a.add(logRecord{At: ts, Client: ipler[rnd.Intn(farkliIP)], Frontend: "fe",
				Backend: beler[rnd.Intn(len(beler))], Server: "s1", Status: kod, Method: "GET",
				Path: yol, RawPath: yol, Host: "ornek.com", TLS: true, Kind: KindServed})
		}
	}
	// Gerçek çalışmadaki son durum: eski dakikalar kademeli sadeleşmiş hâlde
	a.SetDetailWindows(detay, liste)
	a.mu.Lock()
	a.bakimAt(simdi.Unix() / 60)
	a.mu.Unlock()

	runtime.GC()
	runtime.ReadMemStats(&sonra)
	mb := (sonra.HeapAlloc - once.HeapAlloc) / 1024 / 1024
	runtime.KeepAlive(a)
	return mb
}

// Varsayılan ayarın bellek kullanımı servisin tavanının (256 MB) çok altında kalmalı.
// Bu test aynı zamanda ayarları değiştirmenin maliyetini belgeler.
func TestBellekKullanimi(t *testing.T) {
	if testing.Short() {
		t.Skip("uzun süren ölçüm")
	}
	// Varsayılan kurulum: 24 saat tam ayrıntı. Bütçe (500 MB) ve servis tavanı (768M)
	// buna göre seçildi; ölçüm bu varsayımı doğrular.
	tam := bellekOlc(t, 1440, 1440, 1440)
	t.Logf("24 saat tam ayrıntı (varsayılan kurulum): %d MB", tam)
	if tam > 600 {
		t.Fatalf("%d MB, varsayılan bütçenin (500 MB) belirgin üstünde", tam)
	}
	for _, c := range []struct {
		ad           string
		detay, liste int
	}{{"ayrıntı 1 saat, liste 6 saat", 60, 360}, {"ayrıntı 6 saat, liste 24 saat", 360, 1440}} {
		t.Logf("24 saat, %s: %d MB", c.ad, bellekOlc(t, 1440, c.detay, c.liste))
	}
}
