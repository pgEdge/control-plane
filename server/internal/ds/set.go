package ds

import (
	"slices"
)

// Set is a generic set type.
type Set[T comparable] map[T]bool

func NewSet[T comparable](elements ...T) Set[T] {
	s := Set[T]{}
	for _, element := range elements {
		s.Add(element)
	}
	return s
}

// Add adds an element to the set.
func (s Set[T]) Add(element T) {
	s[element] = true
}

// Remove removes an element to the set.
func (s Set[T]) Remove(element T) {
	delete(s, element)
}

// Has returns true if the given element is in the set.
func (s Set[T]) Has(element T) bool {
	return s[element]
}

// Intersection (s∩o) returns elements that are in both sets.
func (s Set[T]) Intersection(o Set[T]) Set[T] {
	intersection := Set[T]{}
	for element := range s {
		if o.Has(element) {
			intersection.Add(element)
		}
	}
	for element := range o {
		if s.Has(element) {
			intersection.Add(element)
		}
	}
	return intersection
}

// Union (s∪o) returns all elements from both sets.
func (s Set[T]) Union(o Set[T]) Set[T] {
	union := Set[T]{}
	for element := range s {
		union.Add(element)
	}
	for element := range o {
		union.Add(element)
	}
	return union
}

// Difference (s-o) returns elements that are in the receiver set, but not the
// given set 'o'.
func (s Set[T]) Difference(o Set[T]) Set[T] {
	difference := Set[T]{}
	for element := range s {
		if !o.Has(element) {
			difference.Add(element)
		}
	}
	return difference
}

// SymmetricDifference (s-o)∪(o-s) returns elements that are in the receiver set,
// but not the given set 'o', along with those that are in 'o', but not the
// receiver set.
func (s Set[T]) SymmetricDifference(o Set[T]) Set[T] {
	return s.Difference(o).Union(o.Difference(s))
}

// Size returns the number of elements in the set.
func (s Set[T]) Size() int {
	return len(s)
}

// Empty returns whether the set is empty.
func (s Set[T]) Empty() bool {
	return s.Size() == 0
}

// Equal returns whether the given set is equal to the receiver set.
func (s Set[T]) Equal(o Set[T]) bool {
	if s.Size() != o.Size() {
		return false
	}
	for element := range s {
		if !o.Has(element) {
			return false
		}
	}
	return true
}

// ToSlice returns a slice of all the elements in the set.
func (s Set[T]) ToSlice() []T {
	slice := make([]T, s.Size())
	idx := 0
	for element := range s {
		slice[idx] = element
		idx++
	}
	return slice
}

// ToSortedSlice returns a sorted slice of all the elements in the set.
func (s Set[T]) ToSortedSlice(cmp func(a T, b T) int) []T {
	slice := s.ToSlice()
	slices.SortFunc(slice, cmp)

	return slice
}
