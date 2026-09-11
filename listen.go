package main

// Panelin dinleyeceği IP'nin seçimi. Hiçbir şeyi değiştirmez, sadece okur:
//   1. Varsayılan rotanın geçtiği arayüzü bulur (/proc/net/route).
//   2. O arayüzün IPv4 adreslerini alır (ip -o -4 addr).
//   3. keepalived VIP'lerini atlar (VIP başka sunucuya geçince panel kopmasın).
//   4. Ağ ayarlarında (netplan, ifupdown, ifcfg, NetworkManager, systemd-networkd)
//      sabit tanımlı adresi tercih eder; yoksa arayüzün ilk (birincil) adresini alır.
//   5. Herkese açık bir IP seçmez; o durumda panel 127.0.0.1'de kalır.

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ifAddr struct {
	IP        string
	Prefix    int
	Secondary bool
	Label     string
}

type listenChoice struct {
	Iface   string
	IP      string   // boşsa 127.0.0.1 kullanılır
	Why     string   // neden bu adres
	Skipped []string // atlanan adresler ve sebepleri
	Note    string   // IP boşsa sebebi
}

var reIPv4 = regexp.MustCompile(`\b(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})\b`)

func detectListen() listenChoice {
	iface := defaultRouteIface()
	if iface == "" {
		return listenChoice{Note: "varsayılan rota bulunamadı"}
	}
	addrs, err := ifaceAddrs(iface)
	if err != nil || len(addrs) == 0 {
		return listenChoice{Iface: iface, Note: iface + " arayüzünde IPv4 adresi bulunamadı"}
	}
	vips := keepalivedVIPs()
	c := chooseListen(addrs, vips, staticConfigIPs())
	c.Iface = iface
	if c.IP == "" {
		if other := privateAddrsElsewhere(iface, vips); len(other) > 0 {
			c.Note += "; diğer arayüzlerdeki iç adresler: " + strings.Join(other, ", ")
		}
	}
	return c
}

// Sadece bilgi için: varsayılan arayüz dışındaki iç adresler (otomatik seçilmez).
func privateAddrsElsewhere(skip string, vips map[string]bool) []string {
	var out []string
	ifs, _ := net.Interfaces()
	for _, ifc := range ifs {
		if ifc.Name == skip || ifc.Flags&net.FlagLoopback != 0 || ifc.Flags&net.FlagUp == 0 {
			continue
		}
		as, _ := ifc.Addrs()
		for _, a := range as {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil && isPrivateIP(ipn.IP) && !vips[ipn.IP.String()] {
				out = append(out, ifc.Name+" "+ipn.IP.String())
			}
		}
	}
	return out
}

// Seçim kuralları; test edilebilsin diye dışarıdan bağımsız.
func chooseListen(addrs []ifAddr, vips, static map[string]bool) listenChoice {
	var c listenChoice
	var usable []ifAddr
	for _, a := range addrs {
		ip := net.ParseIP(a.IP)
		switch {
		case ip == nil:
			continue
		case vips[a.IP]:
			c.Skipped = append(c.Skipped, a.IP+" (keepalived VIP)")
		case ip.IsLinkLocalUnicast():
			c.Skipped = append(c.Skipped, a.IP+" (link-local)")
		default:
			usable = append(usable, a)
		}
	}
	pick := func(a ifAddr, why string) listenChoice {
		if !isPrivateIP(net.ParseIP(a.IP)) {
			c.Skipped = append(c.Skipped, a.IP+" (herkese açık IP; panel internete açılmasın diye seçilmedi)")
			c.Note = "sunucunun ana IP'si herkese açık bir adres"
			return c
		}
		c.IP, c.Why = a.IP, why
		for _, o := range usable {
			if o.IP != a.IP {
				c.Skipped = append(c.Skipped, o.IP+" (ana adres değil)")
			}
		}
		return c
	}
	// 1) Ağ ayarında sabit tanımlı ve ikincil olmayan adres
	for _, a := range usable {
		if static[a.IP] && !a.Secondary {
			return pick(a, "ağ ayarında sabit tanımlı ana adres")
		}
	}
	// 2) Arayüzün birincil adresi; /32, ikincil ve etiketli (eth0:1) adresler VIP olabilir
	for _, a := range usable {
		if !a.Secondary && a.Prefix != 32 && !strings.Contains(a.Label, ":") {
			return pick(a, "arayüzün birincil adresi")
		}
	}
	// 3) Kalan ilk adres
	if len(usable) > 0 {
		return pick(usable[0], "arayüzün ilk adresi")
	}
	c.Note = "kullanılabilir adres kalmadı"
	return c
}

func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsPrivate() || ip.IsLoopback() {
		return true
	}
	_, cgnat, _ := net.ParseCIDR("100.64.0.0/10")
	return cgnat.Contains(ip)
}

// /proc/net/route: hedefi 0.0.0.0/0 olan, metriği en düşük satırın arayüzü
func defaultRouteIface() string {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return ""
	}
	defer f.Close()
	best, bestMetric := "", -1
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) < 8 || fs[1] != "00000000" || fs[7] != "00000000" {
			continue
		}
		flags, _ := strconv.ParseInt(fs[3], 16, 64)
		if flags&1 == 0 { // RTF_UP
			continue
		}
		m, _ := strconv.Atoi(fs[6])
		if bestMetric < 0 || m < bestMetric {
			best, bestMetric = fs[0], m
		}
	}
	return best
}

// "ip -o -4 addr show dev X" çıktısı: adres sırası, ikincil bayrağı ve etiket bilgisi içerir.
func ifaceAddrs(iface string) ([]ifAddr, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ip", "-o", "-4", "addr", "show", "dev", iface).Output()
	if err == nil {
		return parseIPAddrOutput(string(out)), nil
	}
	// ip komutu yoksa Go'nun listesi (bayraksız)
	ifc, err2 := net.InterfaceByName(iface)
	if err2 != nil {
		return nil, fmt.Errorf("%v / %v", err, err2)
	}
	as, _ := ifc.Addrs()
	var res []ifAddr
	for _, a := range as {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
			p, _ := ipn.Mask.Size()
			res = append(res, ifAddr{IP: ipn.IP.String(), Prefix: p, Label: iface})
		}
	}
	return res, nil
}

func parseIPAddrOutput(s string) []ifAddr {
	var res []ifAddr
	for _, line := range strings.Split(s, "\n") {
		line = strings.SplitN(line, `\`, 2)[0]
		fs := strings.Fields(line)
		for i := 0; i+1 < len(fs); i++ {
			if fs[i] != "inet" {
				continue
			}
			ip, ipn, err := net.ParseCIDR(fs[i+1])
			if err != nil {
				break
			}
			p, _ := ipn.Mask.Size()
			a := ifAddr{IP: ip.String(), Prefix: p}
			for j := i + 2; j < len(fs); j++ {
				if fs[j] == "secondary" {
					a.Secondary = true
				}
			}
			a.Label = fs[len(fs)-1]
			res = append(res, a)
			break
		}
	}
	return res
}

// keepalived.conf (ve include ettiği dosyalar) içindeki virtual_ipaddress blokları
func keepalivedVIPs() map[string]bool {
	vips := map[string]bool{}
	seen := map[string]bool{}
	var read func(path string, depth int)
	read = func(path string, depth int) {
		if depth > 5 || seen[path] {
			return
		}
		seen[path] = true
		b, err := os.ReadFile(path)
		if err != nil {
			return
		}
		ips, includes := parseKeepalived(string(b))
		for _, ip := range ips {
			vips[ip] = true
		}
		for _, inc := range includes {
			if !filepath.IsAbs(inc) {
				inc = filepath.Join(filepath.Dir(path), inc)
			}
			m, _ := filepath.Glob(inc)
			sort.Strings(m)
			for _, f := range m {
				read(f, depth+1)
			}
		}
	}
	read("/etc/keepalived/keepalived.conf", 0)
	return vips
}

func parseKeepalived(s string) (ips []string, includes []string) {
	var toks []string
	for _, line := range strings.Split(s, "\n") {
		if i := strings.IndexAny(line, "#!"); i >= 0 {
			line = line[:i]
		}
		line = strings.NewReplacer("{", " { ", "}", " } ").Replace(line)
		toks = append(toks, strings.Fields(line)...)
	}
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t == "include" && i+1 < len(toks) {
			includes = append(includes, toks[i+1])
			continue
		}
		if (t == "virtual_ipaddress" || t == "virtual_ipaddress_excluded") && i+1 < len(toks) && toks[i+1] == "{" {
			depth := 0
			for j := i + 1; j < len(toks); j++ {
				switch toks[j] {
				case "{":
					depth++
				case "}":
					depth--
				default:
					ip := strings.SplitN(toks[j], "/", 2)[0]
					if net.ParseIP(ip) != nil && depth == 1 {
						ips = append(ips, ip)
					}
				}
				if depth == 0 {
					i = j
					break
				}
			}
		}
	}
	return ips, includes
}

// Ağ ayar dosyalarında geçen IPv4 adresleri. Sadece arayüzdeki adreslerle kesişimi kullanılır,
// bu yüzden ağ geçidi, DNS gibi başka adreslerin burada olması sorun değil.
func staticConfigIPs() map[string]bool {
	res := map[string]bool{}
	var files []string
	for _, g := range []string{
		"/etc/netplan/*.yaml", "/etc/netplan/*.yml",
		"/etc/network/interfaces", "/etc/network/interfaces.d/*",
		"/etc/sysconfig/network-scripts/ifcfg-*",
		"/etc/NetworkManager/system-connections/*",
		"/etc/systemd/network/*.network",
	} {
		m, _ := filepath.Glob(g)
		files = append(files, m...)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, m := range reIPv4.FindAllString(string(b), -1) {
			res[m] = true
		}
	}
	return res
}
