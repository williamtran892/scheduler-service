package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEncodeError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantDetail bool
	}{
		{"decoder failure is a 400", fmt.Errorf("%w: body: unexpected EOF", ErrBadRequest), http.StatusBadRequest, "BAD_REQUEST", true},
		{"unknown error is a 500 without detail", errors.New("connection reset"), http.StatusInternalServerError, "INTERNAL", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			EncodeError(context.Background(), tt.err, rec)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("Content-Type = %q", ct)
			}
			var p Problem
			if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if p.Status != tt.wantStatus || p.Code != tt.wantCode {
				t.Errorf("problem = %+v, want status %d code %s", p, tt.wantStatus, tt.wantCode)
			}
			if (p.Detail != "") != tt.wantDetail {
				t.Errorf("detail = %q, want present=%v", p.Detail, tt.wantDetail)
			}
		})
	}
}
