package validation

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/pgEdge/control-plane/server/internal/ds"
)

var ErrUnique = errors.New("must be unique")

type Unique[T cmp.Ordered] struct {
	seen map[T]ds.Set[string]
}

func NewUnique[T cmp.Ordered]() *Unique[T] {
	return &Unique[T]{
		seen: map[T]ds.Set[string]{},
	}
}

func (u *Unique[T]) RecordSeen(path Path, value T) {
	if u.seen == nil {
		u.seen = make(map[T]ds.Set[string])
	}
	if _, ok := u.seen[value]; !ok {
		u.seen[value] = ds.NewSet[string]()
	}
	u.seen[value].Add(path.String())
}

func (u *Unique[T]) Validate(base error) []error {
	if base == nil {
		base = ErrUnique
	}
	var errs []error
	for _, key := range slices.Sorted(maps.Keys(u.seen)) {
		paths := u.seen[key]
		if len(paths) <= 1 {
			continue
		}
		errs = append(errs, &Error{
			Err: fmt.Errorf("%w: '%v' duplicated in: %s", base, key, ds.SetToString(paths)),
		})
	}
	return errs
}
