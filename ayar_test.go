package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Hatalı parametreler sessizce düzeltilmemeli, açıkça reddedilmeli
func TestAyarDogrulama(t *testing.T) {
	h, dk := time.Hour, time.Minute
	gecerli := []struct {
		ret, detay, liste time.Duration
		butce             int
	}{
		{24 * h, h, 6 * h, 250},       // varsayılan
		{24 * h, 24 * h, 24 * h, 600}, // her şey tam ayrıntı
		{h, 5 * dk, h, 16},            // en küçük değerler
		{30 * 24 * h, 6 * h, 24 * h, 500},
	}
	for _, g := range gecerli {
		if err := ayarlariDogrula(g.ret, g.detay, g.liste, g.butce, 2*time.Second); err != nil {
			t.Errorf("geçerli ayar reddedildi %+v: %v", g, err)
		}
	}
	hatali := []struct {
		ad                string
		ret, detay, liste time.Duration
		butce             int
		aralik            time.Duration
		icermeli          string
	}{
		{"saklama 30 dk", 30 * dk, 5 * dk, 30 * dk, 250, 2 * time.Second, "RETENTION"},
		{"saklama 60 gün", 60 * 24 * h, h, 6 * h, 250, 2 * time.Second, "30 gün"},
		{"ayrıntı 1 dk", 24 * h, dk, 6 * h, 250, 2 * time.Second, "DETAIL"},
		{"liste < ayrıntı", 24 * h, 6 * h, h, 250, 2 * time.Second, "kısa olamaz"},
		{"liste > saklama", 24 * h, h, 48 * h, 250, 2 * time.Second, "uzun olamaz"},
		{"negatif bütçe", 24 * h, h, 6 * h, -5, 2 * time.Second, "BUDGET"},
		{"çok küçük bütçe", 24 * h, h, 6 * h, 8, 2 * time.Second, "BUDGET"},
		{"aralık 100 ms", 24 * h, h, 6 * h, 250, 100 * time.Millisecond, "aralığı"},
	}
	for _, x := range hatali {
		err := ayarlariDogrula(x.ret, x.detay, x.liste, x.butce, x.aralik)
		if err == nil {
			t.Errorf("%s: kabul edilmemeliydi", x.ad)
			continue
		}
		if !strings.Contains(err.Error(), x.icermeli) {
			t.Errorf("%s: hata mesajı anlaşılır değil: %v", x.ad, err)
		}
	}
}

func TestSureYaz(t *testing.T) {
	for d, bekle := range map[time.Duration]string{
		24 * time.Hour: "1 gün", 48 * time.Hour: "2 gün", 6 * time.Hour: "6 saat",
		30 * time.Minute: "30 dakika", 90 * time.Minute: "90 dakika",
	} {
		if got := sureYaz(d); got != bekle {
			t.Errorf("%v: %q, beklenen %q", d, got, bekle)
		}
	}
}

// Panel başka siteye gömülmemeli ve tarayıcı korumaları açık olmalı
func TestGuvenlikBasliklari(t *testing.T) {
	h := guvenlikBasliklari(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	for ad, bekle := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	} {
		if got := rec.Header().Get(ad); got != bekle {
			t.Errorf("%s: %q, beklenen %q", ad, got, bekle)
		}
	}
	csp := rec.Header().Get("Content-Security-Policy")
	for _, parca := range []string{"default-src 'self'", "frame-ancestors 'none'", "script-src 'self'"} {
		if !strings.Contains(csp, parca) {
			t.Errorf("CSP'de %q yok: %s", parca, csp)
		}
	}
}

// Ajan geçmişi yüklerken veri uçları "yükleniyor" demeli, arayüz dosyaları ise açılmalı
func TestHazirKapisi(t *testing.T) {
	ic := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("tamam")) })
	h := hazirKapisi(ic)
	istek := func(yol string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", yol, nil))
		return rec
	}
	hazir.Store(false)
	defer hazir.Store(true)
	if rec := istek("/api/state"); rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"loading":true`) {
		t.Fatalf("yüklenirken /api/state: %d %s", rec.Code, rec.Body.String())
	}
	if rec := istek("/"); rec.Code != http.StatusOK {
		t.Fatalf("yüklenirken arayüz açılmalı: %d", rec.Code)
	}
	hazir.Store(true)
	if rec := istek("/api/state"); rec.Code != http.StatusOK || rec.Body.String() != "tamam" {
		t.Fatalf("hazır olduktan sonra /api/state: %d %s", rec.Code, rec.Body.String())
	}
}
