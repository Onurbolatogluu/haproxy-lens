import { useState, useEffect, useRef, useMemo, createContext, useContext } from "react";
import { AreaChart, Area, XAxis, YAxis, Tooltip, ResponsiveContainer, CartesianGrid } from "recharts";
import { ChevronDown, ChevronRight, Info, X, Pause, Play } from "lucide-react";

/* haproxy-lens paneli — ajan API'sinden (/api/state, /api/logs) beslenir. */

const C = {
  bg: "#14202B", panel: "#1A2835", panel2: "#223344", line: "#2B3F52",
  text: "#E6EDF3", muted: "#93A6B8", faint: "#62788C",
  ok: "#4FB39A", warn: "#E8B04A", bad: "#E5625E", info: "#6FA8DC", maint: "#A78BDB",
};
const FONT = "'IBM Plex Sans', system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif";
const TICK_SEC = 2;

const CSS = `
.hl-root{font-family:${FONT}}
.tnum{font-variant-numeric:tabular-nums}
/* Yol, alan adı gibi boşluksuz uzun metinler kutudan taşmasın (sadece gerektiğinde kırılır) */
.brk{overflow-wrap:anywhere;min-width:0}
/* Sayı + birim ("218 ms", "1,5 sn") alt satıra bölünmesin */
.nw{white-space:nowrap}
.hl-root button:focus-visible{outline:2px solid ${C.info};outline-offset:2px}
.hl-row:hover{background:${C.panel2}}
.be-row{display:flex;flex-wrap:wrap;align-items:center;gap:8px 20px}
.be-head{display:none}
.be-mlabel{color:${C.faint};margin-right:6px;font-size:12px}
@media (min-width:768px){
  .be-row,.be-head{display:grid;grid-template-columns:minmax(0,1.5fr) minmax(0,1fr) repeat(4,minmax(0,.65fr));gap:16px;align-items:center}
  .be-mlabel{display:none}
}
`;

const GLOSSARY = {
  pxname: ["Proxy adı", "Frontend, backend veya listen bölümünün adı."],
  svname: ["Satır adı", "FRONTEND, BACKEND ya da sunucunun adı."],
  qcur: ["Kuyrukta bekleyen", "Sunucular dolu olduğu için sırada bekleyen istek. Normalde 0 olmalı."],
  qmax: ["En uzun kuyruk", "HAProxy açıldığından beri görülen en uzun kuyruk."],
  scur: ["Açık bağlantı", "Şu anda açık olan bağlantı sayısı."],
  smax: ["En yüksek eşzamanlı bağlantı", "Açıldığından beri aynı anda görülen en fazla bağlantı."],
  slim: ["Bağlantı sınırı", "Aynı anda izin verilen en fazla bağlantı (maxconn)."],
  stot: ["Toplam bağlantı", "Açıldığından beri kurulan toplam bağlantı."],
  bin: ["Gelen veri", "İstemcilerden alınan toplam veri."],
  bout: ["Giden veri", "İstemcilere gönderilen toplam veri."],
  dreq: ["Engellenen istek", "Erişim kuralı (ACL, deny) yüzünden reddedilen istek."],
  dresp: ["Engellenen yanıt", "Kural yüzünden istemciye gönderilmeyen sunucu yanıtı."],
  ereq: ["Hatalı istek", "İstemciden bozuk, yarım ya da geçersiz gelen istek."],
  econ: ["Bağlanamama", "Sunucuya bağlantı kurulamayan deneme sayısı."],
  eresp: ["Yanıt hatası", "Sunucu yanıtı bozuk geldi ya da yarıda kesildi."],
  wretr: ["Tekrar deneme", "Bağlantı kurulamayınca aynı sunucuya yeniden denendi."],
  wredis: ["Başka sunucuya aktarma", "İstek, sorunlu sunucu yerine başka bir sunucuya gönderildi."],
  status: ["Durum", "Çalışıyor, çalışmıyor, bakımda, boşaltılıyor gibi anlık durum."],
  weight: ["Ağırlık", "Yük dağıtımındaki payı. Büyük olan daha çok istek alır."],
  act: ["Aktif sunucu", "Sunucu satırında 1 ise ana sunucudur; backend satırında çalışan ana sunucu sayısıdır."],
  bck: ["Yedek sunucu", "Sunucu satırında 1 ise yedektir; ana sunucular düşünce devreye girer."],
  chkfail: ["Başarısız kontrol", "Toplam başarısız sağlık kontrolü sayısı."],
  chkdown: ["Düşme sayısı", "Çalışır durumdan kapalıya kaç kez geçtiği."],
  lastchg: ["Son durum değişimi", "Durumun en son ne kadar önce değiştiği."],
  downtime: ["Toplam kapalı kalma", "Açıldığından beri toplam erişilemez kaldığı süre."],
  qlimit: ["Kuyruk sınırı", "Sunucu kuyruğunda bekleyebilecek en fazla istek."],
  pid: ["Süreç no", "Bu veriyi veren HAProxy sürecinin numarası."],
  iid: ["Proxy no", "Proxy'nin iç kimlik numarası."],
  sid: ["Sunucu no", "Sunucunun iç kimlik numarası."],
  throttle: ["Yavaş başlatma", "Sunucu yeni açıldıysa yükün yüzde kaçını aldığı (slowstart)."],
  lbtot: ["Seçilme sayısı", "Yük dengeleyicinin bu sunucuyu kaç kez seçtiği."],
  tracked: ["Takip edilen", "Sağlığını başka bir sunucudan takip ediyorsa onun kimliği."],
  type: ["Satır türü", "0 frontend, 1 backend, 2 sunucu, 3 dinleyici."],
  rate: ["Yeni bağlantı/sn", "Son 1 saniyede açılan yeni bağlantı."],
  rate_lim: ["Bağlantı hızı sınırı", "Saniyede izin verilen en fazla yeni bağlantı."],
  rate_max: ["En yüksek bağlantı/sn", "Açıldığından beri görülen en yüksek saniyelik yeni bağlantı."],
  check_status: ["Sağlık kontrolü", "Son sağlık kontrolünün sonucu."],
  check_code: ["Kontrol yanıt kodu", "Sağlık kontrolünde sunucunun döndüğü kod (ör. HTTP 200)."],
  check_duration: ["Kontrol süresi", "Son sağlık kontrolünün sürdüğü süre."],
  hrsp_1xx: ["1xx yanıt", "Bilgi yanıtları."],
  hrsp_2xx: ["2xx yanıt", "Başarılı yanıtlar (200, 201, 204...)."],
  hrsp_3xx: ["3xx yanıt", "Yönlendirme ve önbellek yanıtları (301, 302, 304...)."],
  hrsp_4xx: ["4xx yanıt", "İstemci hataları (404, 403, 401, 429...)."],
  hrsp_5xx: ["5xx yanıt", "Sunucu hataları (500, 502, 503, 504...)."],
  hrsp_other: ["Diğer yanıt", "Yukarıdaki gruplara girmeyen yanıtlar."],
  hanafail: ["Ciddi hata", "Sunucuda gözlenen ciddi hata sayısı (observe)."],
  req_rate: ["İstek/sn", "Son 1 saniyede gelen HTTP isteği."],
  req_rate_max: ["En yüksek istek/sn", "Açıldığından beri görülen en yüksek saniyelik istek."],
  req_tot: ["Toplam istek", "Açıldığından beri işlenen HTTP isteği."],
  cli_abrt: ["İstemci vazgeçti", "İstemci, yanıt gelmeden bağlantıyı kapattı."],
  srv_abrt: ["Sunucu kesti", "Sunucu, işlem bitmeden bağlantıyı kapattı."],
  comp_in: ["Sıkıştırmaya giren", "Sıkıştırıcıya giren veri."],
  comp_out: ["Sıkıştırmadan çıkan", "Sıkıştırma sonrası veri."],
  comp_byp: ["Sıkıştırılmadan geçen", "Sıkıştırılmadan geçirilen veri."],
  comp_rsp: ["Sıkıştırılan yanıt", "Sıkıştırılarak gönderilen yanıt sayısı."],
  lastsess: ["Son bağlantı", "En son ne kadar önce bağlantı aldığı. -1 ise hiç almadı."],
  last_chk: ["Son kontrol mesajı", "Son sağlık kontrolünün ayrıntılı mesajı."],
  last_agt: ["Son ajan mesajı", "Agent-check kullanılıyorsa son mesaj."],
  qtime: ["Kuyrukta bekleme", "İsteğin kuyrukta ortalama bekleme süresi (son 1024 istek)."],
  ctime: ["Bağlanma süresi", "Sunucuya bağlanmanın ortalama süresi (son 1024 istek)."],
  rtime: ["Sunucu yanıt süresi", "Sunucunun yanıtı hazırlama süresi ortalaması (son 1024 istek)."],
  ttime: ["Toplam süre", "Bir isteğin baştan sona ortalama süresi (son 1024 istek)."],
  agent_status: ["Ajan durumu", "Agent-check sonucu."],
  mode: ["Çalışma modu", "http ya da tcp."],
  algo: ["Dağıtım yöntemi", "İsteklerin sunuculara nasıl paylaştırıldığı."],
  addr: ["Adres", "Sunucunun IP ve portu."],
  conn_rate: ["Bağlantı/sn", "Son 1 saniyede kabul edilen bağlantı."],
  conn_tot: ["Toplam kabul edilen bağlantı", "Açıldığından beri kabul edilen bağlantı."],
  intercepted: ["HAProxy'nin yanıtladığı", "Sunucuya gitmeden HAProxy'nin kendisinin yanıtladığı istek (stats, redirect...)."],
  dcon: ["Bağlantıda engellenen", "tcp-request connection kuralıyla reddedilen bağlantı."],
  dses: ["Oturumda engellenen", "tcp-request session kuralıyla reddedilen oturum."],
  wrew: ["Yeniden yazma hatası", "Başlık yeniden yazma kuralının başarısız olduğu sayı."],
  connect: ["Yeni sunucu bağlantısı", "Sunucuya açılan yeni bağlantı."],
  reuse: ["Yeniden kullanılan bağlantı", "Açık bir sunucu bağlantısının tekrar kullanıldığı sayı."],
  cache_lookups: ["Önbellek sorgusu", "Önbelleğe bakılan istek sayısı."],
  cache_hits: ["Önbellek isabeti", "Önbellekten verilen yanıt sayısı."],
  qtime_max: ["En uzun kuyruk süresi", "Görülen en uzun kuyrukta bekleme."],
  ctime_max: ["En uzun bağlanma", "Görülen en uzun bağlanma süresi."],
  rtime_max: ["En uzun yanıt", "Görülen en uzun sunucu yanıt süresi."],
  ttime_max: ["En uzun toplam süre", "Görülen en uzun toplam istek süresi."],
  eint: ["İç hata", "HAProxy içinde oluşan hata sayısı."],
  idle_conn_cur: ["Boştaki bağlantı", "Yeniden kullanılmayı bekleyen boştaki sunucu bağlantısı."],
  used_conn_cur: ["Kullanımdaki bağlantı", "Şu an kullanımda olan sunucu bağlantısı."],
  uweight: ["Yapılandırmadaki ağırlık", "haproxy.cfg'de verilen ağırlık."],
  _reqps: ["İstek/sn", "Tüm frontend'lere saniyede gelen istek.", "Hesaplanan: iki ölçüm arasındaki req_tot farkı"],
  _err5: ["Sunucu hatası oranı", "Seçili zaman aralığında yanıtların yüzde kaçının 5xx (sunucu hatası) olduğu.", "Hesaplanan: hrsp_5xx farkı / tüm yanıtların farkı"],
  _srv4xx: ["Sunucunun 4xx'leri", "Seçili zaman aralığında bu sunucudan dönen istemci hatası (4xx) sayısı; altında bu sunucunun kendi yanıtları içindeki oranı.", "Hesaplanan: sunucu satırının hrsp_4xx farkı"],
  _srv5xx: ["Sunucunun 5xx'leri", "Seçili zaman aralığında bu sunucudan dönen sunucu hatası (5xx) sayısı; altında bu sunucunun kendi yanıtları içindeki oranı. En çok hata dönen sunucu kırmızı görünür.", "Hesaplanan: sunucu satırının hrsp_5xx farkı"],
  _traffic: ["Trafik", "Saniyede gelen ve giden veri.", "Hesaplanan: bin ve bout farkı"],
  _healthy: ["Sağlıklı sunucu", "Sorunsuz çalışan sunucuların tüm sunuculara oranı.", "Hesaplanan: status alanından"],
  _uptime: ["Çalışma süresi", "HAProxy'nin son yeniden başlatmadan beri açık kaldığı süre.", "show info: Uptime_sec"],
};

const CHECK = {
  UNK: "Bilinmiyor", INI: "Kontrol başlıyor", SOCKERR: "Soket hatası (HAProxy tarafında)",
  L4OK: "Port açık, bağlantı kuruldu", L4TOUT: "Bağlantı zaman aşımı: sunucu hiç yanıt vermiyor",
  L4CON: "Bağlantı reddedildi: port kapalı ya da servis çalışmıyor",
  L6OK: "SSL bağlantısı başarılı", L6TOUT: "SSL el sıkışması zaman aşımı", L6RSP: "SSL el sıkışması hatalı",
  L7OK: "HTTP kontrolü başarılı", L7OKC: "HTTP kontrolü şartlı başarılı",
  L7TOUT: "HTTP kontrolü zaman aşımı: sunucu geç yanıt veriyor", L7RSP: "Sunucudan anlamsız yanıt geldi",
  L7STS: "Sunucu beklenmeyen HTTP kodu döndü", PROCERR: "Harici kontrol betiği hata verdi",
  PROCTOUT: "Harici kontrol betiği zaman aşımı", PROCOK: "Harici kontrol başarılı",
};

const ALGO = {
  roundrobin: "Sırayla, ağırlığa göre", "static-rr": "Sırayla, sabit ağırlık", leastconn: "En az bağlantısı olana",
  first: "İlk boş sunucuya", source: "İstemci IP'sine göre hep aynı sunucuya", uri: "Adrese göre hep aynı sunucuya",
  url_param: "URL parametresine göre", hdr: "Başlık değerine göre", random: "Rastgele",
  rdp_cookie: "RDP çerezine göre", hash: "Özel hash'e göre",
};

const STATUS_LABEL = {
  up: "Çalışıyor", failing: "Hata veriyor", recovering: "Toparlanıyor", down: "Çalışmıyor",
  maint: "Bakımda", drain: "Boşaltılıyor", nocheck: "Kontrolsüz", full: "Dolu", none: "Bilinmiyor",
};
const KIND_COLOR = {
  up: C.ok, failing: C.warn, recovering: C.warn, down: C.bad, maint: C.maint,
  drain: C.info, nocheck: C.faint, full: C.bad, none: C.faint,
};
const LEVEL_COLOR = { bad: C.bad, warn: C.warn, info: C.info };
const LEVEL_ORDER = { bad: 0, warn: 1, info: 2 };
// "nocheck": sağlık kontrolü yok, durumu bilinmiyor. Sağlıklı sayılmaz ama trafik alır.
const HEALTHY = new Set(["up"]);
const REACHABLE = new Set(["up", "failing", "nocheck", "drain"]);
const CODE_F = ["hrsp_1xx", "hrsp_2xx", "hrsp_3xx", "hrsp_4xx", "hrsp_5xx", "hrsp_other"];
const BYTE_F = new Set(["bin", "bout", "comp_in", "comp_out", "comp_byp"]);
const MS_F = new Set(["qtime", "ctime", "rtime", "ttime", "check_duration", "qtime_max", "ctime_max", "rtime_max", "ttime_max"]);

const KIND = {
  served: ["Sunucu yanıtladı", C.ok, "İstek bir backend sunucusuna ulaştı ve yanıt aldı."],
  denied: ["Engellendi (403)", C.warn, "http-request deny kurallarından birine takıldı."],
  nomatch: ["Backend eşleşmedi (503)", C.bad, "İstek hiçbir use_backend kuralına uymadı ve default_backend tanımlı değil."],
  noserver: ["Çalışan sunucu yok (503)", C.bad, "Backend seçildi ama içinde istek alabilecek sunucu yok."],
  redirect: ["Yönlendirildi", C.info, "HAProxy'nin kendisi yönlendirdi (ör. http'den https'e)."],
  proxy: ["HAProxy yanıtladı", C.faint, "Hatalı istek, zaman aşımı ya da istemcinin bağlantıyı kesmesi gibi durumlar."],
};
const KIND_ORDER = ["served", "denied", "nomatch", "noserver", "redirect", "proxy"];
// Yol satırının yanında görünen kısa etiket ("served" için etiket yok, olağan durum)
const KIND_TAG = {
  redirect: "HAProxy yönlendirdi", denied: "engellendi", nomatch: "backend eşleşmedi",
  noserver: "çalışan sunucu yok", proxy: "HAProxy yanıtladı", "": "karışık",
};


// ---------------- Biçimlendirme ----------------
const nf = new Intl.NumberFormat("tr-TR");
const num = (v) => (v === undefined || v === null || v === "" || !Number.isFinite(Number(v)) ? null : Number(v));
const trDec = (x, d = 1) => x.toFixed(d).replace(".", ",");
const fmtNum = (v) => (v == null ? "—" : nf.format(Math.round(v)));
const fmtRate = (v) => (v == null ? "—" : v < 10 ? trDec(v) : nf.format(Math.round(v)));
const fmtPct = (x) => (x == null ? "—" : `%${trDec(x * 100)}`);
// Sayı ile birimi bölünmeyen boşlukla (U+00A0) birleştirir; cümle içinde de tabloda da
// "218" ve "ms" ayrı satırlara düşmez.
const NB = "\u00A0";
function fmtBytes(b) {
  if (b == null) return "—";
  const u = ["B", "KB", "MB", "GB", "TB", "PB"];
  let i = 0;
  while (b >= 1024 && i < u.length - 1) { b /= 1024; i++; }
  return `${i > 0 && b < 10 ? trDec(b) : Math.round(b)}${NB}${u[i]}`;
}

function fmtBits(bytesPerSec) {
  if (bytesPerSec == null) return "—";
  let v = bytesPerSec * 8;
  const u = ["bit/sn", "Kbit/sn", "Mbit/sn", "Gbit/sn"];
  let i = 0;
  while (v >= 1000 && i < u.length - 1) { v /= 1000; i++; }
  return `${v < 10 ? trDec(v) : Math.round(v)}${NB}${u[i]}`;
}

function fmtDur(sec) {
  if (sec == null || sec < 0) return "—";
  sec = Math.floor(sec);
  const d = Math.floor(sec / 86400), h = Math.floor((sec % 86400) / 3600), m = Math.floor((sec % 3600) / 60), s = sec % 60;
  if (d) return h ? `${d}${NB}gün ${h}${NB}saat` : `${d}${NB}gün`;
  if (h) return m ? `${h}${NB}saat ${m}${NB}dk` : `${h}${NB}saat`;
  if (m) return s ? `${m}${NB}dk ${s}${NB}sn` : `${m}${NB}dk`;
  return `${s}${NB}sn`;
}

const fmtMs = (ms) => (ms == null ? "—" : ms >= 1000 ? `${trDec(ms / 1000)}${NB}sn` : `${Math.round(ms)}${NB}ms`);

function kindOf(status) {
  if (!status) return "none";
  const u = String(status).toUpperCase().trim();
  if (u.startsWith("MAINT")) return "maint";
  if (u.startsWith("DRAIN") || u === "NOLB") return "drain";
  if (u.startsWith("DOWN")) return u.includes("/") ? "recovering" : "down";
  if (u.startsWith("UP")) return u.includes("/") ? "failing" : "up";
  if (u === "OPEN") return "up";
  if (u === "FULL") return "full";
  if (u === "STOP") return "down";
  if (u === "NO CHECK") return "nocheck";
  return "none";
}

function explainCheck(status, code) {
  if (!status) return null;
  const k = String(status).replace(/^\*\s*/, "").trim();
  if (k === "L7STS") return `Sunucu HTTP ${code || "?"} döndü, beklenen yanıt bu değil`;
  return CHECK[k] || k;
}

function fmtField(k, v, row) {
  if (v === "" || v == null) return "—";
  const n = num(v);
  if (k === "status") return STATUS_LABEL[kindOf(v)];
  if (k === "check_status") return explainCheck(v, row.check_code);
  if (k === "type") return ["Frontend", "Backend", "Sunucu", "Dinleyici"][n] ?? String(v);
  if (k === "algo") return ALGO[String(v).split("(")[0]] ?? String(v);
  if (n == null) return String(v);
  if (BYTE_F.has(k)) return fmtBytes(n);
  if (k === "lastchg") return `${fmtDur(n)} önce`;
  if (k === "lastsess") return n < 0 ? "Hiç almadı" : `${fmtDur(n)} önce`;
  if (k === "downtime") return n === 0 ? "Hiç" : fmtDur(n);
  if (MS_F.has(k)) return fmtMs(n);
  if (k === "throttle") return `%${n}`;
  return fmtNum(n);
}

// ---------------- Zaman aralığı ----------------
const RANGES = [[5, `5${NB}dk`], [15, `15${NB}dk`], [60, `1${NB}saat`]];
// Seçili aralığın satır bazındaki sayaç farkları; errRatio/codeCounts ile aynı biçimde
const WinCtx = createContext({ wrates: {}, label: "açıldığından beri", minutes: 60 });

function windowToRates(win) {
  const out = {};
  for (const [k, v] of Object.entries(win?.rows || {})) out[k] = { codes: v.codes, n: v.n, econ: v.econ, eresp: v.eresp };
  return out;
}
function winLabel(win, minutes) {
  if (!win) return "açıldığından beri";
  if (win.seconds < minutes * 60 - 20) return `son ${fmtDur(win.seconds)}`;
  return minutes >= 60 ? `son 1${NB}saat` : `son ${minutes}${NB}dakika`;
}
const cap = (t) => t.charAt(0).toLocaleUpperCase("tr-TR") + t.slice(1);

// ---------------- Model ----------------
const keyOf = (r) => `${r.pxname}|${r.svname}`;

function rowType(r) {
  const t = num(r.type);
  if (t != null) return t;
  return r.svname === "FRONTEND" ? 0 : r.svname === "BACKEND" ? 1 : 2;
}

function buildModel(rows) {
  const frontends = [];
  const bmap = new Map();
  const servers = [];
  for (const r of rows) {
    const t = rowType(r);
    if (t === 0) frontends.push(r);
    else if (t === 1) bmap.set(r.pxname, { ...r, servers: [] });
    else if (t === 2) servers.push(r);
  }
  for (const s of servers) {
    if (!bmap.has(s.pxname)) bmap.set(s.pxname, { pxname: s.pxname, svname: "BACKEND", servers: [] });
    bmap.get(s.pxname).servers.push(s);
  }
  return { frontends, backends: [...bmap.values()] };
}

// İki ölçüm arasındaki sayaç farkından saniyelik değerler

function rpsOf(r, rates) {
  const x = rates[keyOf(r)];
  if (x && x.rps != null) return x.rps;
  return num(r.req_rate) ?? num(r.rate);
}

function codeCounts(r, rates) {
  const x = rates[keyOf(r)];
  return x ? x.codes : CODE_F.map((f) => num(r[f]) || 0);
}

function errRatio(rows, rates) {
  let e = 0, t = 0;
  for (const r of rows) {
    const c = codeCounts(r, rates);
    e += c[4];
    t += c.reduce((a, b) => a + b, 0);
  }
  return t > 0 ? e / t : null;
}

// Sade dilde "ne oluyor" bulguları

// ---------------- Bileşenler ----------------
function Term({ k, children }) {
  const ref = useRef(null);
  const [pos, setPos] = useState(null);
  const g = GLOSSARY[k];
  if (!g) return <span>{children}</span>;
  const show = () => {
    const r = ref.current.getBoundingClientRect();
    const w = Math.min(260, window.innerWidth - 16);
    const h = 150; // yaklaşık yükseklik; altta yer yoksa yukarı açılır
    const below = window.innerHeight - r.bottom > h;
    setPos({
      top: below ? r.bottom + 6 : Math.max(8, r.top - h - 6),
      left: Math.min(Math.max(8, r.left), Math.max(8, window.innerWidth - w - 8)),
      w,
    });
  };
  const hide = () => setPos(null);
  return (
    <span ref={ref} className="inline-flex items-center gap-1" onMouseEnter={show} onMouseLeave={hide}>
      <span>{children ?? g[0]}</span>
      <button type="button" aria-label={`${g[0]} nedir?`} onFocus={show} onBlur={hide} onClick={() => (pos ? hide() : show())}
        className="inline-flex" style={{ color: C.faint }}>
        <Info size={12} />
      </button>
      {pos && (
        <span role="tooltip" className="rounded-md p-3 text-left text-xs leading-relaxed shadow-lg"
          style={{ position: "fixed", top: pos.top, left: pos.left, width: pos.w, zIndex: 60, background: C.panel2, border: `1px solid ${C.line}`, color: C.text, fontWeight: 400, whiteSpace: "normal" }}>
          <span className="block font-semibold mb-1">{g[0]}</span>
          <span className="block" style={{ color: C.muted }}>{g[1]}</span>
          <span className="block mt-2" style={{ color: C.faint }}>{g[2] ?? `HAProxy alanı: ${k}`}</span>
        </span>
      )}
    </span>
  );
}

function Btn({ children, onClick, primary }) {
  return (
    <button type="button" onClick={onClick} className="inline-flex items-center gap-2 rounded-md px-3 py-1.5 text-sm font-medium"
      style={primary ? { background: C.info, color: "#0E1822" } : { color: C.text, border: `1px solid ${C.line}` }}>
      {children}
    </button>
  );
}

function StatusBadge({ status }) {
  const k = kindOf(status);
  return (
    <span className="inline-flex items-center gap-2 whitespace-nowrap" title={String(status)}>
      <span style={{ width: 8, height: 8, borderRadius: 99, background: KIND_COLOR[k] }} />
      <span>{STATUS_LABEL[k]}</span>
    </span>
  );
}

function Meter({ value, max }) {
  if (!max) return <span className="tnum">{fmtNum(value)}</span>;
  const p = Math.min(1, value / max);
  const col = p > 0.8 ? C.bad : p > 0.6 ? C.warn : C.ok;
  return (
    <div style={{ minWidth: 96 }}>
      <div className="tnum nw">{fmtNum(value)} <span style={{ color: C.faint }}>/ {fmtNum(max)}</span></div>
      <div style={{ height: 4, background: C.line, borderRadius: 2, marginTop: 4 }}>
        <div style={{ width: `${Math.max(2, p * 100)}%`, height: 4, background: col, borderRadius: 2 }} />
      </div>
    </div>
  );
}

function Panel({ title, note, badge, children }) {
  return (
    <section className="rounded-lg p-4 md:p-5" style={{ background: C.panel, border: `1px solid ${C.line}` }}>
      <div className="flex items-start justify-between gap-3 mb-3">
        <div>
          <h3 className="text-base font-semibold">{title}</h3>
          {note && <p className="text-xs mt-1 leading-relaxed" style={{ color: C.muted }}>{note}</p>}
        </div>
        {badge && (
          <span className="text-xs rounded px-2 py-0.5 whitespace-nowrap" style={{ border: `1px dashed ${C.warn}`, color: C.warn }}>{badge}</span>
        )}
      </div>
      {children}
    </section>
  );
}

function SectionTitle({ title, sub }) {
  return (
    <div className="mt-12 mb-3">
      <h2 className="text-xl font-semibold" style={{ letterSpacing: "-0.01em" }}>{title}</h2>
      {sub && <p className="text-sm mt-1" style={{ color: C.muted }}>{sub}</p>}
    </div>
  );
}

const CODE_PARTS = [["Başarılı", 1, C.ok], ["Yönlendirme", 2, C.info], ["İstemci hatası", 3, C.warn], ["Sunucu hatası", 4, C.bad]];
// HTTP kodunun kısa açıklaması (log'daki hata yollarında gösterilir)
const CODE_TEXT = {
  301: "kalıcı yönlendirme", 302: "geçici yönlendirme", 303: "başka adrese", 304: "değişmemiş (önbellek)",
  307: "geçici yönlendirme", 308: "kalıcı yönlendirme",
  400: "hatalı istek", 401: "yetki gerekli", 403: "erişim yok", 404: "bulunamadı", 405: "yöntem izinli değil",
  408: "istek zaman aşımı", 409: "çakışma", 410: "kaldırıldı", 413: "istek çok büyük", 415: "desteklenmeyen tür",
  429: "çok fazla istek", 431: "başlık çok büyük",
  500: "sunucu hatası", 501: "desteklenmiyor", 502: "geçit hatası (backend yanıt vermedi)",
  503: "servis yok (sunucu meşgul ya da kapalı)", 504: "geçit zaman aşımı (backend yavaş)", 507: "yetersiz alan",
  0: "diğer",
};
const codeColor = (code) => (code >= 500 || code === 0 ? C.bad : code >= 400 ? C.warn : C.info);
function CodeChip({ code, n }) {
  return (
    <span className="inline-flex items-center gap-1.5 rounded px-2 py-0.5 text-xs tnum" style={{ background: C.panel2, border: `1px solid ${C.line}` }} title={CODE_TEXT[code] || ""}>
      <span style={{ width: 7, height: 7, borderRadius: 2, background: codeColor(code) }} />
      <b style={{ color: C.text }}>{code === 0 ? "diğer" : code}</b>
      {CODE_TEXT[code] && code !== 0 ? <span style={{ color: C.faint }}>{CODE_TEXT[code]}</span> : null}
      <span style={{ color: C.muted }}>×{fmtNum(n)}</span>
    </span>
  );
}

// Satıra tıklayınca açılan ayrıntı: tam adres (alan adı log'da varsa), gerçek yollar, IP'ler
function ReqDetail({ d, path }) {
  if (!d) {
    return <p className="text-xs mt-2" style={{ color: C.faint }}>Bu satır için ayrıntı tutulmadı (o dakikada çok fazla farklı adres vardı).</p>;
  }
  const unknown = d.total - d.hostKnown;
  const samples = (d.samples || []).filter((x) => x.name !== path || d.samples.length > 1);
  const pathFor = (origin) => (samples.length === 1 ? samples[0].name : path);
  const box = { background: C.bg, border: `1px solid ${C.line}` };
  const Head = ({ children }) => <div className="text-xs mb-1" style={{ color: C.muted }}>{children}</div>;
  return (
    <div className="mt-2 rounded-md p-3 space-y-3 text-sm" style={box}>
      <div>
        <Head>Tam adres</Head>
        {d.hostKnown === 0 ? (
          <>
            <p className="text-xs leading-relaxed" style={{ color: C.faint }}>
              Bu LB'nin log biçiminde alan adı yok, bu yüzden isteğin hangi alan adına geldiği bilinmiyor.
            </p>
            <p className="brk mt-1" style={{ color: C.text }}>{path}</p>
          </>
        ) : (
          <ul className="space-y-1">
            {d.origins.filter((o) => o.name).map((o) => (
              <li key={o.name} className="flex items-baseline justify-between gap-3">
                <span className="tnum brk" style={{ color: C.text }}>{o.name === "(diğer)" ? "(diğer alan adları)" : `${o.name}${pathFor(o.name)}`}</span>
                <span className="tnum whitespace-nowrap" style={{ color: C.muted }}>×{fmtNum(o.n)}</span>
              </li>
            ))}
            {unknown > 0 && (
              <li className="text-xs" style={{ color: C.faint }}>
                {fmtNum(unknown)} istekte alan adı log'a yazılmamış (yakalama sadece bazı isteklerde olabilir).
              </li>
            )}
          </ul>
        )}
      </div>
      {samples.length > 0 && (samples.length > 1 || samples[0].name !== path) && (
        <div>
          <Head>Gerçek yollar (sorgu parametreleri saklanmaz)</Head>
          <ul className="space-y-1">
            {samples.map((x) => (
              <li key={x.name} className="flex items-baseline justify-between gap-3">
                <span className="brk" style={{ color: x.name === "(diğer)" ? C.faint : C.text }}>{x.name === "(diğer)" ? "(diğer yollar)" : x.name}</span>
                <span className="tnum whitespace-nowrap" style={{ color: C.muted }}>×{fmtNum(x.n)}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
      <div>
        <Head>Bu istekleri gönderen IP'ler</Head>
        <ul className="grid gap-x-6 md:grid-cols-2">
          {(d.ips || []).map((c) => (
            <li key={c.ip} className="flex items-baseline justify-between gap-3 py-0.5">
              <span className="tnum brk" style={{ color: c.ip === "(diğer)" ? C.faint : C.text }}>{c.ip === "(diğer)" ? "(diğer IP'ler)" : c.ip}{c.cloudflare && <span className="text-xs ml-2" style={{ color: C.faint }}>Cloudflare</span>}</span>
              <span className="tnum nw" style={{ color: C.muted }}>×{fmtNum(c.n)}</span>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}

// Tıklanınca açılıp kapanan satır
function ExpandRow({ open, onToggle, head, children, first }) {
  return (
    <div style={{ borderTop: first ? "none" : `1px solid ${C.line}` }}>
      <button type="button" onClick={onToggle} aria-expanded={open} className="hl-row w-full text-left flex gap-2 items-start py-2 px-1 rounded">
        <span className="mt-0.5" style={{ color: C.muted, flexShrink: 0 }}>{open ? <ChevronDown size={14} /> : <ChevronRight size={14} />}</span>
        <span className="flex-1" style={{ minWidth: 0 }}>{head}</span>
      </button>
      {open && <div className="pl-6 pb-2">{children}</div>}
    </div>
  );
}
function CodeBar({ row, compact }) {
  const { wrates, label } = useContext(WinCtx);
  const c = codeCounts(row, wrates);
  const total = c.reduce((a, b) => a + b, 0);
  if (!total) return compact ? <span style={{ color: C.faint }}>—</span> : null;
  const bar = (
    <div className="flex rounded overflow-hidden" style={{ height: compact ? 6 : 8, background: C.line, minWidth: compact ? 90 : 0 }}>
      {CODE_PARTS.map(([n, i, col]) => (c[i] > 0 ? <div key={n} title={`${n} ${fmtPct(c[i] / total)}`} style={{ width: `${(c[i] / total) * 100}%`, background: col }} /> : null))}
    </div>
  );
  if (compact) return bar;
  return (
    <div className="mb-5">
      <div className="text-xs mb-2" style={{ color: C.muted }}>
        Yanıt türleri ({wrates[keyOf(row)] ? label : "açıldığından beri"})
      </div>
      {bar}
      <div className="flex flex-wrap gap-x-5 gap-y-1 mt-2 text-xs" style={{ color: C.muted }}>
        {CODE_PARTS.map(([n, i, col]) => (
          <span key={n} className="inline-flex items-center gap-2">
            <span style={{ width: 8, height: 8, borderRadius: 2, background: col }} />
            {n} <span className="tnum" style={{ color: C.text }}>{fmtPct(c[i] / total)}</span>
          </span>
        ))}
      </div>
    </div>
  );
}

function Metric({ label, value, color }) {
  return (
    <span className="tnum text-sm md:text-right" style={{ color: color || C.text }}>
      <span className="be-mlabel">{label}</span>{value}
    </span>
  );
}

function Fact({ k, children }) {
  return (
    <div>
      <dt className="text-xs" style={{ color: C.muted }}><Term k={k} /></dt>
      <dd className="tnum mt-0.5">{children}</dd>
    </div>
  );
}

function Th({ k, children, right }) {
  return (
    <th className={`py-2 pr-4 font-normal whitespace-nowrap ${right ? "text-right" : "text-left"}`}>
      {k ? <Term k={k}>{children}</Term> : children}
    </th>
  );
}

function ServerTable({ servers, rates, onFields }) {
  const { wrates, label } = useContext(WinCtx);
  if (!servers.length) return <p className="text-sm" style={{ color: C.faint }}>Bu backend'de sunucu yok.</p>;
  const td = "py-2.5 pr-4 align-top";
  const codeOf = (s, i) => wrates[keyOf(s)]?.codes?.[i] ?? null;
  const tot = (s) => (wrates[keyOf(s)]?.codes || []).reduce((a, x) => a + x, 0);
  const maxOf = (i) => Math.max(0, ...servers.map((s) => codeOf(s, i) || 0));
  const max4 = maxOf(3), max5 = maxOf(4);
  const errCell = (s, i, mx, warnCol) => {
    const v = codeOf(s, i), t = tot(s);
    if (v == null) return <td className={`${td} text-right tnum nw`}>—</td>;
    return (
      <td className={`${td} text-right tnum nw`} title={label}>
        <div style={{ color: v > 0 && v === mx ? warnCol : C.text }}>{fmtNum(v)}</div>
        {t > 0 && v > 0 && <div className="text-xs mt-0.5" style={{ color: C.faint }}>{fmtPct(v / t)}</div>}
      </td>
    );
  };
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm" style={{ borderCollapse: "collapse", minWidth: 1020 }}>
        <thead>
          <tr className="text-xs" style={{ color: C.muted }}>
            <Th>Sunucu</Th>
            <Th k="status">Durum</Th>
            <Th k="check_status">Sağlık kontrolü</Th>
            <Th k="weight" right>Ağırlık</Th>
            <Th k="scur">Açık bağlantı</Th>
            <Th k="req_rate" right>İstek/sn</Th>
            <Th k="_srv4xx" right>4xx</Th>
            <Th k="_srv5xx" right>5xx</Th>
            <Th k="rtime" right>Yanıt süresi</Th>
            <Th k="econ" right>Bağlanamama</Th>
            <Th k="eresp" right>Yanıt hatası</Th>
            <Th k="chkdown" right>Düşme</Th>
            <Th k="downtime" right>Kapalı kaldı</Th>
          </tr>
        </thead>
        <tbody>
          {servers.map((s) => {
            const k = kindOf(s.status);
            const lc = num(s.lastchg);
            const dur = num(s.check_duration);
            const econ = num(s.econ);
            const downtime = num(s.downtime);
            return (
              <tr key={s.svname} className="hl-row" style={{ borderTop: `1px solid ${C.line}`, boxShadow: `inset 3px 0 0 ${KIND_COLOR[k]}` }}>
                <td className={`${td} pl-3`}>
                  <button type="button" onClick={() => onFields(s)} title="Tüm alanları göster" className="font-medium text-left"
                    style={{ color: C.text, textDecoration: "underline", textDecorationStyle: "dotted", textUnderlineOffset: 3, textDecorationColor: C.faint }}>
                    {s.svname}
                  </button>
                  {s.addr && <div className="text-xs mt-0.5" style={{ color: C.faint }}>{s.addr}</div>}
                  {num(s.bck) === 1 && <div className="text-xs" style={{ color: C.faint }}>yedek sunucu</div>}
                </td>
                <td className={td}>
                  <StatusBadge status={s.status} />
                  {lc != null && <div className="text-xs mt-0.5" style={{ color: C.faint }}>{fmtDur(lc)} önce değişti</div>}
                </td>
                <td className={td} style={{ maxWidth: 260 }}>
                  <div>{explainCheck(s.check_status, s.check_code) ?? "Kontrol tanımlı değil"}</div>
                  {s.check_status && (
                    <div className="text-xs mt-0.5" style={{ color: C.faint }}>{s.check_status}{dur != null ? `, ${fmtMs(dur)}` : ""}</div>
                  )}
                </td>
                <td className={`${td} text-right tnum nw`}>{fmtNum(num(s.weight))}</td>
                <td className={td}><Meter value={num(s.scur) || 0} max={num(s.slim)} /></td>
                <td className={`${td} text-right tnum nw`}>{fmtRate(rpsOf(s, rates))}</td>
                {errCell(s, 3, max4, C.warn)}
                {errCell(s, 4, max5, C.bad)}
                <td className={`${td} text-right tnum nw`}>{fmtMs(num(s.rtime))}</td>
                <td className={`${td} text-right tnum nw`} style={{ color: econ > 0 ? C.warn : C.text }}>{fmtNum(econ)}</td>
                <td className={`${td} text-right tnum nw`}>{fmtNum(num(s.eresp))}</td>
                <td className={`${td} text-right tnum nw`}>{fmtNum(num(s.chkdown))}</td>
                <td className={`${td} text-right tnum nw`}>{downtime === 0 ? "Hiç" : fmtDur(downtime)}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

// Backend'in 4xx ve 5xx özeti. Sıfır olanlar tek satırda toplanır, ortak yönlendirme
// cümlesi bir kez yazılır; böylece iki kutu aynı cümleyi tekrar etmez.
function errorStats(b, wrates, idx) {
  const wb = wrates[keyOf(b)];
  const per = b.servers.map((s) => {
    const w = wrates[keyOf(s)];
    return { s, e: w?.codes?.[idx] || 0, tot: w ? w.codes.reduce((a, x) => a + x, 0) : 0 };
  });
  const sum = per.reduce((a, x) => a + x.e, 0);
  return { beN: wb ? wb.codes[idx] : 0, per, sum, total: Math.max(wb ? wb.codes[idx] : 0, sum) };
}

function ErrorBox({ st, idx, label }) {
  const isSrv = idx === 4;
  const adi = isSrv ? "sunucu hatası (5xx)" : "istemci hatası (4xx)";
  const top = [...st.per].sort((a, c) => c.e - a.e)[0];
  const busy = st.per.filter((x) => x.tot >= 20);
  const ratios = busy.map((x) => x.e / x.tot);
  const even = busy.length >= 2 && Math.min(...ratios) > 0 && Math.max(...ratios) / Math.min(...ratios) < 1.5;
  const fromProxy = st.beN - st.sum;
  return (
    <p className="rounded-md px-4 py-3 text-sm leading-relaxed" style={{ background: C.panel2, boxShadow: `inset 3px 0 0 ${isSrv ? C.bad : C.warn}` }}>
      {cap(label)} içinde bu backend'den {fmtNum(st.total)} {adi} döndü.
      {top && top.e > 0 && st.sum > 0 && <> En çok <b className="brk">{top.s.svname}</b> sunucusundan: {fmtNum(top.e)} tane, sunuculardan dönenlerin {fmtPct(top.e / st.sum)} kadarı.</>}
      {isSrv && even && <> Hata oranları sunucular arasında birbirine yakın; sorun büyük ihtimalle tek bir sunucuda değil, hepsinin kullandığı ortak bir yerde (uygulama, veritabanı, dış servis).</>}
      {isSrv && fromProxy > Math.max(5, st.beN * 0.1) && <> {fmtNum(fromProxy)} tanesi hiçbir sunucuya ulaşmadan HAProxy tarafından üretildi (ör. çalışan sunucu yokken 503).</>}
      {!isSrv && <> 4xx genelde istemci kaynaklıdır (404 bulunamadı, 401/403 yetki, 429 çok istek); her zaman sunucu sorunu değildir.</>}
    </p>
  );
}

function ErrorSummaries({ b }) {
  const { wrates, label } = useContext(WinCtx);
  if (!wrates[keyOf(b)] || !b.servers.length) return null;
  const s5 = errorStats(b, wrates, 4);
  const s4 = errorStats(b, wrates, 3);
  const bos = [s5.total === 0 && "sunucu hatası (5xx)", s4.total === 0 && "istemci hatası (4xx)"].filter(Boolean);
  return (
    <div className="space-y-2 mb-5">
      {s5.total > 0 && <ErrorBox st={s5} idx={4} label={label} />}
      {s4.total > 0 && <ErrorBox st={s4} idx={3} label={label} />}
      {bos.length > 0 && (
        <p className="rounded-md px-4 py-2.5 text-sm" style={{ background: C.panel2, color: C.muted }}>
          {cap(label)} içinde bu backend'den hiç {bos.join(" ve ")} dönmedi.
        </p>
      )}
      {(s5.total > 0 || s4.total > 0) && (
        <p className="text-xs" style={{ color: C.faint }}>
          Hangi adreslerin hata aldığını aşağıdaki "Log'dan gelenler" bölümünde görebilirsin.
        </p>
      )}
    </div>
  );
}

function BackendDetail({ b, rates, onFields }) {
  const algoKey = String(b.algo || "").split("(")[0];
  return (
    <div className="px-4 pb-6 pt-2">
      <dl className="grid grid-cols-2 md:grid-cols-4 gap-x-8 gap-y-4 text-sm mb-5">
        <Fact k="algo">{b.algo ? ALGO[algoKey] || b.algo : "—"}</Fact>
        <Fact k="req_tot">{fmtNum(num(b.req_tot) ?? num(b.stot))}</Fact>
        <Fact k="scur"><span className="nw">{fmtNum(num(b.scur))}</span> <span className="nw" style={{ color: C.faint }}>(en fazla {fmtNum(num(b.smax))})</span></Fact>
        <Fact k="bout"><span className="nw">{fmtBytes(num(b.bout))}</span> <span className="nw" style={{ color: C.faint }}>({fmtBytes(num(b.bin))} gelen)</span></Fact>
        <Fact k="qtime">{fmtMs(num(b.qtime))}</Fact>
        <Fact k="ctime">{fmtMs(num(b.ctime))}</Fact>
        <Fact k="rtime">{fmtMs(num(b.rtime))}</Fact>
        <Fact k="ttime">{fmtMs(num(b.ttime))}</Fact>
        <Fact k="econ">{fmtNum(num(b.econ))}</Fact>
        <Fact k="eresp">{fmtNum(num(b.eresp))}</Fact>
        <Fact k="wredis">{fmtNum(num(b.wredis))}</Fact>
        <Fact k="cli_abrt">{fmtNum(num(b.cli_abrt))}</Fact>
      </dl>
      <CodeBar row={b} />
      <ErrorSummaries b={b} />
      <ServerTable servers={b.servers} rates={rates} onFields={onFields} />
      <button type="button" className="mt-4 text-sm" style={{ color: C.info }} onClick={() => onFields(b)}>
        Bu backend'in tüm alanlarını göster
      </button>
    </div>
  );
}

function FrontendTable({ model, rates, onFields }) {
  if (!model.frontends.length) return <p style={{ color: C.faint }}>Veride frontend yok.</p>;
  const td = "py-3 pr-4 align-top";
  return (
    <div className="rounded-lg overflow-x-auto" style={{ background: C.panel, border: `1px solid ${C.line}` }}>
      <table className="w-full text-sm" style={{ borderCollapse: "collapse", minWidth: 900 }}>
        <thead>
          <tr className="text-xs" style={{ color: C.muted }}>
            <th className="py-2 pl-4 pr-4 font-normal text-left">Frontend</th>
            <Th k="status">Durum</Th>
            <Th k="scur">Açık bağlantı</Th>
            <Th k="req_rate" right>İstek/sn</Th>
            <Th k="req_tot" right>Toplam istek</Th>
            <Th k="dreq" right>Engellenen</Th>
            <Th k="ereq" right>Hatalı istek</Th>
            <Th k="bout" right>Giden / gelen</Th>
            <Th>Yanıt türleri</Th>
          </tr>
        </thead>
        <tbody>
          {model.frontends.map((f) => (
            <tr key={f.pxname} id={`fe-${f.pxname}`} className="hl-row" style={{ borderTop: `1px solid ${C.line}` }}>
              <td className={`${td} pl-4`}>
                <button type="button" onClick={() => onFields(f)} title="Tüm alanları göster" className="font-medium text-left"
                  style={{ color: C.text, textDecoration: "underline", textDecorationStyle: "dotted", textUnderlineOffset: 3, textDecorationColor: C.faint }}>
                  {f.pxname}
                </button>
                {f.mode && <div className="text-xs mt-0.5" style={{ color: C.faint }}>{f.mode} modu</div>}
              </td>
              <td className={td}><StatusBadge status={f.status} /></td>
              <td className={td}><Meter value={num(f.scur) || 0} max={num(f.slim)} /></td>
              <td className={`${td} text-right tnum nw`}>{fmtRate(rpsOf(f, rates))}</td>
              <td className={`${td} text-right tnum nw`}>{fmtNum(num(f.req_tot) ?? num(f.stot))}</td>
              <td className={`${td} text-right tnum nw`}>{fmtNum(num(f.dreq))}</td>
              <td className={`${td} text-right tnum nw`}>{fmtNum(num(f.ereq))}</td>
              <td className={`${td} text-right tnum whitespace-nowrap`}>{fmtBytes(num(f.bout))} / {fmtBytes(num(f.bin))}</td>
              <td className={td} style={{ minWidth: 120 }}><CodeBar row={f} compact /></td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function RankRow({ label, sub, value, share, color }) {
  return (
    <li className="py-2">
      <div className="flex items-baseline justify-between gap-3 text-sm">
        <span className="truncate" style={{ minWidth: 0 }} title={label}>
          <span className="font-medium">{label}</span>
          {sub && <span className="text-xs ml-2" style={{ color: C.faint }}>{sub}</span>}
        </span>
        <span className="tnum whitespace-nowrap">{value}</span>
      </div>
      <div style={{ height: 4, background: C.line, borderRadius: 2, marginTop: 6 }}>
        <div style={{ width: `${Math.max(1.5, share * 100)}%`, height: 4, background: color, borderRadius: 2 }} />
      </div>
    </li>
  );
}

function TopBackends({ model, rates }) {
  const { wrates, label } = useContext(WinCtx);
  const win = Object.keys(wrates).length > 0;
  const items = model.backends
    .map((b) => ({ name: b.pxname, n: wrates[keyOf(b)]?.n ?? 0, rps: rpsOf(b, rates) || 0, tot: num(b.req_tot) ?? num(b.stot) ?? 0 }))
    .sort((a, b) => (win ? b.n - a.n : b.tot - a.tot) || a.name.localeCompare(b.name, "tr"));
  const total = items.reduce((a, b) => a + (win ? b.n : b.tot), 0) || 1;
  return (
    <Panel title="En çok istek alan backend'ler" note={win ? `${cap(label)} gelen isteğe göre sıralı.` : "HAProxy açıldığından beri toplam isteğe göre sıralı."}>
      <ul>
        {items.map((it) => {
          const v = win ? it.n : it.tot;
          return (
            <RankRow key={it.name} label={it.name} sub={`toplam ${fmtNum(it.tot)}`}
              value={win ? `${fmtNum(it.n)} istek, ${fmtPct(v / total)}` : fmtPct(v / total)} share={v / total} color={C.info} />
          );
        })}
      </ul>
    </Panel>
  );
}

function FieldsModal({ row, onClose }) {
  const closeRef = useRef(null);
  useEffect(() => {
    closeRef.current?.focus();
    const h = (e) => { if (e.key === "Escape") onClose(); };
    window.addEventListener("keydown", h);
    return () => window.removeEventListener("keydown", h);
  }, [onClose]);
  const entries = Object.entries(row).filter(([k]) => k !== "servers");
  return (
    <div role="dialog" aria-modal="true" aria-label={`${row.pxname} ${row.svname} tüm alanlar`}
      className="fixed inset-0 z-40 flex items-start justify-center p-4 overflow-y-auto" style={{ background: "rgba(8,14,20,0.72)" }} onClick={onClose}>
      <div className="w-full max-w-3xl rounded-lg my-8" style={{ background: C.panel, border: `1px solid ${C.line}` }} onClick={(e) => e.stopPropagation()}>
        <div className="flex items-start justify-between gap-4 px-5 py-4" style={{ borderBottom: `1px solid ${C.line}` }}>
          <div>
            <div className="text-lg font-semibold brk">{row.pxname} / {row.svname}</div>
            <div className="text-sm mt-0.5" style={{ color: C.muted }}>HAProxy'nin bu satır için verdiği {entries.length} alanın tamamı, sade açıklamalarıyla.</div>
          </div>
          <button ref={closeRef} type="button" onClick={onClose} aria-label="Kapat" style={{ color: C.muted }}><X size={20} /></button>
        </div>
        <div className="overflow-x-auto">
          <table className="w-full text-sm" style={{ borderCollapse: "collapse" }}>
            <thead>
              <tr className="text-xs" style={{ color: C.muted }}>
                <th className="py-2 pl-5 pr-4 font-normal text-left">Anlamı</th>
                <th className="py-2 pr-4 font-normal text-left">Okunur değer</th>
                <th className="py-2 pr-4 font-normal text-left">Ham değer</th>
                <th className="py-2 pr-5 font-normal text-left">Alan</th>
              </tr>
            </thead>
            <tbody>
              {entries.map(([k, v]) => {
                const g = GLOSSARY[k];
                return (
                  <tr key={k} style={{ borderTop: `1px solid ${C.line}` }}>
                    <td className="py-2 pl-5 pr-4 align-top" style={{ maxWidth: 320 }}>
                      <div>{g ? g[0] : k}</div>
                      {g && <div className="text-xs mt-0.5" style={{ color: C.faint }}>{g[1]}</div>}
                    </td>
                    <td className="py-2 pr-4 align-top tnum brk" style={{ maxWidth: 220 }}>{fmtField(k, v, row)}</td>
                    <td className="py-2 pr-4 align-top tnum brk" style={{ maxWidth: 220, color: C.muted }}>{v === "" ? "boş" : String(v)}</td>
                    <td className="py-2 pr-5 align-top text-xs" style={{ color: C.faint }}>{k}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}

function computeRates(prevRows, rows, dt) {
  const out = {};
  if (!prevRows || !dt || dt <= 0) return out;
  const pm = new Map(prevRows.map((r) => [keyOf(r), r]));
  const d = (p, r, f) => {
    const a = num(r[f]), b = num(p[f]);
    return a == null || b == null ? null : Math.max(0, a - b) / dt;
  };
  for (const r of rows) {
    const p = pm.get(keyOf(r));
    if (!p) continue;
    out[keyOf(r)] = {
      rps: d(p, r, "req_tot") ?? d(p, r, "stot"),
      codes: CODE_F.map((f) => d(p, r, f) ?? 0),
      bin: d(p, r, "bin"),
      bout: d(p, r, "bout"),
      econ: d(p, r, "econ"),
    };
  }
  return out;
}

function buildFindings(model, rates, logs, wrates = {}, label = "") {
  const out = [];
  const add = (level, target, text) => out.push({ level, target, text });
  let nocheck = 0, total = 0;
  for (const b of model.backends) {
    const sv = b.servers;
    const name = b.pxname;
    if (sv.length && !sv.some((s) => REACHABLE.has(kindOf(s.status))))
      add("bad", name, `${name} tamamen erişilemez: çalışan sunucusu yok. Buraya gelen istekler 503 hatası alıyor.`);
    for (const s of sv) {
      total++;
      const k = kindOf(s.status);
      const since = fmtDur(num(s.lastchg));
      const why = explainCheck(s.check_status, s.check_code) ?? "bilinmiyor";
      if (k === "nocheck") nocheck++;
      if (k === "down") add("bad", name, `${name} içindeki ${s.svname}, ${since} süredir çalışmıyor. Sebep: ${why}.`);
      else if (k === "failing") add("warn", name, `${s.svname} (${name}) sağlık kontrollerinde hata veriyor: ${why}. Böyle devam ederse devre dışı kalacak.`);
      else if (k === "recovering") add("warn", name, `${s.svname} (${name}) tekrar ayağa kalkıyor; kontroller geçmeye başladı ama henüz trafik almıyor.`);
      else if (k === "maint") add("info", name, `${s.svname} (${name}) ${since} süredir bakım modunda. Elle devre dışı bırakılmış, trafik almıyor.`);
      else if (k === "drain") add("info", name, `${s.svname} (${name}) boşaltılıyor: yeni kullanıcı almıyor, mevcut bağlantıları bitiriyor.`);
      // Aralık toplamı kullanılıyor: saniyelik değer her yenilemede bulguyu görünüp kaybeder,
      // bu da listenin boyunu değiştirip sayfayı oynatırdı.
      const ec = wrates[keyOf(s)]?.econ || 0;
      if (ec > 0) add(k === "nocheck" ? "bad" : "warn", name, `${s.svname} (${name}) sunucusuna bağlanılamıyor: ${label ? `${label} içinde` : "son ölçümde"} ${fmtNum(ec)} başarısız bağlantı denemesi.${k === "nocheck" ? " Sağlık kontrolü olmadığı için HAProxy onu devre dışı bırakmıyor, istekler ona gitmeye devam ediyor." : ""}`);
      const cd = num(s.chkdown);
      if (cd >= 5) add("warn", name, `${s.svname} (${name}) HAProxy açıldığından beri ${cd} kez düşüp kalktı. Kararsız çalışıyor olabilir.`);
    }
    const q = num(b.qcur);
    if (q > 0) add("warn", name, `${name} kuyruğunda ${fmtNum(q)} istek bekliyor. Sunucular bağlantı sınırına dayanmış, kapasite yetmiyor.`);
    const er = errRatio([b], wrates);
    const wn = (wrates[keyOf(b)]?.codes || []).reduce((a, x) => a + x, 0);
    if (er != null && er > 0.02 && wn >= 50) {
      let top = null, sum = 0;
      for (const s of sv) {
        const e = wrates[keyOf(s)]?.codes?.[4] || 0;
        sum += e;
        if (!top || e > top.e) top = { s, e };
      }
      const who = top && top.e > 0 && sv.length > 1 ? ` En çok ${top.s.svname} sunucusundan (${fmtPct(top.e / sum)}).` : "";
      add("warn", name, `${cap(label)} içinde ${name} yanıtlarının ${fmtPct(er)} kadarı sunucu hatası (5xx).${who}`);
    }
    const rt = num(b.rtime);
    if (rt != null && rt > 800) add("warn", name, `${name} yavaş: sunucular ortalama ${fmtMs(rt)} içinde yanıt veriyor.`);
  }
  for (const f of model.frontends) {
    const sc = num(f.scur), sl = num(f.slim);
    if (kindOf(f.status) === "full") add("bad", `fe:${f.pxname}`, `${f.pxname} bağlantı sınırına ulaştı, yeni bağlantılar bekletiliyor.`);
    else if (sl && sc / sl > 0.8) add("warn", `fe:${f.pxname}`, `${f.pxname} bağlantı sınırının ${fmtPct(sc / sl)} kadarını kullanıyor (${fmtNum(sc)} / ${fmtNum(sl)}).`);
  }
  if (logs?.enabled && logs.kinds) {
    const sure = label ? `${cap(label)} içinde` : `Son ${logs.minutes}${NB}dakikada`;
    const top = (kind) => (logs.blocked || []).find((x) => x.kind === kind);
    const nm = logs.kinds.nomatch || 0;
    if (nm > 0) {
      const t = top("nomatch");
      add("warn", "logs", `${sure} ${fmtNum(nm)} istek hiçbir backend'e eşleşmediği için 503 aldı.${t ? ` En çok: ${t.method} ${t.path} (${fmtNum(t.n)}).` : ""}`);
    }
    const ns = logs.kinds.noserver || 0;
    if (ns > 0) add("bad", "logs", `${sure} ${fmtNum(ns)} istek, seçilen backend'de çalışan sunucu olmadığı için 503 aldı.`);
  }
  // Sağlık kontrolü olmayan sunucular artık "Yapılandırma notları" bölümünde (config'ten, backend adlarıyla)
  void nocheck; void total;
  return out.sort((a, b) => LEVEL_ORDER[a.level] - LEVEL_ORDER[b.level]);
}

function Header({ info, running, onToggleRun, ok, lastAt, agentVersion }) {
  const up = num(info?.Uptime_sec);
  return (
    <header className="flex flex-wrap items-center justify-between gap-4 pb-5" style={{ borderBottom: `1px solid ${C.line}` }}>
      <div>
        <div className="text-lg font-semibold">
          haproxy-lens{info?.node ? ` ${info.node}` : ""}
          {agentVersion && <span className="text-sm font-normal ml-2" style={{ color: C.faint }}>sürüm {agentVersion}</span>}
        </div>
        <div className="text-sm mt-0.5" style={{ color: C.muted }}>
          HAProxy {info?.Version || ""}
          {up != null ? `, ${fmtDur(up)} süredir açık` : ""}
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-sm inline-flex items-center gap-2 mr-2" style={{ color: C.muted }}>
          <span style={{ width: 8, height: 8, borderRadius: 99, background: !ok ? C.bad : running ? C.ok : C.faint }} />
          {!ok ? "Bağlantı sorunu" : running ? `Canlı, ${TICK_SEC} saniyede bir` : "Duraklatıldı"}
          {lastAt ? <span style={{ color: C.faint }}>({new Date(lastAt).toLocaleTimeString("tr-TR")})</span> : null}
        </span>
        <Btn onClick={onToggleRun}>{running ? <Pause size={14} /> : <Play size={14} />}{running ? "Duraklat" : "Devam et"}</Btn>
      </div>
    </header>
  );
}

// Config'te ya da ortamda paneli kısıtlayan ne varsa: ne eksik, neyi etkiliyor, hangi satır eklenebilir
function ConfigNotes({ cfg }) {
  const [open, setOpen] = useState(null);
  if (!cfg) return null;
  const notes = cfg.notes || [];
  const warn = notes.filter((n) => n.level === "warn").length;
  const info = notes.length - warn;
  const isOpen = open ?? warn > 0;
  const checked = cfg.checkedAt ? new Date(cfg.checkedAt).toLocaleTimeString("tr-TR") : null;
  const summary = notes.length === 0 ? "Paneli kısıtlayan bir ayar yok." : [warn && `${warn} uyarı`, info && `${info} öneri`].filter(Boolean).join(", ");
  const mono = { fontFamily: "ui-monospace, Menlo, Consolas, monospace" };
  return (
    <section id="notes" className="mb-6 rounded-lg" style={{ background: C.panel, border: `1px solid ${C.line}` }}>
      <button type="button" onClick={() => setOpen(!isOpen)} aria-expanded={isOpen}
        className="hl-row w-full text-left flex flex-wrap items-center justify-between gap-x-4 gap-y-1 px-4 py-3 rounded-lg">
        <span className="flex items-center gap-2">
          {isOpen ? <ChevronDown size={16} color={C.muted} /> : <ChevronRight size={16} color={C.muted} />}
          <span className="font-medium">Yapılandırma notları</span>
          <span className="text-sm" style={{ color: warn ? C.warn : C.muted }}>{summary}</span>
        </span>
        {checked && <span className="text-xs" style={{ color: C.faint }}>Config en son {checked}'de kontrol edildi; değişiklikler kendiliğinden algılanır.</span>}
      </button>
      {isOpen && (
        <div className="px-4 pb-4 space-y-3">
          {notes.map((n, i) => (
            <div key={i} className="rounded-md px-4 py-3" style={{ background: C.panel2, boxShadow: `inset 3px 0 0 ${n.level === "warn" ? C.warn : C.info}` }}>
              <div className="text-sm font-medium brk">
                {n.title}
                {n.where && <span className="text-xs ml-2 font-normal" style={{ color: C.faint }}>{n.where}</span>}
              </div>
              <p className="text-sm mt-1 leading-relaxed brk" style={{ color: C.muted }}>{n.text}</p>
              {n.fix && (
                <div className="mt-2 text-xs flex flex-wrap items-center gap-2">
                  <span style={{ color: C.faint }}>Eklenebilecek satır:</span>
                  <code className="rounded px-2 py-0.5 brk" style={{ ...mono, background: C.bg, border: `1px solid ${C.line}`, color: C.text }}>{n.fix}</code>
                </div>
              )}
              {(n.samples || []).length > 0 && (
                <div className="mt-2 space-y-1">
                  {n.samples.map((x, j) => (
                    <div key={j} className="text-xs rounded px-2 py-1 overflow-x-auto whitespace-nowrap" style={{ ...mono, background: C.bg, color: C.text }}>{x}</div>
                  ))}
                </div>
              )}
            </div>
          ))}
          {(cfg.frontends || []).length > 0 && (
            <div className="text-xs leading-relaxed" style={{ color: C.faint }}>
              Okunan frontend'ler:{" "}
              {cfg.frontends.map((f, i) => (
                <span key={f.name}>
                  {i > 0 && ", "}
                  <span style={{ color: C.muted }}>{f.name}</span> ({f.mode}{f.mode === "http" ? `, ${f.format}` : ""}{f.hostFrom ? `, alan adı: ${f.hostFrom}` : ""}{!f.logs ? ", log yok" : ""})
                </span>
              ))}
              {cfg.logSource ? `. Log kaynağı: ${cfg.logSource.replace(/^file:/, "").replace(/^journal:/, "journald, ")}.` : "."}
            </div>
          )}
          <p className="text-xs" style={{ color: C.faint }}>
            Ajan config'e hiçbir şey yazmaz. Önerilen satırları eklemek senin kararın; eklersen HAProxy reload edildikten sonra panel en geç 30 saniye içinde kendiliğinden uyum sağlar.
          </p>
        </div>
      )}
    </section>
  );
}

function StatusHero({ findings, model, onJump }) {
  const [showAll, setShowAll] = useState(false);
  const bad = findings.filter((f) => f.level === "bad").length;
  const warn = findings.filter((f) => f.level === "warn").length;
  const servers = model.backends.flatMap((b) => b.servers);
  const healthy = servers.filter((s) => HEALTHY.has(kindOf(s.status))).length;
  const nocheck = servers.filter((s) => kindOf(s.status) === "nocheck").length;
  const [color, headline] = bad
    ? [C.bad, `${bad} ciddi sorun var${warn ? `, ${warn} konu da dikkat istiyor.` : ", gerisi yolunda."}`]
    : warn
      ? [C.warn, `Trafik akıyor, ama ${warn} konu dikkat istiyor.`]
      : [C.ok, "Her şey yolunda."];
  const visible = showAll ? findings : findings.slice(0, 6);
  return (
    <section className="pt-8 pb-6">
      <h1 className="text-3xl md:text-4xl font-semibold" style={{ color, letterSpacing: "-0.02em", lineHeight: 1.15 }}>{headline}</h1>
      <p className="mt-3" style={{ color: C.muted }}>
        {model.frontends.length} frontend, {model.backends.length} backend ve {servers.length} sunucu izleniyor; {healthy} sunucu sağlıklı
        {nocheck ? `, ${nocheck} sunucunun durumu bilinmiyor (sağlık kontrolü yok)` : ""}.
      </p>
      {findings.length > 0 && (
        <ul className="mt-6 space-y-2">
          {visible.map((f, i) => (
            <li key={`${f.target}-${i}`}>
              <button type="button" onClick={() => f.target && onJump(f.target)} className="hl-row w-full text-left flex gap-3 items-stretch rounded-md px-4 py-3"
                style={{ background: C.panel, border: `1px solid ${C.line}`, cursor: f.target ? "pointer" : "default" }}>
                <span style={{ width: 3, borderRadius: 2, background: LEVEL_COLOR[f.level], flexShrink: 0 }} />
                <span className="text-sm leading-relaxed brk">{f.text}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
      {findings.length > 6 && (
        <button type="button" onClick={() => setShowAll((s) => !s)} className="mt-3 text-sm" style={{ color: C.info }}>
          {showAll ? "Daha az göster" : `${findings.length - 6} bulgu daha göster`}
        </button>
      )}
    </section>
  );
}

function PulseStrip({ model, rates, info }) {
  const { wrates, label } = useContext(WinCtx);
  const live = Object.keys(rates).length > 0;
  const fes = model.frontends;
  const sum = (a) => a.reduce((x, y) => x + (y || 0), 0);
  const reqps = sum(fes.map((f) => rpsOf(f, rates)));
  const totReq = sum(fes.map((f) => num(f.req_tot) ?? num(f.stot)));
  const scur = sum(fes.map((f) => num(f.scur)));
  const maxconn = num(info.Maxconn);
  const err = errRatio(fes, wrates);
  const servers = model.backends.flatMap((b) => b.servers);
  const healthy = servers.filter((s) => HEALTHY.has(kindOf(s.status))).length;
  const others = {};
  servers.forEach((s) => { const k = kindOf(s.status); if (!HEALTHY.has(k)) others[k] = (others[k] || 0) + 1; });
  const othersText = Object.entries(others).map(([k, n]) => `${n} ${STATUS_LABEL[k].toLocaleLowerCase("tr-TR")}`).join(", ");
  const traffic = live
    ? { value: `${fmtBits(sum(fes.map((f) => rates[keyOf(f)]?.bout)))} giden`, sub: `${fmtBits(sum(fes.map((f) => rates[keyOf(f)]?.bin)))} gelen` }
    : { value: `${fmtBytes(sum(fes.map((f) => num(f.bout))))} giden`, sub: `${fmtBytes(sum(fes.map((f) => num(f.bin))))} gelen, açıldığından beri` };
  const items = [
    { k: "_reqps", value: fmtRate(reqps), sub: `Toplam ${fmtNum(totReq)} istek` },
    { k: "scur", value: fmtNum(scur), sub: maxconn ? `Genel sınırın ${fmtPct(scur / maxconn)} kadarı` : "Tüm frontend'lerde" },
    { k: "_err5", value: fmtPct(err), sub: cap(label), color: err > 0.02 ? C.warn : null },
    { k: "_traffic", ...traffic },
    { k: "_healthy", value: `${healthy} / ${servers.length}`, sub: othersText || "Hepsi çalışıyor", color: healthy < servers.length ? C.warn : null },
    { k: "_uptime", value: fmtDur(num(info.Uptime_sec)), sub: info.Nbthread ? `${info.Nbthread} iş parçacığı` : "" },
  ];
  return (
    <section className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-px rounded-lg overflow-hidden" style={{ background: C.line, border: `1px solid ${C.line}` }}>
      {items.map((it) => (
        <div key={it.k} className="px-4 py-4" style={{ background: C.panel }}>
          <div className="text-sm" style={{ color: C.muted }}><Term k={it.k} /></div>
          <div className="text-2xl font-semibold tnum mt-1" style={{ color: it.color || C.text }}>{it.value}</div>
          <div className="text-xs mt-1" style={{ color: C.faint }}>{it.sub}</div>
        </div>
      ))}
    </section>
  );
}

function TrafficCharts({ points }) {
  const { label, minutes } = useContext(WinCtx);
  if (points.length < 2) {
    return (
      <Panel title="Zaman içindeki trafik" note="Grafikler birkaç saniye içinde dolmaya başlar. Ajan son 1 saati hafızada tutar.">
        <div style={{ height: 60 }} />
      </Panel>
    );
  }
  const history = points.map((p) => ({
    t: new Date(p.t).toLocaleTimeString("tr-TR", minutes > 5 ? { hour: "2-digit", minute: "2-digit" } : { hour: "2-digit", minute: "2-digit", second: "2-digit" }),
    "2xx": Math.round(p.c2 * 10) / 10, "3xx": Math.round(p.c3 * 10) / 10, "4xx": Math.round(p.c4 * 10) / 10, "5xx": Math.round(p.c5 * 10) / 10,
    Gelen: Math.round((p.in * 8) / 1e5) / 10, Giden: Math.round((p.out * 8) / 1e5) / 10,
  }));
  const tick = { fill: C.faint, fontSize: 11 };
  const tip = {
    contentStyle: { background: C.panel2, border: `1px solid ${C.line}`, borderRadius: 6, color: C.text, fontSize: 12 },
    labelStyle: { color: C.muted },
  };
  const Legend = ({ items }) => (
    <div className="flex flex-wrap gap-x-4 gap-y-1 mt-2 text-xs" style={{ color: C.muted }}>
      {items.map(([n, col]) => (
        <span key={n} className="inline-flex items-center gap-2"><span style={{ width: 10, height: 3, background: col, borderRadius: 2 }} />{n}</span>
      ))}
    </div>
  );
  const codeSeries = [["2xx", "Başarılı", C.ok], ["3xx", "Yönlendirme", C.info], ["4xx", "İstemci hatası", C.warn], ["5xx", "Sunucu hatası", C.bad]];
  return (
    <div className="grid gap-4 md:grid-cols-2">
      <Panel title="Saniyedeki istek, yanıt türüne göre" note={`${cap(label)}. En üstteki kırmızı şerit ne kadar kalınsa o kadar çok sunucu hatası var.`}>
        <ResponsiveContainer width="100%" height={210}>
          <AreaChart data={history} margin={{ top: 6, right: 6, left: -14, bottom: 0 }}>
            <CartesianGrid stroke={C.line} vertical={false} />
            <XAxis dataKey="t" tick={tick} tickLine={false} axisLine={false} minTickGap={48} />
            <YAxis tick={tick} tickLine={false} axisLine={false} width={48} />
            <Tooltip {...tip} formatter={(v, name) => [`${fmtRate(v)} istek/sn`, name]} />
            {codeSeries.map(([key, name, col]) => (
              <Area key={key} type="monotone" dataKey={key} name={name} stackId="1" stroke={col} fill={col} fillOpacity={0.22} isAnimationActive={false} dot={false} />
            ))}
          </AreaChart>
        </ResponsiveContainer>
        <Legend items={codeSeries.map(([, n, c]) => [n, c])} />
      </Panel>
      <Panel title="Trafik" note={`${cap(label)}. İstemcilere giden ve onlardan gelen veri, megabit/saniye.`}>
        <ResponsiveContainer width="100%" height={210}>
          <AreaChart data={history} margin={{ top: 6, right: 6, left: -14, bottom: 0 }}>
            <CartesianGrid stroke={C.line} vertical={false} />
            <XAxis dataKey="t" tick={tick} tickLine={false} axisLine={false} minTickGap={48} />
            <YAxis tick={tick} tickLine={false} axisLine={false} width={48} />
            <Tooltip {...tip} formatter={(v, name) => [`${trDec(v)} Mbit/sn`, name]} />
            <Area type="monotone" dataKey="Giden" stroke={C.info} fill={C.info} fillOpacity={0.2} isAnimationActive={false} dot={false} />
            <Area type="monotone" dataKey="Gelen" stroke={C.maint} fill={C.maint} fillOpacity={0.2} isAnimationActive={false} dot={false} />
          </AreaChart>
        </ResponsiveContainer>
        <Legend items={[["Giden", C.info], ["Gelen", C.maint]]} />
      </Panel>
    </div>
  );
}

function RangePicker({ minutes, setMinutes }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 mt-8 mb-3">
      <p className="text-sm" style={{ color: C.muted }}>Grafikler, 5xx oranları, yanıt türleri ve log bölümü bu zaman aralığını kullanır.</p>
      <div className="flex gap-1" role="group" aria-label="Zaman aralığı">
        {RANGES.map(([m, t]) => (
          <button key={m} type="button" onClick={() => setMinutes(m)} aria-pressed={minutes === m}
            className="rounded-md px-3 py-1.5 text-sm"
            style={minutes === m ? { background: C.panel2, color: C.text, border: `1px solid ${C.info}` } : { color: C.muted, border: `1px solid ${C.line}` }}>
            {t}
          </button>
        ))}
      </div>
    </div>
  );
}

// Listeler 2-5 saniyede bir yenilendiği için sıralama da sürekli değişiyordu; okurken
// satırlar yer değiştirip sayfa kayıyordu. Bu kanca sırayı hatırlar: bir satır açıkken
// (kullanıcı incelerken) sıra hiç değişmez, yeni gelenler sona eklenir.
function mergeOrder(prev, present, desired, frozen) {
  if (!frozen) return desired;
  const kept = prev.filter((k) => present.has(k));
  const seen = new Set(kept);
  return [...kept, ...desired.filter((k) => !seen.has(k))];
}

function useStableOrder(items, keyOf, desired, frozen) {
  const ref = useRef([]);
  const byKey = new Map(items.map((it) => [keyOf(it), it]));
  const order = mergeOrder(ref.current, new Set(byKey.keys()), desired, frozen);
  ref.current = order;
  return order.map((k) => byKey.get(k)).filter(Boolean);
}

// Sıralama ölçütü seçili aralıktaki istek sayısı: saniyelik değerin aksine yavaş değişir,
// bu yüzden sıra kendiliğinden de oynamaz.
function backendScore(b, wrates) {
  const kinds = b.servers.map((s) => kindOf(s.status));
  const dead = kinds.length > 0 && !kinds.some((k) => REACHABLE.has(k));
  const bad = kinds.some((k) => k === "down" || k === "failing" || k === "recovering") ||
    b.servers.some((s) => (wrates[keyOf(s)]?.econ || 0) > 0); // aralık toplamı: saniyelik değerin aksine oynamaz
  const n = wrates[keyOf(b)]?.n ?? num(b.req_tot) ?? num(b.stot) ?? 0;
  return (dead ? 2e9 : 0) + (bad ? 1e9 : 0) + n;
}

function BackendList({ model, rates, expanded, onToggle, onFields }) {
  const { wrates } = useContext(WinCtx);
  if (!model.backends.length) return <p style={{ color: C.faint }}>HAProxy'de backend yok.</p>;
  const desired = [...model.backends]
    .sort((a, b) => backendScore(b, wrates) - backendScore(a, wrates) || a.pxname.localeCompare(b.pxname, "tr"))
    .map((b) => b.pxname);
  const sorted = useStableOrder(model.backends, (b) => b.pxname, desired, expanded.size > 0);
  return (
    <div className="rounded-lg" style={{ background: C.panel, border: `1px solid ${C.line}` }}>
      <div className="be-head px-4 py-2 text-xs" style={{ color: C.muted }}>
        <span className="pl-6">Backend</span>
        <span>Sunucular</span>
        <span className="text-right"><Term k="req_rate" /></span>
        <span className="text-right"><Term k="_err5">5xx oranı</Term></span>
        <span className="text-right"><Term k="rtime">Yanıt süresi</Term></span>
        <span className="text-right"><Term k="qcur">Kuyruk</Term></span>
      </div>
      {sorted.map((b) => {
        const sv = b.servers;
        const open = expanded.has(b.pxname);
        const kinds = sv.map((s) => kindOf(s.status));
        const healthy = kinds.filter((k) => HEALTHY.has(k)).length;
        const allNoCheck = sv.length > 0 && kinds.every((k) => k === "nocheck");
        const dead = sv.length > 0 && !kinds.some((k) => REACHABLE.has(k));
        const err = errRatio([b], wrates);
        const rt = num(b.rtime);
        const q = num(b.qcur) || 0;
        return (
          <div key={b.pxname} id={`be-${b.pxname}`} style={{ borderTop: `1px solid ${C.line}` }}>
            <button type="button" onClick={() => onToggle(b.pxname)} aria-expanded={open} className="be-row hl-row w-full text-left px-4 py-3">
              <span className="flex items-center gap-2" style={{ minWidth: 0 }}>
                {open ? <ChevronDown size={16} color={C.muted} /> : <ChevronRight size={16} color={C.muted} />}
                <span className="font-medium truncate" style={{ color: dead ? C.bad : C.text }}>{b.pxname}</span>
              </span>
              <span className="flex items-center gap-2">
                <span className="flex gap-1">
                  {sv.map((s) => (
                    <span key={s.svname} title={`${s.svname}: ${STATUS_LABEL[kindOf(s.status)]}`}
                      style={{ width: 10, height: 10, borderRadius: 2, background: KIND_COLOR[kindOf(s.status)] }} />
                  ))}
                </span>
                <span className="text-xs tnum" style={{ color: C.muted }}>
                  {sv.length === 0 ? "sunucu yok" : allNoCheck ? "kontrol yok" : `${healthy}/${sv.length} sağlıklı`}
                </span>
              </span>
              <Metric label="İstek/sn" value={fmtRate(rpsOf(b, rates))} />
              <Metric label="5xx" value={fmtPct(err)} color={err > 0.02 ? C.warn : null} />
              <Metric label="Yanıt" value={fmtMs(rt)} color={rt > 800 ? C.warn : null} />
              <Metric label="Kuyruk" value={fmtNum(q)} color={q > 0 ? C.warn : null} />
            </button>
            {open && <BackendDetail b={b} rates={rates} onFields={onFields} />}
          </div>
        );
      })}
    </div>
  );
}

// Hangi adres hangi yanıt kodunu döndürüyor: 3xx / 4xx / 5xx sekmeleri.
// Yönlendirmeler de burada; HAProxy'nin kendi ürettiği http->https atlaması dahil.
const CLASSES = [
  { id: 3, key: "s3", ad: "Yönlendirme (3xx)", kisa: "3xx", col: C.info },
  { id: 4, key: "s4", ad: "İstemci hatası (4xx)", kisa: "4xx", col: C.warn },
  { id: 5, key: "s5", ad: "Sunucu hatası (5xx)", kisa: "5xx", col: C.bad },
];

// Üstteki iki şerit farklı soruları cevaplar: bu şerit "hangi yanıt kodu döndü",
// diğeri "isteğe ne oldu". Bir 500, HAProxy açısından "sunucu yanıtladı"dır.
const CLASS_CELLS = [
  ["Başarılı (2xx)", 0, C.ok],
  ["Yönlendirme (3xx)", 1, C.info],
  ["İstemci hatası (4xx)", 2, C.warn],
  ["Sunucu hatası (5xx)", 3, C.bad],
];

function ClassStrip({ classes, minutes }) {
  const c = classes || [0, 0, 0, 0];
  const toplam = c.reduce((a, b) => a + b, 0);
  return (
    <div className="grid grid-cols-2 md:grid-cols-4 gap-px rounded-lg overflow-hidden" style={{ background: C.line, border: `1px solid ${C.line}` }}>
      {CLASS_CELLS.map(([ad, i, col]) => (
        <div key={ad} className="px-4 py-4" style={{ background: C.panel }}>
          <div className="text-sm inline-flex items-center gap-2" style={{ color: C.muted }}>
            <span style={{ width: 8, height: 8, borderRadius: 2, background: col }} />{ad}
          </div>
          <div className="text-2xl font-semibold tnum mt-1" style={{ color: i === 3 && c[i] > 0 ? C.bad : C.text }}>{fmtNum(c[i])}</div>
          <div className="text-xs mt-1" style={{ color: C.faint }}>
            {toplam ? fmtPct(c[i] / toplam) : "—"}, {fmtRate(c[i] / minutes)}/dk
          </div>
        </div>
      ))}
    </div>
  );
}

function CodePathsPanel({ logs, openRows, toggleRow, frozen }) {
  const rows = logs.codePaths || [];
  // Gerçek toplamlar API'den gelir; rows listesi her sınıfın en yoğun 20 adresiyle sınırlıdır,
  // dolayısıyla sekme sayıları onun üzerinden hesaplanamaz.
  const cls = logs.classes || [0, 0, 0, 0];
  const toplam = { s3: cls[1], s4: cls[2], s5: cls[3] };
  // Varsayılan sekme: sunucu hatası varsa o, yoksa istemci hatası, o da yoksa yönlendirme
  const ilk = toplam.s5 > 0 ? 5 : toplam.s4 > 0 ? 4 : 3;
  const [sec, setSec] = useState(null);
  const aktif = CLASSES.find((c) => c.id === (sec ?? ilk)) || CLASSES[2];
  const liste = rows.filter((r) => r[aktif.key] > 0);
  const desired = [...liste].sort((a, b) => b[aktif.key] - a[aktif.key]).map((r) => `${r.backend}|${r.method}|${r.path}`);
  const sirali = useStableOrder(liste, (r) => `${r.backend}|${r.method}|${r.path}`, desired, frozen);
  const kodFiltre = (c) => Math.floor(c.code / 100) === aktif.id || (c.code === 0 && aktif.id === 5);
  return (
    <Panel title="Hangi adres ne döndürüyor"
      note="2xx dışındaki yanıtlar, sınıfına göre. Satıra tıklayınca tam adres (alan adı log'da varsa), gerçek yollar ve IP'ler açılır.">
      <div className="flex flex-wrap gap-1 mb-3" role="group" aria-label="Yanıt sınıfı">
        {CLASSES.map((c) => {
          const n = toplam[c.key];
          const on = c.id === aktif.id;
          return (
            <button key={c.id} type="button" onClick={() => setSec(c.id)} aria-pressed={on}
              className="rounded-md px-3 py-1.5 text-sm inline-flex items-center gap-2"
              style={on ? { background: C.panel2, color: C.text, border: `1px solid ${c.col}` } : { color: C.muted, border: `1px solid ${C.line}` }}>
              <span style={{ width: 8, height: 8, borderRadius: 2, background: c.col }} />
              {c.ad}
              <span className="tnum nw" style={{ color: on ? C.text : C.faint }}>{fmtNum(n)}</span>
            </button>
          );
        })}
      </div>
      {sirali.length > 0 && sirali.length >= 20 && (
        <p className="text-xs mb-2" style={{ color: C.faint }}>Bu sınıfta en çok görülen 20 adres listeleniyor.</p>
      )}
      {sirali.length === 0 ? (
        <p className="text-sm" style={{ color: C.faint }}>Bu aralıkta {aktif.ad.toLocaleLowerCase("tr-TR")} dönen adres yok.</p>
      ) : (
        <div className="text-sm">
          {sirali.slice(0, 20).map((e, i) => {
            const key = `c|${aktif.id}|${e.backend}|${e.method}|${e.path}`;
            const n = e[aktif.key];
            const oran = e.n > 0 ? n / e.n : 0;
            const etiket = KIND_TAG[e.kind];
            return (
              <ExpandRow key={key} first={i === 0} open={openRows.has(key)} onToggle={() => toggleRow(key)}
                head={
                  <>
                    <span className="flex items-baseline justify-between gap-3">
                      <span className="brk">
                        <span style={{ color: C.faint }}>{e.method} </span>{e.path}
                        <span className="text-xs ml-2" style={{ color: C.faint }}>{etiket ? `${e.backend} · ${etiket}` : e.backend}</span>
                      </span>
                      <span className="tnum nw" style={{ color: aktif.col }}>
                        {fmtNum(n)} {aktif.kisa}
                        <span className="text-xs ml-1" style={{ color: C.faint }}>/ {fmtNum(e.n)} ({fmtPct(oran)})</span>
                      </span>
                    </span>
                    <span className="flex flex-wrap gap-1.5 mt-1.5">
                      {(e.codes || []).filter(kodFiltre).map((c) => <CodeChip key={c.code} code={c.code} n={c.n} />)}
                    </span>
                  </>
                }>
                <ReqDetail d={e.detail} path={e.path} />
              </ExpandRow>
            );
          })}
        </div>
      )}
    </Panel>
  );
}

function LogSection({ logs, minutes }) {
  const [openRows, setOpenRows] = useState(() => new Set());
  const toggleRow = (key) => setOpenRows((cur) => {
    const n = new Set(cur);
    if (n.has(key)) n.delete(key); else n.add(key);
    return n;
  });
  const frozen = openRows.size > 0;
  const pathKey = (x) => `${x.backend}|${x.method}|${x.path}`;
  const blockKey = (x) => `${x.kind}|${x.method}|${x.path}`;
  const paths = useStableOrder(logs?.paths || [], pathKey, (logs?.paths || []).map(pathKey), frozen);
  const blocked = useStableOrder(logs?.blocked || [], blockKey, (logs?.blocked || []).map(blockKey), frozen);
  const clients = useStableOrder(logs?.clients || [], (c) => c.ip, (logs?.clients || []).map((c) => c.ip), frozen);
  if (!logs) return null;
  if (!logs.enabled) {
    return (
      <section id="logs" className="mt-12">
        {logs.searching ? (
          <Panel title="HAProxy log'u aranıyor"
            note={`${logs.error || "Henüz okunabilir bir HAProxy log'u bulunamadı."} Ajan birkaç dakikada bir tekrar arıyor; log oluşunca bu bölüm kendiliğinden dolar. Stats bölümü etkilenmez.`} />
        ) : (
          <Panel title="Log analizi bu sunucuda kapalı"
            note={`${logs.error || "Ajan log analizi kapalı olarak başlatılmış."} Yukarıdaki stats bölümü bundan etkilenmez.`} />
        )}
      </section>
    );
  }
  const kinds = logs.kinds || {};
  const total = KIND_ORDER.reduce((a, k) => a + (kinds[k] || 0), 0);
  const perMin = (n) => `${fmtRate(n / minutes)}/dk`;
  const ago = logs.lastAt ? (Date.now() - logs.lastAt) / 1000 : null;
  return (
    <section id="logs">
      <div className="flex flex-wrap items-end justify-between gap-3 mt-12 mb-3">
        <div>
          <h2 className="text-xl font-semibold" style={{ letterSpacing: "-0.01em" }}>Log'dan gelenler</h2>
          <p className="text-sm mt-1" style={{ color: C.muted }}>
            {minutes >= 60 ? `Son 1${NB}saatte` : `Son ${minutes}${NB}dakikada`} {fmtNum(total)} istek.
            {ago != null ? ` En son satır ${fmtDur(Math.max(0, ago))} önce.` : " Henüz satır okunmadı."}
            {" "}Sorgu parametreleri (?...) saklanmaz.
            {logs.source ? <span style={{ color: C.faint }}> Kaynak: {logs.source.replace(/^file:/, "").replace(/^journal:/, "journald, ")}.</span> : null}
            {total > 0 && (
              <span style={{ color: C.faint }}>
                {" "}{logs.hostLines === 0
                  ? "Bu LB'nin log biçiminde alan adı yok; ayrıntılarda yol ve IP görünür."
                  : logs.hostLines >= total * 0.9
                    ? "Log'da alan adı var; ayrıntılarda tam adres görünür."
                    : `Alan adı satırların sadece ${fmtPct(logs.hostLines / total)} kadarında var.`}
              </span>
            )}
          </p>
        </div>
      </div>
      {logs.error && (
        <div className="rounded-md px-4 py-3 mb-4 text-sm" style={{ background: C.panel, boxShadow: `inset 3px 0 0 ${C.bad}` }}>
          Log dosyası okunamıyor: {logs.error}
        </div>
      )}
      <h3 className="text-sm font-medium mb-2">Hangi yanıt kodu döndü</h3>
      <ClassStrip classes={logs.classes} minutes={minutes} />

      <h3 className="text-sm font-medium mb-2 mt-6">İsteğe ne oldu</h3>
      <p className="text-xs mb-2" style={{ color: C.muted }}>
        Bu şerit isteğin nereye gittiğini anlatır, hangi kodu aldığını değil: sunucu 500 döndürdüyse istek yine
        "Sunucu yanıtladı" sayılır. Kod dökümü için yukarıdaki şeride ve aşağıdaki "Hangi adres ne döndürüyor" bölümüne bak.
      </p>
      <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-px rounded-lg overflow-hidden" style={{ background: C.line, border: `1px solid ${C.line}` }}>
        {KIND_ORDER.map((k) => {
          const [label, col, desc] = KIND[k];
          const n = kinds[k] || 0;
          return (
            <div key={k} className="px-4 py-4" style={{ background: C.panel }} title={desc}>
              <div className="text-sm inline-flex items-center gap-2" style={{ color: C.muted }}>
                <span style={{ width: 8, height: 8, borderRadius: 2, background: col }} />{label}
              </div>
              <div className="text-2xl font-semibold tnum mt-1" style={{ color: n && (k === "nomatch" || k === "noserver") ? C.bad : C.text }}>{fmtNum(n)}</div>
              <div className="text-xs mt-1" style={{ color: C.faint }}>{total ? fmtPct(n / total) : "—"}, {perMin(n)}</div>
            </div>
          );
        })}
      </div>

      <div className="grid gap-4 lg:grid-cols-2 mt-4">
        <Panel title="En çok istenen adresler" note="Tüm istekler: sunucuya ulaşanlar, yönlendirilenler ve engellenenler. Sayı içeren yol parçaları {id} olarak birleştirildi.">
          {(logs.paths || []).length === 0 ? <p className="text-sm" style={{ color: C.faint }}>Bu aralıkta kayıt yok.</p> : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm" style={{ borderCollapse: "collapse", minWidth: 520 }}>
                <thead>
                  <tr className="text-xs" style={{ color: C.muted }}>
                    <th className="py-2 pr-3 font-normal text-left">Adres</th>
                    <th className="py-2 pr-3 font-normal text-right">İstek</th>
                    <th className="py-2 pr-3 font-normal text-right">2xx</th>
                    <th className="py-2 pr-3 font-normal text-right">3xx</th>
                    <th className="py-2 pr-3 font-normal text-right">4xx</th>
                    <th className="py-2 pr-3 font-normal text-right">5xx</th>
                    <th className="py-2 font-normal text-right">Ort. süre</th>
                  </tr>
                </thead>
                <tbody>
                  {paths.slice(0, 20).map((p, i) => {
                    const e5 = p.n ? p.s5 / p.n : 0;
                    return (
                      <tr key={i} style={{ borderTop: `1px solid ${C.line}` }}>
                        <td className="py-2 pr-3 align-top brk" style={{ maxWidth: 360 }}>
                          <span style={{ color: C.faint }}>{p.method} </span>{p.path}
                          <div className="text-xs" style={{ color: C.faint }}>{p.backend}</div>
                        </td>
                        <td className="py-2 pr-3 text-right tnum nw align-top">{fmtNum(p.n)}<div className="text-xs" style={{ color: C.faint }}>{perMin(p.n)}</div></td>
                        <td className="py-2 pr-3 text-right tnum nw align-top" style={{ color: p.s2 ? C.ok : C.faint }}>{p.s2 ? fmtNum(p.s2) : "—"}</td>
                        <td className="py-2 pr-3 text-right tnum nw align-top" style={{ color: p.s3 ? C.info : C.faint }}>{p.s3 ? fmtNum(p.s3) : "—"}</td>
                        <td className="py-2 pr-3 text-right tnum nw align-top" style={{ color: p.s4 ? C.warn : C.faint }}>{p.s4 ? fmtNum(p.s4) : "—"}</td>
                        <td className="py-2 pr-3 text-right tnum nw align-top" style={{ color: p.s5 ? C.bad : C.faint }}>
                          {p.s5 ? fmtNum(p.s5) : "—"}
                          {p.s5 > 0 && <div className="text-xs" style={{ color: C.faint }}>{fmtPct(e5)}</div>}
                        </td>
                        <td className="py-2 text-right tnum nw align-top">{p.avgMs ? fmtMs(p.avgMs) : "—"}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </Panel>

        <Panel title="Engellenen ve karşılıksız kalan istekler"
          note="403 alanlar ve hiçbir backend'e yönlendirilemeyip 503 alanlar. Satıra tıklayınca tam adres (alan adı log'da varsa), gerçek yollar ve isteği gönderen IP'ler açılır.">
          {(logs.blocked || []).length === 0 ? <p className="text-sm" style={{ color: C.faint }}>Bu aralıkta engellenen ya da karşılıksız istek yok.</p> : (
            <div className="text-sm">
              {blocked.slice(0, 20).map((b, i) => {
                const key = `b|${b.kind}|${b.method}|${b.path}`;
                return (
                  <ExpandRow key={key} first={i === 0} open={openRows.has(key)} onToggle={() => toggleRow(key)}
                    head={
                      <span className="flex items-baseline justify-between gap-3">
                        <span className="brk"><span style={{ color: C.faint }}>{b.method} </span>{b.path}</span>
                        <span className="flex items-baseline gap-3 whitespace-nowrap">
                          <span className="inline-flex items-center gap-2 text-xs" style={{ color: C.muted }}>
                            <span style={{ width: 8, height: 8, borderRadius: 2, background: KIND[b.kind]?.[1] }} />{KIND[b.kind]?.[0] || b.kind}
                          </span>
                          <span className="tnum">{fmtNum(b.n)}</span>
                        </span>
                      </span>
                    }>
                    <ReqDetail d={b.detail} path={b.path} />
                  </ExpandRow>
                );
              })}
            </div>
          )}
        </Panel>
      </div>

      <div className="mt-4">
        <CodePathsPanel logs={logs} openRows={openRows} toggleRow={toggleRow} frozen={frozen} />
      </div>

      <div className="mt-4">
        <Panel title="En çok istek atan IP'ler"
          note="Log'daki IP'ler. Cloudflare üzerinden gelen isteklerde bu IP Cloudflare'e aittir ve etiketlenir; doğrudan gelenlerde kullanıcının kendi IP'sidir.">
          {(logs.clients || []).length === 0 ? <p className="text-sm" style={{ color: C.faint }}>Bu aralıkta kayıt yok.</p> : (
            <ul className="grid gap-x-8 md:grid-cols-2">
              {clients.slice(0, 20).map((c) => (
                <li key={c.ip} className="flex items-baseline justify-between gap-3 py-1.5 text-sm" style={{ borderTop: `1px solid ${C.line}` }}>
                  <span className="tnum">{c.ip}{c.cloudflare && <span className="text-xs ml-2" style={{ color: C.faint }}>Cloudflare</span>}</span>
                  <span className="tnum whitespace-nowrap">
                    {fmtNum(c.n)}
                    {c.blocked ? <span className="text-xs ml-2" style={{ color: C.warn }}>{fmtNum(c.blocked)} engellenen</span> : null}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </Panel>
      </div>
    </section>
  );
}

export default function App() {
  const [state, setState] = useState(null);
  const [fetchErr, setFetchErr] = useState(null);
  const [logs, setLogs] = useState(null);
  const [minutes, setMinutes] = useState(60);
  const [running, setRunning] = useState(true);
  const [expanded, setExpanded] = useState(() => new Set());
  const [fieldsRow, setFieldsRow] = useState(null);
  const [cfg, setCfg] = useState(null);

  useEffect(() => {
    let alive = true;
    const load = async () => {
      try {
        const r = await fetch("/api/config", { cache: "no-store" });
        const j = await r.json();
        if (alive) setCfg(j);
      } catch (e) { /* state isteği hatayı zaten gösteriyor */ }
    };
    load();
    const id = setInterval(load, 30000);
    return () => { alive = false; clearInterval(id); };
  }, []);

  useEffect(() => {
    if (!running) return undefined;
    let alive = true;
    const load = async () => {
      try {
        const r = await fetch(`/api/state?minutes=${minutes}`, { cache: "no-store" });
        const j = await r.json();
        if (alive) { setState(j); setFetchErr(null); }
      } catch (e) {
        if (alive) setFetchErr("Ajana ulaşılamıyor. Servis çalışıyor mu, SSH tüneli açık mı?");
      }
    };
    load();
    const id = setInterval(load, TICK_SEC * 1000);
    return () => { alive = false; clearInterval(id); };
  }, [running, minutes]);

  useEffect(() => {
    if (!running) return undefined;
    let alive = true;
    const load = async () => {
      try {
        const r = await fetch(`/api/logs?minutes=${minutes}`, { cache: "no-store" });
        const j = await r.json();
        if (alive) setLogs(j);
      } catch (e) { /* state isteği hatayı zaten gösteriyor */ }
    };
    load();
    const id = setInterval(load, 5000);
    return () => { alive = false; clearInterval(id); };
  }, [running, minutes]);

  // Sekme başlığında sunucu adı: birden fazla LB açıkken hangisinin hangisi olduğu belli olsun
  useEffect(() => {
    const node = state?.cur?.info?.node;
    document.title = node ? `${node} · haproxy-lens` : "haproxy-lens";
  }, [state]);

  const cur = state?.cur;
  const prev = state?.prev;
  const model = useMemo(() => buildModel(cur?.rows || []), [cur]);
  const rates = useMemo(() => (cur && prev ? computeRates(prev.rows, cur.rows, (cur.at - prev.at) / 1000) : {}), [cur, prev]);
  const wrates = useMemo(() => windowToRates(state?.window), [state]);
  const label = winLabel(state?.window, minutes);
  const win = useMemo(() => ({ wrates, label, minutes }), [wrates, label, minutes]);
  const findings = useMemo(() => buildFindings(model, rates, logs, wrates, label), [model, rates, logs, wrates, label]);
  const points = state?.history || [];

  const toggle = (name) => setExpanded((s) => {
    const n = new Set(s);
    if (n.has(name)) n.delete(name); else n.add(name);
    return n;
  });
  const jump = (target) => {
    if (target === "logs") { document.getElementById("logs")?.scrollIntoView({ behavior: "smooth", block: "start" }); return; }
    if (target.startsWith("fe:")) {
      document.getElementById(`fe-${target.slice(3)}`)?.scrollIntoView({ behavior: "smooth", block: "center" });
      return;
    }
    setExpanded((s) => new Set(s).add(target));
    setTimeout(() => document.getElementById(`be-${target}`)?.scrollIntoView({ behavior: "smooth", block: "start" }), 60);
  };

  const problem = fetchErr || (state && !state.ok ? `HAProxy'den veri alınamıyor: ${state.error || "bilinmeyen hata"}` : null);

  return (
    <WinCtx.Provider value={win}>
    <div className="hl-root" style={{ background: C.bg, color: C.text, minHeight: "100vh" }}>
      <style>{CSS}</style>
      <div className="max-w-6xl mx-auto px-4 py-6 md:px-8 md:py-8">
        <Header info={cur?.info} running={running} onToggleRun={() => setRunning((r) => !r)} ok={!problem} lastAt={cur?.at} agentVersion={cfg?.version} />
        {problem && (
          <div className="mt-6 rounded-md px-4 py-3 text-sm leading-relaxed" style={{ background: C.panel, boxShadow: `inset 3px 0 0 ${C.bad}` }}>
            <div>{problem}</div>
            {state && !state.ok && (
              <div className="mt-1" style={{ color: C.muted }}>
                En sık sebep: servis kullanıcısının socket'e erişimi yok. Kontrol için: <code>ls -l /run/haproxy/admin.sock</code> ve <code>journalctl -u haproxy-lens -n 20</code>
              </div>
            )}
            {cur && <div className="mt-1" style={{ color: C.faint }}>Aşağıdaki veriler son başarılı okumadan ({new Date(cur.at).toLocaleTimeString("tr-TR")}).</div>}
          </div>
        )}
        {!cur ? (
          <p className="mt-10" style={{ color: C.muted }}>{problem ? "" : "HAProxy'den ilk veri bekleniyor..."}</p>
        ) : (
          <>
            <StatusHero findings={findings} model={model} onJump={jump} />
            <ConfigNotes cfg={cfg} />
            <PulseStrip model={model} rates={rates} info={cur.info} />
            <RangePicker minutes={minutes} setMinutes={setMinutes} />
            <TrafficCharts points={points} />

            <SectionTitle title="Backend'ler" sub={`Sorunlu olanlar ve en yoğunlar üstte. İstek/sn şu anki değer; 5xx oranı ${label} için. Satıra tıkla, sunucuları gör; sunucu adına tıklarsan HAProxy'nin verdiği tüm alanlar açılır.`} />
            <BackendList model={model} rates={rates} expanded={expanded} onToggle={toggle} onFields={setFieldsRow} />

            <LogSection logs={logs} minutes={minutes} />

            <SectionTitle title="Frontend'ler" sub="Kullanıcıların bağlandığı giriş noktaları." />
            <FrontendTable model={model} rates={rates} onFields={setFieldsRow} />

            <div className="mt-12"><TopBackends model={model} rates={rates} /></div>

            <p className="mt-10 pb-4 text-xs leading-relaxed" style={{ color: C.faint }}>
              Bu panel HAProxy'ye yalnızca "show info" ve "show stat" komutlarını gönderir ve log dosyasını sadece okur.
              Bir terimin ne demek olduğunu görmek için yanındaki bilgi simgesine gel.
            </p>
          </>
        )}
      </div>
      {fieldsRow && <FieldsModal row={fieldsRow} onClose={() => setFieldsRow(null)} />}
    </div>
    </WinCtx.Provider>
  );
}
