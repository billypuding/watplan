package wat

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func tunnelProxy(t *testing.T, hits *atomic.Int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Method != http.MethodConnect {
			http.Error(w, "tylko CONNECT", http.StatusMethodNotAllowed)
			return
		}
		target, err := net.Dial("tcp", r.Host)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			target.Close()
			return
		}
		_, _ = conn.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
		go func() { _, _ = io.Copy(target, buf); target.Close() }()
		go func() { _, _ = io.Copy(conn, target); conn.Close() }()
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestBackupProxyTakesOverAndPrimaryReturns(t *testing.T) {
	site := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>plan</html>"))
	}))
	t.Cleanup(site.Close)

	var badHits, goodHits atomic.Int64
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		badHits.Add(1)
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	t.Cleanup(bad.Close)
	good := tunnelProxy(t, &goodHits)

	t.Setenv("PLANWAT_PROXY", bad.URL)
	t.Setenv("PLANWAT_PROXY_BACKUP", good.URL)
	t.Setenv("PLANWAT_FETCH_INTERVAL", "0")
	f, err := NewFetcher()
	if err != nil {
		t.Fatal(err)
	}
	f.client.Transport.(*http.Transport).TLSClientConfig = site.Client().Transport.(*http.Transport).TLSClientConfig.Clone()

	body, err := f.FetchHTML(site.URL + "/plan.htm")
	if err != nil || !strings.Contains(string(body), "plan") {
		t.Fatalf("pobranie przez zapasowe proxy: %q %v", body, err)
	}
	if badHits.Load() != 1 || goodHits.Load() != 1 {
		t.Fatalf("zapytań: główne %d, zapasowe %d", badHits.Load(), goodHits.Load())
	}
	if host := f.ProxyHost(); !strings.Contains(host, strings.TrimPrefix(good.URL, "http://")+" (aktywne)") {
		t.Fatalf("aktywne proxy: %s", host)
	}

	if _, err := f.FetchHTML(site.URL + "/drugi.htm"); err != nil {
		t.Fatal(err)
	}
	if badHits.Load() != 1 {
		t.Fatalf("w czasie pauzy główne proxy dostało zapytanie (%d)", badHits.Load())
	}

	f.proxyMu.Lock()
	f.proxyBad[0] = time.Now().Add(-time.Second)
	f.proxyMu.Unlock()
	_, _ = f.FetchHTML(site.URL + "/trzeci.htm")
	if badHits.Load() != 2 {
		t.Fatalf("po pauzie serwer nie wrócił do głównego proxy (%d)", badHits.Load())
	}
}

func TestAllProxiesDownPausesFetching(t *testing.T) {
	var hits atomic.Int64
	reject := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusPaymentRequired)
	})
	a := httptest.NewServer(reject)
	b := httptest.NewServer(reject)
	t.Cleanup(a.Close)
	t.Cleanup(b.Close)
	t.Setenv("PLANWAT_PROXY", a.URL)
	t.Setenv("PLANWAT_PROXY_BACKUP", b.URL)
	t.Setenv("PLANWAT_FETCH_INTERVAL", "0")
	f, err := NewFetcher()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.FetchHTML("https://wel.wat.edu.pl/x.htm"); err == nil {
		t.Fatal("pobranie przy dwóch martwych proxy się udało")
	}
	if hits.Load() != 2 {
		t.Fatalf("zapytań %d, chcę 2 (po jednym na proxy)", hits.Load())
	}
	if !f.Blocked("https://wcy.wat.edu.pl/y") {
		t.Fatal("brak pauzy, gdy oba proxy nie działają")
	}
}
