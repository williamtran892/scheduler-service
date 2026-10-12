package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
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

// response is one HTTP exchange as the suite saw it.
type response struct {
	status int
	header http.Header
	body   []byte
}

// json decodes the body as a JSON object.
func (r response) json() (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(r.body, &m); err != nil {
		return nil, fmt.Errorf("response %d is not a JSON object: %w; body: %s", r.status, err, r.body)
	}
	return m, nil
}

// world is the per-scenario state. A fresh one is made before every scenario.
type world struct {
	target *target
	status int
	body   []byte
	header http.Header

	prepared     *bookingRequest  // built by "prepares a booking", edited, then sent
	current      map[string]any   // "the appointment": the last appointment returned (201 or 200)
	booked       map[string]any   // the last 201 booking response, for "equals the booking response"
	appointments []map[string]any // every appointment confirmed in this scenario, in order
	raced        []response       // outcomes of the last "at the same time" step, in request order
}

func newWorld(t *target) *world {
	return &world{target: t}
}

// send makes an HTTP request to the service and records the response.
func (w *world) send(ctx context.Context, method, path, body string) error {
	resp, err := w.do(ctx, method, path, []byte(body), nil)
	if err != nil {
		return err
	}
	w.record(resp)
	return nil
}

// do performs one request with the given headers and returns the response without recording it,
// so race steps can call it from several goroutines.
func (w *world) do(ctx context.Context, method, path string, body []byte, headers map[string]string) (response, error) {
	var r io.Reader
	if len(body) > 0 {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, w.target.baseURL+path, r)
	if err != nil {
		return response{}, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := w.target.client.Do(req)
	if err != nil {
		return response{}, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return response{}, fmt.Errorf("%s %s: read body: %w", method, path, err)
	}
	return response{status: resp.StatusCode, header: resp.Header, body: b}, nil
}

// record makes resp the scenario's last response and, when it carries an appointment,
// "the appointment".
func (w *world) record(resp response) {
	w.status, w.body, w.header = resp.status, resp.body, resp.header
	if resp.status != http.StatusOK && resp.status != http.StatusCreated {
		return
	}
	appt, err := resp.json()
	if err != nil || appt["id"] == nil {
		return
	}
	w.current = appt
	if resp.status == http.StatusCreated {
		w.booked = appt
		w.appointments = append(w.appointments, appt)
	}
}

// last returns the last response as a response value.
func (w *world) last() response {
	return response{status: w.status, header: w.header, body: w.body}
}
