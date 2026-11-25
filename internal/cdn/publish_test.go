package cdn

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeBucket struct {
	mu      sync.Mutex
	objects map[string][]byte
	heads   int
	headErr error
}

func (f *fakeBucket) Put(key string, value []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[key] = append([]byte(nil), value...)
	return nil
}

func (f *fakeBucket) All(string) (map[string][]byte, error) { return nil, nil }

func (f *fakeBucket) ETags(prefix string) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.heads++
	if f.headErr != nil {
		return nil, f.headErr
	}
	out := map[string]string{}
	for k, v := range f.objects {
		if strings.HasPrefix(k, prefix) {
			out[k] = ContentETag(v)
		}
	}
	return out, nil
}

func (f *fakeBucket) get(key string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.objects[key]
	return string(v), ok
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("nie doczekałem się")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestBackfillUploadsOnlyMissingOrChanged(t *testing.T) {
	bucket := &fakeBucket{objects: map[string][]byte{
		PlanKey("SAME"):    []byte(`{"v":1}`),
		PlanKey("CHANGED"): []byte(`{"v":"stare"}`),
	}}
	local := map[string][]byte{"SAME": []byte(`{"v":1}`), "CHANGED": []byte(`{"v":"nowe"}`), "NEW": []byte(`{"v":3}`)}
	p := NewPublisher(bucket, nil)
	queued, current, err := p.Backfill([]string{"SAME", "CHANGED", "NEW", "GONE"}, func(id string) ([]byte, error) {
		if v, ok := local[id]; ok {
			return v, nil
		}
		return nil, errors.New("brak pliku")
	})
	if err != nil || queued != 2 || current != 1 || bucket.heads != 1 {
		t.Fatalf("queued %d current %d err %v", queued, current, err)
	}
	waitFor(t, func() bool {
		a, _ := bucket.get(PlanKey("CHANGED"))
		_, b := bucket.get(PlanKey("NEW"))
		return a == `{"v":"nowe"}` && b
	})
}

func TestBackfillStopsWhenBucketUnreachable(t *testing.T) {
	bucket := &fakeBucket{objects: map[string][]byte{}, headErr: errors.New("403")}
	p := NewPublisher(bucket, nil)
	_, _, err := p.Backfill([]string{"A", "B", "C"}, func(string) ([]byte, error) { return []byte("{}"), nil })
	if err == nil || bucket.heads != 1 {
		t.Fatalf("err %v, zapytań o listę %d (chcę przerwać po pierwszym błędzie)", err, bucket.heads)
	}
}

func TestPublishUploadsPlanKey(t *testing.T) {
	bucket := &fakeBucket{objects: map[string][]byte{}}
	p := NewPublisher(bucket, nil)
	p.Publish("WMT26AX3S1", []byte(`{"events":[]}`))
	waitFor(t, func() bool { v, _ := bucket.get("plans/WMT26AX3S1.json"); return v == `{"events":[]}` })
	if failed, saved := p.Status(); failed != "" || saved != 1 {
		t.Fatalf("status %q %d", failed, saved)
	}
}
