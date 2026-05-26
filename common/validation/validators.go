package validation

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/pgEdge/control-plane/common/ds"
)

var ErrRequired = errors.New("is a required field")
var ErrOneOf = errors.New("must be one of")
var ErrInRange = errors.New("must be in range")
var ErrUnique = errors.New("must be unique")

func Required[T comparable](path Path, value T) error {
	var zero T
	if value == zero {
		return &Error{
			Err:  ErrRequired,
			Path: path,
		}
	}
	return nil
}

func OneOf[T cmp.Ordered](path Path, oneOf ds.Set[T], value T) error {
	if !oneOf.Has(value) {
		valid := make([]string, oneOf.Size())
		for i, v := range oneOf.ToSortedSlice(cmp.Compare) {
			valid[i] = fmt.Sprintf("%v", v)
		}
		return &Error{
			Err: fmt.Errorf("%w %s", ErrOneOf, strings.Join(valid, ", ")),
		}
	}
	return nil
}

func InRange[T cmp.Ordered](path Path, min, max, value T) error {
	if value < min || value > max {
		return &Error{
			Path: path,
			Err:  fmt.Errorf("%w [%v, %v]", ErrInRange, min, max),
		}
	}
	return nil
}

type Unique[T cmp.Ordered] struct {
	seen map[T][]Path
}

func (u *Unique[T]) RecordSeen(path Path, value T) {
	if u.seen == nil {
		u.seen = make(map[T][]Path)
	}
	u.seen[value] = append(u.seen[value], path)
}

func (u *Unique[T]) Validate() []error {
	var errs []error
	for _, key := range slices.Sorted(maps.Keys(u.seen)) {
		paths := u.seen[key]
		if len(paths) <= 1 {
			continue
		}
		for i, path := range paths {
			var others strings.Builder
			for j, other := range paths {
				if i == j {
					continue
				}
				if others.Len() > 0 {
					others.WriteString(", ")
				}
				others.WriteString(other.String())
			}
			errs = append(errs, &Error{
				Path: path,
				Err:  fmt.Errorf("%w. '%v' duplicated in: %s", ErrUnique, key, others.String()),
			})
		}
	}
	return errs
}
