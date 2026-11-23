package push

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/billypuding/watplan/internal/durable"
	"github.com/billypuding/watplan/internal/model"
)

func newTestService(t *testing.T) (*Service, *[]string) {
	t.Helper()
	pub, priv, err := GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(pub, priv, "mailto:test@example.com", filepath.Join(t.TempDir(), "push.json"))
	if err != nil {
		t.Fatal(err)
	}
	var sent []string
	s.send = func(payload []byte, sub Subscription) (int, error) {
		if strings.Contains(sub.Endpoint, "gone") {
			return http.StatusGone, nil
		}
		sent = append(sent, sub.Endpoint+" "+string(payload))
		return http.StatusCreated, nil
	}
	return s, &sent
}

func sub(endpoint string, plans ...string) Subscription {
	return Subscription{Endpoint: endpoint, Keys: Keys{P256dh: "BKey", Auth: "auth"}, Plans: plans}
}

func TestSubscribeValidatesEndpoints(t *testing.T) {
	s, _ := newTestService(t)
	for _, bad := range []string{"http://fcm.googleapis.com/x", "https://evil.example.com/x", "https://user@fcm.googleapis.com/x", "https://fcm.googleapis.com.evil.com/x"} {
		if err := s.Subscribe(sub(bad, "WMT26AX3S1")); err == nil {
			t.Errorf("%s: przyjęto", bad)
		}
	}
	for _, good := range []string{"https://fcm.googleapis.com/fcm/send/abc", "https://web.push.apple.com/abc", "https://updates.push.services.mozilla.com/wpush/v2/abc"} {
		if err := s.Subscribe(sub(good, "WMT26AX3S1")); err != nil {
			t.Errorf("%s: %v", good, err)
		}
	}
	if err := s.Subscribe(sub("https://fcm.googleapis.com/x", "A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K")); err == nil {
		t.Error("przyjęto ponad 10 grup")
	}
}

func TestNotifyTargetsPlanAndDropsGoneEndpoints(t *testing.T) {
	s, sent := newTestService(t)
	_ = s.Subscribe(sub("https://fcm.googleapis.com/a", "WMT26AX3S1", "WEL26EX1S0"))
	_ = s.Subscribe(sub("https://fcm.googleapis.com/b", "WEL26EX1S0"))
	_ = s.Subscribe(sub("https://fcm.googleapis.com/gone", "WMT26AX3S1"))

	if got := s.Watched(); strings.Join(got, ",") != "WEL26EX1S0,WMT26AX3S1" {
		t.Fatalf("Watched = %v", got)
	}
	if n := s.Notify("WMT26AX3S1", Payload{Title: "t"}); n != 1 || len(*sent) != 1 || !strings.HasPrefix((*sent)[0], "https://fcm.googleapis.com/a ") {
		t.Fatalf("wysłano %d: %v", n, *sent)
	}
	if s.Count() != 2 {
		t.Fatalf("wygasła subskrypcja nie została usunięta, zostało %d", s.Count())
	}

	reloaded, err := New(s.public, s.private, s.subject, s.path)
	if err != nil || reloaded.Count() != 2 {
		t.Fatalf("po ponownym wczytaniu %d subskrypcji, err %v", reloaded.Count(), err)
	}

	_ = s.Subscribe(sub("https://fcm.googleapis.com/b"))
	if s.Count() != 1 {
		t.Fatalf("pusta lista grup nie usunęła subskrypcji")
	}
}

func TestDescribeChange(t *testing.T) {
	c := model.Change{
		Moved: []model.EventChange{{
			Before: model.Event{Date: "2026-11-20", Start: "08:00", Subject: "Matematyka 2", Type: "wykład"},
			After:  model.Event{Date: "2026-11-23", Start: "09:50", Subject: "Matematyka 2", Type: "wykład"},
		}},
		Changed: []model.EventChange{{
			Before: model.Event{Date: "2026-11-24", Start: "08:00", Subject: "Fizyka", Room: "19 36"},
			After:  model.Event{Date: "2026-11-24", Start: "08:00", Subject: "Fizyka", Room: "233 K"},
		}},
		Added: []model.Event{{Date: "2026-11-25", Start: "11:40", Subject: "Chemia"}},
	}
	p := Describe("WMT26AX3S1", "", c)
	if p.Title != "Zmiana w planie WMT26AX3S1" || p.URL != "/app?plan=WMT26AX3S1&changes=1" {
		t.Fatalf("payload %+v", p)
	}
	want := "Przeniesione: Matematyka 2 (wykład), pt 20.11 08:00 → pn 23.11 09:50 · i 2 inne zmiany"
	if p.Body != want {
		t.Fatalf("body %q\nchcę %q", p.Body, want)
	}
	lines := Lines(c)
	if lines[1] != "Fizyka, wt 24.11 08:00: sala 19 36 → 233 K" {
		t.Fatalf("linia zmiany sali %q", lines[1])
	}
	if _, err := json.Marshal(p); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreSubscriptionsFromMirror(t *testing.T) {
	first, _ := newTestService(t)
	mirror := durable.NewMemory()
	if _, err := first.Restore(mirror); err != nil {
		t.Fatal(err)
	}
	_ = first.Subscribe(sub("https://fcm.googleapis.com/a", "WMT26AX3S1"))

	second, _ := newTestService(t)
	added, err := second.Restore(mirror)
	if err != nil || added != 1 || second.Count() != 1 {
		t.Fatalf("po deployu odtworzono %d (%v), jest %d", added, err, second.Count())
	}
}

func TestValidateKeys(t *testing.T) {
	public, private, err := GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	_, other, _ := GenerateKeys()
	if err := ValidateKeys(public, private); err != nil {
		t.Fatalf("poprawne klucze: %v", err)
	}
	if err := ValidateKeys(" "+public+"\n", private); err != nil {
		t.Fatalf("klucz z białymi znakami (wklejony z terminala): %v", err)
	}
	for name, keys := range map[string][2]string{
		"puste":       {"", private},
		"nie base64":  {"!!!", private},
		"zamienione":  {private, public},
		"niepasujące": {public, other},
		"krótki":      {public, "AAAA"},
	} {
		if err := ValidateKeys(keys[0], keys[1]); err == nil {
			t.Errorf("%s: brak błędu", name)
		}
	}
	if _, err := New("x", "y", "mailto:a@b.c", t.TempDir()+"/push.json"); err == nil {
		t.Fatal("New przyjął złe klucze")
	}
}
