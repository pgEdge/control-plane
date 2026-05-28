package systemd

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractMemTotal(t *testing.T) {
	const expected uint64 = 8_307_171_328
	out, err := extractMemTotal(strings.NewReader(memInfo))
	require.NoError(t, err)
	require.Equal(t, expected, out)
}

const memInfo = `MemTotal:        8112472 kB
MemFree:         4689284 kB
MemAvailable:    7561956 kB
Buffers:           40324 kB
Cached:          2822440 kB
SwapCached:            0 kB
Active:           408872 kB
Inactive:        2623792 kB
Active(anon):     193908 kB
Inactive(anon):        0 kB
Active(file):     214964 kB
Inactive(file):  2623792 kB
Unevictable:       26096 kB
Mlocked:           26096 kB
SwapTotal:             0 kB
SwapFree:              0 kB
Zswap:                 0 kB
Zswapped:              0 kB
Dirty:                 0 kB
Writeback:             0 kB
AnonPages:        196032 kB
Mapped:           304360 kB
Shmem:             16564 kB
KReclaimable:     236936 kB
Slab:             319544 kB
SReclaimable:     236936 kB
SUnreclaim:        82608 kB
KernelStack:        2752 kB
ShadowCallStack:     704 kB
PageTables:         4980 kB
SecPageTables:         0 kB
NFS_Unstable:          0 kB
Bounce:                0 kB
WritebackTmp:          0 kB
CommitLimit:     4056236 kB
Committed_AS:     858372 kB
VmallocTotal:   133141626880 kB
VmallocUsed:       12196 kB
VmallocChunk:          0 kB
Percpu:             2608 kB
HardwareCorrupted:     0 kB
AnonHugePages:         0 kB
ShmemHugePages:        0 kB
ShmemPmdMapped:        0 kB
FileHugePages:         0 kB
FilePmdMapped:         0 kB
CmaTotal:          32768 kB
CmaFree:           29696 kB
HugePages_Total:       0
HugePages_Free:        0
HugePages_Rsvd:        0
HugePages_Surp:        0
Hugepagesize:       2048 kB
Hugetlb:               0 kB
`
