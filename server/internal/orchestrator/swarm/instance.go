package swarm

import (
	"context"
	"errors"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/resource"
	"github.com/samber/do"
)

type Instance struct {
	Spec *database.InstanceSpec `json:"spec"`
}

func (i *Instance) Identifier() resource.Identifier {
	return resource.Identifier{
		Type: "swarm.database_instance",
		ID:   i.Spec.InstanceID.String(),
	}
}

func (i *Instance) Validate() error {
	var errs []error
	if i.Spec == nil {
		errs = append(errs, errors.New("spec: must be set"))
	}
	return errors.Join(errs...)
}

func (i *Instance) Dependencies() []resource.Identifier {
	return []resource.Identifier{
		{
			Type: "swarm.service",
			ID:   i.Spec.InstanceID.String(),
		},
	}
}

func (i *Instance) Create(
	ctx context.Context,
	state *resource.State,
	inj *do.Injector,
) error {

	return nil
}

// ID() string
// 	Type() string
// 	Dependencies() []Resource
// 	Create(ctx context.Context, i *do.Injector) error
