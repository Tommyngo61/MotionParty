package localapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"
)

// Client talks to a running agent over its unix socket.
type Client struct {
	http *http.Client
	path string
}

// NewClient builds a client for the socket at path.
func NewClient(path string) *Client {
	return &Client{
		path: path,
		http: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, "unix", path)
				},
			},
		},
	}
}

// Status fetches the agent's status.
//
// The errors here are written for a field tech, because they are the most
// likely thing that tech will see: "connection refused" means the agent is not
// running, "permission denied" means they are not in the right group, and
// neither is obvious from the raw syscall error.
func (c *Client) Status(ctx context.Context) (*Status, error) {
	body, err := c.get(ctx, "/status")
	if err != nil {
		return nil, err
	}
	var s Status
	if err := json.Unmarshal(body, &s); err != nil {
		return nil, fmt.Errorf("meternodectl: could not parse the agent's response: %w", err)
	}
	return &s, nil
}

// Sample fetches the most recent raw sample.
func (c *Client) Sample(ctx context.Context) (json.RawMessage, error) {
	return c.get(ctx, "/sample")
}

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	if _, err := os.Stat(c.path); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("meternodectl: no agent socket at %s — the agent is not running.\n"+
				"  Try: systemctl status meternode-agent", c.path)
		}
		if os.IsPermission(err) {
			return nil, fmt.Errorf("meternodectl: permission denied on %s — you are not in the agent's group.\n"+
				"  Try: sudo meternodectl status", c.path)
		}
		return nil, err
	}

	// The host in the URL is ignored: the transport dials the socket. It has
	// to be syntactically valid, hence the placeholder.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://agent"+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("meternodectl: could not reach the agent on %s: %w.\n"+
			"  The socket exists but nothing is listening; the agent may be starting or wedged.\n"+
			"  Try: systemctl status meternode-agent", c.path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("meternodectl: agent returned %s: %s", resp.Status, string(body))
	}
	return body, nil
}
