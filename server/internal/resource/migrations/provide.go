package migrations

import (
	"github.com/samber/do"

	"github.com/pgEdge/control-plane/server/internal/resource"
)

func Provide(i *do.Injector) {
	provideStateMigrations(i)
}

func provideStateMigrations(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*resource.StateMigrations, error) {
		return resource.NewStateMigrations([]resource.StateMigration{
			&Version_1_0_0{},
			&Version_1_1_0{},
		}), nil
	})
}
