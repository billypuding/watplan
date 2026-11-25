package cdn

import (
	"github.com/billypuding/watplan/internal/alert"
	"github.com/billypuding/watplan/internal/durable"
)

type objectStore interface {
	durable.Mirror
	ETags(prefix string) (map[string]string, error)
}

type Publisher struct {
	bucket objectStore
	queue  *durable.Queue
}

func NewPublisher(bucket objectStore, alerts *alert.Notifier) *Publisher {
	q := durable.NewQueue(bucket)
	q.SetAlert(alerts)
	q.SetAlertName("cdn", "Wysyłanie planów do CDN (R2)")
	return &Publisher{bucket: bucket, queue: q}
}

func (p *Publisher) Publish(id string, data []byte) {
	_ = p.queue.Put(PlanKey(id), data)
}

func (p *Publisher) Backfill(ids []string, raw func(id string) ([]byte, error)) (queued, current int, err error) {
	etags, err := p.bucket.ETags("plans/")
	if err != nil {
		return 0, 0, err
	}
	for _, id := range ids {
		data, rerr := raw(id)
		if rerr != nil {
			continue
		}
		if etags[PlanKey(id)] == ContentETag(data) {
			current++
			continue
		}
		p.Publish(id, data)
		queued++
	}
	return queued, current, nil
}

func (p *Publisher) Status() (string, int) {
	return p.queue.Status()
}
