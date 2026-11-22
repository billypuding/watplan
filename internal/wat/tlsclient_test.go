package wat

import (
	"errors"
	"io"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestProxyLogin(t *testing.T) {
	cases := map[string]string{
		"resi_abc":                "customer-resi_abc-cc-PL",
		"customer-resi_abc-cc-PL": "customer-resi_abc-cc-PL",
		"resi_abc-cc-DE":          "customer-resi_abc-cc-DE",
		"customer-resi_abc-cc-PL-sessid-old-sesstime-5": "customer-resi_abc-cc-PL",
	}
	for input, want := range cases {
		if got := proxyLogin(input); got != want {
			t.Errorf("proxyLogin(%q) = %q, chcę %q", input, got, want)
		}
	}
}

func TestNewTLSProxyClient(t *testing.T) {
	t.Setenv("PLANWAT_PROXY_HOST", "pr.oxylabs.io:7777")
	t.Setenv("PLANWAT_PROXY_USER", "resi_abc")
	t.Setenv("PLANWAT_PROXY_PASS", "secret")
	c, err := newTLSProxyClient()
	if err != nil {
		t.Fatal(err)
	}
	if c == nil || c.client == nil || c.host != "pr.oxylabs.io:7777" {
		t.Fatalf("client = %+v", c)
	}
}

func TestNewTLSProxyClientRequiresPassword(t *testing.T) {
	t.Setenv("PLANWAT_PROXY_HOST", "pr.oxylabs.io:7777")
	t.Setenv("PLANWAT_PROXY_USER", "resi_abc")
	t.Setenv("PLANWAT_PROXY_PASS", "")
	if _, err := newTLSProxyClient(); err == nil {
		t.Fatal("chcę błąd bez hasła")
	}
}

func TestTLSProxyURL(t *testing.T) {
	c := &tlsProxyClient{
		host:    "pr.oxylabs.io:7777",
		user:    "resi_abc",
		pass:    "p@ss",
		session: "abc123",
	}
	u, err := url.Parse(c.proxyURL())
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "http" || u.Host != "pr.oxylabs.io:7777" {
		t.Fatalf("proxy = %q", u)
	}
	if u.User.Username() != "customer-resi_abc-cc-PL-sessid-abc123-sesstime-1440" {
		t.Fatalf("username = %q", u.User.Username())
	}
	if pass, _ := u.User.Password(); pass != "p@ss" {
		t.Fatalf("password = %q", pass)
	}
}

func startTestProxy(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.Method == stdhttp.MethodConnect {
			upstream, err := net.Dial("tcp", r.Host)
			if err != nil {
				stdhttp.Error(w, err.Error(), stdhttp.StatusBadGateway)
				return
			}
			conn, _, err := w.(stdhttp.Hijacker).Hijack()
			if err != nil {
				upstream.Close()
				return
			}
			conn.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
			go func() {
				io.Copy(upstream, conn)
				upstream.Close()
			}()
			io.Copy(conn, upstream)
			conn.Close()
			return
		}
		r.RequestURI = ""
		resp, err := stdhttp.DefaultTransport.RoundTrip(r)
		if err != nil {
			stdhttp.Error(w, err.Error(), stdhttp.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

func newTestTLSProxyClient(t *testing.T) *tlsProxyClient {
	t.Helper()
	t.Setenv("PLANWAT_PROXY_HOST", startTestProxy(t))
	t.Setenv("PLANWAT_PROXY_USER", "resi_abc")
	t.Setenv("PLANWAT_PROXY_PASS", "secret")
	c, err := newTLSProxyClient()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestTLSProxyConcurrentRotate(t *testing.T) {
	var hits atomic.Int64
	target := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if hits.Add(1)%3 == 0 {
			w.WriteHeader(stdhttp.StatusForbidden)
			return
		}
		w.Write([]byte("<html>plan</html>"))
	}))
	defer target.Close()

	c := newTestTLSProxyClient(t)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.fetch(target.URL, Validators{})
		}()
	}
	wg.Wait()
	if hits.Load() < 16 {
		t.Fatalf("hits = %d", hits.Load())
	}
}

func TestTLSProxyRotateOncePerSession(t *testing.T) {
	c := newTestTLSProxyClient(t)
	seen := c.session
	if err := c.rotateFrom(seen); err != nil {
		t.Fatal(err)
	}
	rotated := c.session
	if rotated == seen {
		t.Fatal("sesja się nie zmieniła")
	}
	if err := c.rotateFrom(seen); err != nil {
		t.Fatal(err)
	}
	if c.session != rotated {
		t.Fatal("druga rotacja ze starej sesji zmieniła sesję")
	}
}

func TestTLSProxyValidators(t *testing.T) {
	const etag = `"v1"`
	const modified = "Mon, 02 Nov 2026 10:00:00 GMT"
	target := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.Header.Get("If-None-Match") == etag && r.Header.Get("If-Modified-Since") == modified {
			w.WriteHeader(stdhttp.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Last-Modified", modified)
		w.Write([]byte("<html>plan</html>"))
	}))
	defer target.Close()

	c := newTestTLSProxyClient(t)
	body, got, err := c.fetch(target.URL, Validators{})
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "<html>plan</html>" || got.ETag != etag || got.LastModified != modified {
		t.Fatalf("body = %q, validators = %+v", body, got)
	}

	body, again, err := c.fetch(target.URL, got)
	if !errors.Is(err, ErrNotModified) {
		t.Fatalf("err = %v, body = %q", err, body)
	}
	if again != got {
		t.Fatalf("validators = %+v", again)
	}
}
