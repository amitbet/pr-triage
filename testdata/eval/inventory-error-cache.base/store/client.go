package store

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	requestTimeout = time.Second
	inventory      = NewClient(inventoryAddr())

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

type Client struct {
	addr  string
	conn  *Conn
	cache map[string]map[string]string
}

func NewClient(addr string) *Client {
	return &Client{
		addr:  addr,
		cache: map[string]map[string]string{},
	}
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

func (c *Client) GetRecord(ctx context.Context, key string, retry bool) (map[string]string, error) {
	if res, ok := c.cache[key]; ok {
		return res, nil
	}
	if c.conn == nil {
		if err := c.connect(); err != nil {
			return nil, err
		}
	}
	res, err := c.conn.Get(ctx, key)
	switch {
	case err == nil:
		c.cache[key] = res
		return res, nil
	case retry:
		c.close()
		return c.GetRecord(ctx, key, false)
	default:
		return nil, err
	}
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
	fields, err := inventory.GetRecord(ctx, rec.Key, true)
	if err != nil {
		return rec, fmt.Errorf("failed to get inventory record: %w", err)
	}
	rec.Owner = fields["owner"]
	if v, ok := fields["version"]; ok {
		rec.Version, _ = strconv.Atoi(v)
	}
	return rec, nil
}
