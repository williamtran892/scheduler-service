package acceptance

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cucumber/godog"
)

// registerHTTPSteps holds the generic request and response steps every feature can use.
func registerHTTPSteps(sc *godog.ScenarioContext, w *world) {
	sc.Step(`^I send a (GET|POST|PUT|PATCH|DELETE) request to "([^"]*)"$`, func(ctx context.Context, method, path string) error {
		return w.send(ctx, method, path, "")
	})
	sc.Step(`^I send a (POST|PUT|PATCH) request to "([^"]*)" with body:$`, func(ctx context.Context, method, path string, body *godog.DocString) error {
		return w.send(ctx, method, path, body.Content)
	})
	sc.Step(`^the response status is (\d+)$`, w.responseStatusIs)
	sc.Step(`^the response JSON field "([^"]*)" is "([^"]*)"$`, w.responseJSONFieldIs)
}

func (w *world) responseStatusIs(want int) error {
	if w.status != want {
		return fmt.Errorf("response status is %d, want %d; body: %s", w.status, want, w.body)
	}
	return nil
}

func (w *world) responseJSONFieldIs(field, want string) error {
	var body map[string]any
	if err := json.Unmarshal(w.body, &body); err != nil {
		return fmt.Errorf("response is not a JSON object: %w; body: %s", err, w.body)
	}
	got, ok := body[field]
	if !ok {
		return fmt.Errorf("response has no field %q; body: %s", field, w.body)
	}
	if fmt.Sprint(got) != want {
		return fmt.Errorf("field %q is %q, want %q", field, fmt.Sprint(got), want)
	}
	return nil
}
