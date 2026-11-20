package wat

import (
	"crypto/tls"
	"errors"
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

func fixedTunnelProxy(t *testing.T, target string, hits *atomic.Int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Method != http.MethodConnect {
			http.Error(w, "tylko CONNECT", http.StatusMethodNotAllowed)
			return
		}
		conn, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		backend, err := net.Dial("tcp", target)
		if err != nil {
			conn.Close()
			return
		}
		_, _ = conn.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
		go func() { _, _ = io.Copy(backend, buf); backend.Close() }()
		go func() { _, _ = io.Copy(conn, backend); conn.Close() }()
	}))
	t.Cleanup(srv.Close)
	return srv
}

func blackholeProxy(t *testing.T) *httptest.Server {
	t.Helper()
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "tylko CONNECT", http.StatusMethodNotAllowed)
			return
		}
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		_, _ = conn.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
		<-done
		conn.Close()
	}))
	t.Cleanup(func() {
		close(done)
		srv.Close()
	})
	return srv
}

func TestWAFBlockOnPrimarySwitchesToBackup(t *testing.T) {
	badSite := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Iinfo", "62-966453-0 0NNN")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("Request unsuccessful. Incapsula incident ID: 1"))
	}))
	t.Cleanup(badSite.Close)
	var goodHits atomic.Int64
	goodSite := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		goodHits.Add(1)
		_, _ = w.Write([]byte("<html>plan</html>"))
	}))
	t.Cleanup(goodSite.Close)

	var primaryHits, backupHits atomic.Int64
	primary := fixedTunnelProxy(t, badSite.Listener.Addr().String(), &primaryHits)
	backup := fixedTunnelProxy(t, goodSite.Listener.Addr().String(), &backupHits)

	t.Setenv("PLANWAT_PROXY", primary.URL)
	t.Setenv("PLANWAT_PROXY_BACKUP", backup.URL)
	t.Setenv("PLANWAT_FETCH_INTERVAL", "0")
	f, err := NewFetcher()
	if err != nil {
		t.Fatal(err)
	}
	f.client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	body, err := f.FetchHTML("https://wml.wat.edu.pl/plan.htm")
	if err != nil || !strings.Contains(string(body), "plan") {
		t.Fatalf("pobranie przez zapasowe proxy: %q %v", body, err)
	}
	if primaryHits.Load() != 1 || backupHits.Load() != 1 {
		t.Fatalf("zapytań: główne %d, zapasowe %d", primaryHits.Load(), backupHits.Load())
	}

	host := f.ProxyHost()
	if !strings.Contains(host, "(pauza do") || !strings.Contains(host, strings.TrimPrefix(backup.URL, "http://")+" (aktywne)") {
		t.Fatalf("stan proxy: %q", host)
	}
	if status, _ := f.ProxyError(); !strings.Contains(status, "WAT") {
		t.Fatalf("ProxyError = %q, chcę informację o blokadzie WAT", status)
	}

	if _, err := f.FetchHTML("https://wml.wat.edu.pl/drugi.htm"); err != nil {
		t.Fatal(err)
	}
	if primaryHits.Load() != 1 {
		t.Fatalf("w czasie pauzy główne proxy dostało zapytanie (%d)", primaryHits.Load())
	}
	if goodHits.Load() != 2 {
		t.Fatalf("zapasowe proxy obsłużyło %d żądań, chcę 2", goodHits.Load())
	}
}

func TestBothProxiesWAFBlockedPausesHost(t *testing.T) {
	var badHits atomic.Int64
	badSite := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		badHits.Add(1)
		w.Header().Set("X-Iinfo", "62-966453-0 0NNN")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("Request unsuccessful. Incapsula incident ID: 1"))
	}))
	t.Cleanup(badSite.Close)

	var primaryHits, backupHits atomic.Int64
	primary := fixedTunnelProxy(t, badSite.Listener.Addr().String(), &primaryHits)
	backup := fixedTunnelProxy(t, badSite.Listener.Addr().String(), &backupHits)

	t.Setenv("PLANWAT_PROXY", primary.URL)
	t.Setenv("PLANWAT_PROXY_BACKUP", backup.URL)
	t.Setenv("PLANWAT_FETCH_INTERVAL", "0")
	f, err := NewFetcher()
	if err != nil {
		t.Fatal(err)
	}
	f.client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	_, err = f.FetchHTML("https://wml.wat.edu.pl/plan.htm")
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("FetchHTML = %v, chcę ErrBlocked", err)
	}
	if primaryHits.Load() != 1 || backupHits.Load() != 1 {
		t.Fatalf("zapytań: główne %d, zapasowe %d", primaryHits.Load(), backupHits.Load())
	}
	if badHits.Load() != 2 {
		t.Fatalf("zapytań do WAT = %d, chcę 2 (po jednym na proxy)", badHits.Load())
	}
	if !f.Blocked("https://wml.wat.edu.pl/plan.htm") {
		t.Fatal("domena nie została wstrzymana po blokadzie obu proxy")
	}
}

func TestProxyTimeoutSwitchesToBackup(t *testing.T) {
	goodSite := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>plan</html>"))
	}))
	t.Cleanup(goodSite.Close)

	primary := blackholeProxy(t)
	var backupHits atomic.Int64
	backup := fixedTunnelProxy(t, goodSite.Listener.Addr().String(), &backupHits)

	t.Setenv("PLANWAT_PROXY", primary.URL)
	t.Setenv("PLANWAT_PROXY_BACKUP", backup.URL)
	t.Setenv("PLANWAT_FETCH_INTERVAL", "0")
	f, err := NewFetcher()
	if err != nil {
		t.Fatal(err)
	}
	f.client.Timeout = 150 * time.Millisecond
	f.client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	body, err := f.FetchHTML("https://wml.wat.edu.pl/plan.htm")
	if err != nil || !strings.Contains(string(body), "plan") {
		t.Fatalf("pobranie przez zapasowe proxy: %q %v", body, err)
	}
	if backupHits.Load() != 1 {
		t.Fatalf("zapasowe proxy: %d zapytań, chcę 1", backupHits.Load())
	}
	if host := f.ProxyHost(); !strings.Contains(host, strings.TrimPrefix(backup.URL, "http://")+" (aktywne)") {
		t.Fatalf("stan proxy: %q", host)
	}
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
