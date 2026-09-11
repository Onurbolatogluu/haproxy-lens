package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

const ipOut = `2: eth0    inet 10.10.0.5/24 brd 10.10.0.255 scope global eth0\       valid_lft forever preferred_lft forever
2: eth0    inet 10.10.0.100/24 scope global secondary eth0\       valid_lft forever preferred_lft forever
2: eth0    inet 10.10.0.200/32 scope global eth0:vip\       valid_lft forever preferred_lft forever
`

func TestParseIPAddrOutput(t *testing.T) {
	got := parseIPAddrOutput(ipOut)
	want := []ifAddr{
		{IP: "10.10.0.5", Prefix: 24, Label: "eth0"},
		{IP: "10.10.0.100", Prefix: 24, Secondary: true, Label: "eth0"},
		{IP: "10.10.0.200", Prefix: 32, Label: "eth0:vip"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestParseKeepalived(t *testing.T) {
	conf := `
! yorum
include /etc/keepalived/conf.d/*.conf
vrrp_instance VI_1 {
    state MASTER
    interface eth0
    virtual_router_id 51
    authentication {
        auth_pass 10.99.99.99
    }
    virtual_ipaddress {
        10.10.0.100/24 dev eth0 label eth0:1
        10.10.0.101
    }
    virtual_ipaddress_excluded { 10.10.0.102 }
}`
	ips, inc := parseKeepalived(conf)
	if !reflect.DeepEqual(ips, []string{"10.10.0.100", "10.10.0.101", "10.10.0.102"}) {
		t.Fatalf("VIP'ler: %v", ips)
	}
	if !reflect.DeepEqual(inc, []string{"/etc/keepalived/conf.d/*.conf"}) {
		t.Fatalf("include: %v", inc)
	}
}

func TestChooseListen(t *testing.T) {
	addrs := parseIPAddrOutput(ipOut)
	cases := []struct {
		name         string
		addrs        []ifAddr
		vips, static map[string]bool
		want         string
	}{
		{"netplan'daki adres seçilir, VIP atlanır", addrs, map[string]bool{"10.10.0.100": true}, map[string]bool{"10.10.0.5": true, "10.10.0.1": true}, "10.10.0.5"},
		{"ağ ayarı bulunamazsa birincil adres", addrs, map[string]bool{}, map[string]bool{}, "10.10.0.5"},
		{"VIP ilk sırada olsa bile atlanır", []ifAddr{{IP: "10.10.0.100", Prefix: 24}, {IP: "10.10.0.5", Prefix: 24}}, map[string]bool{"10.10.0.100": true}, map[string]bool{}, "10.10.0.5"},
		{"keepalived ayarı okunamasa da /32 VIP seçilmez", []ifAddr{{IP: "10.10.0.200", Prefix: 32}, {IP: "10.10.0.5", Prefix: 24}}, map[string]bool{}, map[string]bool{}, "10.10.0.5"},
		{"herkese açık IP seçilmez", []ifAddr{{IP: "88.255.1.10", Prefix: 24}}, map[string]bool{}, map[string]bool{"88.255.1.10": true}, ""},
		{"sadece VIP varsa hiçbiri", []ifAddr{{IP: "10.10.0.100", Prefix: 24}}, map[string]bool{"10.10.0.100": true}, map[string]bool{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := chooseListen(c.addrs, c.vips, c.static)
			if got.IP != c.want {
				t.Fatalf("seçilen %q, beklenen %q (%+v)", got.IP, c.want, got)
			}
		})
	}
}

func TestAllowOnly(t *testing.T) {
	nets, err := parseAllow("10.10.0.0/16, 192.168.1.7")
	if err != nil {
		t.Fatal(err)
	}
	h := allowOnly(nets, net.ParseIP("172.20.0.5"), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	cases := map[string]int{
		"10.10.3.4:5000":   200, // izinli ağ
		"192.168.1.7:5000": 200, // tek IP
		"192.168.1.8:5000": 403,
		"88.255.1.10:5000": 403, // internet
		"127.0.0.1:5000":   200, // sunucunun kendisi
		"172.20.0.5:5000":  200, // panelin kendi adresi (sunucudan gelen istek)
	}
	for remote, want := range cases {
		r := httptest.NewRequest("GET", "/api/state", nil)
		r.RemoteAddr = remote
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("%s: kod %d, beklenen %d", remote, w.Code, want)
		}
	}
	if _, err := parseAllow("10.0.0.0/33"); err == nil {
		t.Fatal("geçersiz ağ kabul edildi")
	}
	def, _ := parseAllow("")
	if len(def) != 5 {
		t.Fatalf("varsayılan liste %d ağ", len(def))
	}
}
