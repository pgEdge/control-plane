package systemd_test

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/orchestrator/systemd"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstalledVersionsProbe(t *testing.T) {
	const (
		debianPg17Config   = "/usr/lib/postgresql/17/bin/pg_config"
		debianPg18Config   = "/usr/lib/postgresql/18/bin/pg_config"
		debianPg17Spock    = "/usr/share/postgresql/17/extension/spock.control"
		debianPg18Spock    = "/usr/share/postgresql/18/extension/spock.control"
		rhelPg17Config     = "/usr/pgsql-17/bin/pg_config"
		rhelPg18Config     = "/usr/pgsql-18/bin/pg_config"
		rhelPg17Spock      = "/usr/pgsql-17/share/extension/spock.control"
		rhelPg18Spock      = "/usr/pgsql-18/share/extension/spock.control"
		customPg17Config   = "/opt/postgresql/17/bin/pg_config"
		customPg17Spock    = "/opt/postgresql/17/share/extension/spock.control"
		customPg17ShareDir = "/opt/postgresql/17/share"
	)

	for _, tc := range []struct {
		name           string
		fsys           func() afero.Fs
		osFamily       config.OSFamily
		cfgBinDirs     map[string]string
		outputs        map[string]mockPgConfigOutput
		expectedOutput []*ds.PgEdgeVersion
		expectedErr    string
	}{
		{
			name:     "debian no postgres installed",
			fsys:     testFs(nil),
			osFamily: config.OSFamilyDebian,
		},
		{
			name: "debian one postgres version with spock",
			fsys: testFs(map[string]string{
				debianPg17Config: "",
				debianPg17Spock:  spock5Control,
			}),
			osFamily: config.OSFamilyDebian,
			outputs: map[string]mockPgConfigOutput{
				debianPg17Config: {stdout: testDebianPg17ConfigOut},
			},
			expectedOutput: []*ds.PgEdgeVersion{
				{
					PostgresVersion: ds.MustParseVersion("17.11"),
					SpockVersion:    ds.MustParseVersion("5"),
				},
			},
		},
		{
			name: "debian two postgres versions with spock",
			fsys: testFs(map[string]string{
				debianPg17Config: "",
				debianPg17Spock:  spock5Control,
				debianPg18Config: "",
				debianPg18Spock:  spock5Control,
			}),
			osFamily: config.OSFamilyDebian,
			outputs: map[string]mockPgConfigOutput{
				debianPg17Config: {stdout: testDebianPg17ConfigOut},
				debianPg18Config: {stdout: testDebianPg18ConfigOut},
			},
			expectedOutput: []*ds.PgEdgeVersion{
				{
					PostgresVersion: ds.MustParseVersion("18.4"),
					SpockVersion:    ds.MustParseVersion("5"),
				},
				{
					PostgresVersion: ds.MustParseVersion("17.11"),
					SpockVersion:    ds.MustParseVersion("5"),
				},
			},
		},
		{
			name: "debian one postgres version without spock",
			fsys: testFs(map[string]string{
				debianPg17Config: "",
			}),
			osFamily: config.OSFamilyDebian,
			outputs: map[string]mockPgConfigOutput{
				debianPg17Config: {stdout: testDebianPg17ConfigOut},
			},
		},
		{
			name: "debian spock 6",
			fsys: testFs(map[string]string{
				debianPg17Config: "",
				debianPg17Spock:  spock5Control,
				debianPg18Config: "",
				debianPg18Spock:  spock6Control,
			}),
			osFamily: config.OSFamilyDebian,
			outputs: map[string]mockPgConfigOutput{
				debianPg17Config: {stdout: testDebianPg17ConfigOut},
				debianPg18Config: {stdout: testDebianPg18ConfigOut},
			},
			expectedOutput: []*ds.PgEdgeVersion{
				{
					PostgresVersion: ds.MustParseVersion("18.4"),
					SpockVersion:    ds.MustParseVersion("6"),
				},
				{
					PostgresVersion: ds.MustParseVersion("17.11"),
					SpockVersion:    ds.MustParseVersion("5"),
				},
			},
		},
		{
			name: "rhel one postgres version with spock",
			fsys: testFs(map[string]string{
				rhelPg17Config: "",
				rhelPg17Spock:  spock5Control,
			}),
			osFamily: config.OSFamilyRedHat,
			outputs: map[string]mockPgConfigOutput{
				rhelPg17Config: {stdout: testRHELPg17ConfigOut},
			},
			expectedOutput: []*ds.PgEdgeVersion{
				{
					PostgresVersion: ds.MustParseVersion("17.11"),
					SpockVersion:    ds.MustParseVersion("5"),
				},
			},
		},
		{
			name: "rhel two postgres versions with spock",
			fsys: testFs(map[string]string{
				rhelPg17Config: "",
				rhelPg17Spock:  spock5Control,
				rhelPg18Config: "",
				rhelPg18Spock:  spock5Control,
			}),
			osFamily: config.OSFamilyRedHat,
			outputs: map[string]mockPgConfigOutput{
				rhelPg17Config: {stdout: testRHELPg17ConfigOut},
				rhelPg18Config: {stdout: testRHELPg18ConfigOut},
			},
			expectedOutput: []*ds.PgEdgeVersion{
				{
					PostgresVersion: ds.MustParseVersion("18.4"),
					SpockVersion:    ds.MustParseVersion("5"),
				},
				{
					PostgresVersion: ds.MustParseVersion("17.11"),
					SpockVersion:    ds.MustParseVersion("5"),
				},
			},
		},
		{
			name: "rhel one postgres version without spock",
			fsys: testFs(map[string]string{
				rhelPg17Config: "",
			}),
			osFamily: config.OSFamilyRedHat,
			outputs: map[string]mockPgConfigOutput{
				rhelPg17Config: {stdout: testRHELPg17ConfigOut},
			},
		},
		{
			name: "overridden bin dirs",
			fsys: testFs(map[string]string{
				customPg17Config: "",
				customPg17Spock:  spock5Control,
				debianPg18Config: "",
				debianPg18Spock:  spock5Control,
			}),
			cfgBinDirs: map[string]string{
				"17": "/opt/postgresql/17/bin",
			},
			osFamily: config.OSFamilyDebian,
			outputs: map[string]mockPgConfigOutput{
				customPg17Config: {
					stdout: withPgConfigValues(testDebianPg17ConfigOut, map[string]string{
						"BINDIR":   "/opt/postgresql/17/bin",
						"SHAREDIR": customPg17ShareDir,
						"VERSION":  "PostgreSQL 17.6",
					}),
				},
				debianPg18Config: {stdout: testDebianPg18ConfigOut},
			},
			expectedOutput: []*ds.PgEdgeVersion{
				{
					PostgresVersion: ds.MustParseVersion("18.4"),
					SpockVersion:    ds.MustParseVersion("5"),
				},
				{
					PostgresVersion: ds.MustParseVersion("17.6"),
					SpockVersion:    ds.MustParseVersion("5"),
				},
			},
		},
		{
			name: "pg_config is a directory",
			fsys: func() afero.Fs {
				fsys := afero.NewMemMapFs()
				if err := fsys.MkdirAll(debianPg17Config, 0o755); err != nil {
					panic(err)
				}
				return fsys
			},
			osFamily:    config.OSFamilyDebian,
			expectedErr: "/usr/lib/postgresql/17/bin/pg_config is a directory",
		},
		{
			name: "pg_config execution fails",
			fsys: testFs(map[string]string{
				debianPg17Config: "",
			}),
			osFamily: config.OSFamilyDebian,
			outputs: map[string]mockPgConfigOutput{
				debianPg17Config: {
					stderr: "pg_config: could not find own program executable",
					err:    errors.New("exit status 2"),
				},
			},
			expectedErr: "failed to get installed postgres 17 version: failed to execute pg_config: exit status 2, stderr: pg_config: could not find own program executable",
		},
		{
			name: "pg_config output missing VERSION",
			fsys: testFs(map[string]string{
				debianPg17Config: "",
			}),
			osFamily: config.OSFamilyDebian,
			outputs: map[string]mockPgConfigOutput{
				debianPg17Config: {
					stdout: withoutPgConfigKey(testDebianPg17ConfigOut, "VERSION"),
				},
			},
			expectedErr: "VERSION not found in pg_config output",
		},
		{
			name: "pg_config output missing SHAREDIR",
			fsys: testFs(map[string]string{
				rhelPg17Config: "",
			}),
			osFamily: config.OSFamilyRedHat,
			outputs: map[string]mockPgConfigOutput{
				rhelPg17Config: {
					stdout: withoutPgConfigKey(testRHELPg17ConfigOut, "SHAREDIR"),
				},
			},
			expectedErr: "SHAREDIR not found in pg_config output",
		},
		{
			name: "pg_config output unrecognized version",
			fsys: testFs(map[string]string{
				debianPg17Config: "",
			}),
			osFamily: config.OSFamilyDebian,
			outputs: map[string]mockPgConfigOutput{
				debianPg17Config: {
					stdout: withPgConfigValues(testDebianPg17ConfigOut, map[string]string{
						"VERSION": "NotPostgres 17.11",
					}),
				},
			},
			expectedErr: `unrecognized pg_config version "NotPostgres 17.11"`,
		},
		{
			name: "release candidate",
			fsys: testFs(map[string]string{
				debianPg18Config: "",
				debianPg18Spock:  spock5Control,
			}),
			osFamily: config.OSFamilyDebian,
			outputs: map[string]mockPgConfigOutput{
				debianPg18Config: {stdout: testDebianRCPgConfigOut},
			},
			expectedOutput: []*ds.PgEdgeVersion{
				{
					PostgresVersion: ds.MustParseVersion("18.0"),
					SpockVersion:    ds.MustParseVersion("5"),
					Stability:       ds.StabilityRC,
				},
			},
		},
		{
			name: "spock pre-release",
			fsys: testFs(map[string]string{
				debianPg18Config: "",
				debianPg18Spock: withPgConfigValues(spock6Control, map[string]string{
					"default_version": "'6.1.0-beta.1'",
				}),
			}),
			osFamily: config.OSFamilyDebian,
			outputs: map[string]mockPgConfigOutput{
				debianPg18Config: {stdout: testDebianPg18ConfigOut},
			},
			expectedOutput: []*ds.PgEdgeVersion{
				{
					PostgresVersion: ds.MustParseVersion("18.4"),
					SpockVersion:    ds.MustParseVersion("6"),
					Stability:       ds.StabilityBeta,
				},
			},
		},
		{
			name: "unrecognized pre-release falls back to dev",
			fsys: testFs(map[string]string{
				debianPg18Config: "",
				debianPg18Spock:  spock5Control,
			}),
			osFamily: config.OSFamilyDebian,
			outputs: map[string]mockPgConfigOutput{
				debianPg18Config: {
					stdout: withPgConfigValues(testDebianPg18ConfigOut, map[string]string{
						"VERSION": "PostgreSQL 18devel",
					}),
				},
			},
			expectedOutput: []*ds.PgEdgeVersion{
				{
					PostgresVersion: ds.MustParseVersion("18.0"),
					SpockVersion:    ds.MustParseVersion("5"),
					Stability:       ds.StabilityDev,
				},
			},
		},
		{
			name: "pg_config reports unexpected major version",
			fsys: testFs(map[string]string{
				debianPg18Config: "",
			}),
			cfgBinDirs: map[string]string{
				"17": "/usr/lib/postgresql/18/bin",
			},
			osFamily: config.OSFamilyDebian,
			outputs: map[string]mockPgConfigOutput{
				debianPg18Config: {stdout: testDebianPg18ConfigOut},
			},
			expectedErr: "pg_config for postgres 17 reported unexpected version 18.4",
		},
		{
			name: "spock control file missing default_version",
			fsys: testFs(map[string]string{
				rhelPg17Config: "",
				rhelPg17Spock:  withoutPgConfigKey(spock5Control, "default_version"),
			}),
			osFamily: config.OSFamilyRedHat,
			outputs: map[string]mockPgConfigOutput{
				rhelPg17Config: {stdout: testRHELPg17ConfigOut},
			},
			expectedErr: "failed to get installed spock version for postgres 17: default_version not found in /usr/pgsql-17/share/extension/spock.control",
		},
		{
			name: "spock control file invalid default_version",
			fsys: testFs(map[string]string{
				rhelPg17Config: "",
				rhelPg17Spock: withPgConfigValues(spock5Control, map[string]string{
					"default_version": "'five'",
				}),
			}),
			osFamily: config.OSFamilyRedHat,
			outputs: map[string]mockPgConfigOutput{
				rhelPg17Config: {stdout: testRHELPg17ConfigOut},
			},
			expectedErr: `failed to get installed spock version for postgres 17: failed to parse default_version "five"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe, err := systemd.NewInstalledVersionsProbe(
				mockPgConfigExecCommand(t, tc.outputs),
				tc.fsys(),
				config.SystemD{
					OSFamily:  tc.osFamily,
					PgBinDirs: tc.cfgBinDirs,
				},
			)
			require.NoError(t, err)

			actual, err := probe.InstalledPostgresVersions(t.Context())
			if tc.expectedErr != "" {
				assert.ErrorContains(t, err, tc.expectedErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.expectedOutput, actual)
			}
		})
	}
}

func TestNewInstalledVersionsProbeUnknownOSFamily(t *testing.T) {
	_, err := systemd.NewInstalledVersionsProbe(
		mockPgConfigExecCommand(t, nil),
		afero.NewMemMapFs(),
		config.SystemD{OSFamily: config.OSFamilyUnknown},
	)
	assert.ErrorContains(t, err, "unrecognized os family 'unknown'")
}

func testFs(files map[string]string) func() afero.Fs {
	return func() afero.Fs {
		fsys := afero.NewMemMapFs()
		for path, contents := range files {
			if err := afero.WriteFile(fsys, path, []byte(contents), 0o755); err != nil {
				panic(err)
			}
		}
		return fsys
	}
}

// withPgConfigValues replaces the values of the given keys in "KEY = value"
// formatted output, such as pg_config output or extension control files.
func withPgConfigValues(output string, values map[string]string) string {
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		key, _, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if value, ok := values[key]; ok {
			lines[i] = key + " = " + value
		}
	}
	return strings.Join(lines, "\n")
}

// withoutPgConfigKey removes the given key from "KEY = value" formatted
// output, such as pg_config output or extension control files.
func withoutPgConfigKey(output, key string) string {
	lines := strings.Split(output, "\n")
	lines = slices.DeleteFunc(lines, func(line string) bool {
		k, _, ok := strings.Cut(line, "=")
		return ok && strings.TrimSpace(k) == key
	})
	return strings.Join(lines, "\n")
}

type mockPgConfigOutput struct {
	stdout string
	stderr string
	err    error
}

func mockPgConfigExecCommand(t testing.TB, outputs map[string]mockPgConfigOutput) systemd.ExecCommand {
	t.Helper()

	return func(_ context.Context, stdout, stderr io.Writer, name string, args ...string) error {
		output, ok := outputs[name]
		require.True(t, ok)

		if output.stdout != "" {
			_, err := stdout.Write([]byte(output.stdout))
			require.NoError(t, err)
		}
		if output.stderr != "" {
			_, err := stderr.Write([]byte(output.stderr))
			require.NoError(t, err)
		}
		return output.err
	}
}

const testDebianPg17ConfigOut = `BINDIR = /usr/lib/postgresql/17/bin
DOCDIR = /usr/share/doc/postgresql-doc-17
HTMLDIR = /usr/share/doc/postgresql-doc-17
INCLUDEDIR = /usr/include/postgresql
PKGINCLUDEDIR = /usr/include/postgresql
INCLUDEDIR-SERVER = /usr/include/postgresql/17/server
LIBDIR = /usr/lib/aarch64-linux-gnu
PKGLIBDIR = /usr/lib/postgresql/17/lib
LOCALEDIR = /usr/share/locale
MANDIR = /usr/share/postgresql/17/man
SHAREDIR = /usr/share/postgresql/17
SYSCONFDIR = /etc/postgresql-common
PGXS = /usr/lib/postgresql/17/lib/pgxs/src/makefiles/pgxs.mk
CONFIGURE =  '--build=aarch64-linux-gnu' '--prefix=/usr' '--includedir=${prefix}/include' '--mandir=${prefix}/share/man' '--infodir=${prefix}/share/info' '--sysconfdir=/etc' '--localstatedir=/var' '--disable-option-checking' '--disable-silent-rules' '--libdir=${prefix}/lib/aarch64-linux-gnu' '--runstatedir=/run' '--disable-maintainer-mode' '--disable-dependency-tracking' '--with-tcl' '--with-perl' '--with-python' '--with-pam' '--with-openssl' '--with-libxml' '--with-libxslt' '--mandir=/usr/share/postgresql/17/man' '--docdir=/usr/share/doc/postgresql-doc-17' '--sysconfdir=/etc/postgresql-common' '--datarootdir=/usr/share/' '--datadir=/usr/share/postgresql/17' '--bindir=/usr/lib/postgresql/17/bin' '--libdir=/usr/lib/aarch64-linux-gnu/' '--libexecdir=/usr/lib/postgresql/' '--includedir=/usr/include/postgresql/' '--with-extra-version= (Ubuntu 17.11-1.noble)' '--enable-nls' '--enable-thread-safety' '--enable-debug' '--disable-rpath' '--with-uuid=e2fs' '--with-gnu-ld' '--with-gssapi' '--with-ldap' '--with-pgport=5432' '--with-system-tzdata=/usr/share/zoneinfo' 'AWK=mawk' 'MKDIR_P=/bin/mkdir -p' 'PROVE=/usr/bin/prove' 'PYTHON=/usr/bin/python3' 'TAR=/bin/tar' 'XSLTPROC=xsltproc --nonet' 'CFLAGS=-g -O2 -fno-omit-frame-pointer -mno-omit-leaf-frame-pointer -flto=auto -ffat-lto-objects -fstack-protector-strong -fstack-clash-protection -Wformat -Werror=format-security -mbranch-protection=standard' 'LDFLAGS=-Wl,-Bsymbolic-functions -flto=auto -ffat-lto-objects -Wl,-z,relro -Wl,-z,now' '--enable-tap-tests' '--with-icu' '--with-llvm' 'LLVM_CONFIG=/usr/bin/llvm-config-18' 'CLANG=/usr/bin/clang-18' '--with-lz4' '--with-zstd' '--with-systemd' '--with-selinux' '--enable-dtrace' 'build_alias=aarch64-linux-gnu' 'CPPFLAGS=-Wdate-time -D_FORTIFY_SOURCE=3' 'CXXFLAGS=-g -O2 -fno-omit-frame-pointer -mno-omit-leaf-frame-pointer -flto=auto -ffat-lto-objects -fstack-protector-strong -fstack-clash-protection -Wformat -Werror=format-security -mbranch-protection=standard'
CC = gcc
CPPFLAGS = -Wdate-time -D_FORTIFY_SOURCE=3 -D_GNU_SOURCE -I/usr/include/libxml2
CFLAGS = -Wall -Wmissing-prototypes -Wpointer-arith -Wdeclaration-after-statement -Werror=vla -Wendif-labels -Wmissing-format-attribute -Wimplicit-fallthrough=3 -Wcast-function-type -Wshadow=compatible-local -Wformat-security -fno-strict-aliasing -fwrapv -fexcess-precision=standard -Wno-format-truncation -Wno-stringop-truncation -g -g -O2 -fno-omit-frame-pointer -mno-omit-leaf-frame-pointer -flto=auto -ffat-lto-objects -fstack-protector-strong -fstack-clash-protection -Wformat -Werror=format-security -mbranch-protection=standard
CFLAGS_SL = -fPIC
LDFLAGS = -Wl,-Bsymbolic-functions -flto=auto -ffat-lto-objects -Wl,-z,relro -Wl,-z,now -Wl,--as-needed
LDFLAGS_EX = 
LDFLAGS_SL = 
LIBS = -lpgcommon -lpgport -lselinux -lzstd -llz4 -lxslt -lxml2 -lpam -lssl -lcrypto -lgssapi_krb5 -lz -lreadline -lm 
VERSION = PostgreSQL 17.11 (Ubuntu 17.11-1.noble)`

const testDebianPg18ConfigOut = `BINDIR = /usr/lib/postgresql/18/bin
DOCDIR = /usr/share/doc/postgresql-doc-18
HTMLDIR = /usr/share/doc/postgresql-doc-18
INCLUDEDIR = /usr/include/postgresql
PKGINCLUDEDIR = /usr/include/postgresql
INCLUDEDIR-SERVER = /usr/include/postgresql/18/server
LIBDIR = /usr/lib/aarch64-linux-gnu
PKGLIBDIR = /usr/lib/postgresql/18/lib
LOCALEDIR = /usr/share/locale
MANDIR = /usr/share/postgresql/18/man
SHAREDIR = /usr/share/postgresql/18
SYSCONFDIR = /etc/postgresql-common
PGXS = /usr/lib/postgresql/18/lib/pgxs/src/makefiles/pgxs.mk
CONFIGURE =  '--build=aarch64-linux-gnu' '--prefix=/usr' '--includedir=${prefix}/include' '--mandir=${prefix}/share/man' '--infodir=${prefix}/share/info' '--sysconfdir=/etc' '--localstatedir=/var' '--disable-option-checking' '--disable-silent-rules' '--libdir=${prefix}/lib/aarch64-linux-gnu' '--runstatedir=/run' '--disable-maintainer-mode' '--disable-dependency-tracking' '--with-tcl' '--with-perl' '--with-python' '--with-pam' '--with-openssl' '--with-libxml' '--with-libxslt' '--mandir=/usr/share/postgresql/18/man' '--docdir=/usr/share/doc/postgresql-doc-18' '--sysconfdir=/etc/postgresql-common' '--datarootdir=/usr/share/' '--datadir=/usr/share/postgresql/18' '--bindir=/usr/lib/postgresql/18/bin' '--libdir=/usr/lib/aarch64-linux-gnu/' '--libexecdir=/usr/lib/postgresql/' '--includedir=/usr/include/postgresql/' '--with-extra-version= (Ubuntu 18.4-1.noble)' '--enable-nls' '--enable-thread-safety' '--enable-debug' '--disable-rpath' '--with-uuid=e2fs' '--with-gnu-ld' '--with-gssapi' '--with-ldap' '--with-pgport=5432' '--with-system-tzdata=/usr/share/zoneinfo' 'AWK=mawk' 'MKDIR_P=/bin/mkdir -p' 'PROVE=/usr/bin/prove' 'PYTHON=/usr/bin/python3' 'TAR=/bin/tar' 'XSLTPROC=xsltproc --nonet' 'CFLAGS=-g -O2 -fno-omit-frame-pointer -mno-omit-leaf-frame-pointer -flto=auto -ffat-lto-objects -fstack-protector-strong -fstack-clash-protection -Wformat -Werror=format-security -mbranch-protection=standard' 'LDFLAGS=-Wl,-Bsymbolic-functions -flto=auto -ffat-lto-objects -Wl,-z,relro -Wl,-z,now' '--enable-tap-tests' '--with-icu' '--with-llvm' 'LLVM_CONFIG=/usr/bin/llvm-config-18' 'CLANG=/usr/bin/clang-18' '--with-lz4' '--with-zstd' '--with-libcurl' '--with-libnuma' 'LIBNUMA_CFLAGS= ' 'LIBNUMA_LIBS=-lnuma' '--with-liburing' '--with-systemd' '--with-selinux' '--enable-dtrace' 'build_alias=aarch64-linux-gnu' 'CPPFLAGS=-Wdate-time -D_FORTIFY_SOURCE=3' 'CXXFLAGS=-g -O2 -fno-omit-frame-pointer -mno-omit-leaf-frame-pointer -flto=auto -ffat-lto-objects -fstack-protector-strong -fstack-clash-protection -Wformat -Werror=format-security -mbranch-protection=standard'
CC = gcc
CPPFLAGS = -Wdate-time -D_FORTIFY_SOURCE=3 -D_GNU_SOURCE -I/usr/include/libxml2
CFLAGS = -Wall -Wmissing-prototypes -Wpointer-arith -Wdeclaration-after-statement -Werror=vla -Wendif-labels -Wmissing-format-attribute -Wimplicit-fallthrough=3 -Wcast-function-type -Wshadow=compatible-local -Wformat-security -fno-strict-aliasing -fwrapv -fexcess-precision=standard -Wno-format-truncation -Wno-stringop-truncation -g -g -O2 -fno-omit-frame-pointer -mno-omit-leaf-frame-pointer -flto=auto -ffat-lto-objects -fstack-protector-strong -fstack-clash-protection -Wformat -Werror=format-security -mbranch-protection=standard
CFLAGS_SL = -fPIC
LDFLAGS = -Wl,-Bsymbolic-functions -flto=auto -ffat-lto-objects -Wl,-z,relro -Wl,-z,now -Wl,--as-needed
LDFLAGS_EX = 
LDFLAGS_SL = 
LIBS = -lpgcommon -lpgport -lselinux -lzstd -llz4 -lxslt -lxml2 -lpam -lssl -lcrypto -lgssapi_krb5 -lz -lreadline -lm -lnuma 
VERSION = PostgreSQL 18.4 (Ubuntu 18.4-1.noble)`

const testRHELPg17ConfigOut = `BINDIR = /usr/pgsql-17/bin
DOCDIR = /usr/pgsql-17/doc
HTMLDIR = /usr/pgsql-17/doc/html
INCLUDEDIR = /usr/pgsql-17/include
PKGINCLUDEDIR = /usr/pgsql-17/include
INCLUDEDIR-SERVER = /usr/pgsql-17/include/server
LIBDIR = /usr/pgsql-17/lib
PKGLIBDIR = /usr/pgsql-17/lib
LOCALEDIR = /usr/pgsql-17/share/locale
MANDIR = /usr/pgsql-17/share/man
SHAREDIR = /usr/pgsql-17/share
SYSCONFDIR = /etc/sysconfig/pgsql
PGXS = /usr/pgsql-17/lib/pgxs/src/makefiles/pgxs.mk
CONFIGURE =  '--enable-rpath' '--prefix=/usr/pgsql-17' '--includedir=/usr/pgsql-17/include' '--mandir=/usr/pgsql-17/share/man' '--datadir=/usr/pgsql-17/share' '--libdir=/usr/pgsql-17/lib' '--with-lz4' '--with-zstd' '--enable-tap-tests' '--with-icu' '--with-llvm' '--with-perl' '--with-python' '--with-tcl' '--with-tclconfig=/usr/lib64' '--with-openssl' '--with-pam' '--with-gssapi' '--with-includes=/usr/include' '--with-libraries=/usr/lib64' '--enable-nls' '--enable-dtrace' '--with-uuid=e2fs' '--with-libxml' '--with-libxslt' '--with-ldap' '--with-selinux' '--with-systemd' '--with-system-tzdata=/usr/share/zoneinfo' '--sysconfdir=/etc/sysconfig/pgsql' '--docdir=/usr/pgsql-17/doc' '--htmldir=/usr/pgsql-17/doc/html' 'CFLAGS=-O2 -flto=auto -ffat-lto-objects -fexceptions -g -grecord-gcc-switches -pipe -Wall -Werror=format-security -Wp,-D_FORTIFY_SOURCE=2 -Wp,-D_GLIBCXX_ASSERTIONS -specs=/usr/lib/rpm/redhat/redhat-hardened-cc1 -fstack-protector-strong -specs=/usr/lib/rpm/redhat/redhat-annobin-cc1 -fasynchronous-unwind-tables -fstack-clash-protection' 'LDFLAGS=-Wl,--as-needed' 'LLVM_CONFIG=/usr/bin/llvm-config' 'CLANG=/usr/bin/clang' 'PKG_CONFIG_PATH=:/usr/lib64/pkgconfig:/usr/share/pkgconfig'
CC = gcc
CPPFLAGS = -D_GNU_SOURCE -I/usr/include -I/usr/include/libxml2
CFLAGS = -Wall -Wmissing-prototypes -Wpointer-arith -Wdeclaration-after-statement -Werror=vla -Wendif-labels -Wmissing-format-attribute -Wimplicit-fallthrough=3 -Wcast-function-type -Wshadow=compatible-local -Wformat-security -fno-strict-aliasing -fwrapv -fexcess-precision=standard -Wno-format-truncation -Wno-stringop-truncation -O2 -flto=auto -ffat-lto-objects -fexceptions -g -grecord-gcc-switches -pipe -Wall -Werror=format-security -Wp,-D_FORTIFY_SOURCE=2 -Wp,-D_GLIBCXX_ASSERTIONS -specs=/usr/lib/rpm/redhat/redhat-hardened-cc1 -fstack-protector-strong -specs=/usr/lib/rpm/redhat/redhat-annobin-cc1 -fasynchronous-unwind-tables -fstack-clash-protection
CFLAGS_SL = -fPIC
LDFLAGS = -Wl,--as-needed -L/usr/lib64 -Wl,--as-needed -Wl,-rpath,'/usr/pgsql-17/lib',--enable-new-dtags
LDFLAGS_EX = 
LDFLAGS_SL = 
LIBS = -lpgcommon -lpgport -lselinux -lzstd -llz4 -lxslt -lxml2 -lpam -lssl -lcrypto -lgssapi_krb5 -lz -lreadline -lm 
VERSION = PostgreSQL 17.11`

const testRHELPg18ConfigOut = `BINDIR = /usr/pgsql-18/bin
DOCDIR = /usr/pgsql-18/doc
HTMLDIR = /usr/pgsql-18/doc/html
INCLUDEDIR = /usr/pgsql-18/include
PKGINCLUDEDIR = /usr/pgsql-18/include
INCLUDEDIR-SERVER = /usr/pgsql-18/include/server
LIBDIR = /usr/pgsql-18/lib
PKGLIBDIR = /usr/pgsql-18/lib
LOCALEDIR = /usr/pgsql-18/share/locale
MANDIR = /usr/pgsql-18/share/man
SHAREDIR = /usr/pgsql-18/share
SYSCONFDIR = /etc/sysconfig/pgsql
PGXS = /usr/pgsql-18/lib/pgxs/src/makefiles/pgxs.mk
CONFIGURE =  '--enable-rpath' '--prefix=/usr/pgsql-18' '--includedir=/usr/pgsql-18/include' '--mandir=/usr/pgsql-18/share/man' '--datadir=/usr/pgsql-18/share' '--libdir=/usr/pgsql-18/lib' '--with-lz4' '--with-zstd' '--enable-tap-tests' '--with-icu' '--with-gssapi' '--with-includes=/usr/include' '--with-libraries=/usr/lib64' '--with-libnuma' '--with-liburing' '--with-ldap' '--with-llvm' '--enable-nls' '--with-pam' '--with-perl' '--with-python' '--with-tcl' '--with-tclconfig=/usr/lib64' '--enable-dtrace' '--with-selinux' '--with-openssl' '--with-uuid=e2fs' '--with-libxml' '--with-libxslt' '--with-libcurl' '--with-systemd' '--with-system-tzdata=/usr/share/zoneinfo' '--sysconfdir=/etc/sysconfig/pgsql' '--docdir=/usr/pgsql-18/doc' '--htmldir=/usr/pgsql-18/doc/html' 'CFLAGS=-O2 -flto=auto -ffat-lto-objects -fexceptions -g -grecord-gcc-switches -pipe -Wall -Werror=format-security -Wp,-D_FORTIFY_SOURCE=2 -Wp,-D_GLIBCXX_ASSERTIONS -specs=/usr/lib/rpm/redhat/redhat-hardened-cc1 -fstack-protector-strong -specs=/usr/lib/rpm/redhat/redhat-annobin-cc1 -fasynchronous-unwind-tables -fstack-clash-protection' 'LDFLAGS=-Wl,--as-needed' 'LLVM_CONFIG=/usr/bin/llvm-config' 'CLANG=/usr/bin/clang' 'PKG_CONFIG_PATH=:/usr/lib64/pkgconfig:/usr/share/pkgconfig'
CC = gcc
CPPFLAGS = -D_GNU_SOURCE -I/usr/include -I/usr/include/libxml2
CFLAGS = -Wall -Wmissing-prototypes -Wpointer-arith -Wdeclaration-after-statement -Werror=vla -Wendif-labels -Wmissing-format-attribute -Wimplicit-fallthrough=3 -Wcast-function-type -Wshadow=compatible-local -Wformat-security -fno-strict-aliasing -fwrapv -fexcess-precision=standard -Wno-format-truncation -Wno-stringop-truncation -O2 -flto=auto -ffat-lto-objects -fexceptions -g -grecord-gcc-switches -pipe -Wall -Werror=format-security -Wp,-D_FORTIFY_SOURCE=2 -Wp,-D_GLIBCXX_ASSERTIONS -specs=/usr/lib/rpm/redhat/redhat-hardened-cc1 -fstack-protector-strong -specs=/usr/lib/rpm/redhat/redhat-annobin-cc1 -fasynchronous-unwind-tables -fstack-clash-protection
CFLAGS_SL = -fPIC
LDFLAGS = -Wl,--as-needed -L/usr/lib64 -Wl,--as-needed -Wl,-rpath,'/usr/pgsql-18/lib',--enable-new-dtags
LDFLAGS_EX = 
LDFLAGS_SL = 
LIBS = -lpgcommon -lpgport -lselinux -lzstd -llz4 -lxslt -lxml2 -lpam -lssl -lcrypto -lgssapi_krb5 -lz -lreadline -lm -lnuma 
VERSION = PostgreSQL 18.4`

const testDebianRCPgConfigOut = `BINDIR = /usr/lib/postgresql/18/bin
DOCDIR = /usr/share/doc/postgresql-doc-18
HTMLDIR = /usr/share/doc/postgresql-doc-18
INCLUDEDIR = /usr/include/postgresql
PKGINCLUDEDIR = /usr/include/postgresql
INCLUDEDIR-SERVER = /usr/include/postgresql/18/server
LIBDIR = /usr/lib/aarch64-linux-gnu
PKGLIBDIR = /usr/lib/postgresql/18/lib
LOCALEDIR = /usr/share/locale
MANDIR = /usr/share/postgresql/18/man
SHAREDIR = /usr/share/postgresql/18
SYSCONFDIR = /etc/postgresql-common
PGXS = /usr/lib/postgresql/18/lib/pgxs/src/makefiles/pgxs.mk
CONFIGURE =  '--build=aarch64-linux-gnu' '--prefix=/usr' '--includedir=${prefix}/include' '--mandir=${prefix}/share/man' '--infodir=${prefix}/share/info' '--sysconfdir=/etc' '--localstatedir=/var' '--disable-option-checking' '--disable-silent-rules' '--libdir=${prefix}/lib/aarch64-linux-gnu' '--runstatedir=/run' '--disable-maintainer-mode' '--disable-dependency-tracking' '--with-tcl' '--with-perl' '--with-python' '--with-pam' '--with-openssl' '--with-libxml' '--with-libxslt' '--mandir=/usr/share/postgresql/18/man' '--docdir=/usr/share/doc/postgresql-doc-18' '--sysconfdir=/etc/postgresql-common' '--datarootdir=/usr/share/' '--datadir=/usr/share/postgresql/18' '--bindir=/usr/lib/postgresql/18/bin' '--libdir=/usr/lib/aarch64-linux-gnu/' '--libexecdir=/usr/lib/postgresql/' '--includedir=/usr/include/postgresql/' '--with-extra-version= (Ubuntu 18.4-1.noble)' '--enable-nls' '--enable-thread-safety' '--enable-debug' '--disable-rpath' '--with-uuid=e2fs' '--with-gnu-ld' '--with-gssapi' '--with-ldap' '--with-pgport=5432' '--with-system-tzdata=/usr/share/zoneinfo' 'AWK=mawk' 'MKDIR_P=/bin/mkdir -p' 'PROVE=/usr/bin/prove' 'PYTHON=/usr/bin/python3' 'TAR=/bin/tar' 'XSLTPROC=xsltproc --nonet' 'CFLAGS=-g -O2 -fno-omit-frame-pointer -mno-omit-leaf-frame-pointer -flto=auto -ffat-lto-objects -fstack-protector-strong -fstack-clash-protection -Wformat -Werror=format-security -mbranch-protection=standard' 'LDFLAGS=-Wl,-Bsymbolic-functions -flto=auto -ffat-lto-objects -Wl,-z,relro -Wl,-z,now' '--enable-tap-tests' '--with-icu' '--with-llvm' 'LLVM_CONFIG=/usr/bin/llvm-config-18' 'CLANG=/usr/bin/clang-18' '--with-lz4' '--with-zstd' '--with-libcurl' '--with-libnuma' 'LIBNUMA_CFLAGS= ' 'LIBNUMA_LIBS=-lnuma' '--with-liburing' '--with-systemd' '--with-selinux' '--enable-dtrace' 'build_alias=aarch64-linux-gnu' 'CPPFLAGS=-Wdate-time -D_FORTIFY_SOURCE=3' 'CXXFLAGS=-g -O2 -fno-omit-frame-pointer -mno-omit-leaf-frame-pointer -flto=auto -ffat-lto-objects -fstack-protector-strong -fstack-clash-protection -Wformat -Werror=format-security -mbranch-protection=standard'
CC = gcc
CPPFLAGS = -Wdate-time -D_FORTIFY_SOURCE=3 -D_GNU_SOURCE -I/usr/include/libxml2
CFLAGS = -Wall -Wmissing-prototypes -Wpointer-arith -Wdeclaration-after-statement -Werror=vla -Wendif-labels -Wmissing-format-attribute -Wimplicit-fallthrough=3 -Wcast-function-type -Wshadow=compatible-local -Wformat-security -fno-strict-aliasing -fwrapv -fexcess-precision=standard -Wno-format-truncation -Wno-stringop-truncation -g -g -O2 -fno-omit-frame-pointer -mno-omit-leaf-frame-pointer -flto=auto -ffat-lto-objects -fstack-protector-strong -fstack-clash-protection -Wformat -Werror=format-security -mbranch-protection=standard
CFLAGS_SL = -fPIC
LDFLAGS = -Wl,-Bsymbolic-functions -flto=auto -ffat-lto-objects -Wl,-z,relro -Wl,-z,now -Wl,--as-needed
LDFLAGS_EX = 
LDFLAGS_SL = 
LIBS = -lpgcommon -lpgport -lselinux -lzstd -llz4 -lxslt -lxml2 -lpam -lssl -lcrypto -lgssapi_krb5 -lz -lreadline -lm -lnuma 
VERSION = PostgreSQL 18rc1 (Ubuntu 18~rc1-1.noble)`

const spock5Control = `# spock extension
comment = 'PostgreSQL Logical Replication'
default_version = '5.0.9'
module_pathname = '$libdir/spock'
relocatable = false

schema = spock`

const spock6Control = `# spock extension
comment = 'PostgreSQL Logical Replication'
default_version = '6.0.0'
module_pathname = '$libdir/spock'
relocatable = false

schema = spock`
