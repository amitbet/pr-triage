package indexer

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
)

// parseSem bounds the files being parsed (or resolved) at once, across all
// repos.
var parseSem = make(chan struct{}, runtime.GOMAXPROCS(0))

// parallel runs fn(0..n-1) concurrently, bounded by parseSem. fn must not
// wait on parseSem itself.
func parallel(n int, fn func(i int)) {
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			parseSem <- struct{}{}
			defer func() { <-parseSem }()
			fn(i)
		}()
	}
	wg.Wait()
}

// A parse cache keeps each file's parse result from the last extraction of
// a repo, keyed by path and content, so re-extracting a repo re-parses only
// the files that changed. Name resolution still runs over every file, so the
// graph is the same as a full extraction's. It is used for the tree-sitter
// languages, where parsing is most of the work; decoding an entry is about
// 14x faster than parsing the file.
type parseCache struct {
	dir          string // one file per language
	fresh        bool   // parse everything (a forced extraction), still saving the results
	reused, seen atomic.Int64
}

// parseCaches holds the cache of each repo being extracted, by repo root.
// Without one, parseFiles parses every file.
var parseCaches sync.Map

type parseCacheFile struct {
	Version int               // extractorVersion
	Deps    string            // depsHash: a grammar upgrade changes parses
	Entries map[string][]byte // sha256(path, content) -> gob of the parse
}

// relinker is a parse result that restores internal pointers after decoding.
type relinker interface{ Relink() }

// parseFiles reads and parses repo-relative paths in parallel. out[i] is nil
// when paths[i] can't be read or is over 2 MiB. lang names the parse cache
// ("" for none).
func parseFiles[F any](root, lang string, paths []string, parse func(p, src string) *F) []*F {
	out := make([]*F, len(paths))
	var pc *parseCache
	if v, ok := parseCaches.Load(root); ok && lang != "" {
		pc = v.(*parseCache)
	}
	var old map[string][]byte
	fresh := map[string][]byte{}
	var mu sync.Mutex
	if pc != nil {
		old = pc.load(lang)
	}
	var wg sync.WaitGroup
	for i, p := range paths {
		wg.Add(1)
		go func() {
			defer wg.Done()
			parseSem <- struct{}{}
			defer func() { <-parseSem }()
			b, err := os.ReadFile(filepath.Join(root, p))
			if err != nil || len(b) > 2<<20 {
				return
			}
			if pc == nil {
				out[i] = parse(p, string(b))
				return
			}
			h := sha256.New()
			h.Write([]byte(p))
			h.Write([]byte{0})
			h.Write(b)
			key := string(h.Sum(nil))
			pc.seen.Add(1)
			if blob, ok := old[key]; ok {
				f := new(F)
				if gob.NewDecoder(bytes.NewReader(blob)).Decode(f) == nil {
					if r, ok := any(f).(relinker); ok {
						r.Relink()
					}
					out[i] = f
					pc.reused.Add(1)
					mu.Lock()
					fresh[key] = blob
					mu.Unlock()
					return
				}
			}
			f := parse(p, string(b))
			out[i] = f
			var buf bytes.Buffer
			if gob.NewEncoder(&buf).Encode(f) == nil {
				mu.Lock()
				fresh[key] = buf.Bytes()
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if pc != nil && !sameKeys(old, fresh) {
		pc.store(lang, fresh)
	}
	return out
}

func sameKeys(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			return false
		}
	}
	return true
}

func (pc *parseCache) path(lang string) string { return filepath.Join(pc.dir, lang+".gob") }

// load returns the entries saved by the last extraction, nil if there are
// none or they were written by another extractor version or build of the
// grammars.
func (pc *parseCache) load(lang string) map[string][]byte {
	if pc.fresh {
		return nil
	}
	f, err := os.Open(pc.path(lang))
	if err != nil {
		return nil
	}
	defer f.Close()
	var c parseCacheFile
	if gob.NewDecoder(f).Decode(&c) != nil || c.Version != extractorVersion || c.Deps != depsHash() {
		return nil
	}
	return c.Entries
}

// store replaces the language's entries, so files that are gone drop out.
// A failed write only costs the next run its reuse.
func (pc *parseCache) store(lang string, entries map[string][]byte) {
	if os.MkdirAll(pc.dir, 0o755) != nil {
		return
	}
	p := pc.path(lang)
	tmp := p + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return
	}
	err = gob.NewEncoder(f).Encode(parseCacheFile{Version: extractorVersion, Deps: depsHash(), Entries: entries})
	if cerr := f.Close(); err != nil || cerr != nil {
		os.Remove(tmp)
		return
	}
	os.Rename(tmp, p)
}
