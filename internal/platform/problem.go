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

// ProblemTypeBase prefixes a problem code to form the problem's type URI (api/openapi.yaml).
const ProblemTypeBase = "https://scheduler.example.com/problems/"

// Coded is implemented by domain errors that carry a stable problem code from the contract's
// ProblemCode list. EncodeError maps the code to a status; domains never know statuses.
type Coded interface {
	ProblemCode() string
}

// Extended is implemented by domain errors that add members to the problem body
// ("alternatives" on SLOT_TAKEN and AVAILABILITY_CHANGED, "existing" on DUPLICATE_BOOKING).
type Extended interface {
	ProblemExtensions() map[string]any
}

// FieldProblem is one entry of a problem's errors[] member.
type FieldProblem struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// FieldError is a request field the transport or the service rejected: VALIDATION_ERROR (400)
// when the request is malformed, INVALID_VALUE (422) when a value is out of range or unknown.
type FieldError struct {
	Code    string
	Field   string
	Message string
}

func (e *FieldError) Error() string { panic("not implemented") }

// ProblemCode returns VALIDATION_ERROR or INVALID_VALUE.
func (e *FieldError) ProblemCode() string { panic("not implemented") }

// Unwrap makes a VALIDATION_ERROR field error match ErrBadRequest.
func (e *FieldError) Unwrap() error { panic("not implemented") }

// BadRequest is a malformed request field: 400 VALIDATION_ERROR naming the field.
func BadRequest(field, message string) error { panic("not implemented") }

// InvalidValue is an unknown or out-of-range value: 422 INVALID_VALUE naming the field.
func InvalidValue(field, message string) error { panic("not implemented") }

// StatusFor returns the HTTP status of a problem code; an unknown code is 500.
func StatusFor(code string) int { panic("not implemented") }

// Problem is an RFC 9457 problem-details body, as api/openapi.yaml defines it.
type Problem struct {
	Type         string         `json:"type"`
	Title        string         `json:"title"`
	Status       int            `json:"status"`
	Code         string         `json:"code"`
	Detail       string         `json:"detail,omitempty"`
	Alternatives any            `json:"alternatives,omitempty"`
	Existing     any            `json:"existing,omitempty"`
	Errors       []FieldProblem `json:"errors,omitempty"`
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
