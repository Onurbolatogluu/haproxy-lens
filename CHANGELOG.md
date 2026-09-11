# Değişiklikler

## 0.3.0

- Panel artık sunucunun kendi iç IP'sinde açılıyor. Adres, varsayılan rotanın geçtiği arayüzden seçiliyor; keepalived VIP'leri atlanıyor, ağ ayarlarında (netplan, ifupdown, ifcfg, NetworkManager, systemd-networkd) sabit tanımlı adres tercih ediliyor.
- Herkese açık bir IP seçilmiyor; bu durumda panel 127.0.0.1'de kalıyor.
- Panel sadece izin verilen ağlardan gelen isteklere cevap veriyor (varsayılan: özel ağlar). `ALLOW=ağ/önek ./install.sh` ile daraltılabiliyor; güncellemede önceki liste korunuyor.
- `LISTEN=IP ./install.sh` ile adres elle verilebiliyor.
- `./install.sh --check` raporuna "Panel adresi" bölümü eklendi.
- Servis ağ hazır olduktan sonra başlıyor (`network-online.target`).

## 0.2.0 (2026-09-10)

- Kurulum her sunucuda çalışan HAProxy'nin config'ini sadece okuyarak stats socket'ini ve log'u kendisi buluyor ve dener.
- `./install.sh --check`: hiçbir şey kurmadan uyumluluk raporu.
- TCP (`ipv4@`, `host:port`) ve soyut (`abns@`) socket desteği, `conf.d` gibi config klasörleri.
- Log kaynağı olarak journald desteği.
- Özel `log-format` görülürse log analizi kendiliğinden kapanıyor; stats paneli çalışmaya devam ediyor.
- Kurulum ve kaldırma sonunda config sha256 özeti ve HAProxy süreç numarası doğrulaması.
- Cloudflare IP aralıkları programın içinde; ayrı bir liste dosyası gerekmiyor.

## 0.1.0 (2026-09-10)

- İlk sürüm: stats paneli, log analizi, systemd servisi, kurulum ve kaldırma betikleri.
