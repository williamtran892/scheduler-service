package service

import (
	"net/http"

	kithttp "github.com/go-kit/kit/transport/http"
)

// RegisterRoutes puts POST /appointments and GET /appointments/{appointmentId} on mux.
func RegisterRoutes(mux *http.ServeMux, e Endpoints, opts ...kithttp.ServerOption) {
	// No routes yet: they arrive with the transport (tasks.md T051).
}
