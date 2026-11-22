package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

var cloudflareRanges = func() []*net.IPNet {
	var out []*net.IPNet
	for _, cidr := range []string{
		"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
		"141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
		"197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
		"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
		"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32",
		"2405:8100::/32", "2a06:98c0::/29", "2c0f:f248::/32",
	} {
		_, n, err := net.ParseCIDR(cidr)
		if err == nil {
			out = append(out, n)
		}
	}
	return out
}()

func fromCloudflare(ip net.IP) bool {
	for _, n := range cloudflareRanges {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func (s *Server) clientIP(r *http.Request) string {
	peer := r.RemoteAddr
	if h, _, err := net.SplitHostPort(peer); err == nil {
		peer = h
	}
	peerIP := net.ParseIP(peer)
	if peerIP == nil {
		return peer
	}
	trusted := fromCloudflare(peerIP) || (s.options.TrustProxy && (peerIP.IsPrivate() || peerIP.IsLoopback()))
	if !trusted {
		return peerIP.String()
	}
	if cf := net.ParseIP(strings.TrimSpace(r.Header.Get("CF-Connecting-IP"))); cf != nil {
		return cf.String()
	}
	return peerIP.String()
}

type rateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	hits    map[string][]time.Time
	checked time.Time
	now     func() time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{limit: limit, window: window, hits: map[string][]time.Time{}, now: time.Now}
}

func (l *rateLimiter) allow(key string) bool {
	if l == nil || l.limit <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	cutoff := now.Add(-l.window)
	if now.Sub(l.checked) > l.window {
		for k, list := range l.hits {
			if len(list) == 0 || !list[len(list)-1].After(cutoff) {
				delete(l.hits, k)
			}
		}
		l.checked = now
	}
	list := l.hits[key]
	kept := list[:0]
	for _, t := range list {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.limit {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}
