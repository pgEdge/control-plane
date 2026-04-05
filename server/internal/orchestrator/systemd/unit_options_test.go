package systemd_test

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	"github.com/coreos/go-systemd/v22/unit"
	"github.com/pgEdge/control-plane/server/internal/orchestrator/common"
	"github.com/pgEdge/control-plane/server/internal/orchestrator/systemd"
	"github.com/pgEdge/control-plane/server/internal/testutils"
)

func TestUnitOptions(t *testing.T) {
	golden := &testutils.GoldenTest[[]*unit.UnitOption]{
		FileExtension: ".service",
		Marshal: func(v any) ([]byte, error) {
			opts, ok := v.([]*unit.UnitOption)
			if !ok {
				return nil, fmt.Errorf("expected []*unit.UnitOption, but got %T", v)
			}

			var buf bytes.Buffer
			_, err := io.Copy(&buf, unit.Serialize(opts))
			if err != nil {
				return nil, fmt.Errorf("failed to serialize unit options: %w", err)
			}

			return buf.Bytes(), nil
		},
		Unmarshal: func(data []byte, v any) error {
			opts, ok := v.(*[]*unit.UnitOption)
			if !ok {
				return fmt.Errorf("expected *[]*unit.UnitOption, but got %T", v)
			}
			out, err := unit.Deserialize(bytes.NewReader(data))
			if err != nil {
				return fmt.Errorf("failed to deserialize unit options: %w", err)
			}
			*opts = out

			return nil
		},
	}

	t.Run("PatroniUnitOptions", func(t *testing.T) {
		paths := common.InstancePaths{
			Instance:       common.Paths{BaseDir: "/var/lib/pgsql/18/storefront-n1-689qacsi"},
			Host:           common.Paths{BaseDir: "/var/lib/pgsql/18/storefront-n1-689qacsi"},
			PgBackRestPath: "/usr/bin/pgbackrest",
			PatroniPath:    "/usr/local/bin/patroni",
		}
		pgBinPath := "/usr/pgsql-18/bin"

		for _, tc := range []struct {
			name        string
			paths       common.InstancePaths
			pgBinPath   string
			cpus        float64
			memoryBytes uint64
		}{
			{
				name:      "minimal",
				paths:     paths,
				pgBinPath: pgBinPath,
			},
			{
				name:      "cpu limit",
				paths:     paths,
				pgBinPath: pgBinPath,
				cpus:      14,
			},
			{
				name:      "fractional cpu limit",
				paths:     paths,
				pgBinPath: pgBinPath,
				cpus:      0.5,
			},
			{
				name:        "memory max",
				paths:       paths,
				pgBinPath:   pgBinPath,
				memoryBytes: 8_589_934_592, // 8GiB in bytes
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				t.Setenv("PATH", "/root/.local/bin:/root/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/usr/local/go/bin")

				actual := systemd.PatroniUnitOptions(tc.paths, tc.pgBinPath, tc.cpus, tc.memoryBytes)
				golden.Run(t, actual, update)
			})
		}
	})
}
