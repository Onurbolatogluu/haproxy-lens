#!/bin/bash
# haproxy-lens kurulumu.
# HAProxy'nin config'ine ve servisine DOKUNMAZ, reload/restart yapmaz.
#
#   ./install.sh --check   Hiçbir şey kurmadan bu sunucu için uyumluluk raporu verir
#   ./install.sh           Raporu gösterir, onay ister, kurar (güncelleme için de aynı komut)
#   ./install.sh -y        Onay sormadan kurar
#   PORT=8415 ./install.sh Panel için farklı port
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
echo "  - Panel: 127.0.0.1:$PORT (dışarıdan erişilemez, SSH tüneliyle açılır)"
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
After=network.target haproxy.service

[Service]
Type=simple
User=haproxy-lens
Group=haproxy-lens
SupplementaryGroups=$DET_GROUPS
Environment="LENS_SOCKET=$(esc "$DET_SOCKET")"
Environment="LENS_LOG=$(esc "$DET_LOG")"
Environment="LENS_LOG_NOTE=$(esc "$DET_LOG_NOTE")"
ExecStart=/usr/local/bin/haproxy-lens -socket \${LENS_SOCKET} -log \${LENS_LOG} -log-note \${LENS_LOG_NOTE} -listen 127.0.0.1:$PORT
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
    if curl -s "http://127.0.0.1:$PORT/api/state" | grep -q '"ok":true'; then OK=1; break; fi
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
echo "Paneli açmak için KENDİ bilgisayarında:"
echo "  ssh -L $PORT:127.0.0.1:$PORT $(whoami)@$(hostname)"
echo "Sonra tarayıcıda: http://localhost:$PORT"
echo "Kaldırmak için: $HERE/uninstall.sh"
