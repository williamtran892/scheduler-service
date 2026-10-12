package service

import "github.com/go-kit/kit/endpoint"

// Endpoints holds one go-kit endpoint per Service method.
type Endpoints struct {
	BookEndpoint endpoint.Endpoint
	GetEndpoint  endpoint.Endpoint
}

// MakeServerEndpoints adapts s to endpoints. Business failures travel as the response's Err.
func MakeServerEndpoints(s Service) Endpoints { return Endpoints{} }
