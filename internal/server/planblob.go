package server

import (
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"encoding/hex"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/andybalholm/brotli"
)

const fastBrotli = 5

type planBlob struct {
	etag   string
	raw    []byte
	gz     []byte
	fastBr []byte
	best   atomic.Pointer[[]byte]
	done   chan struct{}
}

type planBlobs struct {
	mu sync.Mutex
	m  map[string]*planBlob
}

func blobETag(data []byte) string {
	sum := sha1.Sum(data)
	return `"` + hex.EncodeToString(sum[:10]) + `"`
}

func (c *planBlobs) get(id string, data []byte) *planBlob {
	etag := blobETag(data)
	c.mu.Lock()
	defer c.mu.Unlock()
	if b := c.m[id]; b != nil && b.etag == etag {
		return b
	}
	b := &planBlob{etag: etag, raw: data, gz: gzipBytes(data, gzip.BestCompression), fastBr: brotliBytes(data, fastBrotli), done: make(chan struct{})}
	go func() {
		best := brotliBytes(data, brotli.BestCompression)
		b.best.Store(&best)
		close(b.done)
	}()
	if c.m == nil {
		c.m = make(map[string]*planBlob)
	}
	c.m[id] = b
	return b
}

func (c *planBlobs) warm(id string, data []byte) {
	c.mu.Lock()
	_, seen := c.m[id]
	c.mu.Unlock()
	if seen {
		<-c.get(id, data).done
	}
}

func (b *planBlob) br() []byte {
	if best := b.best.Load(); best != nil {
		return *best
	}
	return b.fastBr
}

func (b *planBlob) encoded(acceptEncoding string) ([]byte, string) {
	if acceptsEncoding(acceptEncoding, "br") {
		return b.br(), "br"
	}
	if acceptsEncoding(acceptEncoding, "gzip") {
		return b.gz, "gzip"
	}
	return b.raw, ""
}

func acceptsEncoding(header, name string) bool {
	for _, part := range strings.Split(header, ",") {
		token, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(token), name) {
			continue
		}
		q := strings.ReplaceAll(strings.TrimSpace(params), " ", "")
		return q != "q=0" && q != "q=0.0" && q != "q=0.00" && q != "q=0.000"
	}
	return false
}

func gzipBytes(data []byte, level int) []byte {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, level)
	zw.Write(data)
	zw.Close()
	return buf.Bytes()
}

func brotliBytes(data []byte, level int) []byte {
	var buf bytes.Buffer
	bw := brotli.NewWriterLevel(&buf, level)
	bw.Write(data)
	bw.Close()
	return buf.Bytes()
}
