/*
Copyright 2025 pgEdge, Inc.
Copyright 2020 Authors of Cilium.
Copyright 2015 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use
this file except in compliance with the License. You may obtain a copy of the
License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed
under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR
CONDITIONS OF ANY KIND, either express or implied. See the License for the
specific language governing permissions and limitations under the License.
*/

// This is based on the IP allocator from
// github.com/cilium/ipam/service/ipallocator, but modified to allocate subnets
// rather than individual IPs.
package ipam

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/netip"

	"github.com/cilium/ipam/service/allocator"
)

var (
	ErrFull              = errors.New("range is full")
	ErrAllocated         = errors.New("provided subnet is already allocated")
	ErrMismatchedNetwork = errors.New("the provided network does not match the current range")
)

type ErrNotInRange struct {
	ValidRange string
}

func (e *ErrNotInRange) Error() string {
	return fmt.Sprintf("provided subnet is not in the valid range. The range of valid IPs is %s", e.ValidRange)
}

// SubnetRangeSpec is used to identify the allocator in snapshots, any changes
// to its fields or format will affect the ability to restore older ranges from
// storage.
type SubnetRangeSpec struct {
	CIDR       netip.Prefix `json:"cidr"`
	SubnetBits int          `json:"subnet_bits"`
}

func (r SubnetRangeSpec) String() (string, error) {
	raw, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("failed to marshal range spec: %w", err)
	}
	return string(raw), nil
}

func ParseSubnetRangeSpec(str string) (SubnetRangeSpec, error) {
	var r SubnetRangeSpec
	if err := json.Unmarshal([]byte(str), &r); err != nil {
		return SubnetRangeSpec{}, fmt.Errorf("failed to unmarshal range spec: %w", err)
	}
	return r, nil
}

// SubnetRange is a contiguous block of IP subnets that can be allocated
// atomically.
//
// The internal structure of the range is:
//
//	For CIDR 172.17.96.0/20 with a subnet size of 16:
//	256 subnets
//
//	First subnet                  Last subnet
//	172.17.96.0/28                172.17.111.255/28
//	      |                             |
//	   r.base                    r.base + r.max
//	      |                             |
//	offset #0 of r.allocated   last offset of r.allocated
type SubnetRange struct {
	net netip.Prefix
	// base is a cached version of the start IP in the CIDR range as a *big.Int
	base *big.Int
	// max is the maximum size of the usable addresses in the range
	max int
	// subnetSize is the size of each allocated subnet
	subnetSize int
	// subnetBits is the suffix appended to the CIDR to create the subnets
	subnetBits int

	alloc allocator.Interface
}

// NewAllocatorSubnetRange creates a SubnetRange for the given spec, calling
// allocatorFactory to construct the backing store.
func NewAllocatorSubnetRange(spec SubnetRangeSpec, allocatorFactory allocator.AllocatorFactory) (*SubnetRange, error) {
	// Normalize the cidr range so that cidr.Addr() is the first IP in the
	// range.
	spec = SubnetRangeSpec{
		CIDR:       spec.CIDR.Masked(),
		SubnetBits: spec.SubnetBits,
	}

	if spec.SubnetBits < spec.CIDR.Bits() {
		return nil, fmt.Errorf("subnet size is larger than the given CIDR")
	}
	subnet := netip.PrefixFrom(spec.CIDR.Addr(), spec.SubnetBits)
	if !subnet.IsValid() {
		return nil, fmt.Errorf("given cidr and subnet bits produced an invalid subnet: %s, %d", spec.CIDR.String(), spec.SubnetBits)
	}
	rangeSize := RangeSize(spec.CIDR)
	subnetSize := RangeSize(subnet)

	m := rangeSize / subnetSize
	base := bigForIP(spec.CIDR.Addr())

	specStr, err := spec.String()
	if err != nil {
		return nil, err
	}

	r := SubnetRange{
		net:        spec.CIDR,
		base:       base,
		max:        int(max(0, m)),
		subnetSize: int(subnetSize),
		subnetBits: spec.SubnetBits,
	}
	r.alloc, err = allocatorFactory(r.max, specStr)
	return &r, err
}

// Helper that wraps NewAllocatorSubnetRange, for creating a range backed by an
// in-memory store.
func NewSubnetRange(spec SubnetRangeSpec) (*SubnetRange, error) {
	return NewAllocatorSubnetRange(spec, func(max int, rangeSpec string) (allocator.Interface, error) {
		return allocator.NewAllocationMap(max, rangeSpec), nil
	})
}

// Free returns the count of subnets left in the range.
func (r *SubnetRange) Free() int {
	return r.alloc.Free()
}

// Used returns the count of subnets used in the range.
func (r *SubnetRange) Used() int {
	return r.max - r.alloc.Free()
}

// CIDR returns the CIDR covered by the range.
func (r *SubnetRange) CIDR() netip.Prefix {
	return r.net
}

// Allocate attempts to reserve the provided IP. ErrNotInRange or ErrAllocated
// will be returned if the IP is not valid for this range or has already been
// reserved.  ErrFull will be returned if there are no addresses left.
func (r *SubnetRange) Allocate(cidr netip.Prefix) error {
	ok, offset := r.contains(cidr)
	if !ok {
		return &ErrNotInRange{r.net.String()}
	}

	allocated, err := r.alloc.Allocate(offset)
	if err != nil {
		return err
	}
	if !allocated {
		return ErrAllocated
	}
	return nil
}

// AllocateNext reserves one of the IPs from the pool. ErrFull may be returned
// if there are no addresses left.
func (r *SubnetRange) AllocateNext() (netip.Prefix, error) {
	offset, ok, err := r.alloc.AllocateNext()
	if err != nil {
		return netip.Prefix{}, err
	}
	if !ok {
		return netip.Prefix{}, ErrFull
	}
	ip, err := addIPOffset(r.base, offset*r.subnetSize)
	if err != nil {
		return netip.Prefix{}, err
	}
	return ip.Prefix(r.subnetBits)
}

// Release releases the IP back to the pool. Releasing an unallocated IP or an
// IP out of the range is a no-op and returns no error.
func (r *SubnetRange) Release(subnet netip.Prefix) error {
	ok, offset := r.contains(subnet)
	if !ok {
		return nil
	}

	return r.alloc.Release(offset)
}

// ForEach calls the provided function for each allocated IP.
func (r *SubnetRange) ForEach(fn func(netip.Prefix)) {
	r.alloc.ForEach(func(offset int) {
		ip, _ := GetIndexedIP(r.net, offset*r.subnetSize)
		prefix, _ := ip.Prefix(r.subnetBits)
		fn(prefix)
	})
}

// Has returns true if the provided IP is already allocated and a call to
// Allocate(ip) would fail with ErrAllocated.
func (r *SubnetRange) Has(subnet netip.Prefix) bool {
	ok, offset := r.contains(subnet)
	if !ok {
		return false
	}

	return r.alloc.Has(offset)
}

// Snapshot saves the current state of the pool.
func (r *SubnetRange) Snapshot() (string, []byte, error) {
	snapshottable, ok := r.alloc.(allocator.Snapshottable)
	if !ok {
		return "", nil, fmt.Errorf("not a snapshottable allocator")
	}
	str, data := snapshottable.Snapshot()
	return str, data, nil
}

// Restore restores the pool to the previously captured state.
// ErrMismatchedNetwork is returned if the provided IPNet range doesn't exactly
// match the previous range.
func (r *SubnetRange) Restore(specStr string, data []byte) error {
	spec, err := ParseSubnetRangeSpec(specStr)
	if err != nil {
		return err
	}
	if r.net.String() != spec.CIDR.String() || r.subnetBits != spec.SubnetBits {
		return ErrMismatchedNetwork
	}
	snapshottable, ok := r.alloc.(allocator.Snapshottable)
	if !ok {
		return fmt.Errorf("not a snapshottable allocator")
	}
	if err := snapshottable.Restore(specStr, data); err != nil {
		return fmt.Errorf("restoring snapshot encountered %v", err)
	}
	return nil
}

// contains returns true and the offset if the ip is in the range, and false and
// nil otherwise. The first and last addresses of the CIDR are omitted.
func (r *SubnetRange) contains(cidr netip.Prefix) (bool, int) {
	if cidr.Bits() != r.subnetBits {
		return false, 0
	}
	if !r.net.Contains(cidr.Addr()) {
		return false, 0
	}

	offset := calculateIPOffset(r.base, cidr.Addr())
	if offset < 0 || offset%r.subnetSize != 0 || offset/r.subnetSize >= r.max {
		return false, 0
	}
	return true, offset / r.subnetSize
}

// bigForIP creates a big.Int based on the provided net.IP
func bigForIP(ip netip.Addr) *big.Int {
	return big.NewInt(0).SetBytes(ip.AsSlice())
}

// addIPOffset adds the provided integer offset to a base big.Int representing a
// net.IP NOTE: If you started with a v4 address and overflow it, you get a v6
// result.
func addIPOffset(base *big.Int, offset int) (netip.Addr, error) {
	r := big.NewInt(0).Add(base, big.NewInt(int64(offset))).Bytes()
	// r = append(make([]byte, 16), r...)
	addr, ok := netip.AddrFromSlice(r)
	if !ok {
		return netip.Addr{}, fmt.Errorf("invalid ip address generated from offset %d", offset)
	}
	return addr, nil
}

// calculateIPOffset calculates the integer offset of ip from base such that
// base + offset = ip. It requires ip >= base.
func calculateIPOffset(base *big.Int, ip netip.Addr) int {
	return int(big.NewInt(0).Sub(bigForIP(ip), base).Int64())

}

// RangeSize returns the size of a range in valid addresses.
func RangeSize(prefix netip.Prefix) int64 {
	bits := prefix.Addr().BitLen()
	ones := prefix.Bits()

	if bits == 32 && (bits-ones) >= 31 || bits == 128 && (bits-ones) >= 127 {
		return 0
	}

	return int64(1) << uint(bits-ones)
}

// GetIndexedIP returns a net.IP that is subnet.IP + index in the contiguous IP
// space.
func GetIndexedIP(subnet netip.Prefix, index int) (netip.Addr, error) {
	ip, err := addIPOffset(bigForIP(subnet.Addr()), index)
	if err != nil {
		return netip.Addr{}, err
	}
	if !subnet.Contains(ip) {
		return netip.Addr{}, fmt.Errorf("can't generate IP with index %d from subnet. subnet too small. subnet: %q", index, subnet)
	}
	return ip, nil
}
