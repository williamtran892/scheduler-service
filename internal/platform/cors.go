package platform

import (
	"net/http"
	"slices"
	"strings"
)

// CORS lets browsers on other origins call the service, for local tools such as
// api/console.html. allowed is CORS_ALLOWED_ORIGINS: a comma-separated list of origins, or "*"
// for any. Empty turns CORS off and returns next unchanged, which is the default. A page opened
// from disk sends the origin "null", which only "*" or an explicit "null" admits.
func CORS(allowed string) func(http.Handler) http.Handler {
	var origins []string
	for o := range strings.SplitSeq(allowed, ",") {
		if o = strings.TrimSpace(o); o != "" {
			origins = append(origins, o)
		}
	}
	if len(origins) == 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	anyOrigin := slices.Contains(origins, "*")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if !anyOrigin {
				w.Header().Add("Vary", "Origin")
			}
			if origin == "" || (!anyOrigin && !slices.Contains(origins, origin)) {
				next.ServeHTTP(w, r)
				return
			}

			h := w.Header()
			if anyOrigin {
				h.Set("Access-Control-Allow-Origin", "*")
			} else {
				h.Set("Access-Control-Allow-Origin", origin)
			}
			h.Set("Access-Control-Expose-Headers", "Location")

			// A preflight is answered here; the mux has no OPTIONS routes.
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Content-Type, Accept, X-Channel")
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
