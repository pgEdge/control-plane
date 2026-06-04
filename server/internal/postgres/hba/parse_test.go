package hba

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseEntry(t *testing.T) {
	tests := []struct {
		name string
		line string
		want Entry
	}{
		{
			name: "host with cidr and md5",
			line: "host all all 0.0.0.0/0 md5",
			want: Entry{Type: EntryTypeHost, Database: "all", User: "all", Address: "0.0.0.0/0", AuthMethod: AuthMethodMD5},
		},
		{
			name: "hostssl scram for specific user and cidr",
			line: "hostssl all myapp_user 203.0.113.0/24 scram-sha-256",
			want: Entry{Type: EntryTypeHostSSL, Database: "all", User: "myapp_user", Address: "203.0.113.0/24", AuthMethod: AuthMethodScramSHA256},
		},
		{
			name: "cert with options and map",
			line: "hostssl all alice 0.0.0.0/0 cert clientcert=verify-full map=ssl_users",
			want: Entry{Type: EntryTypeHostSSL, Database: "all", User: "alice", Address: "0.0.0.0/0", AuthMethod: AuthMethodCert, AuthOptions: "clientcert=verify-full map=ssl_users"},
		},
		{
			name: "separate ip mask form",
			line: "host all all 192.168.0.0 255.255.0.0 md5",
			want: Entry{Type: EntryTypeHost, Database: "all", User: "all", Address: "192.168.0.0", Mask: "255.255.0.0", AuthMethod: AuthMethodMD5},
		},
		{
			name: "local trust",
			line: "local all all trust",
			want: Entry{Type: EntryTypeLocal, Database: "all", User: "all", AuthMethod: AuthMethodTrust},
		},
		{
			name: "comma-separated users",
			line: "host all pgedge,patroni_replicator 0.0.0.0/0 reject",
			want: Entry{Type: EntryTypeHost, Database: "all", User: "pgedge,patroni_replicator", Address: "0.0.0.0/0", AuthMethod: AuthMethodReject},
		},
		{
			name: "quoted database with space",
			line: `host "my db" all 10.0.0.0/8 md5`,
			want: Entry{Type: EntryTypeHost, Database: "my db", User: "all", Address: "10.0.0.0/8", AuthMethod: AuthMethodMD5},
		},
		{
			name: "trailing comment is ignored",
			line: "host all all 0.0.0.0/0 md5 # allow everyone",
			want: Entry{Type: EntryTypeHost, Database: "all", User: "all", Address: "0.0.0.0/0", AuthMethod: AuthMethodMD5},
		},
		{
			name: "include directive",
			line: "include /etc/pg_hba_extra.conf",
			want: Entry{Type: EntryTypeInclude, IncludePath: "/etc/pg_hba_extra.conf"},
		},
		{
			// The auth method is captured verbatim and not validated against a
			// known set (design: no enum-style restrictions on auth_method).
			name: "unrecognized auth method is accepted verbatim",
			line: "host all all 0.0.0.0/0 ldap2",
			want: Entry{Type: EntryTypeHost, Database: "all", User: "all", Address: "0.0.0.0/0", AuthMethod: AuthMethod("ldap2")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseEntry(tc.line)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestParseEntryErrors(t *testing.T) {
	tests := []struct {
		name string
		line string
		err  string
	}{
		{"unknown connection type", "tcp all all 0.0.0.0/0 md5", `unknown connection type "tcp"`},
		{"missing auth method", "host all all 0.0.0.0/0", "requires database, user, address, and an auth method"},
		{"local missing method", "local all all", "local entry requires database, user, and an auth method"},
		{"include without path", "include", "requires exactly one path argument"},
		{"include with too many args", "include a b", "requires exactly one path argument"},
		{"empty", "", "empty entry"},
		{"unterminated quote", `host "all all 0.0.0.0/0 md5`, "unterminated quoted string"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseEntry(tc.line)
			require.ErrorContains(t, err, tc.err)
		})
	}
}

func TestParseIdent(t *testing.T) {
	tests := []struct {
		name string
		line string
		want IdentEntry
	}{
		{
			name: "basic mapping",
			line: "ssl_users CN=alice,O=example alice",
			want: IdentEntry{MapName: "ssl_users", SystemUsername: "CN=alice,O=example", PostgresUsername: "alice"},
		},
		{
			name: "quoted system username with spaces",
			line: `ssl_users "CN=alice smith,O=example" alice`,
			want: IdentEntry{MapName: "ssl_users", SystemUsername: "CN=alice smith,O=example", PostgresUsername: "alice"},
		},
		{
			name: "include directive",
			line: "include_if_exists /etc/pg_ident_extra.conf",
			want: IdentEntry{Include: EntryTypeIncludeIfExists, IncludePath: "/etc/pg_ident_extra.conf"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseIdent(tc.line)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestParseIdentErrors(t *testing.T) {
	tests := []struct {
		name string
		line string
		err  string
	}{
		{"too few fields", "ssl_users alice", "pg_ident entry requires map-name, system-username, and postgres-username"},
		{"too many fields", "ssl_users CN=alice alice extra", "pg_ident entry requires map-name, system-username, and postgres-username"},
		{"empty", "", "empty entry"},
		{"unterminated quote", `ssl_users "CN=alice alice`, "unterminated quoted string"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseIdent(tc.line)
			require.ErrorContains(t, err, tc.err)
		})
	}
}
