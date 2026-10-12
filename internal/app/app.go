// Package app builds the service's HTTP handler: every route on one mux, in one place.
package app

import (
	"net/http"

	kithttp "github.com/go-kit/kit/transport/http"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"scheduler/internal/platform"
	schedsvc "scheduler/internal/scheduling/service"
)

// Deps is everything the handler needs. Each domain adds its repositories and
// collaborators here as it is built; main fills them with real implementations.
type Deps struct {
	DB         platform.Pinger
	Scheduling schedsvc.Endpoints
}

// NewHandler registers every route on one mux and wraps it in otelhttp, which opens
// one server span and records the HTTP metrics for each request.
func NewHandler(d Deps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", platform.Health(d.DB))
	// Each domain's transport registers here. Decoder failures are problems too, so every
	// transport encodes its errors with platform.EncodeError.
	schedsvc.RegisterRoutes(mux, d.Scheduling, kithttp.ServerErrorEncoder(platform.EncodeError))

	return otelhttp.NewHandler(mux, "http", otelhttp.WithSpanNameFormatter(routeName))
}

// routeName names the server span after the matched mux pattern ("GET /healthz"),
// so spans group by route rather than by raw path.
func routeName(_ string, r *http.Request) string {
	if r.Pattern != "" {
		return r.Pattern
	}
	return r.Method + " " + r.URL.Path
}
