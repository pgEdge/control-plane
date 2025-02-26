package api

import (
	api "github.com/pgEdge/control-plane/api/gen/control_plane"
)

// DynamicService is a container that makes it easy to swap Service
// implementations at runtime.
type DynamicService struct {
	api.Service
}

func NewDynamicService() *DynamicService {
	return &DynamicService{}
}

func (s *DynamicService) UpdateImpl(svc api.Service) {
	s.Service = svc
}
