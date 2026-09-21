package main

import (
	"strconv"
	"testing"
	"time"
)

// Geç gelen log satırları eski bir dakikaya yazılabiliyor (ajan açılışta birikmiş log'u
// okurken ya da log gecikmeliyse). O kova sadeleştirilmiş olsa bile yeniden dolabilir;
// bakım bunu her seferinde yeniden uygulamalı, yoksa bellek sınırsız büyür.
func TestGecGelenLogSadelestirme(t *testing.T) {
	a := NewLogAnalyzer("file:/yok", "")
	a.SetRetention(1440)
	a.SetDetailWindows(60, 360)
	a.SetBudget(0)
	simdi := time.Now()
	eskiTs := simdi.Add(-120 * time.Minute) // 2 saat öncesine yazılan satırlar
	for i := 0; i < 3000; i++ {
		yol := "/x/" + strconv.Itoa(i)
		a.add(logRecord{At: eskiTs, Client: "203.0.113.1", Frontend: "fe", Backend: "fe", Server: "<NOSRV>",
			Status: 403, Method: "GET", Path: yol, RawPath: yol, Kind: KindDenied})
	}
	k := eskiTs.Unix() / 60
	if b := a.buckets[k]; len(b.paths) != 3000 {
		t.Fatalf("kayıtlar eksik: %d", len(b.paths))
	}
	a.mu.Lock()
	a.bakimAt(simdi.Unix() / 60)
	a.mu.Unlock()
	b := a.buckets[k]
	if len(b.paths) > 60 || len(b.blocked) > 60 {
		t.Fatalf("sadeleştirme tekrar uygulanmadı: %d yol, %d engellenen kayıt duruyor", len(b.paths), len(b.blocked))
	}
	// Sayılar korunmalı
	if b.kinds[KindDenied] != 3000 {
		t.Fatalf("sayımlar bozuldu: %d", b.kinds[KindDenied])
	}
}
