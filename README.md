# haproxy-lens

HAProxy'nin stats sayfasını okunur bir panele çeviren, **config'e dokunmayan** salt okunur ajan.

Her HAProxy sunucusuna kurulur, o sunucunun kendi stats verisini ve log'unu okur, tarayıcıda şunları gösterir:

- Sade bir durum özeti: "api içindeki srv3, 12 dakikadır çalışmıyor. Sebep: bağlantı zaman aşımı."
- Backend'ler ve sunucular: durum, sağlık kontrolünün anlamı, bağlantı doluluğu, yanıt süresi, hatalar.
- Canlı grafikler: saniyedeki istek (yanıt türüne göre) ve trafik; zaman aralığı 5 dk, 15 dk, 1 saat, 6 saat ya da 24 saat seçilebilir.
- 4xx ve 5xx hatalarının en çok hangi sunucudan döndüğü; hatalar sunuculara eşit dağılmışsa sorunun ortak bir yerde olabileceği uyarısı.
- Log bölümünün başında tek bir özet: renkli bir çubukla isteklerin 2xx/3xx/4xx/5xx dağılımı, her birinin ne anlama geldiği ve düz cümlelerle isteklerin nereye gittiği (kaçı sunuculara ulaştı, kaçını HAProxy kendisi yanıtladı, kaçı hiçbir sunucuya ulaşamadı).
- Log'dan "Hangi adres ne döndürüyor": 3xx, 4xx ve 5xx sekmeleri. Hangi path'in hangi kodu (301, 404, 502...) kaç kez döndürdüğü; HAProxy'nin kendi ürettiği http→https yönlendirmeleri dahil.
- Hatalı ve engellenen isteklerde satıra tıklayınca açılan ayrıntı: tam adres (alan adı log'da varsa), gerçek yollar ve isteği gönderen IP'ler.
- "En çok istek atan IP'ler": her IP'nin en çok istediği adresler; bir IP'nin normal kullanıcı mı, tarama botu mu olduğu görülebiliyor.
- "En çok istenen adresler": satıra tıklayınca o adrese en çok istek yapan 20 IP; her IP'nin yanında aldığı yanıt kodları tam olarak (200, 404, 500...). Yanıt kodundan bağımsız çalışır, yani başarılı isteklerde de görünür. Cloudflare'e ait IP'ler etiketlenir, doğrudan gelenler etiketsiz görünür.
- Bir backend'i açınca o backend'e gelen isteklerin adres ve IP dökümü: "buraya hiç trafik gitmemeli" dediğin bir backend'e kimin, nereye istek attığı.
- Log'dan: en çok istenen adresler, engellenen (403) ve hiçbir backend'e eşleşmeyen (503) istekler, en çok istek atan IP'ler.
- Her terimin sade Türkçe açıklaması ve her satır için HAProxy'nin verdiği tüm alanlar.

## Temel kurallar

- **HAProxy config'ine ve servisine dokunmaz.** Reload ve restart yapmaz.
- **Her sunucuya kendini uydurur.** Ajan, çalışan HAProxy'nin config'ini sadece okuyarak stats socket'ini, log kaynağını ve her frontend'in log biçimini kendisi bulur. Özel `log-format` tanımları da okunur.
- **Geçmişi saklar.** Grafikler ve oranlar varsayılan olarak 24 saat geriye gider. Veriler `/var/lib/haproxy-lens` altına yazılır (24 saat için birkaç yüz KB), böylece ajan yeniden başladığında geçmiş kaybolmaz.
- **Sunucunun kendi ölçümleri.** İşlemci, disk beklemesi, bellek, yük ortalaması, disk doluluğu ve disk okuma/yazma hızı; canlı grafiklerle. Veriler `/proc` altından okunur: ek yetki, ek araç ya da ek servis gerekmez.
- **Log'da arama.** Panelden bağımsız olarak log dosyalarında (döndürülmüş ve sıkıştırılmış dahil) arama yapar; saklama süresinin ötesine bakabilir.
- **Çalışırken izler, yeniden kurulum istemez.** Config değişip HAProxy reload edilince (yeni log biçimi, yeni Host yakalaması, yeni backend) ajan bunu en geç 30 saniyede fark eder ve kendini günceller. Log kaynağı susarsa yenisini arar; stats socket çalışmazsa config'teki başka bir socket'e geçer.
- **Eksiği panelde söyler.** Config'te veriyi kısıtlayan bir şey varsa (log kapalı, `dontlog-normal`, alan adı yakalanmıyor, sağlık kontrolü yok, okunamayan log satırları...) panelin üstündeki "Yapılandırma notları" bölümünde ne olduğunu, neyi etkilediğini ve eklenebilecek config satırını yazar.
- **Emin olamazsa kurmaz.** Çalışan bir stats socket bulamazsa hiçbir şey değiştirmeden durur ve sebebini yazar.
- **Sadece okur.** HAProxy'ye yalnızca `show info` ve `show stat` komutlarını gönderir. Başka komut gönderen kod yoktur (bkz. `haproxy.go` içindeki `allowedCommands`).
- **Hassas veri tutmaz.** Log'daki sorgu parametreleri (`?token=...` gibi) hafızada bile tutulmaz.
- **Kaynak tavanı var.** CPU %10 ve RAM varsayılan 512 MB sınırıyla (`MEMMAX` ile ayarlanır), düşük öncelikte çalışır; `/etc` ve `/usr` altına yazamaz. Normal kullanımda ~50 MB tutar.
- **Kapanırken kaydeder.** Servis durdurulurken ya da yeniden başlatılırken (güncellemede olduğu gibi) geçmişi diske yazıp kapanır; yeniden açıldığında kaldığı yerden devam eder, aynı satırı iki kez saymaz, kapalıyken yazılan satırları da atlamaz. Açılışta geçmişi yüklemek birkaç saniye sürebilir; bu sürede panel açıktır ve "geçmiş yükleniyor" der.
- **Tarayıcı korumaları açık.** Panel başka bir siteye gömülemez (`frame-ancestors 'none'`), yalnızca kendi dosyalarını yükler (Content-Security-Policy), içerik türü tahmin edilmez.
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

Hiçbir parametre vermezsen kurulum şu varsayılanlarla çalışır:

| Parametre | Varsayılan | Ne yapar |
|---|---|---|
| `PORT` | `8405` | Panelin portu |
| `LISTEN` | sunucunun iç IP'si | Panelin adresi; bulunamazsa `127.0.0.1` (yalnızca SSH tüneliyle) |
| `ALLOW` | özel ağlar (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `100.64.0.0/10`, `127.0.0.0/8`) | Panele erişebilecek ağlar |
| `LOG` | `auto` | Log kaynağı; ajan kendisi bulur ve çalışırken izler |
| `RETENTION` | `24h` | Sayıların saklanma süresi |
| `DETAIL` | `1h` | Tam ayrıntının (tam adres, gerçek yollar, IP dökümü) saklanma süresi |
| `LISTS` | `6h` | Yol ve IP listelerinin saklanma süresi |
| `BUDGET` | `250` | Ayrıntı için bellek bütçesi (MB) |
| `MEMMAX` | `512M` | Servisin bellek tavanı |

Değiştirmek için satırın sonundaki `./install.sh` yerine kullanabilirsin:

| Komut | Ne yapar |
|---|---|
| `./install.sh -y` | Onay sormadan kurar |
| `PORT=8415 ./install.sh` | Farklı port kullanır |
| `ALLOW=10.20.0.0/16 ./install.sh` | Panele sadece bu ağ(lar)dan erişilebilir; virgülle birden fazla ağ ya da tek IP verilebilir |
| `LISTEN=10.0.0.5 ./install.sh` | Panelin adresini elle verir |
| `LISTEN=127.0.0.1 ./install.sh` | Paneli sadece sunucunun içinden açar (SSH tüneliyle kullanılır) |
| `LOG=/yol/haproxy.log ./install.sh` | Log kaynağını elle sabitler (varsayılan: ajan kendisi bulur ve izler) |
| `RETENTION=48h ./install.sh` | Sayıların ne kadar saklanacağı (varsayılan 24 saat, en az 1 saat) |
| `DETAIL=6h ./install.sh` | Tam ayrıntının (tam adres, gerçek yollar, IP dökümü) saklanacağı süre (varsayılan 1 saat) |
| `LISTS=24h ./install.sh` | Yol ve IP listelerinin saklanacağı süre (varsayılan 6 saat) |
| `BUDGET=500 ./install.sh` | Ayrıntı için bellek bütçesi, MB (varsayılan 250) |
| `MEMMAX=768M ./install.sh` | Servisin bellek tavanı (varsayılan 512M) |

Son dördü ne kadar geriye ne kadar ayrıntı göreceğini belirler; [aşağıdaki bölüme](#ne-kadar-geriye-ne-kadar-ayrıntı) bakın.

Güncellemede komutta vermediğin her ayar önceki kurulumdan korunur (port, adres, erişim listesi, log kaynağı, saklama süreleri, bellek bütçesi ve tavanı); kurulum hangilerini koruduğunu ekrana yazar. Yalnızca değiştirmek istediğini vermen yeterli: `DETAIL=12h ./install.sh` gerisine dokunmaz. Önceki panel adresi artık sunucuda yoksa (IP değiştiyse) adres yeniden tespit edilir. Kurulum, sonunda hangi değerlerle çalıştığını ekrana yazar.

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

## Log'da arama

Panelin en altındaki "Log'da ara" bölümü (sayfanın başındaki **Log'da ara** düğmesi oraya götürür), paneldeki verilerden bağımsız çalışır: doğrudan log dosyalarını okur, döndürülmüş (`haproxy.log.1`) ve sıkıştırılmış (`.gz`) dosyalar dahil. Bu yüzden panelin saklama süresinden (varsayılan 24 saat) çok daha geriye gidebilir.

Aranabilenler: adresin içinde geçen metin, IP (tam ya da başlangıcı), durum kodu (`500` ya da `5xx`) ve zaman aralığı. Paneldeki bir satırın ayrıntısı artık tutulmuyorsa (ayrıntılar panelde yalnızca son 1 saat tutulur), satırdaki "IP'leri ve zamanları log'dan getir" düğmesi o isteği yöntemi ve tam adresiyle burada aratır. Sonuçta toplam eşleşme, kod dağılımı, en çok istek yapan IP'ler, en çok eşleşen adresler ve en yeni eşleşen istekler zaman damgalarıyla listelenir.

Nasıl korunur:

- Kabuk komutu çalıştırılmaz; dosyalar programın içinde okunur, bu yüzden arama metniyle komut çalıştırılamaz.
- Yalnızca ajanın kullandığı log kaynağı ve onun döndürülmüş kopyaları okunur; kullanıcıdan dosya yolu kabul edilmez.
- Aranan metni içermeyen satırlar, ayrıştırılmadan ucuz bir metin karşılaştırmasıyla elenir. Ayrıştırma saniyede ~200 bin satır işlerken bu eleme ~4 milyon satır işler; büyük log'larda aramayı kat kat hızlandırır. Sonuç değişmez: eleme yalnızca "kesinlikle eşleşmez" diyebildiği satırları atar, asıl süzgeç yine ayrıştırılmış kayıt üzerinde çalışır.
- Log dosyaları **sondan başa** okunur: en yeni kayıtlar önce taranır. Bu sayede "son 1 saat" araması dosya ne kadar büyük olursa olsun hızlı biter ve süre sınırına takılsa bile elde edilen sonuçlar en güncel kayıtları kapsar.
- Arama en fazla 20 saniye çalışır ve aynı anda tek arama yapılır (ajanın CPU tavanı düşük). Sınıra takılırsa sonuç bunu açıkça yazar.
- Zaman aralığı verildiğinde, son yazma zamanı aralığın dışında kalan dosyalar hiç açılmaz.

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
| Saklanan geçmiş | `/var/lib/haproxy-lens` (systemd oluşturur, kaldırma betiği siler) |
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
- **Geçmiş bellekte tutulur, disk yalnızca yedektir.** Bu yüzden asıl sınır disk değil bellektir; ayrıntı süresini uzatmadan önce aşağıdaki tabloya bakın.
- **Geçmişin ayrıntısı zamanla azalır:** Sayılar saklama süresi boyunca eksiksiz durur, ama adres ve IP ayrıntısı varsayılan olarak son 1 saati, listeler son 6 saati kapsar. Süreler ayarlanabilir; bkz. [Ne kadar geriye, ne kadar ayrıntı](#ne-kadar-geriye-ne-kadar-ayrıntı).
- **Kalıcı bir veritabanı yok:** Geçmiş tek bir sıkıştırılmış dosyada tutulur. Yıllık trend ya da serbest sorgu gerekiyorsa Prometheus gibi bir sistem gerekir.
- **Yeniden başlatma:** HAProxy yeniden başlarsa sayaçları sıfırlandığı için o andan sonrası yeniden birikir; panel aralığın gerçekte kaç dakikayı kapsadığını yazar.
- **Tek sunucu:** Her kurulum sadece kendi sunucusunu gösterir.
- **Şifre ve HTTPS yok:** Erişim sadece ağ adresine göre sınırlanır. İzinli ağdaki herkes paneli görebilir; gerekirse `ALLOW` ile yönetim ağına daralt.

## Ne kadar geriye, ne kadar ayrıntı

Geçmişin tamamı aynı ayrıntıda saklanmaz: veri yaşlandıkça kademeli olarak sadeleşir. Amaç belleği sınırlı tutmak; hangi kademenin ne kadar süreceğini siz belirlersiniz.

| Veri yaşı | Panelde ne görürsünüz | Parametre (varsayılan) |
|---|---|---|
| 0 – 1 saat | **Her şey.** Sayılar, yol ve IP listeleri, ayrıca satıra tıklayınca açılan ayrıntı: tam adres (alan adı log'da varsa), gerçek yollar ve isteği gönderen IP'ler | `DETAIL` (1 saat) |
| 1 – 6 saat | Sayılar, en yoğun yol ve IP listeleri, ayrıca adres başına IP dökümü. Tam adres ve gerçek yol ayrıntısı yok | `LISTS` (6 saat) |
| 6 – 24 saat | **Yalnızca sayılar:** istek sayısı, 2xx/3xx/4xx/5xx dağılımı, backend başına döküm, grafikler | `RETENTION` (24 saat) |
| 24 saatten eski | Silinir | |

Tablodaki süreler varsayılanlardır; hiçbir parametre vermezsen bu şekilde çalışır.

Sayılar hiçbir kademede eksilmez; kısalan tek şey adres ve IP ayrıntısıdır. Grafikler ve oranlar bu yüzden 24 saat boyunca eksiksizdir.

**Değiştirmek için** kurulum komutunun sonundaki `./install.sh` yerine:

```bash
DETAIL=6h LISTS=24h BUDGET=500 MEMMAX=768M ./install.sh
```

Bu örnekte ayrıntı 6 saat, listeler 24 saat geriye gider. Bellek maliyeti için aşağıdaki tabloya bakın; ayrıntıyı uzatırsanız bütçeyi ve servis tavanını da yükseltin.

Tüm kademeleri aynı yapmak da mümkün: `RETENTION=24h DETAIL=24h LISTS=24h BUDGET=750 MEMMAX=1G ./install.sh` ile 24 saatin tamamı tam ayrıntılı olur (yoğun bir LB'de ~664 MB; bütçe bunun altında kalırsa ajan en eski ayrıntıyı bırakır ve panel bunu yazar).

**Panel ne gördüğünü söyler.** Log bölümü, ayrıntının ve listelerin ayarlanan değil *gerçekte* kapsadığı süreyi yazar. Bir tarama saldırısında bellek bütçesi devreye girip ayrıntıyı kısaltırsa bunu orada görürsünüz.

## Sunucu ölçümleri

Panelde "Sunucu" bölümü, HAProxy'nin çalıştığı makinenin kendi durumunu gösterir: işlemci kullanımı, işlemcinin disk beklediği süre, bellek, yük ortalaması, disk doluluğu ve disk okuma/yazma hızı. Bir yavaşlamanın sebebi çoğu zaman HAProxy'de değil buradadır.

Veriler `/proc/stat`, `/proc/meminfo`, `/proc/diskstats` ve dosya sistemi bilgisinden okunur. Bu dosyalar herkese açık olduğu için ek yetki gerekmez; kabuk komutu da çalıştırılmaz. Disk doluluğu için kök dizin, HAProxy'nin log yazdığı bölüm ve ajanın geçmişi sakladığı bölüm izlenir (aynı dosya sistemiyse bir kez gösterilir).

Disk G/Ç hesaplanırken yalnızca fiziksel aygıtlar sayılır (`sda`, `vda`, `nvme0n1` gibi); bölümler ve `dm-`, `loop` gibi eşlemeler atlanır, yoksa aynı okuma iki kez toplanır.

Geçmiş, HAProxy ölçümleriyle aynı şekilde saklanır: son 1 saat ince, ötesi dakikalık ortalama, ve dakikalık özet diske yazıldığı için ajan yeniden başlasa da kaybolmaz.

## Bellek

Geçmiş bellekte tutulur (disk yalnızca yeniden başlatma için yedektir), bu yüzden asıl sınır diskte değil bellektedir. Varsayılan ayarlarda (24 saat sayı, 6 saat yol/IP listesi, 1 saat tam ayrıntı) yoğun bir LB'de **~51 MB** kullanılır.

Ölçümler `go test -run TestBellekKullanimi` ile tekrarlanabilir; yük olarak saatte ~44.000 istek, dakikada 300 farklı adres, 150 farklı IP alındı.

| Ayar | Bellek |
|---|---|
| **Varsayılan:** ayrıntı 1 saat, listeler 6 saat | ~51 MB |
| `DETAIL=6h LISTS=24h` | ~213 MB |
| `DETAIL=24h LISTS=24h` (her şey tam ayrıntı) | ~664 MB |

Sayılar (istek, yanıt kodu, backend başına döküm) her ayarda saklama süresi boyunca eksiksiz kalır; tablo yalnızca adres ve IP ayrıntısının maliyetidir. Trafiği düşük LB'lerde bu rakamlar çok daha azdır.

**Bellek bütçesi.** Bellek istek sayısından çok *farklı adres sayısına* bağlıdır ve bir tarama saldırısında her istek benzersiz bir adres olabilir. Bu yüzden süre sınırının yanında bir bütçe vardır (varsayılan 250 MB): aşılırsa ajan en eski ayrıntıyı kendiliğinden bırakır ve panel ayrıntının gerçekte kaç dakikayı kapsadığını yazar. Testte dakikada 8.000 benzersiz adresle 6 saat saldırı üretildi (2,88 milyon istek): bellek 161 MB'da kaldı, ayrıntı 50 dakikaya indi ve sayımların tamamı korundu (`go test -run TestAtakDayanikliligi`).

**Servis tavanı** (`MemoryMax`) 512 MB'tır. Bu bir rezervasyon değil üst sınırdır; amacı saldırı anında servisin öldürülmemesidir.

**Bellek iadesi.** Yoğunluk geçtikten sonra ajan belleği yalnızca kendi içinde boşaltmakla kalmaz, işletim sistemine de geri verir. Go bunu kendiliğinden hemen yapmadığı ve systemd'nin tavanı RSS üzerinden uygulandığı için iade tetiklenir: büyük bir gerilemeden sonra hemen, küçük gerilemelerde en fazla 10 dakikada bir. Ölçümde saldırı sonrası RSS birkaç saniye içinde 257 MB'tan 100 MB'a indi.

Ayarlar: `DETAIL=6h LISTS=24h BUDGET=500 MEMMAX=768M ./install.sh`. Bütçeyi yükseltirseniz servis tavanını da yükseltin.

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
| Kurulum "HATA: ... olmalı" diyerek durdu | Verdiğin parametrelerden biri geçersiz; mesaj hangisi olduğunu ve örneğini yazar. Bu durumda sisteme hiçbir şey dokunulmamıştır |
| Aramada "başka bir arama sürüyor" | Ajan aynı anda tek arama yapar (işlemci tavanı düşük olduğu için); birkaç saniye sonra tekrar dene |

### Kurulum hangi değerleri kabul eder

Kurulum, sisteme dokunmadan önce parametreleri kontrol eder ve hatalı bir değerde anlaşılır bir mesajla durur. Kurallar:

- Süreler birimiyle yazılır: `30m`, `6h`, `1h30m`. Birimsiz `6` kabul edilmez.
- `RETENTION` en az 1 saat, en fazla 30 gün. `DETAIL` en az 5 dakika. Sıra şöyle olmalı: `DETAIL` ≤ `LISTS` ≤ `RETENTION`.
- `BUDGET` megabayt cinsinden, en az 16.
- `MEMMAX` birimiyle yazılır (`512M`, `1G`) ve `BUDGET`'tan en az 128 MB büyük olmalı; yoksa servis bütçeye ulaşmadan öldürülür. (systemd birimsiz sayıyı bayt sayar; `MEMMAX=512` servisi açılır açılmaz öldürürdü.)
- `PORT` 1 ile 65535 arasında.

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
| `system.go` | Sunucu ölçümleri (`/proc/stat`, `/proc/meminfo`, `/proc/diskstats`, disk doluluğu) |
| `store.go` | Geçmişin diske yazılması ve yeniden başlatmada yüklenmesi |
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
| `search.go` | Log'da arama: döndürülmüş ve sıkıştırılmış dosyalar dahil, sondan başa okuma |
| `*_test.go` | Ayrıştırıcı, config uyumu, bellek, saldırı dayanıklılığı, arama, ayar doğrulama ve erişim testleri |
| `webapp/` | Panel arayüzü (React) ve simge (`favicon.svg`, `favicon.png`); `build.sh` derleyip programa gömer |
| `deploy/` | `install.sh` ve `uninstall.sh` |

## Yeni sürüm yayınlama

1. `CHANGELOG.md` dosyasına yeni sürümü yaz. Bir önceki sürümün "Kurulum ve güncelleme" bölümünü olduğu gibi kopyala; her sürümde aynıdır.
2. GitHub'da **Releases > Draft a new release**, yeni bir etiket oluştur (ör. `v0.8.1`), başlığa `haproxy-lens 0.8.1` yaz.
3. Açıklama kutusuna `CHANGELOG.md`'deki o sürüm bölümünün tamamını yapıştır (en üstteki sürüm numarası satırı hariç). Kurulum komutları böylece release sayfasında hazır gelir.
4. **Publish release**'e bas. **Set as a pre-release** işaretli olmamalı, yoksa `latest` adresi o sürümü göstermez.
5. **Actions** sekmesindeki `release` işi birkaç dakika içinde paketleri derleyip release'e ekler. Sunucularda `wget` çekmeden önce bu işin yeşile dönmesini bekle.

Sürüm numarası: hata düzeltmesinde son hane (0.8.0 → 0.8.1), yeni özellikte ortadaki hane (0.8.0 → 0.9.0) artar.

## Lisans ve markalar

MIT. Ayrıntılar için `LICENSE` dosyasına bakın.

Bu bağımsız bir açık kaynak projedir; HAProxy Technologies ile bir ilgisi, ortaklığı ya da onayı yoktur. "HAProxy" adı yalnızca uyumlu olunan yazılımı belirtmek için kullanılır. Projenin simgesi özgündür ve HAProxy'nin logosuyla benzerlik taşımaz.
