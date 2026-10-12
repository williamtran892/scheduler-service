package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// ErrBadRequest marks a request the transport could not decode. Decoders wrap it:
// fmt.Errorf("%w: body: %v", platform.ErrBadRequest, err).
var ErrBadRequest = errors.New("bad request")

// Problem is an RFC 9457 problem-details body.
type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

// EncodeError writes err as application/problem+json. It is the only place in the
// service that maps errors to HTTP status codes; domains add their cases to problemFor.
// Its signature matches go-kit's kithttp.ErrorEncoder.
func EncodeError(_ context.Context, err error, w http.ResponseWriter) {
	p := problemFor(err)
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

func problemFor(err error) Problem {
	switch {
	case errors.Is(err, ErrBadRequest):
		return Problem{Type: "about:blank", Title: "Bad Request", Status: http.StatusBadRequest, Code: "BAD_REQUEST", Detail: err.Error()}
	default:
		// The detail of an unexpected error is logged, not returned to the caller.
		return Problem{Type: "about:blank", Title: "Internal Server Error", Status: http.StatusInternalServerError, Code: "INTERNAL"}
	}
}
