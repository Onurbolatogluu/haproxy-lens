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
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
BIN="$HERE/haproxy-lens"
UNIT=/etc/systemd/system/haproxy-lens.service
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
echo "  - Servisin ek grupları: ${DET_GROUPS:-yok} (kullanıcı bu gruplara kalıcı olarak eklenmez)"
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
[ "$UPDATE" -eq 1 ] && systemctl stop haproxy-lens 2>/dev/null || true
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
SupplementaryGroups=$DET_GROUPS
Environment="LENS_SOCKET=$(esc "$DET_SOCKET")"
Environment="LENS_LOG=$(esc "$DET_LOG")"
Environment="LENS_LOG_NOTE=$(esc "$DET_LOG_NOTE")"
Environment="LENS_ALLOW=$(esc "$ALLOW")"
ExecStart=/usr/local/bin/haproxy-lens -socket \${LENS_SOCKET} -log \${LENS_LOG} -log-note \${LENS_LOG_NOTE} -listen $LISTEN_HOST:$PORT -allow \${LENS_ALLOW}
Restart=on-failure
RestartSec=5

# Kaynak tavanı: ajan ne yaparsa yapsın HAProxy'den kaynak çalamaz
Nice=10
CPUQuota=10%
MemoryMax=128M

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
  for _ in $(seq 1 10); do
    if curl -s "http://$LISTEN_HOST:$PORT/api/state" | grep -q '"ok":true'; then OK=1; break; fi
    sleep 1
  done
  if [ "$OK" -eq 1 ]; then
    echo "Ajan çalışıyor ve HAProxy'den veri okuyor."
  else
    echo "Servis kuruldu ama henüz veri gelmedi. Kontrol: journalctl -u haproxy-lens -n 20"
  fi
else
  systemctl is-active -q haproxy-lens && echo "Servis çalışıyor." || echo "Servis başlamadı. Kontrol: journalctl -u haproxy-lens -n 20"
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
