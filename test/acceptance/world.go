package acceptance

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// target is the running service under test. It runs in a container started by
// scripts/integration.sh; the suite reaches it only over HTTP, exactly as a client would.
type target struct {
	baseURL string
	client  *http.Client
}

// newTarget reads SCHEDULER_BASE_URL and checks the service answers /healthz.
func newTarget(ctx context.Context) (*target, error) {
	baseURL := strings.TrimRight(os.Getenv("SCHEDULER_BASE_URL"), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("SCHEDULER_BASE_URL is not set\n\nRun the suite with `make test-integration`, which starts the service and its database")
	}
	t := &target{baseURL: baseURL, client: &http.Client{Timeout: 30 * time.Second}}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/healthz", nil)
	if err != nil {
		return nil, err
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("service at %s is not reachable: %w", baseURL, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("service at %s is not healthy: GET /healthz returned %d", baseURL, resp.StatusCode)
	}
	return t, nil
}

// world is the per-scenario state. A fresh one is made before every scenario.
type world struct {
	target *target
	status int
	body   []byte
}

func newWorld(t *target) *world {
	return &world{target: t}
}

// send makes an HTTP request to the service and records the response.
func (w *world) send(ctx context.Context, method, path, body string) error {
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, w.target.baseURL+path, r)
	if err != nil {
		return err
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := w.target.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	w.status = resp.StatusCode
	w.body, err = io.ReadAll(resp.Body)
	return err
}
