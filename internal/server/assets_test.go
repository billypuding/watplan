package server

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/store"
)

func testAssets() fstest.MapFS {
	return fstest.MapFS{
		"app.html":        {Data: []byte(`<script src="/boot.js?v=0" data-inline></script><link href="/style.css?v=0"><script src="/app.js?v=0"></script><a href="/missing.js?v=0">`)},
		"boot.js":         {Data: []byte("\nwindow.boot = '/favicon-192.png?v=0';\n")},
		"app.js":          {Data: []byte(`console.log(1)`)},
		"style.css":       {Data: []byte(`@font-face{src:url(/fonts/f.woff2?v=0)}`)},
		"fonts/f.woff2":   {Data: []byte("font-1")},
		"favicon-192.png": {Data: []byte("png")},
		serviceWorkerFile: {Data: []byte("const CACHE = 'watplan-dev';\nconst icon = '/favicon-192.png?v=0';")},
	}
}

var unversioned = regexp.MustCompile(`\?v=0["')]`)

func shortHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:10]
}

func readAsset(t *testing.T, a fs.FS, name string) string {
	t.Helper()
	data, err := fs.ReadFile(a, name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func swCache(t *testing.T, a fs.FS) string {
	t.Helper()
	m := regexp.MustCompile(`const CACHE = '(watplan-[0-9a-f]{12})';`).FindStringSubmatch(readAsset(t, a, serviceWorkerFile))
	if m == nil {
		t.Fatalf("sw.js bez wersji cache: %s", readAsset(t, a, serviceWorkerFile))
	}
	return m[1]
}

func TestAssetVersionsFollowContent(t *testing.T) {
	src := testAssets()
	a, err := newAssetFS(src)
	if err != nil {
		t.Fatal(err)
	}

	css := readAsset(t, a, "style.css")
	if want := "/fonts/f.woff2?v=" + shortHash([]byte("font-1")); !strings.Contains(css, want) {
		t.Fatalf("style.css = %q, chcę %q", css, want)
	}
	html := readAsset(t, a, "app.html")
	for _, want := range []string{
		"/app.js?v=" + shortHash([]byte(`console.log(1)`)),
		"/style.css?v=" + shortHash([]byte(css)),
		"/missing.js?v=0",
		"<script>window.boot = '/favicon-192.png?v=" + shortHash([]byte("png")) + "';</script>",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("app.html = %q, brak %q", html, want)
		}
	}
	if sw := readAsset(t, a, serviceWorkerFile); !strings.Contains(sw, "/favicon-192.png?v="+shortHash([]byte("png"))) {
		t.Fatalf("sw.js = %q", sw)
	}
	before := swCache(t, a)

	if strings.Contains(html, "boot.js") {
		t.Fatalf("app.html dalej odwołuje się do boot.js: %q", html)
	}

	src["boot.js"] = &fstest.MapFile{Data: []byte("window.boot = 2;")}
	inlined, err := newAssetFS(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readAsset(t, inlined, "app.html"), "<script>window.boot = 2;</script>") || swCache(t, inlined) == before {
		t.Fatal("zmiana wstawianego skryptu nie zmieniła app.html i wersji cache")
	}
	src["boot.js"] = &fstest.MapFile{Data: []byte("\nwindow.boot = '/favicon-192.png?v=0';\n")}

	src["fonts/f.woff2"] = &fstest.MapFile{Data: []byte("font-2")}
	b, err := newAssetFS(src)
	if err != nil {
		t.Fatal(err)
	}
	if readAsset(t, b, "app.html") == html {
		t.Fatal("zmiana fontu nie zmieniła wersji style.css w app.html")
	}
	if after := swCache(t, b); after == before {
		t.Fatalf("cache w sw.js się nie zmienił (%s)", after)
	}

	c, err := newAssetFS(src)
	if err != nil {
		t.Fatal(err)
	}
	if swCache(t, c) != swCache(t, b) {
		t.Fatal("wersja nie jest deterministyczna")
	}
}

func TestServedAssetsAreVersioned(t *testing.T) {
	st, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewServer(config.NewRegistry(nil), st, nil, Options{Offline: true}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	get := func(path string) *http.Response {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec.Result()
	}
	body := func(resp *http.Response) string {
		data, _ := io.ReadAll(resp.Body)
		return string(data)
	}

	page := body(get("/app"))
	ref := regexp.MustCompile(`/app\.js\?v=([0-9a-f]{10})`).FindStringSubmatch(page)
	if ref == nil {
		t.Fatal("/app bez wersji app.js")
	}
	if unversioned.MatchString(page) {
		t.Fatal("/app zawiera nieprzepisane ?v=0")
	}
	if strings.Contains(page, "theme.js") || !strings.Contains(page, "setAttribute('data-theme'") {
		t.Fatal("/app nie ma wstawionego theme.js")
	}
	js := get("/app.js?v=" + ref[1])
	if js.StatusCode != http.StatusOK || !strings.Contains(js.Header.Get("Cache-Control"), "immutable") {
		t.Fatalf("app.js: status %d, cache %q", js.StatusCode, js.Header.Get("Cache-Control"))
	}
	if shortHash([]byte(body(js))) != ref[1] {
		t.Fatal("wersja app.js nie zgadza się z treścią")
	}

	for _, path := range []string{"/", "/o-watplan", "/style.css?v=x", "/manifest.json?v=x"} {
		resp := get(path)
		text := body(resp)
		if resp.StatusCode != http.StatusOK || unversioned.MatchString(text) {
			t.Fatalf("%s: status %d, zawiera ?v=0: %v", path, resp.StatusCode, unversioned.MatchString(text))
		}
		if resp.ContentLength >= 0 && resp.ContentLength != int64(len(text)) && resp.Header.Get("Content-Length") != "" {
			t.Fatalf("%s: Content-Length %d, treść %d", path, resp.ContentLength, len(text))
		}
	}
	sw := get("/sw.js")
	if sw.Header.Get("Cache-Control") != "no-cache" || !regexp.MustCompile(`const CACHE = 'watplan-[0-9a-f]{12}';`).MatchString(body(sw)) {
		t.Fatal("sw.js bez wersji cache albo z cache")
	}
}
