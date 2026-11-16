package wat

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/billypuding/watplan/internal/config"
)

const wigIndexPath = "/cpp/index.php/studenci/plany-rozklady-terminy/rozklady-zajec"

func TestDiscoverWIG(t *testing.T) {
	index, err := os.ReadFile("testdata/wig_index.html")
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	visited := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Path == wigIndexPath {
			_, _ = w.Write(index)
			return
		}
		mu.Lock()
		visited[r.URL.Path] = true
		mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/7-i-r-budow-stac"):
			fmt.Fprint(w, `<a href="/cpp/phocadownloadpap/budownictwo/stacjonarne/WIG26BX1S1.pdf">Podgląd</a>
<a href="`+wigIndexPath+`/6-budow-stac/7-i-r-budow-stac?download=1188:wig26bx1s1">Pobierz</a>
<a href="`+wigIndexPath+`/6-budow-stac/7-i-r-budow-stac?download=1189:wig26bx2s1">Pobierz</a>
<a href="/cpp/index.php/studenci/kalendarz-studenta">Kalendarz</a>`)
		case strings.HasSuffix(r.URL.Path, "/2-i-r-budow-nstac"):
			fmt.Fprint(w, `<a href="/cpp/phocadownloadpap/budownictwo/niestacjonarne/WIG26BX1N1.pdf">Podgląd</a>`)
		default:
			fmt.Fprint(w, `<html><body>Brak plików</body></html>`)
		}
	}))
	t.Cleanup(srv.Close)

	t.Setenv("PLANWAT_FETCH_INTERVAL", "0")
	f, err := NewFetcher()
	if err != nil {
		t.Fatal(err)
	}
	plans, err := f.DiscoverPlans(config.Source{Faculty: "WIG", IndexURL: srv.URL + wigIndexPath, Format: FormatWIGPDF, StartYear: 2026})
	if err != nil {
		t.Fatal(err)
	}

	if len(visited) != 58 {
		t.Errorf("odwiedzone podstrony: %d, chcę 58", len(visited))
	}
	for p := range visited {
		if !strings.HasPrefix(p, wigIndexPath+"/") {
			t.Errorf("odwiedzona strona spoza katalogu: %s", p)
		}
	}

	want := map[string]string{
		"WIG26BX1S1": srv.URL + "/cpp/phocadownloadpap/budownictwo/stacjonarne/WIG26BX1S1.pdf",
		"WIG26BX2S1": srv.URL + wigIndexPath + "/6-budow-stac/7-i-r-budow-stac?download=1189:wig26bx2s1",
		"WIG26BX1N1": srv.URL + "/cpp/phocadownloadpap/budownictwo/niestacjonarne/WIG26BX1N1.pdf",
	}
	if len(plans) != len(want) {
		t.Fatalf("planów %d, chcę %d: %+v", len(plans), len(want), plans)
	}
	for _, p := range plans {
		if p.URL != want[p.ID] || p.Format != FormatWIGPDF || p.Faculty != "WIG" || p.StartYear != 2026 {
			t.Errorf("zły plan %+v", p)
		}
	}
}

func TestWIGPlansOnRealLeafPage(t *testing.T) {
	body, err := os.ReadFile("testdata/wig_leaf.html")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := url.Parse("https://www.wig.wat.edu.pl" + wigIndexPath + "/6-budow-stac/7-i-r-budow-stac")
	found := map[string]string{}
	plans, skipped := wigPlansOnPage(page, body, config.Source{Faculty: "WIG", Format: FormatWIGPDF, StartYear: 2026})
	if len(skipped) > 0 {
		t.Errorf("pominięte pliki: %v", skipped)
	}
	for _, p := range plans {
		if old, ok := found[p.ID]; !ok || !strings.HasSuffix(old, ".pdf") {
			found[p.ID] = p.URL
		}
	}
	if len(found) != 6 {
		t.Fatalf("grup %d, chcę 6: %v", len(found), found)
	}
	for i := 1; i <= 6; i++ {
		id := fmt.Sprintf("WIG26BX%dS1", i)
		if want := "https://www.wig.wat.edu.pl/cpp/phocadownloadpap/budownictwo/stacjonarne/" + id + ".pdf"; found[id] != want {
			t.Errorf("%s: %q, chcę %q", id, found[id], want)
		}
	}
}
