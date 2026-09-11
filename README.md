# haproxy-lens

HAProxy'nin stats sayfasını okunur bir panele çeviren, **config'e dokunmayan** salt okunur ajan.

Her HAProxy sunucusuna kurulur, o sunucunun kendi stats verisini ve log'unu okur, tarayıcıda şunları gösterir:

- Sade bir durum özeti: "api içindeki srv3, 12 dakikadır çalışmıyor. Sebep: bağlantı zaman aşımı."
- Backend'ler ve sunucular: durum, sağlık kontrolünün anlamı, bağlantı doluluğu, yanıt süresi, hatalar.
- Canlı grafikler: saniyedeki istek (yanıt türüne göre) ve trafik; zaman aralığı 5 dk, 15 dk ya da 1 saat seçilebilir.
- 5xx hatalarının en çok hangi sunucudan döndüğü; hatalar sunuculara eşit dağılmışsa sorunun ortak bir yerde olabileceği uyarısı.
- Log'dan: en çok istenen adresler, engellenen (403) ve hiçbir backend'e eşleşmeyen (503) istekler, en çok istek atan IP'ler.
- Her terimin sade Türkçe açıklaması ve her satır için HAProxy'nin verdiği tüm alanlar.

## Temel kurallar

- **HAProxy config'ine ve servisine dokunmaz.** Reload ve restart yapmaz.
- **Her sunucuya kendini uydurur.** Kurulum, çalışan HAProxy'nin config'ini sadece okuyarak stats socket'ini ve log'unu kendisi bulur ve bulduğunu gerçekten dener.
- **Emin olamazsa kurmaz.** Çalışan bir stats socket bulamazsa hiçbir şey değiştirmeden durur ve sebebini yazar.
- **Sadece okur.** HAProxy'ye yalnızca `show info` ve `show stat` komutlarını gönderir. Başka komut gönderen kod yoktur (bkz. `haproxy.go` içindeki `allowedCommands`).
- **Hassas veri tutmaz.** Log'daki sorgu parametreleri (`?token=...` gibi) hafızada bile tutulmaz.
- **Kaynak tavanı var.** CPU %10 ve RAM 128 MB sınırıyla, düşük öncelikte çalışır; `/etc` ve `/usr` altına yazamaz.
- **İnternete açılmaz.** Panel sunucunun kendi iç IP'sinde açılır (keepalived VIP'inde değil) ve sadece izin verilen ağlardan gelen isteklere cevap verir. Varsayılan liste özel ağlardır (10.x, 172.16-31.x, 192.168.x). Sunucunun ana IP'si herkese açık bir adresse panel `127.0.0.1`'de kalır.
- **Kanıtlar.** Kurulum ve kaldırma sonunda config dosyalarının sha256 özetinin ve HAProxy süreç numaralarının değişmediğini kendisi kontrol edip yazar.

## Kurulum

Tüm komutlar HAProxy sunucusunda, root olarak.

### 1. İndir

```bash
cd /root
wget https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/haproxy-lens-linux-amd64.tar.gz
wget https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/SHA256SUMS
sha256sum -c --ignore-missing SHA256SUMS
tar xzf haproxy-lens-linux-amd64.tar.gz
cd haproxy-lens
```

`sha256sum` satırı `haproxy-lens-linux-amd64.tar.gz: OK` demeli. ARM sunucularda (`uname -m` çıktısı `aarch64` ise) dosya adındaki `amd64` yerine `arm64` yaz.

### 2. Kontrol et (hiçbir şey kurmaz)

```bash
./install.sh --check
```

Rapor, config'te bulunan stats socket'leri, log kaynağını ve en altta bir **SONUÇ** satırını gösterir:

| Sonuç | Anlamı |
|---|---|
| `UYUMLU (stats + log analizi)` | Her şey kurulabilir. |
| `UYUMLU (sadece stats)` | Stats paneli tam çalışır, log analizi kapalı olur. Sebebi raporda yazar. |
| `KURULAMAZ` | Çalışan ve erişilebilir bir stats socket yok. Hiçbir şey kurulmaz. |

### 3. Kur

```bash
./install.sh
```

Önce aynı raporu, sonra yapılacakları gösterir ve onay ister. Sonunda şu iki satırı görmelisin:

```
Doğrulama: HAProxy config dosyaları değişmedi (1 dosya, sha256 aynı).
Doğrulama: HAProxy yeniden başlatılmadı ve reload edilmedi (süreç numaraları aynı).
```

Seçenekler:

| Komut | Ne yapar |
|---|---|
| `./install.sh -y` | Onay sormadan kurar |
| `PORT=8415 ./install.sh` | Farklı port kullanır |
| `ALLOW=10.234.0.0/16 ./install.sh` | Panele sadece bu ağ(lar)dan erişilebilir; virgülle birden fazla ağ ya da tek IP verilebilir |
| `LISTEN=10.0.0.5 ./install.sh` | Panelin adresini elle verir |
| `LISTEN=127.0.0.1 ./install.sh` | Paneli sadece sunucunun içinden açar (SSH tüneliyle kullanılır) |

Güncellemede `ALLOW` verilmezse önceki kurulumdaki liste korunur.

### 4. Paneli aç

Tarayıcıda kurulumun sonunda yazan adresi aç, örneğin `http://10.0.0.5:8405`.

Panel `127.0.0.1`'de kurulduysa kendi bilgisayarında `ssh -L 8405:127.0.0.1:8405 root@SUNUCU_ADRESI` çalıştır, sonra `http://localhost:8405` adresini aç.

Sunucuda güvenlik duvarı açıksa (ufw, firewalld) panelin portuna kendi ağın için izin vermen gerekebilir. Kurulum bunu fark ederse hatırlatır ama güvenlik duvarına dokunmaz.

### Panelin adresi nasıl seçilir

1. Varsayılan rotanın geçtiği arayüz bulunur (`/proc/net/route`).
2. O arayüzdeki keepalived VIP'leri atlanır (`/etc/keepalived/keepalived.conf` ve `include` ettiği dosyalar). VIP master/slave arasında yer değiştirdiği için panel her sunucunun kendi adresinde durur.
3. Kalan adreslerden ağ ayarlarında sabit tanımlı olan seçilir: netplan, `/etc/network/interfaces`, `ifcfg-*`, NetworkManager, systemd-networkd.
4. Bulunamazsa arayüzün birincil adresi seçilir; `/32`, `secondary` ve etiketli (`eth0:1`) adresler VIP olabileceği için atlanır.
5. Seçilen adres herkese açık bir IP ise kullanılmaz, panel `127.0.0.1`'de kalır.

`./install.sh --check` raporundaki "Panel adresi" bölümü hangi adresin neden seçildiğini ve hangilerinin neden atlandığını gösterir.

## Güncelleme

Yeni paketi aynı şekilde indirip `./install.sh` çalıştırmak yeterli. Betik önceki kurulumu görür ve üzerine yazar.

## Kaldırma

```bash
./uninstall.sh
```

Servisi, program dosyasını ve `haproxy-lens` sistem kullanıcısını siler, geride bir şey kalmadığını kontrol eder. Kurulumdaki iki doğrulamayı burada da yapar. Paketin klasörü artık yoksa aynı paketi tekrar indirip içindeki `uninstall.sh`'ı çalıştırabilirsin.

## Sunucunun internete çıkışı yoksa

Paketi kendi bilgisayarına indir, sunucuya kopyala, sonra 1. adımdaki `tar` satırından devam et:

```bash
scp haproxy-lens-linux-amd64.tar.gz SHA256SUMS root@SUNUCU_ADRESI:/root/
```

## Kurulum sunucuda neleri değiştirir

| Ne | Nerede |
|---|---|
| Program | `/usr/local/bin/haproxy-lens` |
| Servis | `/etc/systemd/system/haproxy-lens.service` |
| Sistem kullanıcısı | `haproxy-lens` (giriş yapamaz) |

Başka hiçbir dosyaya yazmaz. Servis, socket'e ve log'a erişmek için gereken gruplarla (`haproxy`, `adm` gibi) çalışır; kullanıcı bu gruplara kalıcı olarak eklenmez.

## Neleri destekler

- **Stats socket:** unix yolu, `unix@`, `abns@`, `ipv4@` / `ipv6@` ve `host:port` biçimleri. Birden fazla socket varsa çalışan ilkini seçer.
- **Config:** çalışan HAProxy'nin komut satırındaki bütün `-f` dosyaları ve klasörleri (`conf.d` gibi).
- **Log kaynağı:** syslog dosyası (yeri rsyslog/syslog-ng ayarından bulunur) ya da journald.
- **Log biçimi:** varsayılan `option httplog`.
- **İşletim sistemi:** systemd kullanan Linux dağıtımları, amd64 ve arm64.

## Bilinen sınırlar

- **Alan adı:** Varsayılan `httplog` biçimi Host bilgisini içermez. Bu yüzden 403 ve 503 alan isteklerde sadece path görünür.
- **Gerçek IP:** Cloudflare arkasından gelen isteklerde log'daki IP Cloudflare'e aittir; panel bu IP'leri "Cloudflare" diye etiketler.
- **Özel log biçimi:** Özel `log-format` kullanan sunucularda log analizi kendiliğinden kapanır; stats paneli tam çalışır.
- **Geçmiş:** Grafik ve 5xx verisi 1 saat hafızada tutulur; ajan ya da HAProxy yeniden başlarsa sıfırdan dolmaya başlar. O sırada panel, aralığın gerçekte kaç dakikayı kapsadığını yazar.
- **Tek sunucu:** Her kurulum sadece kendi sunucusunu gösterir.
- **Şifre ve HTTPS yok:** Erişim sadece ağ adresine göre sınırlanır. İzinli ağdaki herkes paneli görebilir; gerekirse `ALLOW` ile yönetim ağına daralt.

## Nasıl çalışır

```
HAProxy ──(stats socket: show info, show stat)──► haproxy-lens ──► panel (sunucunun iç IP'si:8405)
   │                                                  ▲
   └──► syslog / journald ──(sadece okuma)────────────┘
```

Ajan her 2 saniyede stats'ı okur; saniyelik değerleri iki ölçüm arasındaki sayaç farkından hesaplar. Log satırlarını dakikalık özetlere çevirip son 60 dakikayı tutar. Arayüz programın içine gömülüdür, ayrıca bir web sunucusu gerekmez.

## Geliştirme

Gerekenler: Go 1.22+ ve Node 18+.

```bash
bash build.sh       # arayüzü ve programı derler: ./haproxy-lens
go test ./...       # testler
```

Dosyalar:

| Dosya | İçerik |
|---|---|
| `haproxy.go` | Stats socket'inden okuma (izin verilen komutlar burada) |
| `logtail.go` | Log dosyasını / journald'ı izleme ve özetleme |
| `detect.go` | Kurulum öncesi otomatik tespit (`-detect`) |
| `main.go` | Parametreler ve web sunucusu |
| `listen.go` | Panelin dinleyeceği IP'nin seçimi (keepalived VIP hariç) |
| `access.go` | Panele erişebilecek ağların kontrolü |
| `webapp/` | Panel arayüzü (React) |
| `deploy/` | `install.sh` ve `uninstall.sh` |

## Yeni sürüm yayınlama

1. `CHANGELOG.md` dosyasına yeni sürümü yaz.
2. GitHub'da **Releases > Draft a new release**, yeni bir etiket oluştur (ör. `v0.2.1`) ve **Publish release**'e bas.
3. **Actions** sekmesindeki `release` işi birkaç dakika içinde paketleri derleyip release'e ekler.

## Lisans

MIT. Ayrıntılar için `LICENSE` dosyasına bakın.
