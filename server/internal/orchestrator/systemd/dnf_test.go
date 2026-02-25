package systemd

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/ds"
)

func TestToVersion(t *testing.T) {
	for _, tc := range []struct {
		in          string
		expected    *ds.Version
		expectedErr bool
	}{
		{
			in:       "18.3-1.el9",
			expected: &ds.Version{Components: []uint64{18, 3}},
		},
		{
			in:       "5.0.6-1.el9",
			expected: &ds.Version{Components: []uint64{5, 0, 6}},
		},
		{
			in:       "2.6-1.el9",
			expected: &ds.Version{Components: []uint64{2, 6}},
		},
		{
			in:       "1.3.0^20250625git121ab15-1.el9",
			expected: &ds.Version{Components: []uint64{1, 3, 0}},
		},
		{
			in:       "2025.05.04.gita084c80-1.el9",
			expected: &ds.Version{Components: []uint64{2025, 5, 4}},
		},
		{
			in:       "0~git20230917.9b27c3f-1.el9",
			expected: &ds.Version{Components: []uint64{0}},
		},
		{
			in:       "366-12.el9_6",
			expected: &ds.Version{Components: []uint64{366}},
		},
		{
			in:       "1:5.9.1-1.el9",
			expected: &ds.Version{Components: []uint64{5, 9, 1}},
		},
		{
			in:       "20051222-24.el9",
			expected: &ds.Version{Components: []uint64{20051222}},
		},
		{
			in:       "3.1.12-4.el9_3",
			expected: &ds.Version{Components: []uint64{3, 1, 12}},
		},
		{
			in:       "4.0ga14-2.el9",
			expected: &ds.Version{Components: []uint64{4, 0}},
		},
		{
			in:          "final1-3.20210311gitfinal.el9",
			expectedErr: true,
		},
		{
			in:       "0.20091126-40.el9",
			expected: &ds.Version{Components: []uint64{0, 20091126}},
		},
		{
			in:       "0.2^1.26e5737-1.el9",
			expected: &ds.Version{Components: []uint64{0, 2}},
		},
		{
			in:       "2:9.4.146.26-1.16.18.1.3.el9",
			expected: &ds.Version{Components: []uint64{9, 4, 146}},
		},
		{
			in:       "1.4.0-4.Final.el9",
			expected: &ds.Version{Components: []uint64{1, 4, 0}},
		},
	} {
		t.Run(tc.in, func(t *testing.T) {
			out, err := toVersion(tc.in)
			if tc.expectedErr {
				assert.Error(t, err)
			} else {
				assert.Equal(t, tc.expected, out)
			}
		})
	}
}
