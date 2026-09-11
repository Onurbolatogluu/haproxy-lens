package main

// Panele kimin erişebileceği: sadece izin verilen ağlardan gelen bağlantılar.

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// Varsayılan: özel ağlar ve sunucunun kendisi. İnternetten gelen istek reddedilir.
const defaultAllow = "10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,100.64.0.0/10,127.0.0.0/8"

func parseAllow(s string) ([]*net.IPNet, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		s = defaultAllow
	}
	var out []*net.IPNet
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !strings.Contains(p, "/") {
			if strings.Contains(p, ":") {
				p += "/128"
			} else {
				p += "/32"
			}
		}
		_, n, err := net.ParseCIDR(p)
		if err != nil {
			return nil, fmt.Errorf("geçersiz ağ %q", p)
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("izin listesi boş")
	}
	return out, nil
}

// localIP: panelin dinlediği adres; sunucunun kendisinden gelen istekler bu adresten gelir.
func netsString(nets []*net.IPNet) string {
	var s []string
	for _, n := range nets {
		s = append(s, n.String())
	}
	return strings.Join(s, ", ")
}

func allowOnly(nets []*net.IPNet, localIP net.IP, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(host)
		if err == nil && ip != nil {
			if ip.IsLoopback() || (localIP != nil && ip.Equal(localIP)) {
				next.ServeHTTP(w, r)
				return
			}
			for _, n := range nets {
				if n.Contains(ip) {
					next.ServeHTTP(w, r)
					return
				}
			}
		}
		http.Error(w, "Bu adresten panele erişim izni yok.", http.StatusForbidden)
	})
}
