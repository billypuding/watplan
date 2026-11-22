package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"sort"
)

const serviceWorkerFile = "sw.js"

var (
	assetRef    = regexp.MustCompile(`/([A-Za-z0-9_./-]+)\?v=[A-Za-z0-9]*`)
	swCacheName = regexp.MustCompile(`const CACHE = '[^']*';`)
	inlineRef   = regexp.MustCompile(`<script src="/([A-Za-z0-9_./-]+\.js)(?:\?v=[A-Za-z0-9]*)?" data-inline></script>`)
	textAssets  = map[string]bool{".html": true, ".css": true, ".js": true, ".json": true}
)

type assetFS struct {
	base    fs.FS
	files   map[string][]byte
	hashes  map[string]string
	version string
}

func newAssetFS(base fs.FS) (*assetFS, error) {
	a := &assetFS{base: base, files: make(map[string][]byte), hashes: make(map[string]string)}
	var names []string
	err := fs.WalkDir(base, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		names = append(names, name)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(names)

	sum := sha256.New()
	for _, name := range names {
		if name == serviceWorkerFile {
			continue
		}
		hash, err := a.resolve(name, map[string]bool{})
		if err != nil {
			return nil, err
		}
		io.WriteString(sum, name+"\x00"+hash+"\x00")
	}
	a.version = hex.EncodeToString(sum.Sum(nil))[:12]

	if _, err := fs.Stat(base, serviceWorkerFile); err == nil {
		sw, err := a.rewrite(serviceWorkerFile, map[string]bool{})
		if err != nil {
			return nil, err
		}
		a.files[serviceWorkerFile] = swCacheName.ReplaceAll(sw, []byte("const CACHE = 'watplan-"+a.version+"';"))
	}
	return a, nil
}

func (a *assetFS) resolve(name string, visiting map[string]bool) (string, error) {
	if hash, ok := a.hashes[name]; ok {
		return hash, nil
	}
	var data []byte
	var err error
	if textAssets[path.Ext(name)] && !visiting[name] {
		visiting[name] = true
		data, err = a.rewrite(name, visiting)
		delete(visiting, name)
	} else {
		data, err = fs.ReadFile(a.base, name)
	}
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])[:10]
	a.hashes[name] = hash
	return hash, nil
}

func (a *assetFS) rewrite(name string, visiting map[string]bool) ([]byte, error) {
	data, err := fs.ReadFile(a.base, name)
	if err != nil {
		return nil, err
	}
	var resolveErr error
	if path.Ext(name) == ".html" {
		data = inlineRef.ReplaceAllFunc(data, func(ref []byte) []byte {
			target := string(inlineRef.FindSubmatch(ref)[1])
			script, err := fs.ReadFile(a.base, target)
			if err != nil {
				resolveErr = err
				return ref
			}
			if bytes.Contains(bytes.ToLower(script), []byte("</script")) {
				resolveErr = fmt.Errorf("%s: nie można wstawić skryptu zawierającego </script", target)
				return ref
			}
			return append(append([]byte("<script>"), bytes.TrimSpace(script)...), []byte("</script>")...)
		})
		if resolveErr != nil {
			return nil, resolveErr
		}
	}
	out := assetRef.ReplaceAllFunc(data, func(ref []byte) []byte {
		target := string(assetRef.FindSubmatch(ref)[1])
		if _, err := fs.Stat(a.base, target); err != nil {
			return ref
		}
		hash, err := a.resolve(target, visiting)
		if err != nil {
			resolveErr = err
			return ref
		}
		return []byte("/" + target + "?v=" + hash)
	})
	if resolveErr != nil {
		return nil, resolveErr
	}
	original, err := fs.ReadFile(a.base, name)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(out, original) {
		a.files[name] = out
	}
	return out, nil
}

func (a *assetFS) Open(name string) (fs.File, error) {
	data, ok := a.files[name]
	if !ok {
		return a.base.Open(name)
	}
	f, err := a.base.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	f.Close()
	if err != nil {
		return nil, err
	}
	return &memFile{Reader: bytes.NewReader(data), info: memInfo{FileInfo: info, size: int64(len(data))}}, nil
}

type memFile struct {
	*bytes.Reader
	info memInfo
}

func (f *memFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *memFile) Close() error               { return nil }

type memInfo struct {
	fs.FileInfo
	size int64
}

func (i memInfo) Size() int64 { return i.size }
