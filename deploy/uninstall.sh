#!/bin/bash
# haproxy-lens'i iz bırakmadan kaldırır.
# HAProxy'nin config'ine ve servisine DOKUNMAZ.
#   ./uninstall.sh      Ne silineceğini gösterir, onay ister
#   ./uninstall.sh -y   Onay sormadan kaldırır
set -u
UNIT=/etc/systemd/system/haproxy-lens.service
BIN=/usr/local/bin/haproxy-lens
[ "$(id -u)" -eq 0 ] || { echo "root olarak çalıştır (sudo $0)"; exit 1; }

if [ ! -e "$UNIT" ] && [ ! -e "$BIN" ] && ! id haproxy-lens >/dev/null 2>&1; then
  echo "haproxy-lens bu sunucuda kurulu değil. Yapılacak bir şey yok."
  exit 0
fi

# Kanıt için "önce" kaydı
CFGS=""
if [ -x "$BIN" ]; then
  eval "$("$BIN" -detect-env 2>/dev/null)" || true
  CFGS="${DET_CONFIG_FILES:-}"
fi
[ -n "$CFGS" ] || CFGS="$(ls /etc/haproxy/*.cfg 2>/dev/null | tr '\n' ' ')"
cfg_sum() { for f in $CFGS; do sha256sum "$f" 2>/dev/null || true; done; }
ha_pids() { pgrep -x haproxy 2>/dev/null | sort -n | tr '\n' ' ' || true; }
BEFORE_CFG="$(cfg_sum)"
BEFORE_PID="$(ha_pids)"

echo "Silinecekler:"
echo "  - haproxy-lens servisi ($UNIT)"
echo "  - Program dosyası ($BIN)"
echo "  - 'haproxy-lens' sistem kullanıcısı"
echo "  - Saklanan geçmiş (/var/lib/haproxy-lens)"
echo "  - HAProxy'ye dokunulmaz."
if [ "${1:-}" != "-y" ]; then
  read -r -p "Devam edilsin mi? [e/H] " ans
  [[ "$ans" =~ ^[eEyY]$ ]] || { echo "İptal edildi. Hiçbir şey değiştirilmedi."; exit 0; }
fi

systemctl disable --now haproxy-lens >/dev/null 2>&1 || true
rm -f "$UNIT" "$BIN"
rm -rf /var/lib/haproxy-lens
systemctl daemon-reload
systemctl reset-failed haproxy-lens >/dev/null 2>&1 || true
id haproxy-lens >/dev/null 2>&1 && userdel haproxy-lens

echo
LEFT=0
[ -e "$UNIT" ] && { echo "UYARI: $UNIT hâlâ duruyor"; LEFT=1; }
[ -e "$BIN" ] && { echo "UYARI: $BIN hâlâ duruyor"; LEFT=1; }
id haproxy-lens >/dev/null 2>&1 && { echo "UYARI: haproxy-lens kullanıcısı hâlâ duruyor"; LEFT=1; }
[ "$LEFT" -eq 0 ] && echo "haproxy-lens kaldırıldı: servis, program dosyası ve sistem kullanıcısı silindi. Geride dosya kalmadı."

if [ "$BEFORE_CFG" = "$(cfg_sum)" ]; then
  echo "Doğrulama: HAProxy config dosyaları değişmedi."
fi
if [ "$BEFORE_PID" = "$(ha_pids)" ]; then
  echo "Doğrulama: HAProxy yeniden başlatılmadı ve reload edilmedi (süreç numaraları aynı)."
else
  echo "UYARI: HAProxy süreç numaraları değişti. Bu betik HAProxy'ye dokunmaz; aynı anda başka biri reload yapmış olabilir."
fi
