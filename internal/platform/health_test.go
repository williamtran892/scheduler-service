package platform

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/mock/gomock"
)

func TestHealth_OKWhenTheDatabaseAnswers(t *testing.T) {
	db := NewMockPinger(gomock.NewController(t))
	db.EXPECT().PingContext(gomock.Any()).Return(nil)

	rec := httptest.NewRecorder()
	Health(db)(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("body = %v, want status ok", body)
	}
}

func TestHealth_UnavailableWhenThePingFails(t *testing.T) {
	db := NewMockPinger(gomock.NewController(t))
	db.EXPECT().PingContext(gomock.Any()).Return(errors.New("connection refused"))

	rec := httptest.NewRecorder()
	Health(db)(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	var p Problem
	if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if p.Code != "DATABASE_UNAVAILABLE" {
		t.Errorf("code = %q, want DATABASE_UNAVAILABLE", p.Code)
	}
}
