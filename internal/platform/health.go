package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

//go:generate go tool mockgen -destination=mock_pinger_test.go -package=platform -self_package=scheduler/internal/platform scheduler/internal/platform Pinger

// Pinger is the part of *sql.DB the health check needs.
type Pinger interface {
	PingContext(ctx context.Context) error
}

// Health answers GET /healthz: 200 {"status":"ok"} when the database answers a ping
// within two seconds, otherwise a 503 problem.
func Health(db Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(Problem{
				Type: "about:blank", Title: "Service Unavailable", Status: http.StatusServiceUnavailable,
				Code: "DATABASE_UNAVAILABLE", Detail: fmt.Sprintf("database ping: %v", err),
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}
