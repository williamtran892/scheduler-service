package platform

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// teapot marks a request that reached the wrapped handler.
var teapot = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })

func serveCORS(allowed, method, origin string, preflight bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/appointments", nil)
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if preflight {
		r.Header.Set("Access-Control-Request-Method", http.MethodPost)
	}
	rec := httptest.NewRecorder()
	CORS(allowed)(teapot).ServeHTTP(rec, r)
	return rec
}

func TestCORS_OffWhenNoOriginIsAllowed(t *testing.T) {
	rec := serveCORS("", http.MethodOptions, "null", true)
	if rec.Code != http.StatusTeapot || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("status %d, allow-origin %q: want the request passed through untouched", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORS_AnswersAPreflightFromAnAllowedOrigin(t *testing.T) {
	for allowed, want := range map[string]string{"*": "*", "http://a.test, null": "null"} {
		t.Run(allowed, func(t *testing.T) {
			rec := serveCORS(allowed, http.MethodOptions, "null", true)
			if rec.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want 204", rec.Code)
			}
			h := rec.Header()
			if h.Get("Access-Control-Allow-Origin") != want {
				t.Errorf("allow-origin = %q, want %q", h.Get("Access-Control-Allow-Origin"), want)
			}
			if h.Get("Access-Control-Allow-Headers") != "Content-Type, Accept, X-Channel" {
				t.Errorf("allow-headers = %q", h.Get("Access-Control-Allow-Headers"))
			}
		})
	}
}

func TestCORS_HeadersAnActualRequestAndPassesItOn(t *testing.T) {
	rec := serveCORS("*", http.MethodPost, "http://a.test", false)
	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want the wrapped handler's", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" || rec.Header().Get("Access-Control-Expose-Headers") != "Location" {
		t.Errorf("headers = %v", rec.Header())
	}
}

func TestCORS_LeavesAnOriginNotOnTheListUnheaded(t *testing.T) {
	rec := serveCORS("http://a.test", http.MethodOptions, "http://b.test", true)
	if rec.Code != http.StatusTeapot || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("status %d, allow-origin %q: want no CORS headers", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
	}
	if rec.Header().Get("Vary") != "Origin" {
		t.Errorf("vary = %q, want Origin", rec.Header().Get("Vary"))
	}
}
