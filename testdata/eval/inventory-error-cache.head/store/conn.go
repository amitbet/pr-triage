package store

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
)

// ReplyError is an error answer from the inventory server. It is returned
// by value: the connection is healthy, the server just refused the request.
type ReplyError struct {
	Code    string
	Message string
}

func (e ReplyError) Error() string { return e.Code + ": " + e.Message }

// Conn is one line-protocol connection to the inventory server.
type Conn struct {
	nc net.Conn
	rd *bufio.Reader
}

func Dial(ctx context.Context, addr string) (*Conn, error) {
	var d net.Dialer
	nc, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	return &Conn{nc: nc, rd: bufio.NewReader(nc)}, nil
}

func (c *Conn) Close() error { return c.nc.Close() }

// Get fetches every field of a record.
func (c *Conn) Get(ctx context.Context, key string) (map[string]string, error) {
	if dl, ok := ctx.Deadline(); ok {
		_ = c.nc.SetDeadline(dl)
	}
	if _, err := fmt.Fprintf(c.nc, "GET %s\n", key); err != nil {
		return nil, err
	}
	res := map[string]string{}
	for {
		line, err := c.rd.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		switch {
		case line == "END":
			return res, nil
		case strings.HasPrefix(line, "ERR "):
			code, msg, _ := strings.Cut(strings.TrimPrefix(line, "ERR "), " ")
			return nil, ReplyError{Code: code, Message: msg}
		default:
			k, v, _ := strings.Cut(line, "=")
			res[k] = v
		}
	}
}
