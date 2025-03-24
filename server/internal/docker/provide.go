package docker

import "github.com/samber/do"

func Provide(i *do.Injector) {
	do.Provide(i, func(i *do.Injector) (*Docker, error) {
		cli, err := NewDocker()
		if err != nil {
			return nil, err
		}
		return cli, nil
	})
}
