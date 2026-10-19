package dupes

import (
	"encoding/gob"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// The cache remembers photo hashes between scans, so looking through the
// same folders again is quick. An entry is used only while the file's size
// and modified time are unchanged.

type cacheEntry struct {
	Size   int64
	Mod    int64
	Hash   Hash
	W, H   int32
	Orient int8
	Taken  int64 // unix seconds, 0 = unknown
	OK     bool  // false: the file could not be read as a picture
	Used   int64 // unix seconds of the last scan that used it
}

type hashCache struct {
	mu    sync.Mutex
	path  string
	m     map[string]*cacheEntry
	dirty bool
}

const (
	cacheVersion = 1
	cacheMax     = 300_000
	cacheMaxAge  = 180 * 24 * time.Hour
)

type cacheFile struct {
	Version int
	Entries map[string]*cacheEntry
}

func cacheKey(path string) string { return strings.ToUpper(filepath.Clean(path)) }

func loadCache(path string) *hashCache {
	c := &hashCache{path: path, m: map[string]*cacheEntry{}}
	if path == "" {
		return c
	}
	f, err := os.Open(path)
	if err != nil {
		return c
	}
	defer f.Close()
	var cf cacheFile
	if gob.NewDecoder(f).Decode(&cf) == nil && cf.Version == cacheVersion && cf.Entries != nil {
		c.m = cf.Entries
	}
	return c
}

func (c *hashCache) get(f *File) (cacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[cacheKey(f.Path)]
	if !ok || e.Size != f.Size || e.Mod != f.Modified.UnixNano() {
		return cacheEntry{}, false
	}
	e.Used = time.Now().Unix()
	c.dirty = true
	return *e, true
}

func (c *hashCache) put(f *File, e cacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e.Used = time.Now().Unix()
	c.m[cacheKey(f.Path)] = &e
	c.dirty = true
}

func (c *hashCache) save() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.path == "" || !c.dirty {
		return
	}
	oldest := time.Now().Add(-cacheMaxAge).Unix()
	for k, e := range c.m {
		if e.Used < oldest {
			delete(c.m, k)
		}
	}
	if len(c.m) > cacheMax {
		keys := make([]string, 0, len(c.m))
		for k := range c.m {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return c.m[keys[i]].Used > c.m[keys[j]].Used })
		for _, k := range keys[cacheMax:] {
			delete(c.m, k)
		}
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return
	}
	tmp := c.path + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return
	}
	err = gob.NewEncoder(f).Encode(cacheFile{Version: cacheVersion, Entries: c.m})
	f.Close()
	if err != nil {
		os.Remove(tmp)
		return
	}
	os.Remove(c.path)
	if os.Rename(tmp, c.path) == nil {
		c.dirty = false
	}
}
