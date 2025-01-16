package api

import (
	"context"
	"errors"

	api "github.com/pgEdge/control-plane/api/gen/control_plane"
)

var ErrNotImplemented = errors.New("endpoint not implemented")

var _ api.Service = (*service)(nil)

type service struct{}

func newService() *service {
	return &service{}
}

func (s *service) ServiceDescription(ctx context.Context) (string, error) {
	return "", ErrNotImplemented
}

func (s *service) InspectCluster(ctx context.Context) (*api.Cluster, error) {
	return nil, ErrNotImplemented
}

func (s *service) ListHosts(ctx context.Context) ([]*api.Host, error) {
	return nil, ErrNotImplemented
}

func (s *service) InspectHost(ctx context.Context, req *api.InspectHostPayload) (*api.Host, error) {
	return nil, ErrNotImplemented
}

func (s *service) RemoveHost(ctx context.Context, req *api.RemoveHostPayload) error {
	return ErrNotImplemented
}

func (s *service) ListDatabases(ctx context.Context) ([]*api.Database, error) {
	return nil, ErrNotImplemented
}

func (s *service) CreateDatabase(ctx context.Context, req *api.CreateDatabaseRequest) (*api.Database, error) {
	return nil, ErrNotImplemented
}

func (s *service) InspectDatabase(ctx context.Context, req *api.InspectDatabasePayload) (*api.Database, error) {
	return nil, ErrNotImplemented
}

func (s *service) UpdateDatabase(ctx context.Context, req *api.UpdateDatabasePayload) (*api.Database, error) {
	return nil, ErrNotImplemented
}

func (s *service) DeleteDatabase(ctx context.Context, req *api.DeleteDatabasePayload) (err error) {
	return ErrNotImplemented
}
