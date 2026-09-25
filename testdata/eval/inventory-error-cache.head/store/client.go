package store

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	requestTimeout = time.Second
	inventory      = NewClient(inventoryAddr())

	// How long a failed record lookup is remembered before the server is asked
	// again. Some records are permanently forbidden to this service, so asking
	// on every Track call only burns connections.
	errorCacheTTL = 5 * time.Minute

	systemPrefixes = []string{
		"sys-",
		"internal-",
		"probe-",
		"healthcheck",
		"canary-",
		"bootstrap",
		"migrator",
		"reaper",
		"scheduler-",
		"metrics-",
		"audit-",
		"backup-",
		"cron-",
		"sidecar-",
	}
)

type cachedError struct {
	err       error
	expiresAt time.Time
}

type Client struct {
	addr   string
	conn   *Conn
	cache  map[string]map[string]string
	errors map[string]cachedError

	// overridable in tests
	get func(ctx context.Context, key string) (map[string]string, error)
	now func() time.Time
}

func NewClient(addr string) *Client {
	c := &Client{
		addr:   addr,
		cache:  map[string]map[string]string{},
		errors: map[string]cachedError{},
		now:    time.Now,
	}
	c.get = c.fetchFromServer
	return c
}

func inventoryAddr() string {
	if a := os.Getenv("INVENTORY_ADDR"); a != "" {
		return a
	}
	return "127.0.0.1:7070"
}

func (c *Client) close() {
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

func (c *Client) connect() error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	var err error
	c.conn, err = Dial(ctx, c.addr)
	if err != nil {
		return fmt.Errorf("failed to connect to inventory at %s: %w", c.addr, err)
	}
	return nil
}

func (c *Client) GetRecord(ctx context.Context, key string) (map[string]string, error) {
	if res, ok := c.cache[key]; ok {
		return res, nil
	}
	if e, ok := c.errors[key]; ok {
		if c.now().Before(e.expiresAt) {
			return nil, e.err
		}
		delete(c.errors, key)
	}
	res, err := c.get(ctx, key)
	if err != nil {
		log.Printf("failed to get inventory record %s, not retrying for %s: %s", key, errorCacheTTL, err)
		c.errors[key] = cachedError{err: err, expiresAt: c.now().Add(errorCacheTTL)}
		return nil, err
	}
	c.cache[key] = res
	return res, nil
}

func (c *Client) fetchFromServer(ctx context.Context, key string) (map[string]string, error) {
	for attempt := 0; ; attempt++ {
		if c.conn == nil {
			if err := c.connect(); err != nil {
				return nil, err
			}
		}
		res, err := c.conn.Get(ctx, key)
		if err == nil {
			return res, nil
		}
		// An error reply (e.g. FORBIDDEN) means the connection is healthy and
		// the server answered: reconnecting would only get the same answer.
		if attempt > 0 || isErrorReply(err) {
			return nil, err
		}
		c.close()
	}
}

func isErrorReply(err error) bool {
	var e ReplyError
	return errors.As(err, &e)
}

type Record struct {
	Key     string
	Owner   string
	Version int
}

func (r Record) IsSystem() bool {
	for _, p := range systemPrefixes {
		if strings.HasPrefix(r.Key, p) {
			return true
		}
	}
	return false
}

func getRecord(id string) (Record, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	rec := Record{}
	parts := strings.Split(strings.Trim(id, "/"), "/")
	for _, p := range parts {
		if strings.HasPrefix(p, "rec-") {
			rec.Key = strings.TrimPrefix(p, "rec-")
		}
	}
	if rec.Key == "" {
		rec.Key = parts[len(parts)-1]
	}
	fields, err := inventory.GetRecord(ctx, rec.Key)
	if err != nil {
		return rec, fmt.Errorf("failed to get inventory record: %w", err)
	}
	rec.Owner = fields["owner"]
	if v, ok := fields["version"]; ok {
		rec.Version, _ = strconv.Atoi(v)
	}
	return rec, nil
}
