package wat

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"io"
	"net"
	stdhttp "net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
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

func encodeBody(t *testing.T, encoding string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	var w io.WriteCloser
	switch encoding {
	case "gzip":
		w = gzip.NewWriter(&buf)
	case "deflate":
		w = zlib.NewWriter(&buf)
	case "br":
		w = brotli.NewWriter(&buf)
	case "zstd":
		zw, err := zstd.NewWriter(&buf)
		if err != nil {
			t.Fatal(err)
		}
		w = zw
	default:
		return data
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestTLSProxyDecodesEveryEncoding(t *testing.T) {
	page, err := os.ReadFile("testdata/plan_wat.html")
	if err != nil {
		t.Fatal(err)
	}
	want, err := Parse("", bytes.NewReader(page), 2026)
	if err != nil {
		t.Fatal(err)
	}
	if len(want) != 214 {
		t.Fatalf("fixture ma %d zajęć, chcę 214", len(want))
	}

	var asked atomic.Value
	target := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		asked.Store(r.Header.Get("Accept-Encoding"))
		encoding := r.URL.Query().Get("enc")
		if encoding != "identity" {
			w.Header().Set("Content-Encoding", encoding)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(encodeBody(t, encoding, page))
	}))
	defer target.Close()

	c := newTestTLSProxyClient(t)
	for _, encoding := range []string{"identity", "gzip", "deflate", "br", "zstd"} {
		body, _, err := c.fetch(target.URL+"/?enc="+encoding, Validators{})
		if err != nil {
			t.Fatalf("%s: %v", encoding, err)
		}
		if got := asked.Load(); got != "gzip, deflate, br, zstd" {
			t.Fatalf("%s: Accept-Encoding = %q", encoding, got)
		}
		if !bytes.Equal(body, page) {
			t.Fatalf("%s: treść różni się od oryginału (%d B, chcę %d B)", encoding, len(body), len(page))
		}
		events, err := Parse("", bytes.NewReader(body), 2026)
		if err != nil {
			t.Fatalf("%s: %v", encoding, err)
		}
		if len(events) != len(want) {
			t.Fatalf("%s: %d zajęć, chcę %d", encoding, len(events), len(want))
		}
		for i := range events {
			if events[i] != want[i] {
				t.Fatalf("%s: zajęcia %d różnią się: %+v, chcę %+v", encoding, i, events[i], want[i])
			}
		}
	}
}
