# haproxy-lens

HAProxy'nin stats sayfasını okunur bir panele çeviren, **config'e dokunmayan** salt okunur ajan.

Her HAProxy sunucusuna kurulur, o sunucunun kendi stats verisini ve log'unu okur, tarayıcıda şunları gösterir:

- Sade bir durum özeti: "api içindeki srv3, 12 dakikadır çalışmıyor. Sebep: bağlantı zaman aşımı."
- Backend'ler ve sunucular: durum, sağlık kontrolünün anlamı, bağlantı doluluğu, yanıt süresi, hatalar.
- Canlı grafikler: saniyedeki istek (yanıt türüne göre) ve trafik; zaman aralığı 5 dk, 15 dk ya da 1 saat seçilebilir.
- 4xx ve 5xx hatalarının en çok hangi sunucudan döndüğü; hatalar sunuculara eşit dağılmışsa sorunun ortak bir yerde olabileceği uyarısı.
- Log bölümünde iki ayrı özet: "Hangi yanıt kodu döndü" (2xx/3xx/4xx/5xx) ve "İsteğe ne oldu" (sunucu yanıtladı, engellendi, yönlendirildi...).
- Log'dan "Hangi adres ne döndürüyor": 3xx, 4xx ve 5xx sekmeleri. Hangi path'in hangi kodu (301, 404, 502...) kaç kez döndürdüğü; HAProxy'nin kendi ürettiği http→https yönlendirmeleri dahil.
- Hatalı ve engellenen isteklerde satıra tıklayınca açılan ayrıntı: tam adres (alan adı log'da varsa), gerçek yollar ve isteği gönderen IP'ler.
- Log'dan: en çok istenen adresler, engellenen (403) ve hiçbir backend'e eşleşmeyen (503) istekler, en çok istek atan IP'ler.
- Her terimin sade Türkçe açıklaması ve her satır için HAProxy'nin verdiği tüm alanlar.

## Temel kurallar

- **HAProxy config'ine ve servisine dokunmaz.** Reload ve restart yapmaz.
- **Her sunucuya kendini uydurur.** Ajan, çalışan HAProxy'nin config'ini sadece okuyarak stats socket'ini, log kaynağını ve her frontend'in log biçimini kendisi bulur. Özel `log-format` tanımları da okunur.
- **Çalışırken izler, yeniden kurulum istemez.** Config değişip HAProxy reload edilince (yeni log biçimi, yeni Host yakalaması, yeni backend) ajan bunu en geç 30 saniyede fark eder ve kendini günceller. Log kaynağı susarsa yenisini arar; stats socket çalışmazsa config'teki başka bir socket'e geçer.
- **Eksiği panelde söyler.** Config'te veriyi kısıtlayan bir şey varsa (log kapalı, `dontlog-normal`, alan adı yakalanmıyor, sağlık kontrolü yok, okunamayan log satırları...) panelin üstündeki "Yapılandırma notları" bölümünde ne olduğunu, neyi etkilediğini ve eklenebilecek config satırını yazar.
- **Emin olamazsa kurmaz.** Çalışan bir stats socket bulamazsa hiçbir şey değiştirmeden durur ve sebebini yazar.
- **Sadece okur.** HAProxy'ye yalnızca `show info` ve `show stat` komutlarını gönderir. Başka komut gönderen kod yoktur (bkz. `haproxy.go` içindeki `allowedCommands`).
- **Hassas veri tutmaz.** Log'daki sorgu parametreleri (`?token=...` gibi) hafızada bile tutulmaz.
- **Kaynak tavanı var.** CPU %10 ve RAM 128 MB sınırıyla, düşük öncelikte çalışır; `/etc` ve `/usr` altına yazamaz.
- **İnternete açılmaz.** Panel sunucunun kendi iç IP'sinde açılır (keepalived VIP'inde değil) ve sadece izin verilen ağlardan gelen isteklere cevap verir. Varsayılan liste özel ağlardır (10.x, 172.16-31.x, 192.168.x). Sunucunun ana IP'si herkese açık bir adresse panel `127.0.0.1`'de kalır.
- **Kanıtlar.** Kurulum ve kaldırma sonunda config dosyalarının sha256 özetinin ve HAProxy süreç numaralarının değişmediğini kendisi kontrol edip yazar.

## Kurulum

Tüm komutlar HAProxy sunucusunda, root olarak.

### 1. İndir ve kur

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar. Adımlar `&&` ile bağlı olduğu için biri hata verirse sonrakiler çalışmaz.

```bash
cd /root && rm -rf lens && mkdir lens && cd lens && wget -nv https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/haproxy-lens-linux-amd64.tar.gz https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/SHA256SUMS && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh
```

wget her dosya için bir satır yazar, sonra doğrulama `haproxy-lens-linux-amd64.tar.gz: OK` demeli. Hiçbir şey yazılmadan komut biterse sunucunun GitHub'a erişimi yok demektir; aşağıdaki "Sunucunun internete çıkışı yoksa" bölümüne bakın. ARM sunucularda (`uname -m` çıktısı `aarch64` ise) `amd64` yerine `arm64` yazın.

Kurulum önce bir rapor, sonra yapılacakları gösterir ve onay ister. Sonunda şu iki satırı görmelisin:

```
Doğrulama: HAProxy config dosyaları değişmedi (1 dosya, sha256 aynı).
Doğrulama: HAProxy yeniden başlatılmadı ve reload edilmedi (süreç numaraları aynı).
```

### 2. Kurmadan önce sadece kontrol etmek istersen

Yukarıdaki satırın sonundaki `./install.sh` yerine `./install.sh --check` yaz. Hiçbir şey kurulmaz, sadece rapor verir:

```bash
./install.sh --check
```

Rapor dört bölümden oluşur: bulunan config dosyaları ve stats socket'leri, log kaynağı (ve o LB'de alan adının log'da olup olmadığı), yapılandırma notları, panelin açılacağı adres. En altta bir **SONUÇ** satırı olur:

| Sonuç | Anlamı |
|---|---|
| `UYUMLU (stats + log analizi)` | Her şey kurulabilir. |
| `UYUMLU (sadece stats)` | Stats paneli tam çalışır, log analizi kapalı olur. Sebebi raporda yazar. |
| `KURULAMAZ` | Çalışan ve erişilebilir bir stats socket yok. Hiçbir şey kurulmaz. |

### 3. Kurulum seçenekleri

Satırın sonundaki `./install.sh` yerine kullanabilirsin:

| Komut | Ne yapar |
|---|---|
| `./install.sh -y` | Onay sormadan kurar |
| `PORT=8415 ./install.sh` | Farklı port kullanır |
| `ALLOW=10.234.0.0/16 ./install.sh` | Panele sadece bu ağ(lar)dan erişilebilir; virgülle birden fazla ağ ya da tek IP verilebilir |
| `LISTEN=10.0.0.5 ./install.sh` | Panelin adresini elle verir |
| `LISTEN=127.0.0.1 ./install.sh` | Paneli sadece sunucunun içinden açar (SSH tüneliyle kullanılır) |
| `LOG=/yol/haproxy.log ./install.sh` | Log kaynağını elle sabitler (varsayılan: ajan kendisi bulur ve izler) |

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

Kurulumdaki tek satırın aynısını çalıştırmak yeterli: her seferinde en son sürümü indirir, betik de önceki kurulumu görüp üzerine yazar. Adres, erişim listesi ve log ayarların korunur.

## Kaldırma

```bash
./uninstall.sh
```

Servisi, program dosyasını ve `haproxy-lens` sistem kullanıcısını siler, geride bir şey kalmadığını kontrol eder. Kurulumdaki iki doğrulamayı burada da yapar. Paketin klasörü artık yoksa aynı paketi tekrar indirip içindeki `uninstall.sh`'ı çalıştırabilirsin.

## Sunucunun internete çıkışı yoksa

Paketi kendi bilgisayarına indir, sunucuya kopyala, sonra açıp kur:

```bash
scp haproxy-lens-linux-amd64.tar.gz root@SUNUCU_ADRESI:/root/
```

```bash
cd /root && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh
```

## Yapılandırma notları

Panelin üst kısmındaki bu bölüm, o sunucunun config'inde ya da ortamında paneli kısıtlayan her şeyi listeler. Uyarı varsa kendiliğinden açık gelir. Her notta şunlar yazar: ne eksik, neyi etkiliyor, istersen config'e eklenebilecek satır ve config'teki yeri (`haproxy.cfg:39` gibi). Aynı notlar `./install.sh --check` raporunda da çıkar.

| Not | Ne demek |
|---|---|
| Frontend log yazmıyor | `no log` ya da log hedefi yok; log bölümü bu trafiği göremez |
| Trafik kayıtları log seviyesine takılıyor | Log hedefleri `notice` gibi bir seviyeyle sınırlı; HAProxy trafiği `info` seviyesinde yazar |
| HTTP log biçimi kullanmıyor | `option httplog` yok; yol ve durum kodu log'da yok |
| Log biçiminde eksik alanlar | Özel `log-format` durum kodu, yol, backend/sunucu ya da istemci IP'si içermiyor |
| Başarılı istekleri log'a yazmıyor | `option dontlog-normal` açık; sadece hatalar log'a düşüyor |
| Alan adını log'a yazmıyor / sadece bazı isteklerde yakalıyor | Host başlığı yakalanmıyor ya da koşullu yakalanıyor |
| Sağlık kontrolü yok | `check` olmayan sunucular; düşerlerse HAProxy fark etmez |
| Log satırlarının bir kısmı okunamadı | Satırlar config'teki biçimle uyuşmuyor; örnekleri notta görünür (sorgu parametreleri gizli) |
| Stats'ta trafik var ama log'da yok | Trafiğin geçtiği yerin log'u kapalı ya da başka yere gidiyor |

Ajan config'e hiçbir şey yazmaz. Önerilen bir satırı eklemek senin kararın; eklersen HAProxy reload edildikten sonra panel en geç 30 saniye içinde kendiliğinden uyum sağlar ve not kalkar.

## Kurulum sunucuda neleri değiştirir

| Ne | Nerede |
|---|---|
| Program | `/usr/local/bin/haproxy-lens` |
| Servis | `/etc/systemd/system/haproxy-lens.service` |
| Sistem kullanıcısı | `haproxy-lens` (giriş yapamaz) |

Başka hiçbir dosyaya yazmaz. Servis, stats socket'ine ve log'a erişebilmek için gereken gruplarla çalışır: socket'in grubu (genelde `haproxy`), syslog dosyaları için `adm` ve journald için `systemd-journal` (sunucuda varsa). Böylece log'un yeri sonradan değişse de yeniden kurulum gerekmez. Kullanıcı bu gruplara kalıcı olarak eklenmez; gruplar yalnızca servis çalışırken geçerlidir.

## Neleri destekler

- **Stats socket:** unix yolu, `unix@`, `abns@`, `ipv4@` / `ipv6@` ve `host:port` biçimleri. Birden fazla socket varsa çalışan ilkini seçer.
- **Config:** çalışan HAProxy'nin komut satırındaki bütün `-f` dosyaları ve klasörleri (`conf.d` gibi).
- **Log kaynağı:** syslog dosyası (yeri rsyslog/syslog-ng ayarından bulunur) ya da journald.
- **Log biçimi:** `option httplog`, `option httpslog`, `option tcplog` ve özel `log-format` tanımları (JSON benzeri biçimler dahil). `defaults` mirası, adlı `defaults` bölümleri ve `from` desteklenir. Tanınmayan değişkenler atlanır; panelin ihtiyaç duyduğu bir alan yoksa bu not olarak raporlanır. `option httplog clf` (CLF) henüz desteklenmiyor.
- **İşletim sistemi:** systemd kullanan Linux dağıtımları, amd64 ve arm64.

## Alan adı (hangi domaine istek gelmiş)

haproxy-lens config'e dokunmaz; alan adını log'da bulabildiği kadarıyla gösterir. Her LB'de kendiliğinden şu kaynaklara bakar:

| Log'da alan adı olur, eğer | Örnek |
|---|---|
| Host başlığı yakalanıyorsa | `capture request header Host len 64` ya da `http-request capture req.hdr(host) len 64` |
| İstek HTTP/2 ise | HAProxy istek satırına `https://alan.com/yol` yazar |
| `option httpslog` kullanılıyorsa | Satırın sonundaki SNI alanından |
| `log-format`'ın sonuna host eklenmişse | `... %{+Q}r %[req.hdr(host)]` |

Hiçbiri yoksa ayrıntıda sadece yol ve IP görünür. Yakalama koşulluysa (ör. `if rate_limit_abuse`) alan adı sadece o isteklerde görünür; panel kaç istekte bilindiğini yazar. `./install.sh --check` raporu da o LB'de alan adının log'da olup olmadığını söyler.

Alan adını görmek istediğin bir LB'de bunu sen eklemeye karar verirsen en basit yol frontend'e `capture request header Host len 64` satırıdır. Bu satır mevcut log satırlarına `{alan.com}` bölümünü ekler; o log'u okuyan başka bir araç (Elasticsearch, fail2ban gibi) varsa önce onu kontrol et ve değişikliği önce bir slave'de dene.

## Bilinen sınırlar

- **Alan adı:** Varsayılan `httplog` biçimi Host bilgisini içermez; o LB'de hiçbir kaynaktan alan adı bulunamazsa (yukarıdaki tabloya bakın) 3xx, 4xx ve 5xx dönen isteklerde sadece yol ve IP görünür.
- **Gerçek IP:** Cloudflare arkasından gelen isteklerde log'daki IP Cloudflare'e aittir; panel bu IP'leri "Cloudflare" diye etiketler.
- **Log biçimi tahmini değil:** Ajan satırları config'teki log tanımına göre okur. Config'te olmayan bir biçimle gelen satırlar (ör. başka bir sunucudan aynı dosyaya yazılanlar) okunamaz ve "Yapılandırma notları"nda örnekleriyle görünür.
- **Geçmiş:** Grafikler ve yanıt kodu sayımları 1 saat hafızada tutulur; ajan ya da HAProxy yeniden başlarsa sıfırdan dolmaya başlar. O sırada panel, aralığın gerçekte kaç dakikayı kapsadığını yazar. Kalıcı bir veritabanı yok.
- **Tek sunucu:** Her kurulum sadece kendi sunucusunu gösterir.
- **Şifre ve HTTPS yok:** Erişim sadece ağ adresine göre sınırlanır. İzinli ağdaki herkes paneli görebilir; gerekirse `ALLOW` ile yönetim ağına daralt.

## Sorun giderme

| Durum | Ne yapmalı |
|---|---|
| Panel açılmıyor | `systemctl status haproxy-lens` |
| Servis çalışıyor ama panelde veri yok | `journalctl -u haproxy-lens -n 50` — en sık sebep servis kullanıcısının stats socket'ine erişememesi |
| Panelin adresini unuttun | `systemctl show haproxy-lens -p ExecStart` ya da `ss -ltnp \| grep haproxy-lens` |
| Hangi sürüm kurulu | `haproxy-lens -version` (panelin en üstünde, adın yanında da yazar) |
| Tarayıcıda "Bu adresten panele erişim izni yok" | Bulunduğun ağ izinli listede değil: `ALLOW=<ağ>/<önek> ./install.sh` ile tekrar kur |
| Sayfa hiç açılmıyor, zaman aşımı | Güvenlik duvarı (ufw/firewalld) portu kapatıyor olabilir; kurulum bunu fark ederse uyarır ama kendisi dokunmaz |
| Log bölümü boş ya da eksik | Panelin üstündeki "Yapılandırma notları" sebebini ve varsa eklenebilecek config satırını yazar |
| Ayarları değiştirmek istiyorsun | Aynı paketten `ALLOW=... LISTEN=... ./install.sh` çalıştırmak yeterli; servis dosyası yeniden yazılır |

Ajan çalışırken config'i 30 saniyede bir kontrol eder. HAProxy'de yaptığın bir değişiklikten sonra reload ettiysen panelin kendini güncellemesi için bir şey yapmana gerek yok.

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
| `main.go` | Parametreler, web sunucusu, `/api/*` uçları |
| `haproxy.go` | Stats socket'inden okuma (izin verilen komutlar burada), zaman aralığı hesabı |
| `config.go` | haproxy.cfg'yi okuma: bölümler, `defaults` mirası, log hedefleri, Host yakalama |
| `logformat.go` | `log-format` tanımını ayrıştırıcıya çevirme (httplog, httpslog, tcplog, özel) |
| `logtail.go` | Log dosyasını / journald'ı izleme, dakikalık özetler, yol ve kod dökümü |
| `watch.go` | Çalışırken config, log kaynağı ve socket izleme; `/api/config` |
| `notes.go` | Yapılandırma notları (ne eksik, neyi etkiliyor, hangi satır eklenebilir) |
| `detect.go` | Kurulum öncesi tespit ve uyumluluk raporu (`-detect`) |
| `listen.go` | Panelin dinleyeceği IP'nin seçimi (keepalived VIP hariç) |
| `access.go` | Panele erişebilecek ağların kontrolü |
| `cloudflare.go` | Yerleşik Cloudflare IP aralıkları (etiketleme için) |
| `*_test.go` | Ayrıştırıcı, config uyumu, düzen ve erişim testleri |
| `webapp/` | Panel arayüzü (React); `build.sh` derleyip programa gömer |
| `deploy/` | `install.sh` ve `uninstall.sh` |

## Yeni sürüm yayınlama

1. `CHANGELOG.md` dosyasına yeni sürümü yaz. Bir önceki sürümün "Kurulum ve güncelleme" bölümünü olduğu gibi kopyala; her sürümde aynıdır.
2. GitHub'da **Releases > Draft a new release**, yeni bir etiket oluştur (ör. `v0.8.1`), başlığa `haproxy-lens 0.8.1` yaz.
3. Açıklama kutusuna `CHANGELOG.md`'deki o sürüm bölümünün tamamını yapıştır (en üstteki sürüm numarası satırı hariç). Kurulum komutları böylece release sayfasında hazır gelir.
4. **Publish release**'e bas. **Set as a pre-release** işaretli olmamalı, yoksa `latest` adresi o sürümü göstermez.
5. **Actions** sekmesindeki `release` işi birkaç dakika içinde paketleri derleyip release'e ekler. Sunucularda `wget` çekmeden önce bu işin yeşile dönmesini bekle.

Sürüm numarası: hata düzeltmesinde son hane (0.8.0 → 0.8.1), yeni özellikte ortadaki hane (0.8.0 → 0.9.0) artar.

## Lisans

MIT. Ayrıntılar için `LICENSE` dosyasına bakın.
