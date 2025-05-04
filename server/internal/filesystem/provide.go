package filesystem

import (
	"github.com/samber/do"
	"github.com/spf13/afero"
)

func Provide(i *do.Injector) {
	provideFs(i)
}

func provideFs(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (afero.Fs, error) {
		return afero.NewOsFs(), nil
	})
}
