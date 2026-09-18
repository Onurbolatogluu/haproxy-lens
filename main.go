package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//go:embed all:web/dist
var webFS embed.FS

// Sürüm, derlemede -ldflags "-X main.version=..." ile verilir.
var version = "dev"

func main() {
	socket := flag.String("socket", "/run/haproxy/admin.sock", "HAProxy stats socket: unix yolu ya da tcp:host:port")
	logSrc := flag.String("log", "auto", "Log kaynağı: auto (ajan kendisi bulur ve izler), dosya yolu, journal:haproxy ya da boş (kapalı)")
	logNote := flag.String("log-note", "", "Log analizi kapalıysa panelde gösterilecek sebep")
	cfList := flag.String("cloudflare-list", "", "Ek Cloudflare IP listesi (isteğe bağlı; yerleşik liste zaten var)")
	listen := flag.String("listen", "127.0.0.1:8405", "Panelin dinleyeceği adres (IP:port)")
	allow := flag.String("allow", "", "Panele erişebilecek ağlar, virgülle (boşsa özel ağlar: "+defaultAllow+")")
	interval := flag.Duration("interval", 2*time.Second, "Stats okuma aralığı")
	retention := flag.Duration("retention", 24*time.Hour, "Geçmişin ne kadar saklanacağı (en az 1 saat)")
	stateDir := flag.String("state-dir", "", "Geçmişin diske yazılacağı klasör (boşsa sadece bellekte tutulur)")
	detailWin := flag.Duration("detail", time.Hour, "Tam ayrıntının (adres, IP dökümü) saklanacağı süre; belleği doğrudan etkiler")
	listWin := flag.Duration("lists", 6*time.Hour, "Yol ve IP listelerinin saklanacağı süre")
	budgetMB := flag.Int("memory-budget", 250, "Ayrıntı için bellek bütçesi (MB); aşılırsa en eski ayrıntı bırakılır")
	detect := flag.Bool("detect", false, "Uyumluluk kontrolü: bul, dene, rapor ver ve çık (hiçbir şey değiştirmez)")
	detectEnv := flag.Bool("detect-env", false, "Tespit sonucunu kurulum betiği için yaz ve çık")
	showAllow := flag.Bool("show-allow", false, "-allow listesini doğrula, anlaşılır hâlini yaz ve çık")
	showVersion := flag.Bool("version", false, "Sürümü yaz ve çık")
	flag.Parse()

	if *showVersion {
		fmt.Println("haproxy-lens", version)
		return
	}
	if *showAllow {
		nets, err := parseAllow(*allow)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Hatalı izin listesi:", err)
			os.Exit(1)
		}
		fmt.Println(netsString(nets))
		return
	}
	if *detect || *detectEnv {
		set := map[string]bool{}
		flag.Visit(func(f *flag.Flag) { set[f.Name] = true })
		forceSock, forceLog := "", ""
		if set["socket"] {
			forceSock = *socket
		}
		if set["log"] {
			forceLog = *logSrc
		}
		d := runDetect(forceSock, forceLog)
		if *detectEnv {
			d.printEnv(os.Stdout)
		} else {
			d.printReport(os.Stdout)
		}
		if d.Socket == nil {
			os.Exit(2)
		}
		return
	}

	allowNets, err := parseAllow(*allow)
	if err != nil {
		log.Fatalf("-allow hatalı: %v", err)
	}
	var listenIP net.IP
	if host, _, err := net.SplitHostPort(*listen); err == nil {
		listenIP = net.ParseIP(host)
		if listenIP == nil || !listenIP.IsLoopback() {
			log.Printf("Panel %s adresinde açık; sadece şu ağlardan erişilebilir: %s", *listen, netsString(allowNets))
		}
	}

	retMin := int(retention.Minutes())
	if retMin < 60 {
		retMin = 60
	}
	stats := NewStatsPoller(*socket, *interval, int(time.Hour / *interval), retMin)
	go stats.Run()

	var logs *LogAnalyzer
	logAuto := *logSrc == "auto"
	if *logSrc != "" {
		src := *logSrc
		if logAuto {
			src = "" // gözlemci bulacak
		}
		logs = NewLogAnalyzer(src, *cfList)
		logs.SetRetention(retMin)
		logs.SetDetailWindows(int(detailWin.Minutes()), int(listWin.Minutes()))
		logs.SetBudget(int64(*budgetMB) << 20)
		go logs.Run()
	}
	// Sunucunun kendi ölçümleri (CPU, bellek, disk); /proc altından okunur
	var sysYollar []string
	if logs != nil {
		if p := strings.TrimPrefix(logs.Source(), "file:"); p != "" {
			sysYollar = append(sysYollar, filepath.Dir(p))
		}
	}
	if *stateDir != "" {
		sysYollar = append(sysYollar, *stateDir)
	}
	sys := NewSysPoller(*interval, retMin, sysYollar...)
	go sys.Run()

	// Geçmişi diske yaz: ajan yeniden başladığında (sürüm güncellemesi gibi) veriler kaybolmasın
	if *stateDir != "" {
		store := NewStore(*stateDir, stats, logs, sys)
		if err := store.Load(); err != nil {
			log.Printf("Kayıtlı geçmiş yüklenemedi, sıfırdan başlanıyor: %v", err)
		}
		go store.Run()
	}
	// Config'i, log kaynağını ve socket'i çalışırken izler; değişiklikleri yeniden kurulum olmadan uygular
	env := NewEnv(stats, logs, logAuto)
	go env.Run()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		m, err := strconv.Atoi(r.URL.Query().Get("minutes"))
		if err != nil {
			m = 60
		}
		st := stats.State(m)
		st.System = sys.State(st.Minutes())
		writeJSON(w, st)
	})
	mux.HandleFunc("/api/logs", func(w http.ResponseWriter, r *http.Request) {
		if logs == nil {
			writeJSON(w, LogReport{Enabled: false, Error: *logNote})
			return
		}
		if logs.Source() == "" {
			writeJSON(w, LogReport{Enabled: false, Searching: true, Error: env.LogNote()})
			return
		}
		m, err := strconv.Atoi(r.URL.Query().Get("minutes"))
		if err != nil {
			m = 5
		}
		writeJSON(w, logs.Report(m))
	})
	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		if logs == nil {
			writeJSON(w, map[string]string{"error": "Bu sunucuda log analizi kapalı."})
			return
		}
		q, err := searchQueryFrom(r.URL.Query().Get)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, logs.Search(q))
	})
	mux.HandleFunc("/api/ranges", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ranges": stats.Ranges(), "retention": retMin})
	})
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, env.Info())
	})
	sub, _ := fs.Sub(webFS, "web/dist")
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Arayüz bu derlemeye eklenmemiş. Paketi GitHub Releases'tan indirin ya da build.sh ile derleyin.", http.StatusNotFound)
		})
	} else {
		mux.Handle("/", http.FileServer(http.FS(sub)))
	}

	srv := &http.Server{Addr: *listen, Handler: allowOnly(allowNets, listenIP, mux), ReadHeaderTimeout: 5 * time.Second}
	log.Printf("haproxy-lens %s başladı: http://%s (socket: %s, log: %q)", version, *listen, *socket, *logSrc)
	log.Fatal(srv.ListenAndServe())
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}
