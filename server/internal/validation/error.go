package validation

import (
	"fmt"
	"slices"
	"strings"
)

type Path []string

func NewPath(elems ...string) Path {
	return elems
}

func ArrayIndexElement(idx int) string {
	return fmt.Sprintf("[%d]", idx)
}

func MapKeyElement(key string) string {
	return fmt.Sprintf("[%s]", key)
}

func (p Path) String() string {
	var path strings.Builder
	for i, ele := range p {
		if i > 0 && !strings.HasPrefix(ele, "[") {
			path.WriteString(".")
		}
		path.WriteString(ele)
	}
	return path.String()
}

func (p Path) Append(elem ...string) Path {
	return append(slices.Clone(p), elem...)
}

func (p Path) AppendArrayIndex(idx int) Path {
	return p.Append(ArrayIndexElement(idx))
}

func (p Path) AppendMapKey(key string) Path {
	return p.Append(MapKeyElement(key))
}

type Error struct {
	Path Path
	Err  error
}

func NewError(err error, path Path) *Error {
	return &Error{Err: err, Path: path}
}

func (e *Error) Unwrap() error {
	return e.Err
}

func (e *Error) Error() string {
	if len(e.Path) == 0 {
		return e.Err.Error()
	}
	return fmt.Sprintf("%s: %s", e.Path.String(), e.Err.Error())
}
