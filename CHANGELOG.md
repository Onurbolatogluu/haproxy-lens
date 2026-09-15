# Değişiklikler

Her sürümün altında, o sürüme geçmek için sunucuda çalıştırılacak komutlar da var.
GitHub'da release yayınlarken bu dosyadaki ilgili sürüm bölümünün tamamını (en üstteki
sürüm numarası satırı hariç) açıklama kutusuna yapıştırmak yeterli.

## 0.12.0

- **Bellek hatası düzeltildi (önemli).** Geç gelen log satırları eski bir dakikaya yazılabiliyor: ajan açılışta birikmiş log'u okurken ya da log gecikmeliyse. Böyle bir dakika bir kez sadeleştirilmiş sayılıyor, sonra yeniden dolduğunda bir daha sadeleştirilmiyordu; bellek sessizce büyüyordu. Ölçümde 6 saatlik bir tarama saldırısı **696 MB**'a çıkıyordu, düzeltmeden sonra aynı senaryo **125 MB**. Sadeleştirme artık her bakımda yeniden uygulanıyor.
- **İkinci bellek düzeltmesi:** Sadeleştirmede listeler kırpılıyor ama Go'da map'ten anahtar silmek iç diziyi küçültmediği için bellek gerçekte boşalmıyordu. Map'ler artık yeniden kuruluyor; yoğun bir LB'de ~19 MB fark ediyor.
- **Bellek bütçesi eklendi (varsayılan 250 MB).** Bellek, istek sayısından çok farklı adres sayısına bağlı; tarama saldırılarında her istek benzersiz bir adres olabiliyor ve eskiden bunu sınırlayan bir şey yoktu. Bütçe aşılırsa ajan en eski ayrıntıyı kendiliğinden bırakıyor. Dakikada 8.000 benzersiz adresle 6 saatlik saldırı testinde bellek 161 MB'da kaldı, ayrıntı 50 dakikaya indi ve **2.880.000 isteğin sayımı eksiksiz korundu**.
- Servisin bellek tavanı 512M oldu (bütçe 250 MB + pay). Tavan bir rezervasyon değil üst sınırdır; normal kullanımda ajan yine ~40 MB tutar.
- Panel artık ayrıntının ve listelerin ayarlanan değil **gerçekte kapsadığı** süreyi yazıyor; bütçe devreye girdiğinde bu görünür.
- Ayrıntı ve liste süreleri ayarlanabilir oldu: `DETAIL=6h LISTS=24h BUDGET=500 MEMMAX=768M ./install.sh`. **Varsayılanlar değişmedi** (ayrıntı 1 saat, listeler 6 saat).
- README'ye ölçülmüş bellek tablosu eklendi; ölçümler `go test -run TestBellekKullanimi` ve `go test -run TestAtakDayanikliligi` ile tekrarlanabilir.

### Kurulum ve güncelleme

Sunucuda root olarak aşağıdaki komutlar yeterli. Betik önceki kurulumu görür ve üzerine yazar; adres, erişim listesi ve log ayarların korunur.

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -nv https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/haproxy-lens-linux-amd64.tar.gz https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/SHA256SUMS && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz ve sebebi ekrana yazılır. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).

## 0.11.0

- **Geçmiş artık 24 saat saklanıyor** (eskiden 1 saat). Zaman aralığı seçicisine **6 saat** ve **24 saat** eklendi. Süre `RETENTION=48h ./install.sh` ile değiştirilebiliyor.
- **Ajan yeniden başlasa da geçmiş kaybolmuyor.** Veriler dakikada bir `/var/lib/haproxy-lens` altına sıkıştırılmış tek dosya olarak yazılıyor; 24 saat birkaç yüz KB tutuyor. Dosya bozuksa ajan sıfırdan başlıyor ve bunu günlüğe yazıyor.
- Uzun aralıklarda oranlar dakikalık artışların toplamından hesaplanıyor; grafikler dakikalık ortalamalardan çiziliyor.
- Eski dakikalar kademeli sadeleşiyor: 1 saatten sonra tam adres ve IP dökümü gibi ayrıntılar, 6 saatten sonra yol ve IP listeleri düşüyor. **Sayılar (istek, yanıt kodu, backend başına) saklama süresi boyunca eksiksiz kalıyor.** Panel hangi bilginin ne kadarlık süreyi kapsadığını yazıyor.
- Servisin bellek tavanı 128 MB'tan 256 MB'a çıkarıldı; 24 saatlik geçmiş bellekte de tutuluyor.
- Kaldırma betiği saklanan geçmişi de siliyor.

### Kurulum ve güncelleme

Sunucuda root olarak aşağıdaki komutlar yeterli. Betik önceki kurulumu görür ve üzerine yazar; adres, erişim listesi ve log ayarların korunur.

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -nv https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/haproxy-lens-linux-amd64.tar.gz https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/SHA256SUMS && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz ve sebebi ekrana yazılır. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).

## 0.10.1

- Backend başına adres ve IP dökümü yanlış yerdeydi: backend ayrıntısının içine konmuştu. Artık **"En çok istek alan backend'ler"** panelinde; satıra tıklayınca o backend'e gelen adresler ve IP'ler açılıyor. Böylece "bu backend'e hiç trafik gitmemeli" sorusu listeye bakarken cevaplanıyor.
- Satır açıkken liste yenilemelerde yeniden sıralanmıyor.
- Panel açıklamasına "toplam" sütununun HAProxy açıldığından beri birikmiş sayı olduğu, log dökümünün ise yalnızca seçili aralığı kapsadığı eklendi.
- Log analizi kapalı olan sunucularda açılan satır bunu ve sebebinin nerede yazdığını söylüyor.

### Kurulum ve güncelleme

Sunucuda root olarak aşağıdaki komutlar yeterli. Betik önceki kurulumu görür ve üzerine yazar; adres, erişim listesi ve log ayarların korunur.

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -nv https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/haproxy-lens-linux-amd64.tar.gz https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/SHA256SUMS && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz ve sebebi ekrana yazılır. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).

## 0.10.0

- Bir backend'i açınca artık **o backend'e gelen isteklerin dökümü** görünüyor: kaç istek, hangi yanıt sınıfları, en çok istenen 10 adres ve en çok istek atan 10 IP. "Bu backend'e hiç trafik gitmemeli" dediğin bir yere kimin, nereye istek attığı tek bakışta çıkıyor.
- Log'da o backend'e hiç istek yoksa panel bunu açıkça yazıyor ve stats'taki toplamın HAProxy açıldığından beri biriktiğini hatırlatıyor; iki sayının neden farklı olduğu belli oluyor.
- Bellek sınırı: bir dakikada en fazla 100 backend için döküm ve backend başına en fazla 30 farklı IP tutuluyor. Sınır aşılsa da genel sayımlar eksiksiz kalıyor.

### Kurulum ve güncelleme

Sunucuda root olarak aşağıdaki komutlar yeterli. Betik önceki kurulumu görür ve üzerine yazar; adres, erişim listesi ve log ayarların korunur.

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -nv https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/haproxy-lens-linux-amd64.tar.gz https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/SHA256SUMS && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz ve sebebi ekrana yazılır. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).

## 0.9.0

- "En çok istek atan IP'ler" satırları artık açılıyor: bir IP'nin en çok hangi adresleri istediği görünüyor. Böylece bir IP'nin normal kullanıcı mı yoksa tarama botu mu olduğu anlaşılıyor (ör. tek IP'den ardı ardına `/wp-login.php` ve `/xmlrpc.php`).
- Liste 20 yerine 30 IP gösteriyor.
- Bellek için iki sınır var: bir dakikada en fazla 200 IP'nin adres dökümü tutuluyor ve IP başına en fazla 12 farklı adres saklanıp en yoğun 6 tanesi gösteriliyor. Sınır aşılırsa sayımlar yine eksiksiz kalıyor, yalnızca o IP'nin dökümü tutulmuyor ve panel bunu satırda yazıyor.
- Not: Cloudflare arkasındaki LB'lerde bu IP'ler Cloudflare'e aittir; özellik asıl olarak Cloudflare'in olmadığı ya da doğrudan gelen trafikte işe yarar.

### Kurulum ve güncelleme

Sunucuda root olarak aşağıdaki komutlar yeterli. Betik önceki kurulumu görür ve üzerine yazar; adres, erişim listesi ve log ayarların korunur.

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -nv https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/haproxy-lens-linux-amd64.tar.gz https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/SHA256SUMS && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz ve sebebi ekrana yazılır. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).

## 0.8.6

- "Saniyedeki istek" grafiği yanıltıcıydı: alanlar üst üste yığıldığı için en üstteki kırmızı çizgi 5xx değerini değil toplam isteği gösteriyordu, ipucu ise yalnızca tek tek değerleri yazıyordu. Artık ipucu her türün değerini, payını ve en altta **toplamı** gösteriyor; böylece çizginin neden orada olduğu belli oluyor.
- Yığılma görsel olarak da belirginleşti: bantlar daha dolu, çizgiler daha ince. Grafik açıklaması da ne anlama geldiğini yazıyor.
- Trafik grafiğinin açıklamasına, o grafikte alanların yığılmadığı, iki ölçümün üst üste çizildiği eklendi.
- 15 dakikadan uzun aralıklarda noktalar ortalanarak seyreltildiği için birkaç saniyelik tepe noktaları düşük görünebiliyor; grafik bunu artık açıkça yazıyor.

### Kurulum ve güncelleme

Sunucuda root olarak aşağıdaki komutlar yeterli. Betik önceki kurulumu görür ve üzerine yazar; adres, erişim listesi ve log ayarların korunur.

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -nv https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/haproxy-lens-linux-amd64.tar.gz https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/SHA256SUMS && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz ve sebebi ekrana yazılır. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).

## 0.8.5

- "En çok istenen adresler" tablosu yarım genişlikteki panele sığmıyordu, adres sütunu kesiliyordu. Yanıt sınıfı sütunları küçük punto ve dar boşlukla yeniden düzenlendi; tablonun en az genişlik kısıtı kaldırıldı.
- "Yapılandırma notları" başlığına tıklandığında odak çerçevesi alttaki ilk notun üstüne biniyordu; başlıkla içerik arasına boşluk eklendi.
- Örneklerdeki ağ adresleri belgeleme için ayrılmış aralıklarla değiştirildi (RFC 5737 ve özel ağlar).

### Kurulum ve güncelleme

Sunucuda root olarak aşağıdaki komutlar yeterli. Betik önceki kurulumu görür ve üzerine yazar; adres, erişim listesi ve log ayarların korunur.

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -nv https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/haproxy-lens-linux-amd64.tar.gz https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/SHA256SUMS && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz ve sebebi ekrana yazılır. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).

## 0.8.4

- Panelin kendi simgesi (favicon) eklendi: mercek ve içinden geçen üç akış çizgisi. Açık zeminli olduğu için hem koyu hem açık sekme çubuğunda okunuyor; 16x16'ya kadar test edildi. SVG ve PNG olarak programın içine gömülüyor, ayrı dosya gerekmiyor.
- Sekme başlığında artık sunucu adı yazıyor (`fbe-lb02 · haproxy-lens`); birden fazla LB'yi aynı anda açtığında hangisinin hangisi olduğu belli oluyor.
- "En çok istenen adresler" tablosuna 2xx sütunu eklendi; artık bir adresin yanıtlarının dört sınıfa dağılımı tek satırda görünüyor.
- README'ye marka notu: bu bağımsız bir proje, HAProxy Technologies ile ilgisi yok.

### Kurulum ve güncelleme

Sunucuda root olarak aşağıdaki komutlar yeterli. Betik önceki kurulumu görür ve üzerine yazar; adres, erişim listesi ve log ayarların korunur.

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -nv https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/haproxy-lens-linux-amd64.tar.gz https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/SHA256SUMS && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz ve sebebi ekrana yazılır. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).

## 0.8.3

- **Kurulum komutu düzeltildi.** İki sorunu vardı: `wget -q` indirme başarısız olduğunda hiçbir şey yazmadan çıkıyordu (komut sessizce bitiyor, klasör boş kalıyordu), ve süslü parantezli adres yazımı yalnızca bash'te açılıyordu. Artık `wget -nv` ile her dosya ve her hata ekrana yazılıyor, adresler açık yazıldığı için komut her kabukta çalışıyor.

### Kurulum ve güncelleme

Sunucuda root olarak aşağıdaki komutlar yeterli. Betik önceki kurulumu görür ve üzerine yazar; adres, erişim listesi ve log ayarların korunur.

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -nv https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/haproxy-lens-linux-amd64.tar.gz https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/SHA256SUMS && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz ve sebebi ekrana yazılır. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).

## 0.8.2

- Log bölümüne "Hangi yanıt kodu döndü" şeridi eklendi: 2xx, 3xx, 4xx ve 5xx sayıları, oranları ve dakikadaki hızı. Sunucu hataları artık özet düzeyde görünüyor; eskiden yalnızca alt panellere girince fark ediliyordu.
- Mevcut şeridin başlığı "İsteğe ne oldu" oldu ve altına ne anlattığı yazıldı: bu şerit isteğin nereye gittiğini gösterir, hangi kodu aldığını değil. Sunucu 500 döndürdüyse istek yine "Sunucu yanıtladı" sayılır.
- **Düzeltme:** "Hangi adres ne döndürüyor" sekmelerindeki 3xx/4xx/5xx sayıları, listelenen en yoğun 20 adresin toplamıydı; gerçek toplamdan düşük görünüyordu. Artık aralığın gerçek toplamını gösteriyor. Liste kırpıldığında bunu ayrıca not düşüyor.
- "En çok istenen adresler" tablosuna 3xx ve 4xx sütunları eklendi; 5xx sütunu sayıyı ve oranı birlikte gösteriyor.

### Kurulum ve güncelleme

Sunucuda root olarak aşağıdaki komutlar yeterli. Betik önceki kurulumu görür ve üzerine yazar; adres, erişim listesi ve log ayarların korunur.

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -nv https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/haproxy-lens-linux-amd64.tar.gz https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/SHA256SUMS && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz ve sebebi ekrana yazılır. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).

## 0.8.1

- Ajanın sürümü artık panelde, en üstte adın yanında yazıyor ve `/api/config` ile veriliyor. Kurulum da bittiğinde hangi sürümün çalıştığını söylüyor. Onlarca LB'de hangisinin güncel olduğu böylece görülebiliyor.
- README baştan sona gözden geçirildi:
  - Geliştirme bölümündeki dosya tablosu eksikti; `config.go`, `logformat.go`, `notes.go`, `watch.go` ve `cloudflare.go` yoktu.
  - "Bilinen sınırlar" içindeki alan adı maddesi eskiydi (sadece 403/503 diyordu, artık 3xx dahil tüm sınıflar için geçerli).
  - Servisin `systemd-journal` grubuyla da çalıştığı yazmıyordu.
  - `./install.sh --check` raporunun dört bölümü olduğu (config, log, yapılandırma notları, panel adresi) eksik anlatılmıştı.
  - `install.sh` başlığında `LOG=` seçeneği yazmıyordu.
- Yeni "Sorun giderme" bölümü: servis durumu, günlükler, panelin adresini bulma, erişim reddi, güvenlik duvarı ve ayar değiştirme.
- Kullanılmayan iki düzenli ifade `detect.go`'dan kaldırıldı (log biçimi ayrıştırıcıya geçtiğinde artıkları kalmıştı).

### Kurulum ve güncelleme

Sunucuda root olarak aşağıdaki komutlar yeterli. Betik önceki kurulumu görür ve üzerine yazar; adres, erişim listesi ve log ayarların korunur.

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -nv https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/haproxy-lens-linux-amd64.tar.gz https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/SHA256SUMS && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz ve sebebi ekrana yazılır. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).

## 0.8.0

- Log bölümündeki "Hata alan adresler" paneli "Hangi adres ne döndürüyor" oldu: **3xx, 4xx ve 5xx** için ayrı sekmeler. Her sekmede o sınıfı en çok döndüren adresler, tam kod dökümü (301, 302, 304, 403, 500, 502...) ve açılır ayrıntı var.
- Yönlendirmeler artık görünüyor. HAProxy'nin kendi ürettiği http→https atlamaları hiçbir adres listesine girmiyordu; şimdi "HAProxy yönlendirdi" etiketiyle listeleniyorlar. Bir backend'in 3xx oranı yüksekse hangi adresten geldiği doğrudan görülebiliyor.
- Her satırın yanında türü yazıyor: HAProxy yönlendirdi, engellendi, backend eşleşmedi, çalışan sunucu yok.
- Sekmelerin üstünde seçili aralıktaki 3xx/4xx/5xx toplamları duruyor.
- "En çok istenen adresler" artık sadece sunucuya ulaşanları değil, yönlendirilen ve engellenen istekleri de içeriyor.
- Bir sınıfın çok sayıda satırı, diğer sınıfın az sayıdaki satırını listeden düşürmüyor: her sınıfın kendi en yoğun 20 adresi ayrı seçiliyor.

### Kurulum ve güncelleme

Sunucuda root olarak aşağıdaki komutlar yeterli. Betik önceki kurulumu görür ve üzerine yazar; adres, erişim listesi ve log ayarların korunur.

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -nv https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/haproxy-lens-linux-amd64.tar.gz https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/SHA256SUMS && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz ve sebebi ekrana yazılır. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).

## 0.7.2

- Bir backend'i ya da log satırını açıkken sayfa kendiliğinden kayıyordu: listeler her yenilemede yeniden sıralanıyor, açtığın satır başka yere gidiyordu. Artık bir satır açıkken sıra donuyor, yeni gelenler sona ekleniyor.
- Sıralama ölçütü de kararlı hâle geldi: backend listesi ve "En çok istek alan backend'ler" artık saniyelik değere değil, seçili aralıktaki istek sayısına göre sıralanıyor. Böylece hiçbir satır açık olmasa da sıra kendiliğinden oynamıyor.
- Durum özetindeki bağlantı hatası bulgusu anlık ölçüme bakıyordu; her yenilemede görünüp kaybolarak altındaki her şeyi oynatıyordu. Artık seçili aralığın toplamına bakıyor.
- Panel açıklamaları sağda boş yer varken alt satıra geçiyordu (60 karakter sınırı kaldırıldı); artık panelin tamamını kullanıyorlar.

### Kurulum ve güncelleme

Sunucuda root olarak aşağıdaki komutlar yeterli. Betik önceki kurulumu görür ve üzerine yazar; adres, erişim listesi ve log ayarların korunur.

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -nv https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/haproxy-lens-linux-amd64.tar.gz https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/SHA256SUMS && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz ve sebebi ekrana yazılır. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).

## 0.7.1

- Arayüzde baştan sona düzen denetimi yapıldı ve şunlar düzeltildi:
  - Uzun yollar ve alan adları kutudan taşıyordu (hata ayrıntısında, notlarda, tablolarda ve "tüm alanlar" penceresinde). Artık gerektiğinde satır sonunda kırılıyorlar.
  - Sayı ile birimi ayrı satırlara düşebiliyordu ("218" / "ms"). Biçimlendiriciler artık bölünmeyen boşluk kullanıyor; bu hem tabloda hem cümle içinde geçerli.
  - Durum özetindeki bulgular 900 piksele sıkışıyordu, altındaki "Yapılandırma notları" tam genişlikteydi; ikisi artık aynı genişlikte, aynı iç boşlukta ve aynı çerçevede.
  - Backend ayrıntısındaki 4xx ve 5xx kutuları aynı yönlendirme cümlesini iki kez yazıyordu; artık bir kez yazılıyor ve hatasız türler tek satırda toplanıyor.
  - Terim ipucu balonu sayfanın altında ekran dışına taşıyordu; yer yoksa yukarı açılıyor, dar ekranda daralıyor.
  - Log kaynaklı bulgular "Son 60 dakikada" derken panelin geri kalanı "son 1 saat" diyordu; ifade birleştirildi.
  - Kısaltılan backend adlarının tam hâli artık tooltip olarak görünüyor.

### Kurulum ve güncelleme

Sunucuda root olarak aşağıdaki komutlar yeterli. Betik önceki kurulumu görür ve üzerine yazar; adres, erişim listesi ve log ayarların korunur.

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -nv https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/haproxy-lens-linux-amd64.tar.gz https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/SHA256SUMS && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz ve sebebi ekrana yazılır. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).

## 0.7.0

- Ajan artık her ortama kendini uyduruyor ve çalışırken izliyor: HAProxy config'i ya da süreçleri değişince (reload) en geç 30 saniyede yeniden okuyor; yeniden kurulum gerekmiyor.
- Log biçimi config'ten okunuyor: `option httplog`, `httpslog`, `tcplog` ve özel `log-format` (JSON benzeri biçimler dahil) destekleniyor. `defaults` mirası, adlı `defaults` ve `from` dikkate alınıyor. Özel biçimli sunucularda log analizi artık kapanmıyor.
- Host yakalama slotu config'ten biliniyor; aynı bloktaki başka yakalamalar (User-Agent, X-Forwarded-For) alan adıyla karışmıyor.
- Yeni "Yapılandırma notları" bölümü: log kapalı, seviye filtresi, `dontlog-normal`, eksik log alanları, alan adı yakalanmıyor, sağlık kontrolü yok, okunamayan log satırları (örnekleriyle), stats'ta olup log'da olmayan trafik. Her notta etkisi, eklenebilecek config satırı ve yeri yazıyor. Aynı notlar `./install.sh --check` raporunda da çıkıyor.
- Log kaynağı varsayılan olarak ajan tarafından bulunuyor ve izleniyor; kaynak susarsa yenisi aranıyor, bulunana kadar 5 saniyede bir deneniyor.
- Stats socket bir dakika çalışmazsa config'teki başka bir socket deneniyor.
- Servis, log'u ileride başka bir yerden okuyabilsin diye `adm` ve `systemd-journal` gruplarıyla (varsa) kuruluyor.

### Kurulum ve güncelleme

Sunucuda root olarak aşağıdaki komutlar yeterli. Betik önceki kurulumu görür ve üzerine yazar; adres, erişim listesi ve log ayarların korunur.

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -nv https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/haproxy-lens-linux-amd64.tar.gz https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/SHA256SUMS && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz ve sebebi ekrana yazılır. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).

## 0.6.0

- "Engellenen ve karşılıksız kalan istekler" ve "Hata alan adresler" satırları tıklanınca açılıyor: tam adres, gerçek yollar ({id} ile birleşmiş satırlarda) ve isteği gönderen IP'ler (Cloudflare etiketli).
- Alan adı log'da varsa kendiliğinden bulunuyor: yakalanan Host başlığı, HTTP/2 tam adres, `option httpslog` SNI ya da log-format'ın sonuna eklenmiş host. Yoksa ayrıntıda yol ve IP gösteriliyor; config'e dokunulmuyor.
- Log bölümü ve `./install.sh --check` raporu, o LB'nin log'unda alan adının olup olmadığını (ya da satırların ne kadarında olduğunu) yazıyor.
- Ayrıştırıcı istek satırından sonra ek alanı olan satırları da okuyor (`option httpslog` gibi).
- Ayrıntılar için bellek sınırı: dakikada en fazla 300 satırın ayrıntısı tutuluyor, sayılar yine eksiksiz.

### Kurulum ve güncelleme

Sunucuda root olarak aşağıdaki komutlar yeterli. Betik önceki kurulumu görür ve üzerine yazar; adres, erişim listesi ve log ayarların korunur.

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -nv https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/haproxy-lens-linux-amd64.tar.gz https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/SHA256SUMS && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz ve sebebi ekrana yazılır. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).

## 0.5.0

- Backend ayrıntısında 5xx özetinin yanına 4xx (istemci hatası) özeti eklendi. 4xx'in genelde istemci kaynaklı olduğu (404, 401/403, 429) not ediliyor.
- Sunucu tablosuna 4xx sütunu eklendi (5xx'in yanına). Her ikisi de seçili zaman aralığına göre.
- Log bölümüne "Hata alan adresler" paneli: sunucuya ulaşıp 4xx/5xx dönen path'ler, en çok hata alan üstte. Her path'in yanında tam kod dökümü (ör. 502×88, 503×57) ve kısa açıklaması.
- Ajan artık log'da path başına tam hata kodlarını (4xx/5xx) sayıyor.

### Kurulum ve güncelleme

Sunucuda root olarak aşağıdaki komutlar yeterli. Betik önceki kurulumu görür ve üzerine yazar; adres, erişim listesi ve log ayarların korunur.

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -nv https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/haproxy-lens-linux-amd64.tar.gz https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/SHA256SUMS && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz ve sebebi ekrana yazılır. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).

## 0.4.0

- Zaman aralığı seçici (5 dk, 15 dk, 1 saat; varsayılan 1 saat). Grafikler, 5xx oranları, yanıt türleri ve log bölümü bu aralığı kullanıyor.
- Sunucu tablosuna "5xx" sütunu: seçili aralıkta her sunucudan dönen 5xx sayısı ve sunucunun kendi yanıtları içindeki oranı. En çok hata dönen sunucu vurgulanıyor.
- Backend ayrıntısında 5xx özeti: en çok hangi sunucudan döndüğü, hata oranları sunucular arasında benzerse ortak bir soruna işaret ettiği, HAProxy'nin kendisinin ürettiği 5xx'ler.
- Durum özetindeki 5xx uyarısı artık seçili aralığa göre hesaplanıyor (2 saniyelik ölçümün oynaklığı yok) ve en çok hata dönen sunucuyu yazıyor.
- Ajan her 10 saniyede satır bazında sayaç örneği tutuyor (son 1 saat); grafik verisi en fazla 360 noktaya seyreltilerek gönderiliyor.

### Kurulum ve güncelleme

Sunucuda root olarak aşağıdaki komutlar yeterli. Betik önceki kurulumu görür ve üzerine yazar; adres, erişim listesi ve log ayarların korunur.

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -nv https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/haproxy-lens-linux-amd64.tar.gz https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/SHA256SUMS && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz ve sebebi ekrana yazılır. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).

## 0.3.0

- Panel artık sunucunun kendi iç IP'sinde açılıyor. Adres, varsayılan rotanın geçtiği arayüzden seçiliyor; keepalived VIP'leri atlanıyor, ağ ayarlarında (netplan, ifupdown, ifcfg, NetworkManager, systemd-networkd) sabit tanımlı adres tercih ediliyor.
- Herkese açık bir IP seçilmiyor; bu durumda panel 127.0.0.1'de kalıyor.
- Panel sadece izin verilen ağlardan gelen isteklere cevap veriyor (varsayılan: özel ağlar). `ALLOW=ağ/önek ./install.sh` ile daraltılabiliyor; güncellemede önceki liste korunuyor.
- `LISTEN=IP ./install.sh` ile adres elle verilebiliyor.
- `./install.sh --check` raporuna "Panel adresi" bölümü eklendi.
- Servis ağ hazır olduktan sonra başlıyor (`network-online.target`).

### Kurulum ve güncelleme

Sunucuda root olarak aşağıdaki komutlar yeterli. Betik önceki kurulumu görür ve üzerine yazar; adres, erişim listesi ve log ayarların korunur.

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -nv https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/haproxy-lens-linux-amd64.tar.gz https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/SHA256SUMS && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz ve sebebi ekrana yazılır. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).

## 0.2.0 (2026-09-10)

- Kurulum her sunucuda çalışan HAProxy'nin config'ini sadece okuyarak stats socket'ini ve log'u kendisi buluyor ve dener.
- `./install.sh --check`: hiçbir şey kurmadan uyumluluk raporu.
- TCP (`ipv4@`, `host:port`) ve soyut (`abns@`) socket desteği, `conf.d` gibi config klasörleri.
- Log kaynağı olarak journald desteği.
- Özel `log-format` görülürse log analizi kendiliğinden kapanıyor; stats paneli çalışmaya devam ediyor.
- Kurulum ve kaldırma sonunda config sha256 özeti ve HAProxy süreç numarası doğrulaması.
- Cloudflare IP aralıkları programın içinde; ayrı bir liste dosyası gerekmiyor.

### Kurulum ve güncelleme

Sunucuda root olarak aşağıdaki komutlar yeterli. Betik önceki kurulumu görür ve üzerine yazar; adres, erişim listesi ve log ayarların korunur.

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -nv https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/haproxy-lens-linux-amd64.tar.gz https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/SHA256SUMS && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz ve sebebi ekrana yazılır. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).

## 0.1.0 (2026-09-10)

- İlk sürüm: stats paneli, log analizi, systemd servisi, kurulum ve kaldırma betikleri.

### Kurulum ve güncelleme

Sunucuda root olarak aşağıdaki komutlar yeterli. Betik önceki kurulumu görür ve üzerine yazar; adres, erişim listesi ve log ayarların korunur.

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -nv https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/haproxy-lens-linux-amd64.tar.gz https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/SHA256SUMS && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz ve sebebi ekrana yazılır. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).
