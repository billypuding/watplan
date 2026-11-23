package server

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/model"
	"github.com/billypuding/watplan/internal/store"
)

func TestPlanResponseEncodings(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../e2e/fixtures/data/WMT26AX3S1.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/WMT26AX3S1.json", raw, 0o644); err != nil {
		t.Fatal(err)
	}
	reg := config.NewRegistry([]config.PlanConfig{{ID: "WMT26AX3S1", URL: "http://example.invalid/"}})
	handler, err := NewServer(reg, st, nil, Options{Offline: true}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	get := func(accept, inm string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/plan/WMT26AX3S1", nil)
		if accept != "" {
			req.Header.Set("Accept-Encoding", accept)
		}
		if inm != "" {
			req.Header.Set("If-None-Match", inm)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	cases := []struct{ accept, want string }{
		{"gzip, deflate, br, zstd", "br"},
		{"gzip", "gzip"},
		{"br;q=0, gzip", "gzip"},
		{"identity", ""},
		{"", ""},
	}
	etag := ""
	for _, c := range cases {
		rec := get(c.accept, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%q: status = %d", c.accept, rec.Code)
		}
		if got := rec.Header().Get("Content-Encoding"); got != c.want {
			t.Fatalf("%q: Content-Encoding = %q, chcę %q", c.accept, got, c.want)
		}
		if rec.Header().Get("Vary") != "Accept-Encoding" {
			t.Fatalf("%q: brak Vary", c.accept)
		}
		var body io.Reader = rec.Body
		switch c.want {
		case "br":
			body = brotli.NewReader(rec.Body)
		case "gzip":
			zr, err := gzip.NewReader(rec.Body)
			if err != nil {
				t.Fatal(err)
			}
			body = zr
		}
		got, err := io.ReadAll(body)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, raw) {
			t.Fatalf("%q: treść po rozpakowaniu różni się od pliku", c.accept)
		}
		if etag == "" {
			etag = rec.Header().Get("ETag")
		} else if rec.Header().Get("ETag") != etag {
			t.Fatalf("%q: inny ETag dla innego kodowania", c.accept)
		}
	}
	if rec := get("br", etag); rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Fatalf("If-None-Match: status = %d, treść %d B", rec.Code, rec.Body.Len())
	}

	if err := st.Save("WMT26AX3S1", model.Plan{FetchedAt: "2026-11-22 10:00:00", Events: []model.Event{{Date: "2026-11-23", Subject: "Nowy"}}}); err != nil {
		t.Fatal(err)
	}
	rec := get("gzip", etag)
	if rec.Code != http.StatusOK || rec.Header().Get("ETag") == etag {
		t.Fatalf("po zmianie planu: status = %d, ETag = %q", rec.Code, rec.Header().Get("ETag"))
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := io.ReadAll(zr); !bytes.Contains(got, []byte("Nowy")) {
		t.Fatal("po zmianie planu serwer oddał starą wersję")
	}
}

func TestPlanBlobServesFastThenBest(t *testing.T) {
	raw, err := os.ReadFile("../../e2e/fixtures/data/WMT26AX3S1.json")
	if err != nil {
		t.Fatal(err)
	}
	var blobs planBlobs
	b := blobs.get("A", raw)
	first, enc := b.encoded("br")
	if enc != "br" || len(first) == 0 {
		t.Fatalf("pierwsza odpowiedź: %q, %d B", enc, len(first))
	}
	<-b.done
	best, _ := b.encoded("br")
	if len(best) > len(b.fastBr) {
		t.Fatalf("brotli 11 (%d B) większe niż szybkie (%d B)", len(best), len(b.fastBr))
	}
	for name, body := range map[string][]byte{"szybkie": b.fastBr, "najlepsze": best} {
		got, err := io.ReadAll(brotli.NewReader(bytes.NewReader(body)))
		if err != nil || !bytes.Equal(got, raw) {
			t.Fatalf("%s: zła dekompresja (%v)", name, err)
		}
	}
	if blobs.get("A", raw) != b {
		t.Fatal("ten sam plan nie powinien być kompresowany ponownie")
	}
	changed := append(append([]byte(nil), raw[:len(raw)-1]...), ' ', '}')
	if nb := blobs.get("A", changed); nb == b || nb.etag == b.etag {
		t.Fatal("zmieniony plan musi dostać nowy blob i ETag")
	}
}

func TestPlanBlobWarmOnlyForRequestedPlans(t *testing.T) {
	raw := []byte(`{"events":[]}`)
	var blobs planBlobs
	blobs.warm("NIKT", raw)
	blobs.mu.Lock()
	_, cached := blobs.m["NIKT"]
	blobs.mu.Unlock()
	if cached {
		t.Fatal("plan, którego nikt nie otworzył, nie powinien być kompresowany po zapisie")
	}
	blobs.get("OGLADANY", raw)
	newer := []byte(`{"events":[],"checked_at":"2026-11-23 18:00:00"}`)
	blobs.warm("OGLADANY", newer)
	blobs.mu.Lock()
	b := blobs.m["OGLADANY"]
	blobs.mu.Unlock()
	if b.etag != blobETag(newer) || b.best.Load() == nil {
		t.Fatal("po zapisie oglądanego planu brotli 11 powinno być gotowe przed pierwszym zapytaniem")
	}
}

func BenchmarkPlanBlobNewVersion(b *testing.B) {
	raw, err := os.ReadFile("../../e2e/fixtures/data/WMT26AX3S1.json")
	if err != nil {
		b.Fatal(err)
	}
	var blobs planBlobs
	for i := 0; i < b.N; i++ {
		data := append(append([]byte(nil), raw...), byte(' '+i%64))
		blobs.get("A", data)
	}
}
