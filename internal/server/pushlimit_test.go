package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/billypuding/watplan/internal/config"
	"github.com/billypuding/watplan/internal/push"
	"github.com/billypuding/watplan/internal/store"
)

func TestPushSubscribeLimitCountsOnlyNewDevices(t *testing.T) {
	public, private, err := push.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	svc, err := push.New(public, private, "mailto:test@example.com", filepath.Join(t.TempDir(), "push.json"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := config.NewRegistry([]config.PlanConfig{{ID: "WMT26AX3S1", URL: "http://example.invalid/a.htm", Faculty: "WML"}})
	h, err := NewServer(reg, st, nil, Options{Offline: true, Push: svc, PushPerHour: 2}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	subscribe := func(n int, ip string) int {
		body, _ := json.Marshal(map[string]any{
			"subscription": map[string]any{
				"endpoint": fmt.Sprintf("https://fcm.googleapis.com/fcm/send/device-%d", n),
				"keys":     map[string]string{"p256dh": "key", "auth": "auth"},
			},
			"plans": []string{"WMT26AX3S1"},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/push/subscribe", bytes.NewReader(body))
		req.RemoteAddr = ip + ":1234"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if subscribe(1, "203.0.113.7") != http.StatusOK || subscribe(2, "203.0.113.7") != http.StatusOK {
		t.Fatal("dwie pierwsze nowe subskrypcje powinny przejść")
	}
	if code := subscribe(3, "203.0.113.7"); code != http.StatusTooManyRequests {
		t.Fatalf("trzecia nowa subskrypcja z tego IP: %d, chcę 429", code)
	}
	for i := 0; i < 5; i++ {
		if code := subscribe(1, "203.0.113.7"); code != http.StatusOK {
			t.Fatalf("ponowna synchronizacja znanego urządzenia: %d, chcę 200", code)
		}
	}
	if code := subscribe(3, "198.51.100.9"); code != http.StatusOK {
		t.Fatalf("inne IP: %d, chcę 200", code)
	}
	if svc.Count() != 3 {
		t.Fatalf("subskrypcji %d, chcę 3", svc.Count())
	}
}
