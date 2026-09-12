# Değişiklikler

Her sürümün altında, o sürüme geçmek için sunucuda çalıştırılacak komutlar da var.
GitHub'da release yayınlarken bu dosyadaki ilgili sürüm bölümünün tamamını (en üstteki
sürüm numarası satırı hariç) açıklama kutusuna yapıştırmak yeterli.

## 0.8.0

- Log bölümündeki "Hata alan adresler" paneli "Hangi adres ne döndürüyor" oldu: **3xx, 4xx ve 5xx** için ayrı sekmeler. Her sekmede o sınıfı en çok döndüren adresler, tam kod dökümü (301, 302, 304, 403, 500, 502...) ve açılır ayrıntı var.
- Yönlendirmeler artık görünüyor. HAProxy'nin kendi ürettiği http→https atlamaları hiçbir adres listesine girmiyordu; şimdi "HAProxy yönlendirdi" etiketiyle listeleniyorlar. Bir backend'in 3xx oranı yüksekse hangi adresten geldiği doğrudan görülebiliyor.
- Her satırın yanında türü yazıyor: HAProxy yönlendirdi, engellendi, backend eşleşmedi, çalışan sunucu yok.
- Sekmelerin üstünde seçili aralıktaki 3xx/4xx/5xx toplamları duruyor.
- "En çok istenen adresler" artık sadece sunucuya ulaşanları değil, yönlendirilen ve engellenen istekleri de içeriyor.
- Bir sınıfın çok sayıda satırı, diğer sınıfın az sayıdaki satırını listeden düşürmüyor: her sınıfın kendi en yoğun 20 adresi ayrı seçiliyor.

### Kurulum ve güncelleme

Sunucuda root olarak aşağıdaki komutlar yeterli. Betik önceki kurulumu görür ve üzerine yazar; adres, erişim listesi ve log ayarların korunur.

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -q https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/{haproxy-lens-linux-amd64.tar.gz,SHA256SUMS} && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).

## 0.7.2

- Bir backend'i ya da log satırını açıkken sayfa kendiliğinden kayıyordu: listeler her yenilemede yeniden sıralanıyor, açtığın satır başka yere gidiyordu. Artık bir satır açıkken sıra donuyor, yeni gelenler sona ekleniyor.
- Sıralama ölçütü de kararlı hâle geldi: backend listesi ve "En çok istek alan backend'ler" artık saniyelik değere değil, seçili aralıktaki istek sayısına göre sıralanıyor. Böylece hiçbir satır açık olmasa da sıra kendiliğinden oynamıyor.
- Durum özetindeki bağlantı hatası bulgusu anlık ölçüme bakıyordu; her yenilemede görünüp kaybolarak altındaki her şeyi oynatıyordu. Artık seçili aralığın toplamına bakıyor.
- Panel açıklamaları sağda boş yer varken alt satıra geçiyordu (60 karakter sınırı kaldırıldı); artık panelin tamamını kullanıyorlar.

### Kurulum ve güncelleme

Sunucuda root olarak aşağıdaki komutlar yeterli. Betik önceki kurulumu görür ve üzerine yazar; adres, erişim listesi ve log ayarların korunur.

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -q https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/{haproxy-lens-linux-amd64.tar.gz,SHA256SUMS} && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).

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

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -q https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/{haproxy-lens-linux-amd64.tar.gz,SHA256SUMS} && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).

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

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -q https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/{haproxy-lens-linux-amd64.tar.gz,SHA256SUMS} && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).

## 0.6.0

- "Engellenen ve karşılıksız kalan istekler" ve "Hata alan adresler" satırları tıklanınca açılıyor: tam adres, gerçek yollar ({id} ile birleşmiş satırlarda) ve isteği gönderen IP'ler (Cloudflare etiketli).
- Alan adı log'da varsa kendiliğinden bulunuyor: yakalanan Host başlığı, HTTP/2 tam adres, `option httpslog` SNI ya da log-format'ın sonuna eklenmiş host. Yoksa ayrıntıda yol ve IP gösteriliyor; config'e dokunulmuyor.
- Log bölümü ve `./install.sh --check` raporu, o LB'nin log'unda alan adının olup olmadığını (ya da satırların ne kadarında olduğunu) yazıyor.
- Ayrıştırıcı istek satırından sonra ek alanı olan satırları da okuyor (`option httpslog` gibi).
- Ayrıntılar için bellek sınırı: dakikada en fazla 300 satırın ayrıntısı tutuluyor, sayılar yine eksiksiz.

### Kurulum ve güncelleme

Sunucuda root olarak aşağıdaki komutlar yeterli. Betik önceki kurulumu görür ve üzerine yazar; adres, erişim listesi ve log ayarların korunur.

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -q https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/{haproxy-lens-linux-amd64.tar.gz,SHA256SUMS} && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).

## 0.5.0

- Backend ayrıntısında 5xx özetinin yanına 4xx (istemci hatası) özeti eklendi. 4xx'in genelde istemci kaynaklı olduğu (404, 401/403, 429) not ediliyor.
- Sunucu tablosuna 4xx sütunu eklendi (5xx'in yanına). Her ikisi de seçili zaman aralığına göre.
- Log bölümüne "Hata alan adresler" paneli: sunucuya ulaşıp 4xx/5xx dönen path'ler, en çok hata alan üstte. Her path'in yanında tam kod dökümü (ör. 502×88, 503×57) ve kısa açıklaması.
- Ajan artık log'da path başına tam hata kodlarını (4xx/5xx) sayıyor.

### Kurulum ve güncelleme

Sunucuda root olarak aşağıdaki komutlar yeterli. Betik önceki kurulumu görür ve üzerine yazar; adres, erişim listesi ve log ayarların korunur.

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -q https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/{haproxy-lens-linux-amd64.tar.gz,SHA256SUMS} && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).

## 0.4.0

- Zaman aralığı seçici (5 dk, 15 dk, 1 saat; varsayılan 1 saat). Grafikler, 5xx oranları, yanıt türleri ve log bölümü bu aralığı kullanıyor.
- Sunucu tablosuna "5xx" sütunu: seçili aralıkta her sunucudan dönen 5xx sayısı ve sunucunun kendi yanıtları içindeki oranı. En çok hata dönen sunucu vurgulanıyor.
- Backend ayrıntısında 5xx özeti: en çok hangi sunucudan döndüğü, hata oranları sunucular arasında benzerse ortak bir soruna işaret ettiği, HAProxy'nin kendisinin ürettiği 5xx'ler.
- Durum özetindeki 5xx uyarısı artık seçili aralığa göre hesaplanıyor (2 saniyelik ölçümün oynaklığı yok) ve en çok hata dönen sunucuyu yazıyor.
- Ajan her 10 saniyede satır bazında sayaç örneği tutuyor (son 1 saat); grafik verisi en fazla 360 noktaya seyreltilerek gönderiliyor.

### Kurulum ve güncelleme

Sunucuda root olarak aşağıdaki komutlar yeterli. Betik önceki kurulumu görür ve üzerine yazar; adres, erişim listesi ve log ayarların korunur.

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -q https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/{haproxy-lens-linux-amd64.tar.gz,SHA256SUMS} && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).

## 0.3.0

- Panel artık sunucunun kendi iç IP'sinde açılıyor. Adres, varsayılan rotanın geçtiği arayüzden seçiliyor; keepalived VIP'leri atlanıyor, ağ ayarlarında (netplan, ifupdown, ifcfg, NetworkManager, systemd-networkd) sabit tanımlı adres tercih ediliyor.
- Herkese açık bir IP seçilmiyor; bu durumda panel 127.0.0.1'de kalıyor.
- Panel sadece izin verilen ağlardan gelen isteklere cevap veriyor (varsayılan: özel ağlar). `ALLOW=ağ/önek ./install.sh` ile daraltılabiliyor; güncellemede önceki liste korunuyor.
- `LISTEN=IP ./install.sh` ile adres elle verilebiliyor.
- `./install.sh --check` raporuna "Panel adresi" bölümü eklendi.
- Servis ağ hazır olduktan sonra başlıyor (`network-online.target`).

### Kurulum ve güncelleme

Sunucuda root olarak aşağıdaki komutlar yeterli. Betik önceki kurulumu görür ve üzerine yazar; adres, erişim listesi ve log ayarların korunur.

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -q https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/{haproxy-lens-linux-amd64.tar.gz,SHA256SUMS} && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).

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

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -q https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/{haproxy-lens-linux-amd64.tar.gz,SHA256SUMS} && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).

## 0.1.0 (2026-09-10)

- İlk sürüm: stats paneli, log analizi, systemd servisi, kurulum ve kaldırma betikleri.

### Kurulum ve güncelleme

Sunucuda root olarak aşağıdaki komutlar yeterli. Betik önceki kurulumu görür ve üzerine yazar; adres, erişim listesi ve log ayarların korunur.

    cd /root && rm -rf lens && mkdir lens && cd lens && wget -q https://github.com/Onurbolatogluu/haproxy-lens/releases/latest/download/{haproxy-lens-linux-amd64.tar.gz,SHA256SUMS} && sha256sum -c --ignore-missing SHA256SUMS && tar xzf haproxy-lens-linux-amd64.tar.gz && cd haproxy-lens && ./install.sh

Tek satır: temiz bir klasöre indirir, doğrular, açar ve kurar; bir adım hata verirse sonrakiler çalışmaz. ARM sunucularda `amd64` yerine `arm64` yazın. Kurmadan önce sadece kontrol etmek için satırın sonundaki `./install.sh` yerine `./install.sh --check`, ayrıntılar için [README](https://github.com/Onurbolatogluu/haproxy-lens#readme).
