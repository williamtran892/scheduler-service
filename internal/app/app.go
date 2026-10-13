// Package app builds the service's HTTP handler: every route on one mux, in one place.
package app

import (
	"net/http"

	kithttp "github.com/go-kit/kit/transport/http"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"scheduler/internal/platform"
	schedendpoint "scheduler/internal/scheduling/endpoint"
)

// Deps is everything the handler needs. Each domain adds its repositories and
// collaborators here as it is built; main fills them with real implementations.
type Deps struct {
	DB         platform.Pinger
	Scheduling []schedendpoint.EndpointSpec
	// CORSOrigins is CORS_ALLOWED_ORIGINS; empty leaves CORS off.
	CORSOrigins string
}

// NewHandler registers every route on one mux, wraps it in the CORS middleware (a no-op unless
// origins are configured) and then in otelhttp, which opens one server span and records the
// HTTP metrics for each request.
func NewHandler(d Deps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", platform.Health(d.DB))
	// Each domain's endpoint package registers its specs here. Decoder failures are problems too,
	// so every one encodes its errors with platform.EncodeError.
	schedendpoint.Register(mux, d.Scheduling, kithttp.ServerErrorEncoder(platform.EncodeError))

	return otelhttp.NewHandler(platform.CORS(d.CORSOrigins)(mux), "http", otelhttp.WithSpanNameFormatter(routeName))
}

// routeName names the server span after the matched mux pattern ("GET /healthz"),
// so spans group by route rather than by raw path.
func routeName(_ string, r *http.Request) string {
	if r.Pattern != "" {
		return r.Pattern
	}
	return r.Method + " " + r.URL.Path
}
