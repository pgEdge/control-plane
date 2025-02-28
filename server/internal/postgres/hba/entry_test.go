package hba_test

import (
	"testing"

	"github.com/pgEdge/control-plane/server/internal/postgres/hba"
	"github.com/stretchr/testify/assert"
)

func TestEntry(t *testing.T) {
	for _, tc := range []struct {
		input    hba.Entry
		expected string
	}{
		{
			input: hba.Entry{
				Type:       hba.EntryTypeLocal,
				Database:   "all",
				User:       "all",
				AuthMethod: hba.AuthMethodTrust,
			},
			expected: "local   all             all                                     trust",
		},
		{
			input: hba.Entry{
				Type:       hba.EntryTypeHost,
				Database:   "replication",
				User:       "all",
				Address:    "127.0.0.1/32",
				AuthMethod: hba.AuthMethodTrust,
			},
			expected: "host    replication     all             127.0.0.1/32            trust",
		},
		{
			input: hba.Entry{
				Type:       hba.EntryTypeHostNoSSL,
				Database:   "all",
				User:       "pgedge",
				Address:    "0.0.0.0/0",
				AuthMethod: hba.AuthMethodReject,
			},
			expected: "hostnossl all             pgedge          0.0.0.0/0               reject",
		},
		{
			input: hba.Entry{
				Type:        hba.EntryTypeHostSSL,
				Database:    "all",
				User:        "pgedge",
				Address:     "172.128.128.1/32",
				AuthMethod:  hba.AuthMethodCert,
				AuthOptions: hba.AuthOptionVerifyFull,
			},
			expected: "hostssl all             pgedge          172.128.128.1/32        cert clientcert=verify-full",
		},
		{
			input: hba.Entry{
				Type:        hba.EntryTypeIncludeIfExists,
				IncludePath: "/path/to/extra_hba.conf",
			},
			expected: "include_if_exists /path/to/extra_hba.conf",
		},
	} {
		t.Run(tc.expected, func(t *testing.T) {
			assert.Equal(t, tc.expected, tc.input.String())
		})
	}
}
