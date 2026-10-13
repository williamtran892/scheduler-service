package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// ErrBadRequest marks a request the transport could not decode. Decoders wrap it:
// fmt.Errorf("%w: body: %w", platform.ErrBadRequest, err).
var ErrBadRequest = errors.New("bad request")

// codeValidation is the problem code of a malformed request (400).
const codeValidation = "VALIDATION_ERROR"

// ProblemTypeBase prefixes a problem code to form the problem's type URI (api/openapi.yaml).
const ProblemTypeBase = "https://scheduler.example.com/problems/"

// Coded is implemented by domain errors that carry a stable problem code from the contract's
// ProblemCode list. EncodeError maps the code to a status; domains never know statuses.
type Coded interface {
	ProblemCode() string
}

// Extended is implemented by domain errors that add members to the problem body
// ("existing" on DUPLICATE_BOOKING).
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

func (e *FieldError) Error() string { return e.Field + ": " + e.Message }

// ProblemCode returns VALIDATION_ERROR or INVALID_VALUE.
func (e *FieldError) ProblemCode() string { return e.Code }

// Unwrap makes a VALIDATION_ERROR field error match ErrBadRequest.
func (e *FieldError) Unwrap() error {
	if e.Code == codeValidation {
		return ErrBadRequest
	}
	return nil
}

// BadRequest is a malformed request field: 400 VALIDATION_ERROR naming the field.
func BadRequest(field, message string) error {
	return &FieldError{Code: codeValidation, Field: field, Message: message}
}

// InvalidValue is an unknown or out-of-range value: 422 INVALID_VALUE naming the field.
func InvalidValue(field, message string) error {
	return &FieldError{Code: "INVALID_VALUE", Field: field, Message: message}
}

// problemKind is the status and title of one code in the contract's ProblemCode list.
type problemKind struct {
	status int
	title  string
}

// problemKinds is the one table from problem code to HTTP status (api/openapi.yaml, ProblemCode).
var problemKinds = map[string]problemKind{
	"SLOT_TAKEN":                  {http.StatusConflict, "Slot taken"},
	"AVAILABILITY_CHANGED":        {http.StatusConflict, "Availability changed"},
	"VERSION_CONFLICT":            {http.StatusConflict, "Version conflict"},
	"INVALID_TRANSITION":          {http.StatusConflict, "Invalid transition"},
	"DUPLICATE":                   {http.StatusConflict, "Duplicate"},
	"DUPLICATE_BOOKING":           {http.StatusConflict, "Duplicate booking"},
	"NO_QUALIFIED_TECHNICIAN":     {http.StatusUnprocessableEntity, "No qualified technician"},
	"OUTSIDE_OPENING_HOURS":       {http.StatusUnprocessableEntity, "Outside opening hours"},
	"OFF_GRID_START":              {http.StatusUnprocessableEntity, "Start not on the booking grid"},
	"SERVICE_NOT_BOOKABLE_ONLINE": {http.StatusUnprocessableEntity, "Service not bookable online"},
	"INVALID_VALUE":               {http.StatusUnprocessableEntity, "Invalid value"},
	codeValidation:                {http.StatusBadRequest, "Validation error"},
	"UNAUTHORIZED":                {http.StatusUnauthorized, "Unauthorized"},
	"FORBIDDEN":                   {http.StatusForbidden, "Forbidden"},
	"NOT_FOUND":                   {http.StatusNotFound, "Not found"},
}

// StatusFor returns the HTTP status of a problem code; an unknown code is 500.
func StatusFor(code string) int {
	if k, ok := problemKinds[code]; ok {
		return k.status
	}
	return http.StatusInternalServerError
}

// Problem is an RFC 9457 problem-details body, as api/openapi.yaml defines it.
type Problem struct {
	Type     string         `json:"type"`
	Title    string         `json:"title"`
	Status   int            `json:"status"`
	Code     string         `json:"code"`
	Detail   string         `json:"detail,omitempty"`
	Existing any            `json:"existing,omitempty"`
	Errors   []FieldProblem `json:"errors,omitempty"`
}

// EncodeError writes err as application/problem+json. It is the only place in the
// service that maps errors to HTTP status codes; domain errors reach it through Coded and
// Extended. Its signature matches go-kit's kithttp.ErrorEncoder.
func EncodeError(_ context.Context, err error, w http.ResponseWriter) {
	p := problemFor(err)
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// problemFor builds the problem for err. A coded error (anywhere in its chain) takes its code's
// status and title; a bare ErrBadRequest is a VALIDATION_ERROR; anything else is a 500.
func problemFor(err error) Problem {
	code := codeValidation
	var coded Coded
	switch {
	case errors.As(err, &coded):
		code = coded.ProblemCode()
	case errors.Is(err, ErrBadRequest):
	default:
		return internalProblem()
	}
	kind, known := problemKinds[code]
	if !known {
		return internalProblem()
	}

	p := Problem{Type: ProblemTypeBase + code, Title: kind.title, Status: kind.status, Code: code, Detail: err.Error()}
	var ext Extended
	if errors.As(err, &ext) {
		p.Existing = ext.ProblemExtensions()["existing"]
	}
	var field *FieldError
	if errors.As(err, &field) {
		p.Errors = []FieldProblem{{Field: field.Field, Message: field.Message}}
	}
	return p
}

// internalProblem is the answer to an unexpected error. Its detail is logged, not returned.
func internalProblem() Problem {
	return Problem{Type: "about:blank", Title: "Internal Server Error", Status: http.StatusInternalServerError, Code: "INTERNAL"}
}
