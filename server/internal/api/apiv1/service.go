package apiv1

import (
	"fmt"
	"net/http"

	goahttp "goa.design/goa/v3/http"

	"github.com/pgEdge/control-plane/api/apiv1"
	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/api/apiv1/gen/http/control_plane/server"
	"github.com/samber/do"
)

type Service struct {
	injector *do.Injector
	handlers *dynamicHandlers
}

func NewService(i *do.Injector) *Service {
	return &Service{
		injector: i,
		handlers: &dynamicHandlers{},
	}
}

func (s *Service) Mount(mux goahttp.Muxer) {
	endpoints := api.NewEndpoints(s.handlers)
	dec := goahttp.RequestDecoder
	enc := goahttp.ResponseEncoder
	specFS := http.FS(apiv1.OpenAPISpecFS)
	svr := server.New(endpoints, mux, dec, enc, nil, nil, specFS)
	server.Mount(mux, svr)
}

func (s *Service) UsePreInitHandlers() error {
	preInitHandlers, err := do.Invoke[*PreInitHandlers](s.injector)
	if err != nil {
		return fmt.Errorf("failed to get pre-init handlers: %w", err)
	}
	s.handlers.updateImpl(preInitHandlers)

	return nil
}

func (s *Service) UsePostInitHandlers() error {
	postInitHandlers, err := do.Invoke[*PostInitHandlers](s.injector)
	if err != nil {
		return fmt.Errorf("failed to get post-init handlers: %w", err)
	}
	s.handlers.updateImpl(postInitHandlers)

	return nil
}

// dynamicHandlers is a container that makes it easy to swap handler
// implementations at runtime.
type dynamicHandlers struct {
	api.Service
}

func (s *dynamicHandlers) updateImpl(svc api.Service) {
	s.Service = svc
}
