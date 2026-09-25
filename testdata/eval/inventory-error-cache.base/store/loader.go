package store

import (
	"errors"
	"sort"
	"strings"
	"sync"
)

type Kind int

const (
	KindUnknown Kind = iota
	KindRecord
	KindBlob
	KindLink
)

// Item is one entry the loader tracks.
type Item struct {
	ID    string
	Kind  Kind
	Owner string
}

// Metadata is what the loader knows about an item beyond its ID.
type Metadata struct {
	record Record
	labels map[string]string
	blobID string
}

type Loader struct {
	mu    sync.Mutex
	items map[string]*Item
	meta  map[string]*Metadata
}

func NewLoader() *Loader {
	return &Loader{items: map[string]*Item{}, meta: map[string]*Metadata{}}
}

var errNoBlob = errors.New("item has no blob id")

func (l *Loader) Track(it *Item) error {
	md, err := loadMetadata(it)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.items[it.ID] = it
	l.meta[it.ID] = md
	if md.record.Owner != "" {
		it.Owner = md.record.Owner
	}
	return nil
}

func (l *Loader) Forget(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.items, id)
	delete(l.meta, id)
}

func (l *Loader) IDs() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	ids := make([]string, 0, len(l.items))
	for id := range l.items {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func loadMetadata(it *Item) (*Metadata, error) {
	switch it.Kind {
	case KindRecord:
		var err error
		md := &Metadata{}
		md.record, err = getRecord(it.ID)
		return md, err
	case KindBlob, KindLink:
	default:
		return &Metadata{}, nil
	}
	blob := blobID(it.ID)
	if blob == "" {
		return nil, errNoBlob
	}
	return &Metadata{blobID: blob, labels: parseLabels(it.ID)}, nil
}

func blobID(id string) string {
	i := strings.LastIndex(id, "@")
	if i < 0 {
		return ""
	}
	return id[i+1:]
}

func parseLabels(id string) map[string]string {
	res := map[string]string{}
	q := strings.Index(id, "?")
	if q < 0 {
		return res
	}
	for _, kv := range strings.Split(id[q+1:], "&") {
		k, v, _ := strings.Cut(kv, "=")
		res[k] = v
	}
	return res
}
