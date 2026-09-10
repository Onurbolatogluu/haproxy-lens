import { useState, useEffect, useRef, useMemo } from "react";
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
  _err5: ["Sunucu hatası oranı", "Yanıtların yüzde kaçının 5xx (sunucu hatası) olduğu.", "Hesaplanan: hrsp_5xx / tüm yanıtlar"],
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


// ---------------- Biçimlendirme ----------------
const nf = new Intl.NumberFormat("tr-TR");
const num = (v) => (v === undefined || v === null || v === "" || !Number.isFinite(Number(v)) ? null : Number(v));
const trDec = (x, d = 1) => x.toFixed(d).replace(".", ",");
const fmtNum = (v) => (v == null ? "—" : nf.format(Math.round(v)));
const fmtRate = (v) => (v == null ? "—" : v < 10 ? trDec(v) : nf.format(Math.round(v)));
const fmtPct = (x) => (x == null ? "—" : `%${trDec(x * 100)}`);
function fmtBytes(b) {
  if (b == null) return "—";
  const u = ["B", "KB", "MB", "GB", "TB", "PB"];
  let i = 0;
  while (b >= 1024 && i < u.length - 1) { b /= 1024; i++; }
  return `${i > 0 && b < 10 ? trDec(b) : Math.round(b)} ${u[i]}`;
}

function fmtBits(bytesPerSec) {
  if (bytesPerSec == null) return "—";
  let v = bytesPerSec * 8;
  const u = ["bit/sn", "Kbit/sn", "Mbit/sn", "Gbit/sn"];
  let i = 0;
  while (v >= 1000 && i < u.length - 1) { v /= 1000; i++; }
  return `${v < 10 ? trDec(v) : Math.round(v)} ${u[i]}`;
}

function fmtDur(sec) {
  if (sec == null || sec < 0) return "—";
  sec = Math.floor(sec);
  const d = Math.floor(sec / 86400), h = Math.floor((sec % 86400) / 3600), m = Math.floor((sec % 3600) / 60), s = sec % 60;
  if (d) return h ? `${d} gün ${h} saat` : `${d} gün`;
  if (h) return m ? `${h} saat ${m} dk` : `${h} saat`;
  if (m) return s ? `${m} dk ${s} sn` : `${m} dk`;
  return `${s} sn`;
}

const fmtMs = (ms) => (ms == null ? "—" : ms >= 1000 ? `${trDec(ms / 1000)} sn` : `${Math.round(ms)} ms`);

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
    const w = 260;
    setPos({ top: r.bottom + 6, left: Math.min(Math.max(8, r.left), window.innerWidth - w - 8), w });
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
      <div className="tnum">{fmtNum(value)} <span style={{ color: C.faint }}>/ {fmtNum(max)}</span></div>
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
          {note && <p className="text-xs mt-1 leading-relaxed" style={{ color: C.muted, maxWidth: "60ch" }}>{note}</p>}
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
function CodeBar({ row, rates, compact }) {
  const c = codeCounts(row, rates);
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
        Yanıt türleri {rates[keyOf(row)] ? `(son ${TICK_SEC} saniye)` : "(açıldığından beri)"}
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
  if (!servers.length) return <p className="text-sm" style={{ color: C.faint }}>Bu backend'de sunucu yok.</p>;
  const td = "py-2.5 pr-4 align-top";
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm" style={{ borderCollapse: "collapse", minWidth: 940 }}>
        <thead>
          <tr className="text-xs" style={{ color: C.muted }}>
            <Th>Sunucu</Th>
            <Th k="status">Durum</Th>
            <Th k="check_status">Sağlık kontrolü</Th>
            <Th k="weight" right>Ağırlık</Th>
            <Th k="scur">Açık bağlantı</Th>
            <Th k="req_rate" right>İstek/sn</Th>
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
                <td className={`${td} text-right tnum`}>{fmtNum(num(s.weight))}</td>
                <td className={td}><Meter value={num(s.scur) || 0} max={num(s.slim)} /></td>
                <td className={`${td} text-right tnum`}>{fmtRate(rpsOf(s, rates))}</td>
                <td className={`${td} text-right tnum`}>{fmtMs(num(s.rtime))}</td>
                <td className={`${td} text-right tnum`} style={{ color: econ > 0 ? C.warn : C.text }}>{fmtNum(econ)}</td>
                <td className={`${td} text-right tnum`}>{fmtNum(num(s.eresp))}</td>
                <td className={`${td} text-right tnum`}>{fmtNum(num(s.chkdown))}</td>
                <td className={`${td} text-right tnum`}>{downtime === 0 ? "Hiç" : fmtDur(downtime)}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
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
        <Fact k="scur">{fmtNum(num(b.scur))} <span style={{ color: C.faint }}>(en fazla {fmtNum(num(b.smax))})</span></Fact>
        <Fact k="bout">{fmtBytes(num(b.bout))} <span style={{ color: C.faint }}>({fmtBytes(num(b.bin))} gelen)</span></Fact>
        <Fact k="qtime">{fmtMs(num(b.qtime))}</Fact>
        <Fact k="ctime">{fmtMs(num(b.ctime))}</Fact>
        <Fact k="rtime">{fmtMs(num(b.rtime))}</Fact>
        <Fact k="ttime">{fmtMs(num(b.ttime))}</Fact>
        <Fact k="econ">{fmtNum(num(b.econ))}</Fact>
        <Fact k="eresp">{fmtNum(num(b.eresp))}</Fact>
        <Fact k="wredis">{fmtNum(num(b.wredis))}</Fact>
        <Fact k="cli_abrt">{fmtNum(num(b.cli_abrt))}</Fact>
      </dl>
      <CodeBar row={b} rates={rates} />
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
              <td className={`${td} text-right tnum`}>{fmtRate(rpsOf(f, rates))}</td>
              <td className={`${td} text-right tnum`}>{fmtNum(num(f.req_tot) ?? num(f.stot))}</td>
              <td className={`${td} text-right tnum`}>{fmtNum(num(f.dreq))}</td>
              <td className={`${td} text-right tnum`}>{fmtNum(num(f.ereq))}</td>
              <td className={`${td} text-right tnum whitespace-nowrap`}>{fmtBytes(num(f.bout))} / {fmtBytes(num(f.bin))}</td>
              <td className={td} style={{ minWidth: 120 }}><CodeBar row={f} rates={rates} compact /></td>
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
        <span className="truncate" style={{ minWidth: 0 }}>
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
  const live = model.backends.some((b) => rates[keyOf(b)]);
  const items = model.backends
    .map((b) => ({ name: b.pxname, rps: rpsOf(b, rates) || 0, tot: num(b.req_tot) ?? num(b.stot) ?? 0 }))
    .sort((a, b) => (live ? b.rps - a.rps : b.tot - a.tot));
  const total = items.reduce((a, b) => a + (live ? b.rps : b.tot), 0) || 1;
  return (
    <Panel title="En çok istek alan backend'ler" note={live ? "Şu anki saniyelik isteğe göre sıralı." : "HAProxy açıldığından beri toplam isteğe göre sıralı."}>
      <ul>
        {items.map((it) => {
          const v = live ? it.rps : it.tot;
          return (
            <RankRow key={it.name} label={it.name} sub={`toplam ${fmtNum(it.tot)}`}
              value={live ? `${fmtRate(it.rps)}/sn, ${fmtPct(v / total)}` : fmtPct(v / total)} share={v / total} color={C.info} />
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
            <div className="text-lg font-semibold">{row.pxname} / {row.svname}</div>
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
                    <td className="py-2 pr-4 align-top tnum">{fmtField(k, v, row)}</td>
                    <td className="py-2 pr-4 align-top tnum" style={{ color: C.muted }}>{v === "" ? "boş" : String(v)}</td>
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

function buildFindings(model, rates, logs) {
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
      const ec = rates[keyOf(s)]?.econ;
      if (ec > 0) add(k === "nocheck" ? "bad" : "warn", name, `${s.svname} (${name}) sunucusuna şu an bağlanılamıyor (saniyede ${fmtRate(ec)} başarısız deneme).${k === "nocheck" ? " Sağlık kontrolü olmadığı için HAProxy onu devre dışı bırakmıyor, istekler ona gitmeye devam ediyor." : ""}`);
      const cd = num(s.chkdown);
      if (cd >= 5) add("warn", name, `${s.svname} (${name}) HAProxy açıldığından beri ${cd} kez düşüp kalktı. Kararsız çalışıyor olabilir.`);
    }
    const q = num(b.qcur);
    if (q > 0) add("warn", name, `${name} kuyruğunda ${fmtNum(q)} istek bekliyor. Sunucular bağlantı sınırına dayanmış, kapasite yetmiyor.`);
    const er = errRatio([b], rates);
    const rps = rpsOf(b, rates);
    if (er != null && er > 0.02 && (rps == null || rps >= 1)) add("warn", name, `${name} yanıtlarının ${fmtPct(er)} kadarı sunucu hatası (5xx).`);
    const rt = num(b.rtime);
    if (rt != null && rt > 800) add("warn", name, `${name} yavaş: sunucular ortalama ${fmtMs(rt)} içinde yanıt veriyor.`);
  }
  for (const f of model.frontends) {
    const sc = num(f.scur), sl = num(f.slim);
    if (kindOf(f.status) === "full") add("bad", `fe:${f.pxname}`, `${f.pxname} bağlantı sınırına ulaştı, yeni bağlantılar bekletiliyor.`);
    else if (sl && sc / sl > 0.8) add("warn", `fe:${f.pxname}`, `${f.pxname} bağlantı sınırının ${fmtPct(sc / sl)} kadarını kullanıyor (${fmtNum(sc)} / ${fmtNum(sl)}).`);
  }
  if (logs?.enabled && logs.kinds) {
    const m = logs.minutes;
    const top = (kind) => (logs.blocked || []).find((x) => x.kind === kind);
    const nm = logs.kinds.nomatch || 0;
    if (nm > 0) {
      const t = top("nomatch");
      add("warn", "logs", `Son ${m} dakikada ${fmtNum(nm)} istek hiçbir backend'e eşleşmediği için 503 aldı.${t ? ` En çok: ${t.method} ${t.path} (${fmtNum(t.n)}).` : ""}`);
    }
    const ns = logs.kinds.noserver || 0;
    if (ns > 0) add("bad", "logs", `Son ${m} dakikada ${fmtNum(ns)} istek, seçilen backend'de çalışan sunucu olmadığı için 503 aldı.`);
  }
  if (nocheck > 0) add("info", null, `${total} sunucunun ${nocheck} tanesinde sağlık kontrolü (check) yok. Bunlar düşerse HAProxy fark etmez; panel onları "Kontrolsüz" gösterir ve bağlantı hatalarından yakalamaya çalışır.`);
  return out.sort((a, b) => LEVEL_ORDER[a.level] - LEVEL_ORDER[b.level]);
}

function Header({ info, running, onToggleRun, ok, lastAt }) {
  const up = num(info?.Uptime_sec);
  return (
    <header className="flex flex-wrap items-center justify-between gap-4 pb-5" style={{ borderBottom: `1px solid ${C.line}` }}>
      <div>
        <div className="text-lg font-semibold">haproxy-lens{info?.node ? ` ${info.node}` : ""}</div>
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
        <ul className="mt-6 space-y-2" style={{ maxWidth: 900 }}>
          {visible.map((f, i) => (
            <li key={`${f.target}-${i}`}>
              <button type="button" onClick={() => f.target && onJump(f.target)} className="hl-row w-full text-left flex gap-3 items-stretch rounded-md px-3 py-2.5"
                style={{ background: C.panel, cursor: f.target ? "pointer" : "default" }}>
                <span style={{ width: 3, borderRadius: 2, background: LEVEL_COLOR[f.level], flexShrink: 0 }} />
                <span className="text-sm leading-relaxed">{f.text}</span>
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
  const live = Object.keys(rates).length > 0;
  const fes = model.frontends;
  const sum = (a) => a.reduce((x, y) => x + (y || 0), 0);
  const reqps = sum(fes.map((f) => rpsOf(f, rates)));
  const totReq = sum(fes.map((f) => num(f.req_tot) ?? num(f.stot)));
  const scur = sum(fes.map((f) => num(f.scur)));
  const maxconn = num(info.Maxconn);
  const err = errRatio(fes, rates);
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
    { k: "_err5", value: fmtPct(err), sub: live ? "Son ölçümde" : "Açıldığından beri", color: err > 0.02 ? C.warn : null },
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
  if (points.length < 2) {
    return (
      <Panel title="Zaman içindeki trafik" note="Grafikler birkaç saniye içinde dolmaya başlar. Ajan son 1 saati hafızada tutar.">
        <div style={{ height: 60 }} />
      </Panel>
    );
  }
  const history = points.map((p) => ({
    t: new Date(p.t).toLocaleTimeString("tr-TR", { hour: "2-digit", minute: "2-digit", second: "2-digit" }),
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
      <Panel title="Saniyedeki istek, yanıt türüne göre" note="Son 10 dakika. En üstteki kırmızı şerit ne kadar kalınsa o kadar çok sunucu hatası var.">
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
      <Panel title="Trafik" note="İstemcilere giden ve onlardan gelen veri, megabit/saniye.">
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

function backendScore(b, rates) {
  const kinds = b.servers.map((s) => kindOf(s.status));
  const dead = kinds.length > 0 && !kinds.some((k) => REACHABLE.has(k));
  const bad = kinds.some((k) => k === "down" || k === "failing" || k === "recovering") || b.servers.some((s) => rates[keyOf(s)]?.econ > 0);
  return (dead ? 2e9 : 0) + (bad ? 1e9 : 0) + (rpsOf(b, rates) || 0);
}

function BackendList({ model, rates, expanded, onToggle, onFields }) {
  if (!model.backends.length) return <p style={{ color: C.faint }}>HAProxy'de backend yok.</p>;
  const sorted = [...model.backends].sort((a, b) => backendScore(b, rates) - backendScore(a, rates));
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
        const err = errRatio([b], rates);
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

function LogSection({ logs, minutes, setMinutes }) {
  if (!logs) return null;
  if (!logs.enabled) {
    return (
      <section id="logs" className="mt-12">
        <Panel title="Log analizi bu sunucuda kapalı"
          note={`${logs.error || "Kurulum sırasında okunabilir bir HAProxy log'u bulunamadı."} Yukarıdaki stats bölümü bundan etkilenmez.`} />
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
            Son {minutes} dakikada {fmtNum(total)} istek.
            {ago != null ? ` En son satır ${fmtDur(Math.max(0, ago))} önce.` : " Henüz satır okunmadı."}
            {" "}Sorgu parametreleri (?...) saklanmaz.
            {logs.source ? <span style={{ color: C.faint }}> Kaynak: {logs.source.replace(/^file:/, "").replace(/^journal:/, "journald, ")}.</span> : null}
          </p>
        </div>
        <div className="flex gap-1" role="group" aria-label="Zaman aralığı">
          {[5, 15, 60].map((m) => (
            <button key={m} type="button" onClick={() => setMinutes(m)} aria-pressed={minutes === m}
              className="rounded-md px-3 py-1.5 text-sm"
              style={minutes === m ? { background: C.panel2, color: C.text, border: `1px solid ${C.info}` } : { color: C.muted, border: `1px solid ${C.line}` }}>
              {m} dk
            </button>
          ))}
        </div>
      </div>
      {logs.error && (
        <div className="rounded-md px-4 py-3 mb-4 text-sm" style={{ background: C.panel, boxShadow: `inset 3px 0 0 ${C.bad}` }}>
          Log dosyası okunamıyor: {logs.error}
        </div>
      )}
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
        <Panel title="En çok istenen adresler" note="Sunucuya ulaşan istekler. Sayı içeren yol parçaları {id} olarak birleştirildi.">
          {(logs.paths || []).length === 0 ? <p className="text-sm" style={{ color: C.faint }}>Bu aralıkta kayıt yok.</p> : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm" style={{ borderCollapse: "collapse" }}>
                <thead>
                  <tr className="text-xs" style={{ color: C.muted }}>
                    <th className="py-2 pr-3 font-normal text-left">Adres</th>
                    <th className="py-2 pr-3 font-normal text-right">İstek</th>
                    <th className="py-2 pr-3 font-normal text-right">5xx</th>
                    <th className="py-2 font-normal text-right">Ort. süre</th>
                  </tr>
                </thead>
                <tbody>
                  {logs.paths.slice(0, 20).map((p, i) => {
                    const e5 = p.n ? p.s5 / p.n : 0;
                    return (
                      <tr key={i} style={{ borderTop: `1px solid ${C.line}` }}>
                        <td className="py-2 pr-3 align-top" style={{ maxWidth: 360, wordBreak: "break-all" }}>
                          <span style={{ color: C.faint }}>{p.method} </span>{p.path}
                          <div className="text-xs" style={{ color: C.faint }}>{p.backend}</div>
                        </td>
                        <td className="py-2 pr-3 text-right tnum align-top">{fmtNum(p.n)}<div className="text-xs" style={{ color: C.faint }}>{perMin(p.n)}</div></td>
                        <td className="py-2 pr-3 text-right tnum align-top" style={{ color: e5 > 0.02 ? C.warn : C.text }}>{p.s5 ? fmtPct(e5) : "—"}</td>
                        <td className="py-2 text-right tnum align-top">{p.avgMs ? fmtMs(p.avgMs) : "—"}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </Panel>

        <Panel title="Engellenen ve karşılıksız kalan istekler"
          note="403 alanlar ve hiçbir backend'e yönlendirilemeyip 503 alanlar. Varsayılan log biçiminde alan adı bulunmadığı için path gösteriliyor.">
          {(logs.blocked || []).length === 0 ? <p className="text-sm" style={{ color: C.faint }}>Bu aralıkta engellenen ya da karşılıksız istek yok.</p> : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm" style={{ borderCollapse: "collapse" }}>
                <thead>
                  <tr className="text-xs" style={{ color: C.muted }}>
                    <th className="py-2 pr-3 font-normal text-left">Adres</th>
                    <th className="py-2 pr-3 font-normal text-left">Ne oldu</th>
                    <th className="py-2 font-normal text-right">İstek</th>
                  </tr>
                </thead>
                <tbody>
                  {logs.blocked.slice(0, 20).map((b, i) => (
                    <tr key={i} style={{ borderTop: `1px solid ${C.line}` }}>
                      <td className="py-2 pr-3 align-top" style={{ maxWidth: 340, wordBreak: "break-all" }}>
                        <span style={{ color: C.faint }}>{b.method} </span>{b.path}
                      </td>
                      <td className="py-2 pr-3 align-top whitespace-nowrap">
                        <span className="inline-flex items-center gap-2">
                          <span style={{ width: 8, height: 8, borderRadius: 2, background: KIND[b.kind]?.[1] }} />{KIND[b.kind]?.[0] || b.kind}
                        </span>
                      </td>
                      <td className="py-2 text-right tnum align-top">{fmtNum(b.n)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Panel>
      </div>

      <div className="mt-4">
        <Panel title="En çok istek atan IP'ler"
          note="Log'daki IP'ler. Cloudflare üzerinden gelen isteklerde bu IP Cloudflare'e aittir ve etiketlenir; doğrudan gelenlerde kullanıcının kendi IP'sidir.">
          {(logs.clients || []).length === 0 ? <p className="text-sm" style={{ color: C.faint }}>Bu aralıkta kayıt yok.</p> : (
            <ul className="grid gap-x-8 md:grid-cols-2">
              {logs.clients.slice(0, 20).map((c) => (
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
  const [minutes, setMinutes] = useState(5);
  const [running, setRunning] = useState(true);
  const [expanded, setExpanded] = useState(() => new Set());
  const [fieldsRow, setFieldsRow] = useState(null);

  useEffect(() => {
    if (!running) return undefined;
    let alive = true;
    const load = async () => {
      try {
        const r = await fetch("/api/state", { cache: "no-store" });
        const j = await r.json();
        if (alive) { setState(j); setFetchErr(null); }
      } catch (e) {
        if (alive) setFetchErr("Ajana ulaşılamıyor. Servis çalışıyor mu, SSH tüneli açık mı?");
      }
    };
    load();
    const id = setInterval(load, TICK_SEC * 1000);
    return () => { alive = false; clearInterval(id); };
  }, [running]);

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

  const cur = state?.cur;
  const prev = state?.prev;
  const model = useMemo(() => buildModel(cur?.rows || []), [cur]);
  const rates = useMemo(() => (cur && prev ? computeRates(prev.rows, cur.rows, (cur.at - prev.at) / 1000) : {}), [cur, prev]);
  const findings = useMemo(() => buildFindings(model, rates, logs), [model, rates, logs]);
  const points = useMemo(() => (state?.history || []).slice(-300), [state]);

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
    <div className="hl-root" style={{ background: C.bg, color: C.text, minHeight: "100vh" }}>
      <style>{CSS}</style>
      <div className="max-w-6xl mx-auto px-4 py-6 md:px-8 md:py-8">
        <Header info={cur?.info} running={running} onToggleRun={() => setRunning((r) => !r)} ok={!problem} lastAt={cur?.at} />
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
            <PulseStrip model={model} rates={rates} info={cur.info} />
            <div className="mt-4"><TrafficCharts points={points} /></div>

            <SectionTitle title="Backend'ler" sub="Sorunlu olanlar ve en yoğunlar üstte. Satıra tıkla, sunucuları gör; sunucu adına tıklarsan HAProxy'nin verdiği tüm alanlar açılır." />
            <BackendList model={model} rates={rates} expanded={expanded} onToggle={toggle} onFields={setFieldsRow} />

            <LogSection logs={logs} minutes={minutes} setMinutes={setMinutes} />

            <SectionTitle title="Frontend'ler" sub="Kullanıcıların bağlandığı giriş noktaları." />
            <FrontendTable model={model} rates={rates} onFields={setFieldsRow} />

            <div className="mt-12"><TopBackends model={model} rates={rates} /></div>

            <p className="mt-10 pb-4 text-xs leading-relaxed" style={{ color: C.faint, maxWidth: "80ch" }}>
              Bu panel HAProxy'ye yalnızca "show info" ve "show stat" komutlarını gönderir ve log dosyasını sadece okur.
              Bir terimin ne demek olduğunu görmek için yanındaki bilgi simgesine gel.
            </p>
          </>
        )}
      </div>
      {fieldsRow && <FieldsModal row={fieldsRow} onClose={() => setFieldsRow(null)} />}
    </div>
  );
}
