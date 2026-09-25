package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func newTestClient(get func(ctx context.Context, key string) (map[string]string, error)) (*Client, *time.Time) {
	now := time.Unix(1_700_000_000, 0)
	c := NewClient("127.0.0.1:0")
	c.get = get
	c.now = func() time.Time { return now }
	return c, &now
}

func TestClientCachesFailures(t *testing.T) {
	forbidden := ReplyError{Code: "FORBIDDEN", Message: "record is not visible to this service"}
	calls := 0
	c, now := newTestClient(func(ctx context.Context, key string) (map[string]string, error) {
		calls++
		return nil, forbidden
	})

	for i := 0; i < 100; i++ {
		if _, err := c.GetRecord(context.Background(), "orders"); err != forbidden {
			t.Fatalf("err = %v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("a failed lookup must not be repeated on every call: %d calls", calls)
	}

	*now = now.Add(errorCacheTTL - time.Second)
	_, _ = c.GetRecord(context.Background(), "orders")
	if calls != 1 {
		t.Fatalf("calls = %d before the TTL", calls)
	}

	*now = now.Add(2 * time.Second)
	_, _ = c.GetRecord(context.Background(), "orders")
	if calls != 2 {
		t.Fatalf("the lookup is retried once the TTL has expired: %d calls", calls)
	}

	_, _ = c.GetRecord(context.Background(), "invoices")
	if calls != 3 {
		t.Fatalf("failures are cached per key: %d calls", calls)
	}
}

func TestClientCachesSuccesses(t *testing.T) {
	calls := 0
	c, _ := newTestClient(func(ctx context.Context, key string) (map[string]string, error) {
		calls++
		return map[string]string{"owner": "team-a"}, nil
	})
	for i := 0; i < 10; i++ {
		res, err := c.GetRecord(context.Background(), "orders")
		if err != nil || res["owner"] != "team-a" {
			t.Fatalf("res = %v, err = %v", res, err)
		}
	}
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestIsErrorReply(t *testing.T) {
	forbidden := ReplyError{Code: "FORBIDDEN"}
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{forbidden, true},
		{fmt.Errorf("wrapped: %w", forbidden), true},
		{errors.New("connection reset by peer"), false},
		{context.DeadlineExceeded, false},
	} {
		if got := isErrorReply(tc.err); got != tc.want {
			t.Errorf("isErrorReply(%v) = %v", tc.err, got)
		}
	}
}

func TestLoadMetadataFallsBackOnLookupError(t *testing.T) {
	orig := inventory
	defer func() { inventory = orig }()
	inventory, _ = newTestClient(func(ctx context.Context, key string) (map[string]string, error) {
		return nil, ReplyError{Code: "FORBIDDEN"}
	})

	md, err := loadMetadata(&Item{ID: "/tenants/rec-orders", Kind: KindRecord})
	if err != nil {
		t.Fatal(err)
	}
	if md.record.Key != "orders" || md.record.IsSystem() {
		t.Errorf("record = %+v", md.record)
	}
}
