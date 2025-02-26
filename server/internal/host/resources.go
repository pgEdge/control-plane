package host

import (
	"fmt"
	"runtime"

	"github.com/mackerelio/go-osstat/memory"
)

type Resources struct {
	CPUs     int    `json:"cpus"`
	MemBytes uint64 `json:"mem_bytes"`
}

func (r Resources) NanoCPUs() int64 {
	return int64(r.CPUs) * 1e9
}

func DetectResources() (*Resources, error) {
	mem, err := memory.Get()
	if err != nil {
		return nil, fmt.Errorf("failed to get memory info: %w", err)
	}

	return &Resources{
		CPUs:     runtime.NumCPU(),
		MemBytes: mem.Total,
	}, nil
}
