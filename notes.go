package main

// Config'te ya da ortamda paneli kısıtlayan her şey burada sade bir nota dönüşür.
// Ajan config'e hiçbir şey yazmaz; notlar sadece "bunu eklersen şu da görünür" der.

import (
	"fmt"
	"sort"
	"strings"
)

type Note struct {
	Level   string   `json:"level"` // warn: veri eksik; info: bilgi / öneri
	Title   string   `json:"title"`
	Text    string   `json:"text"`
	Fix     string   `json:"fix,omitempty"`   // config'e eklenebilecek satır
	Where   string   `json:"where,omitempty"` // dosya:satır
	Samples []string `json:"samples,omitempty"`
}

// Log tarafında gözlenenler (son 15 dakika)
type logObservation struct {
	Enabled   bool
	Source    string
	Err       string
	Parsed    int64
	Served    int64
	Unparsed  int64
	HostLines int64
	Samples   []string
}

func isStatsProxy(fe *feCfg) bool { return fe.IsStats }

// Config'e bakarak üretilen notlar (kurulum öncesi kontrolde de kullanılır)
func configNotes(hc *haConfig) []Note {
	notes := []Note{}
	add := func(n Note) { notes = append(notes, n) }
	if hc == nil {
		return notes
	}
	for _, e := range hc.Errors {
		add(Note{Level: "warn", Title: "Config okunamadı", Text: e + " Ajan bu dosyadaki ayarları göremediği için bazı notlar eksik olabilir."})
	}
	var nonStats []*feCfg
	for _, fe := range hc.Frontends {
		if !isStatsProxy(fe) {
			nonStats = append(nonStats, fe)
		}
	}
	for _, fe := range nonStats {
		name := fmt.Sprintf("«%s»", fe.Name)
		if fe.Mode != "http" {
			add(Note{Level: "info", Title: name + " TCP modunda", Where: fe.Where,
				Text: "Bu trafikte yol, durum kodu ve alan adı olmaz; stats bölümünde sadece bağlantı ve trafik sayıları görünür."})
			continue
		}
		if fe.NoLog || len(fe.Targets) == 0 {
			add(Note{Level: "warn", Title: name + " log yazmıyor", Where: fe.Where,
				Text: "Bu frontend'e gelen istekler log'a düşmediği için log bölümündeki paneller bu trafiği göremez. Stats bölümü etkilenmez.",
				Fix:  "log global"})
			continue
		}
		allRemote, allDrop := true, true
		for _, t := range fe.Targets {
			if !t.remote() {
				allRemote = false
			}
			if !t.dropsTraffic() {
				allDrop = false
			}
		}
		if allDrop {
			add(Note{Level: "warn", Title: name + " trafik kayıtları log seviyesine takılıyor", Where: fe.Targets[0].Where,
				Text: "HAProxy trafik kayıtlarını 'info' seviyesinde yazar. Bu frontend'in log hedeflerinin hepsi daha yüksek bir seviyeyle (ör. notice) sınırlı olduğu için kayıtlar log'a düşmüyor.",
				Fix:  "log /dev/log local0"})
		} else if allRemote {
			add(Note{Level: "info", Title: name + " log'ları başka bir sunucuya gidiyor", Where: fe.Targets[0].Where,
				Text: "Bu sunucuda HAProxy log'u oluşmadığı için log bölümü boş kalır. Uzak log sunucusundaki kayıtlar bu panele gelmez."})
		}
		switch fe.FormatKind {
		case "default", "":
			add(Note{Level: "warn", Title: name + " HTTP log biçimi kullanmıyor", Where: fe.Where,
				Text: "Log'da yol, durum kodu ve backend/sunucu bilgisi yok. Hata alan adresler ve engellenen istekler görünmez.",
				Fix:  "option httplog"})
		case "clf":
			add(Note{Level: "info", Title: name + " CLF log biçiminde", Where: fe.FormatWhere,
				Text: "CLF biçimi henüz desteklenmiyor; bu frontend'in satırları okunamayabilir."})
		case "custom":
			if cf := compileFormat("custom", fe.Format); cf != nil {
				var missing []string
				if !cf.has[roleStatus] {
					missing = append(missing, "durum kodu (%ST)")
				}
				if !cf.has[roleRequest] && !cf.has[rolePath] && !cf.has[roleURI] {
					missing = append(missing, "istek yolu (%{+Q}r)")
				}
				if !cf.has[roleBackend] || !cf.has[roleServer] {
					missing = append(missing, "backend ve sunucu (%b/%s)")
				}
				if !cf.has[roleClient] {
					missing = append(missing, "istemci IP'si (%ci)")
				}
				if len(missing) > 0 {
					add(Note{Level: "warn", Title: name + " log biçiminde eksik alanlar", Where: fe.FormatWhere,
						Text: "Özel log-format şu alanları içermiyor: " + strings.Join(missing, ", ") + ". Bu bilgilere dayanan paneller eksik kalır."})
				}
			}
		}
		if fe.DontLogNormal {
			add(Note{Level: "warn", Title: name + " başarılı istekleri log'a yazmıyor", Where: fe.DLNWhere,
				Text: "option dontlog-normal açık: sadece hatalı istekler log'a düşüyor. Hata analizi çalışır ama 'En çok istenen adresler' ve log'daki başarı oranları eksik görünür. Stats bölümü etkilenmez.",
				Fix:  "no option dontlog-normal"})
		}
		hostInFormat := false
		if cf := compileFormat(fe.FormatKind, fe.Format); cf != nil && fe.Format != "" {
			hostInFormat = cf.has[roleHost] || cf.has[roleSNI]
		}
		switch {
		case hostInFormat:
		case fe.HostSlot >= 0 && fe.Captures[fe.HostSlot].Cond:
			add(Note{Level: "info", Title: name + " alan adını sadece bazı isteklerde yakalıyor", Where: fe.Captures[fe.HostSlot].Where,
				Text: "Host başlığı koşullu yakalanıyor; alan adı sadece koşula uyan isteklerde görünür. Diğerlerinde ayrıntıda yol ve IP görünür.",
				Fix:  "capture request header Host len 64"})
		case fe.HostSlot < 0:
			add(Note{Level: "info", Title: name + " alan adını log'a yazmıyor", Where: fe.Where,
				Text: "Hatalı ve engellenen isteklerin hangi alan adına geldiği görünmez (HTTP/2 istekleri hariç). Bu satır mevcut log satırlarına {alan.adı} bölümünü ekler; o log'u okuyan başka bir araç varsa önce onu kontrol et.",
				Fix:  "capture request header Host len 64"})
		}
	}
	// Sağlık kontrolü olmayan sunucular
	var noCheck []string
	total := 0
	for _, b := range hc.Backends {
		if len(b.NoCheck) > 0 {
			noCheck = append(noCheck, fmt.Sprintf("%s (%d)", b.Name, len(b.NoCheck)))
			total += len(b.NoCheck)
		}
	}
	if total > 0 {
		sort.Strings(noCheck)
		list := strings.Join(noCheck, ", ")
		if len(noCheck) > 6 {
			list = strings.Join(noCheck[:6], ", ") + fmt.Sprintf(" ve %d backend daha", len(noCheck)-6)
		}
		add(Note{Level: "info", Title: fmt.Sprintf("%d sunucuda sağlık kontrolü yok", total),
			Text: "Bu sunucular düşerse HAProxy fark etmez ve istek göndermeye devam eder; panel onları 'Kontrolsüz' gösterir. Backend'ler: " + list + ".",
			Fix:  "server <ad> <adres>:<port> check"})
	}
	return notes
}

// Config notlarına, çalışırken gözlenenleri ekler
func runtimeNotes(hc *haConfig, obs logObservation, backendReqs float64, logNote string) []Note {
	notes := configNotes(hc)
	add := func(n Note) { notes = append(notes, n) }
	if !obs.Enabled {
		return notes
	}
	switch {
	case obs.Source == "":
		txt := logNote
		if txt == "" {
			txt = "Bu sunucuda okunabilir bir HAProxy log'u bulunamadı."
		}
		add(Note{Level: "warn", Title: "HAProxy log'u bulunamadı", Text: txt + " Ajan birkaç dakikada bir tekrar arıyor; log oluşunca kendiliğinden başlar. Stats bölümü etkilenmez."})
	case obs.Err != "":
		add(Note{Level: "warn", Title: "Log okunamıyor", Text: fmt.Sprintf("%s okunurken hata: %s. Yetki sorunuysa kurulum betiğini bir kez daha çalıştırmak servisin gruplarını günceller.", strings.TrimPrefix(obs.Source, "file:"), obs.Err)})
	}
	tot := obs.Parsed + obs.Unparsed
	if obs.Unparsed >= 20 && float64(obs.Unparsed) >= float64(tot)*0.05 {
		add(Note{Level: "warn", Title: fmt.Sprintf("Son 15 dakikada log satırlarının %%%.0f kadarı okunamadı", 100*float64(obs.Unparsed)/float64(tot)),
			Text:    fmt.Sprintf("%d satır panele yansımadı. Biçimleri config'teki log tanımıyla uyuşmuyor; örnekler aşağıda (sorgu parametreleri gizlendi).", obs.Unparsed),
			Samples: obs.Samples})
	}
	dln := false
	if hc != nil {
		for _, fe := range hc.Frontends {
			if fe.DontLogNormal {
				dln = true
			}
		}
	}
	if backendReqs >= 100 && obs.Parsed > 0 && obs.Served == 0 && !dln {
		add(Note{Level: "warn", Title: "Stats'ta trafik var ama log'da sunucuya ulaşan istek yok",
			Text: fmt.Sprintf("Son 15 dakikada backend'ler yaklaşık %.0f istek aldı ama log'da hiçbiri görünmüyor. Olası sebepler: bu trafiğin geçtiği frontend'in log'u kapalı ya da farklı bir yere gidiyor, ya da satırlar okunamıyor (varsa yukarıdaki not).", backendReqs)})
	}
	return notes
}
