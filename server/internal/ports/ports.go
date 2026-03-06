package ports

import (
	"errors"
	"fmt"
	"math/big"
	"math/rand"
)

const (
	MinValidPort = 1
	MaxValidPort = 65535
	// bitmapBytes is the fixed size of the serialized port bitmap.
	// Bit N of the big.Int represents port N (1-indexed).
	bitmapBytes = (MaxValidPort + 7) / 8 // 8192 bytes
)

var (
	ErrFull      = errors.New("range is full")
	ErrAllocated = errors.New("provided port is already allocated")
)

type ErrNotInRange struct {
	Min int
	Max int
}

func (e *ErrNotInRange) Error() string {
	return fmt.Sprintf("provided port is not in the valid range. The range of valid ports is [%d, %d]", e.Min, e.Max)
}

// PortRange tracks allocated ports across the full valid range [1, 65535]
// using a big.Int bitmap, where bit N represents port N. Random allocation
// draws only from [min, max], but any port in [1, 65535] can be recorded via
// Allocate. This means min and max can be reconfigured without affecting
// previously stored state.
type PortRange struct {
	min  int
	max  int
	bits big.Int
}

// NewPortRange creates a PortRange for the given spec.
func NewPortRange(min, max int) (*PortRange, error) {
	if min < MinValidPort || max > MaxValidPort {
		return nil, fmt.Errorf("ports must be in the range [%d, %d]", MinValidPort, MaxValidPort)
	}
	if min > max {
		return nil, fmt.Errorf("min port %d is greater than max port %d", min, max)
	}
	return &PortRange{min: min, max: max}, nil
}

// Free returns the count of unallocated ports within [min, max].
func (r *PortRange) Free() int {
	return r.max - r.min + 1 - r.Used()
}

// Used returns the count of allocated ports within [min, max].
func (r *PortRange) Used() int {
	count := 0
	for p := r.min; p <= r.max; p++ {
		if r.bits.Bit(p) == 1 {
			count++
		}
	}
	return count
}

// Allocate reserves the given port. Any port in [1, 65535] may be recorded,
// including ports outside [min, max]. ErrAllocated is returned if the port is
// already reserved.
func (r *PortRange) Allocate(port int) error {
	if port < MinValidPort || port > MaxValidPort {
		return &ErrNotInRange{MinValidPort, MaxValidPort}
	}
	if r.bits.Bit(port) == 1 {
		return ErrAllocated
	}
	r.bits.SetBit(&r.bits, port, 1)
	return nil
}

// AllocateNext reserves a random unallocated port from [min, max]. ErrFull is
// returned if all ports in the range are allocated.
func (r *PortRange) AllocateNext() (int, error) {
	free := r.Free()
	if free == 0 {
		return 0, ErrFull
	}
	n := rand.Intn(free)
	for p := r.min; p <= r.max; p++ {
		if r.bits.Bit(p) == 0 {
			if n == 0 {
				r.bits.SetBit(&r.bits, p, 1)
				return p, nil
			}
			n--
		}
	}
	return 0, ErrFull // unreachable
}

// Release clears the port's allocated bit. Out-of-range or unallocated ports are
// silently ignored.
func (r *PortRange) Release(port int) error {
	if port < MinValidPort || port > MaxValidPort {
		return nil
	}
	r.bits.SetBit(&r.bits, port, 0)
	return nil
}

// Has returns true if the given port is currently allocated.
func (r *PortRange) Has(port int) bool {
	if port < MinValidPort || port > MaxValidPort {
		return false
	}
	return r.bits.Bit(port) == 1
}

// ForEach calls fn for every allocated port across the full valid range
// [1, 65535].
func (r *PortRange) ForEach(fn func(int)) {
	for p := MinValidPort; p <= MaxValidPort; p++ {
		if r.bits.Bit(p) == 1 {
			fn(p)
		}
	}
}

// Snapshot saves the current allocation state. The spec string encodes
// the current min/max, and the data is a fixed-size big-endian bitmap of all
// 65535 ports.
func (r *PortRange) Snapshot() []byte {
	data := make([]byte, bitmapBytes)
	r.bits.FillBytes(data)
	return data
}

// Restore loads a previously saved bitmap. The spec in specStr must be valid
// JSON, but a min/max mismatch does not cause an error — the current range's
// min/max are preserved, allowing the configuration to be changed without
// losing allocation history.
func (r *PortRange) Restore(data []byte) error {
	if len(data) != bitmapBytes {
		return fmt.Errorf("snapshot data size mismatch: expected %d bytes, got %d", bitmapBytes, len(data))
	}
	r.bits.SetBytes(data)
	return nil
}
