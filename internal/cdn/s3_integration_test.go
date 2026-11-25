package cdn

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func testBucket(t *testing.T) *Bucket {
	t.Helper()
	endpoint := os.Getenv("PLANWAT_TEST_S3_URL")
	if endpoint == "" {
		t.Skip("PLANWAT_TEST_S3_URL nie ustawiony (serwer zgodny z S3, np. versity/versitygw)")
	}
	name := fmt.Sprintf("watplan-test-%d", time.Now().UnixNano())
	b, err := New(endpoint, name, os.Getenv("PLANWAT_TEST_S3_KEY"), os.Getenv("PLANWAT_TEST_S3_SECRET"), "us-east-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := b.CreateBucket(); err != nil {
		t.Fatalf("tworzenie bucketu: %v", err)
	}
	return b
}

func (b *Bucket) get(t *testing.T, key string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, b.objectURL(key).String(), nil)
	b.sign(req, nil)
	resp, err := b.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

func TestS3PutHeadGet(t *testing.T) {
	b := testBucket(t)
	plan := []byte(`{"events":[{"subject":"Matematyka 2","room":"19 36"}],"fetched_at":"2026-11-23 12:00:00"}`)
	key := PlanKey("WMT26AX3S1")
	if etag, err := b.ETag(key); err != nil || etag != "" {
		t.Fatalf("brakujący obiekt: %q %v", etag, err)
	}
	if err := b.Put(key, plan); err != nil {
		t.Fatalf("Put: %v", err)
	}
	etag, err := b.ETag(key)
	if err != nil || etag != ContentETag(plan) {
		t.Fatalf("ETag %q (%v), chcę MD5 treści %q", etag, err, ContentETag(plan))
	}
	resp, body := b.get(t, key)
	if resp.StatusCode != http.StatusOK || body != string(plan) {
		t.Fatalf("GET %d %q", resp.StatusCode, body)
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") || resp.Header.Get("Cache-Control") != planCache {
		t.Fatalf("nagłówki %q %q", resp.Header.Get("Content-Type"), resp.Header.Get("Cache-Control"))
	}
	if err := b.Put(key, []byte(`{"events":[]}`)); err != nil {
		t.Fatalf("nadpisanie: %v", err)
	}
	if etag, _ := b.ETag(key); etag != ContentETag([]byte(`{"events":[]}`)) {
		t.Fatalf("ETag po nadpisaniu %q", etag)
	}
}

func TestS3RejectsWrongSecret(t *testing.T) {
	b := testBucket(t)
	b.secret = "zly-sekret"
	err := b.Put(PlanKey("A1"), []byte("{}"))
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("zły sekret: %v, chcę 403", err)
	}
}

func TestS3ListETagsAcrossPages(t *testing.T) {
	b := testBucket(t)
	want := map[string]string{}
	for i := 0; i < 1005; i++ {
		key := PlanKey(fmt.Sprintf("P%04d", i))
		body := []byte(fmt.Sprintf(`{"n":%d}`, i))
		if err := b.Put(key, body); err != nil {
			t.Fatal(err)
		}
		want[key] = ContentETag(body)
	}
	if err := b.Put("other/x.json", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	got, err := b.ETags("plans/")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("kluczy %d, chcę %d", len(got), len(want))
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s: %q, chcę %q", k, got[k], v)
		}
	}
}
