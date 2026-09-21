#!/bin/bash
# haproxy-lens kurulumu.
# HAProxy'nin config'ine ve servisine DOKUNMAZ, reload/restart yapmaz.
#
#   ./install.sh --check   Hiçbir şey kurmadan bu sunucu için uyumluluk raporu verir
#   ./install.sh           Raporu gösterir, onay ister, kurar (güncelleme için de aynı komut)
#   ./install.sh -y        Onay sormadan kurar
#   PORT=8415 ./install.sh Panel için farklı port
#   LISTEN=10.0.0.5 ./install.sh     Panelin adresini elle ver (127.0.0.1 = sadece SSH tüneliyle)
#   ALLOW=10.0.0.0/24 ./install.sh   Panele erişebilecek ağlar (varsayılan: özel ağlar)
#   LOG=/yol/haproxy.log ./install.sh  Log kaynağını elle sabitle (varsayılan: ajan kendisi bulur)
#   RETENTION=48h ./install.sh       Geçmişin ne kadar saklanacağı (varsayılan 24 saat)
#   DETAIL=24h LISTS=24h ./install.sh  Ayrıntının ve listelerin saklanacağı süre (varsayılan: 1 ve 6 saat)
#   BUDGET=500 ./install.sh          Ayrıntı için bellek bütçesi, MB (varsayılan 250)
#   MEMMAX=768M ./install.sh         Servisin bellek tavanı (varsayılan 512M)
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
BIN="$HERE/haproxy-lens"
UNIT=/etc/systemd/system/haproxy-lens.service

# Güncellemede önceki ayarlar korunur: komutta verilmeyen her değer eski servis
# dosyasından okunur. (Eskiden yalnızca ALLOW korunuyordu; DETAIL=6h ile kurup
# standart komutla güncelleyen biri sessizce varsayılana dönüyordu.)
KORUNAN=""
if [ -f "$UNIT" ]; then
  eski_env() { sed -n "s/^Environment=\"LENS_$1=\(.*\)\"\$/\1/p" "$UNIT" | head -1; }
  for v in RETENTION DETAIL LISTS BUDGET; do
    if [ -z "${!v+x}" ]; then
      deger="$(eski_env "$v")"
      [ -n "$deger" ] && { printf -v "$v" '%s' "$deger"; KORUNAN="$KORUNAN $v=$deger"; }
    fi
  done
  if [ -z "${LOG+x}" ]; then
    deger="$(eski_env LOG)"
    [ -n "$deger" ] && [ "$deger" != auto ] && { LOG="$deger"; KORUNAN="$KORUNAN LOG=$deger"; }
  fi
  if [ -z "${MEMMAX+x}" ]; then
    deger="$(sed -n 's/^MemoryMax=\(.*\)$/\1/p' "$UNIT" | head -1)"
    [ -n "$deger" ] && { MEMMAX="$deger"; KORUNAN="$KORUNAN MEMMAX=$deger"; }
  fi
  # Panel adresi ve portu: ExecStart'taki -listen değerinden
  eski_listen="$(sed -n 's/^ExecStart=.* -listen \([^ ]*\) .*/\1/p' "$UNIT" | head -1)"
  if [ -n "$eski_listen" ]; then
    if [ -z "${PORT+x}" ]; then
      PORT="${eski_listen##*:}"
      [ "$PORT" != 8405 ] && KORUNAN="$KORUNAN PORT=$PORT"
    fi
    eski_ip="${eski_listen%:*}"; eski_ip="${eski_ip#[}"; eski_ip="${eski_ip%]}"
    # IP hâlâ bu sunucudaysa korunur; değiştiyse yeniden tespit edilir
    if [ -z "${LISTEN+x}" ] && { [ "$eski_ip" = 127.0.0.1 ] || { command -v ip >/dev/null && ip -o addr show | grep -qF " $eski_ip/"; }; }; then
      LISTEN="$eski_ip"
    fi
  fi
fi
PORT="${PORT:-8405}"
MODE=install
YES=0
for a in "$@"; do
  case "$a" in
    --check) MODE=check ;;
    -y|--yes) YES=1 ;;
    *) echo "Bilinmeyen seçenek: $a"; exit 1 ;;
  esac
done

die() { echo "HATA: $*" >&2; exit 1; }
[ "$(id -u)" -eq 0 ] || die "root olarak çalıştır (sudo $0 $*)"
[ -x "$BIN" ] || die "haproxy-lens dosyası bu klasörde yok"

# 0) Parametre doğrulaması: sisteme hiçbir şey dokunulmadan önce.
#    Hatalı bir değer servisi açılışta çökertir; burada yakalanıp anlaşılır bir mesajla durulur.
RETENTION="${RETENTION:-24h}"
DETAIL="${DETAIL:-1h}"
LISTS="${LISTS:-6h}"
BUDGET="${BUDGET:-250}"
MEMMAX="${MEMMAX:-512M}"
# Süreler birimiyle yazılır: 30m, 6h, 1h30m. Birimsiz "6" anlaşılmaz.
for pair in "RETENTION=$RETENTION" "DETAIL=$DETAIL" "LISTS=$LISTS"; do
  echo "${pair#*=}" | grep -Eq '^([0-9]+[hm])+$' \
    || die "${pair%%=*} süre birimiyle yazılmalı (örnek: ${pair%%=*}=6h ya da ${pair%%=*}=30m), verilen: ${pair#*=}"
done
case "$PORT" in
  ''|*[!0-9]*) die "PORT bir sayı olmalı (örnek: PORT=8405), verilen: $PORT" ;;
esac
if [ "$PORT" -lt 1 ] || [ "$PORT" -gt 65535 ]; then
  die "PORT 1 ile 65535 arasında olmalı, verilen: $PORT"
fi
case "$BUDGET" in
  ''|*[!0-9]*) die "BUDGET megabayt cinsinden bir sayı olmalı (örnek: BUDGET=250), verilen: $BUDGET" ;;
esac
# systemd birimsiz sayıyı BAYT sayar: MEMMAX=512 servisi açılır açılmaz öldürürdü
case "$MEMMAX" in
  *[0-9]K) MEMMAX_MB=$(( ${MEMMAX%K} / 1024 )) ;;
  *[0-9]M) MEMMAX_MB=${MEMMAX%M} ;;
  *[0-9]G) MEMMAX_MB=$(( ${MEMMAX%G} * 1024 )) ;;
  *) die "MEMMAX birimiyle yazılmalı (örnek: MEMMAX=512M ya da MEMMAX=1G), verilen: $MEMMAX" ;;
esac
case "$MEMMAX_MB" in ''|*[!0-9]*) die "MEMMAX anlaşılamadı: $MEMMAX" ;; esac
[ "$MEMMAX_MB" -ge $(( BUDGET + 128 )) ] || die "MEMMAX ($MEMMAX), bellek bütçesinden (BUDGET=${BUDGET} MB) en az 128 MB büyük olmalı; yoksa servis bütçeye ulaşmadan öldürülür. Örnek: MEMMAX=$(( BUDGET + 256 ))M"
# Süre ve bütçe kuralları ajanın kendisinde; aynı mantık iki yerde tutulmasın
"$BIN" -validate -retention "$RETENTION" -detail "$DETAIL" -lists "$LISTS" -memory-budget "$BUDGET" >/dev/null \
  || die "Ayarlar geçersiz; yukarıdaki mesaja bakın. Hiçbir şey değiştirilmedi."
[ -n "$KORUNAN" ] && echo "Önceki kurulumdan korunan ayarlar:$KORUNAN (değiştirmek için komutta yeniden verin)"

# 1) Tespit: config sadece okunur, bulunan socket ve log denenir
if ! "$BIN" -detect; then
  echo
  echo "Bu sunucuda hiçbir şey kurulmadı ve hiçbir şey değiştirilmedi."
  exit 1
fi
if [ "$MODE" = check ]; then
  echo
  echo "Kontrol modu: hiçbir şey kurulmadı ve hiçbir şey değiştirilmedi."
  exit 0
fi

command -v systemctl >/dev/null || die "systemd bulunamadı"
eval "$("$BIN" -detect-env)"

# Panel adresi: elle verilmediyse tespit edilen iç IP, o da yoksa 127.0.0.1
LISTEN="${LISTEN:-$DET_LISTEN_IP}"
[ -n "$LISTEN" ] || LISTEN=127.0.0.1
if [ "$LISTEN" != 127.0.0.1 ] && command -v ip >/dev/null; then
  ip -o addr show | grep -qF " $LISTEN/" || die "$LISTEN bu sunucunun arayüzlerinde yok."
fi
case "$LISTEN" in *:*) LISTEN_HOST="[$LISTEN]" ;; *) LISTEN_HOST="$LISTEN" ;; esac
# Servisin grupları: socket ve log için bulunanlar + log'u ileride başka yerden okuyabilsin diye
# adm (syslog dosyaları) ve systemd-journal (journald). Böylece config değişince yeniden kurulum gerekmez.
GROUPS_ALL="$DET_GROUPS"
for g in adm systemd-journal; do
  getent group "$g" >/dev/null 2>&1 && case " $GROUPS_ALL " in *" $g "*) ;; *) GROUPS_ALL="$GROUPS_ALL $g" ;; esac
done
GROUPS_ALL="$(echo "$GROUPS_ALL" | xargs)"
# Log kaynağı: elle verilmediyse ajan kendisi bulur ve çalışırken izler
LOG_SRC="${LOG:-auto}"
# Geçmişin saklama süresi; veriler /var/lib/haproxy-lens altına yazılır

# Erişim listesi: elle verilmediyse güncellemede önceki kurulumunki korunur
if [ -z "${ALLOW+x}" ] && [ -f "$UNIT" ]; then
  ALLOW="$(sed -n 's/^Environment="LENS_ALLOW=\(.*\)"$/\1/p' "$UNIT")"
fi
ALLOW="${ALLOW:-}"
ALLOW_TEXT="$("$BIN" -show-allow -allow "$ALLOW")" || die "ALLOW listesi hatalı: $ALLOW"

# 2) Port: güncellemede kendi servisimiz zaten o portta olabilir
UPDATE=0
[ -f "$UNIT" ] && UPDATE=1
if [ "$UPDATE" -eq 0 ] && command -v ss >/dev/null && [ -n "$(ss -Hltn "sport = :$PORT" 2>/dev/null)" ]; then
  die "$PORT portu başka bir program tarafından kullanılıyor. Farklı port için: PORT=8415 $0"
fi

echo
[ "$UPDATE" -eq 1 ] && echo "Önceki kurulum bulundu, güncellenecek."
echo "Yapılacaklar:"
echo "  - /usr/local/bin/haproxy-lens ve $UNIT dosyaları"
echo "  - Giriş yapamayan 'haproxy-lens' sistem kullanıcısı"
echo "  - Servisin ek grupları: ${GROUPS_ALL:-yok} (kullanıcı bu gruplara kalıcı olarak eklenmez)"
echo "  - Config, log biçimi ve log kaynağı çalışırken izlenir; config değişince yeniden kurulum gerekmez."
echo "  - Geçmiş $RETENTION saklanır: /var/lib/haproxy-lens (birkaç yüz KB; kaldırırken silinir)"
echo "  - Adres ve IP ayrıntısı $DETAIL, yol/IP listeleri $LISTS saklanır"
echo "  - Ayrıntı için bellek bütçesi ${BUDGET} MB, servis tavanı $MEMMAX"
echo "    Bellek yalnızca gerektiği kadar kullanılır; bütçe aşılırsa ajan en eski ayrıntıyı bırakır."
echo "    Sayılar (istek, yanıt kodu, backend dökümü) her durumda $RETENTION boyunca eksiksiz kalır."
if [ "$LISTEN" = 127.0.0.1 ]; then
  echo "  - Panel: 127.0.0.1:$PORT (dışarıdan erişilemez, SSH tüneliyle açılır)"
else
  echo "  - Panel: http://$LISTEN_HOST:$PORT"
  echo "  - Panele erişebilecek ağlar: $ALLOW_TEXT (şifre yok; değiştirmek için ALLOW=ağ/önek)"
fi
echo "  - HAProxy config'ine ve servisine dokunulmaz."
if [ "$YES" -ne 1 ]; then
  read -r -p "Devam edilsin mi? [e/H] " ans
  [[ "$ans" =~ ^[eEyY]$ ]] || { echo "İptal edildi. Hiçbir şey değiştirilmedi."; exit 0; }
fi

# 3) Kanıt için "önce" kaydı: config özetleri ve HAProxy süreç numaraları
cfg_sum() { for f in $DET_CONFIG_FILES; do sha256sum "$f" 2>/dev/null || true; done; }
ha_pids() { pgrep -x haproxy 2>/dev/null | sort -n | tr '\n' ' ' || true; }
BEFORE_CFG="$(cfg_sum)"
BEFORE_PID="$(ha_pids)"

# 4) Kurulum
id haproxy-lens >/dev/null 2>&1 || useradd --system --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin haproxy-lens
if [ "$UPDATE" -eq 1 ]; then
  systemctl stop haproxy-lens 2>/dev/null || true
fi
install -m 0755 "$BIN" /usr/local/bin/haproxy-lens

esc() { printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' -e 's/%/%%/g'; }
cat > "$UNIT" <<UNITEOF
# haproxy-lens kurulum betiği tarafından oluşturuldu. Kaldırmak için: uninstall.sh
[Unit]
Description=haproxy-lens: HAProxy icin salt okunur panel
After=network-online.target haproxy.service
Wants=network-online.target

[Service]
Type=simple
User=haproxy-lens
Group=haproxy-lens
SupplementaryGroups=$GROUPS_ALL
Environment="LENS_SOCKET=$(esc "$DET_SOCKET")"
Environment="LENS_LOG=$(esc "$LOG_SRC")"
Environment="LENS_ALLOW=$(esc "$ALLOW")"
Environment="LENS_RETENTION=$(esc "$RETENTION")"
Environment="LENS_DETAIL=$(esc "$DETAIL")"
Environment="LENS_LISTS=$(esc "$LISTS")"
Environment="LENS_BUDGET=$(esc "$BUDGET")"
ExecStart=/usr/local/bin/haproxy-lens -socket \${LENS_SOCKET} -log \${LENS_LOG} -listen $LISTEN_HOST:$PORT -allow \${LENS_ALLOW} -retention \${LENS_RETENTION} -detail \${LENS_DETAIL} -lists \${LENS_LISTS} -memory-budget \${LENS_BUDGET} -state-dir /var/lib/haproxy-lens

# Geçmişin yazıldığı klasör; systemd oluşturur ve servis kullanıcısına verir
StateDirectory=haproxy-lens
StateDirectoryMode=0750
Restart=on-failure
RestartSec=5

# Kaynak tavanı: ajan ne yaparsa yapsın HAProxy'den kaynak çalamaz
Nice=10
CPUQuota=10%
MemoryMax=$MEMMAX

# Sertleştirme: /etc ve /usr'e yazamaz, yetki yükseltemez
NoNewPrivileges=yes
ProtectSystem=full
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
CapabilityBoundingSet=
LockPersonality=yes

[Install]
WantedBy=multi-user.target
UNITEOF

systemctl daemon-reload
systemctl enable haproxy-lens >/dev/null 2>&1
systemctl restart haproxy-lens

# 5) Çalışıyor mu?
echo
OK=0
if command -v curl >/dev/null; then
  # Ajan açılışta geçmişi diskten yükler (büyük geçmişte ve düşük işlemci tavanında
  # birkaç saniye sürebilir); bu sürede "loading" der. Bitmesini en fazla 60 sn bekle.
  YAZILDI=0
  for _ in $(seq 1 60); do
    YANIT="$(curl -s "http://$LISTEN_HOST:$PORT/api/state" || true)"
    if echo "$YANIT" | grep -q '"ok":true'; then OK=1; break; fi
    if [ "$YAZILDI" -eq 0 ] && echo "$YANIT" | grep -q '"loading":true'; then
      echo "Ajan açıldı, kayıtlı geçmişi yüklüyor; bitmesi bekleniyor..."
      YAZILDI=1
    fi
    sleep 1
  done
  if [ "$OK" -eq 1 ]; then
    echo "$(/usr/local/bin/haproxy-lens -version) çalışıyor ve HAProxy'den veri okuyor."
  elif ! systemctl is-active -q haproxy-lens; then
    echo "HATA: Servis kuruldu ama çalışmıyor. Son kayıtlar:"
    journalctl -u haproxy-lens -n 8 --no-pager 2>/dev/null | sed 's/^/  /'
    exit 1
  else
    echo "Servis çalışıyor ama henüz HAProxy'den veri gelmedi. Kontrol: journalctl -u haproxy-lens -n 20"
  fi
else
  if systemctl is-active -q haproxy-lens; then
    echo "Servis çalışıyor."
  else
    echo "Servis başlamadı. Kontrol: journalctl -u haproxy-lens -n 20"
  fi
fi

# 6) Kanıt: "sonra" karşılaştırması
echo
if [ "$BEFORE_CFG" = "$(cfg_sum)" ]; then
  echo "Doğrulama: HAProxy config dosyaları değişmedi ($(echo "$DET_CONFIG_FILES" | wc -w) dosya, sha256 aynı)."
else
  echo "UYARI: config dosyalarının özeti değişti. Bu betik config'e yazmaz; kurulum sırasında başka biri değiştirmiş olabilir."
fi
if [ "$BEFORE_PID" = "$(ha_pids)" ]; then
  echo "Doğrulama: HAProxy yeniden başlatılmadı ve reload edilmedi (süreç numaraları aynı)."
else
  echo "UYARI: HAProxy süreç numaraları değişti. Bu betik HAProxy'ye dokunmaz; aynı anda başka biri reload yapmış olabilir."
fi

echo
if [ "$LISTEN" = 127.0.0.1 ]; then
  echo "Paneli açmak için KENDİ bilgisayarında:"
  echo "  ssh -L $PORT:127.0.0.1:$PORT $(whoami)@$(hostname)"
  echo "Sonra tarayıcıda: http://localhost:$PORT"
else
  echo "Panel: http://$LISTEN_HOST:$PORT"
  echo "Erişebilecek ağlar: $ALLOW_TEXT"
  if command -v ufw >/dev/null && ufw status 2>/dev/null | grep -q "Status: active"; then
    echo "Not: ufw açık. Bu betik güvenlik duvarına dokunmaz; $PORT kapalıysa örnek: ufw allow from 10.0.0.0/8 to any port $PORT proto tcp"
  elif command -v firewall-cmd >/dev/null && firewall-cmd --state >/dev/null 2>&1; then
    echo "Not: firewalld açık. Bu betik güvenlik duvarına dokunmaz; $PORT kapalıysa kendi ağın için izin vermen gerekebilir."
  fi
fi
echo "Kaldırmak için: $HERE/uninstall.sh"
