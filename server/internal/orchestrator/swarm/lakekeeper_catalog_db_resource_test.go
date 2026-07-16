package swarm

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/postgres"
)

func TestLakekeeperCatalogDBName(t *testing.T) {
	if got := lakekeeperCatalogDBName("mydb"); got != "mydb_lakekeeper" {
		t.Fatalf("got %q, want %q", got, "mydb_lakekeeper")
	}
	// 63-byte identifier cap: a long ASCII name is truncated to exactly the
	// limit, prefix trimmed and suffix intact.
	long := lakekeeperCatalogDBName(strings.Repeat("a", 70))
	if want := strings.Repeat("a", 52) + "_lakekeeper"; long != want {
		t.Fatalf("long name = %q (%d bytes), want %q (%d bytes)", long, len(long), want, len(want))
	}

	// Multi-byte input: truncation must land on a whole-rune boundary, never
	// leaving a partial rune (which would be an invalid identifier on a UTF8
	// database). "€" is 3 bytes; 26 of them = 78 bytes, so the 52-byte cut
	// falls inside the 18th rune.
	multi := lakekeeperCatalogDBName(strings.Repeat("€", 26))
	if len(multi) > 63 {
		t.Fatalf("multibyte catalog db name %d bytes, exceeds 63", len(multi))
	}
	if !utf8.ValidString(multi) {
		t.Fatalf("truncation produced invalid UTF-8: %q", multi)
	}
	if !strings.HasSuffix(multi, "_lakekeeper") {
		t.Fatalf("truncated name lost its suffix: %q", multi)
	}
	// 17 whole "€" (51 bytes) is the largest that fits under the 52-byte
	// prefix limit without splitting a rune.
	if want := strings.Repeat("€", 17) + "_lakekeeper"; multi != want {
		t.Fatalf("multibyte name = %q, want %q", multi, want)
	}
}

func TestBuildManagedCatalogDBURL(t *testing.T) {
	got := buildManagedCatalogDBURL(
		database.ServiceHostEntry{Host: "postgres-abc123", Port: 5432},
		"app_ro", "p@ss/word", "mydb_lakekeeper",
	)
	want := "postgres://app_ro:p%40ss%2Fword@postgres-abc123:5432/mydb_lakekeeper"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	// The password contains reserved characters; confirm the URL round-trips
	// so the escaping is correct, not merely string-equal to an expectation.
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("produced URL does not parse: %v", err)
	}
	pw, _ := u.User.Password()
	if pw != "p@ss/word" {
		t.Fatalf("password did not round-trip: got %q, want %q", pw, "p@ss/word")
	}
	if user := u.User.Username(); user != "app_ro" {
		t.Fatalf("username did not round-trip: got %q, want %q", user, "app_ro")
	}
}

func TestEnsureCatalogDBStatements(t *testing.T) {
	stmts := ensureCatalogDBStatements("mydb_lakekeeper", "app_ro")
	if len(stmts) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(stmts))
	}
	// First statement: the idempotent conditional CREATE DATABASE.
	cond, ok := stmts[0].(postgres.ConditionalStatement)
	if !ok {
		t.Fatalf("statement 0 is %T, want ConditionalStatement", stmts[0])
	}
	// ConditionalStatement.Then is an IStatement (interface) — assert to the
	// concrete postgres.Statement before reading .SQL (it has no .SQL field
	// through the interface).
	then, ok := cond.Then.(postgres.Statement)
	if !ok {
		t.Fatalf("cond.Then is %T, want postgres.Statement", cond.Then)
	}
	if !strings.Contains(then.SQL, `CREATE DATABASE "mydb_lakekeeper"`) {
		t.Fatalf("unexpected create SQL: %s", then.SQL)
	}
	// Second statement: owner alignment with quoted identifiers.
	own, ok := stmts[1].(postgres.Statement)
	if !ok {
		t.Fatalf("statement 1 is %T, want Statement", stmts[1])
	}
	want := `ALTER DATABASE "mydb_lakekeeper" OWNER TO "app_ro";`
	if own.SQL != want {
		t.Fatalf("owner SQL = %q, want %q", own.SQL, want)
	}
}

func TestLakekeeperCatalogDBResourceRefresh(t *testing.T) {
	r := &LakekeeperCatalogDBResource{}
	if err := r.Refresh(context.Background(), nil); err == nil {
		t.Fatal("expected ErrNotFound before creation")
	}
	r.Created = true
	if err := r.Refresh(context.Background(), nil); err != nil {
		t.Fatalf("expected nil after creation, got %v", err)
	}
}

func TestCatalogDBExtensionNames(t *testing.T) {
	// Pin the resource's actual extension list (the one ensure iterates), and
	// the quoted-identifier form ("uuid-ossp" needs quoting — it has a hyphen).
	// Asserting against catalogDBExtensions() rather than a local literal means
	// reordering or editing the real list is caught here.
	want := []string{
		`"uuid-ossp"`,
		`"pgcrypto"`,
		`"pg_trgm"`,
		`"btree_gin"`,
	}
	exts := catalogDBExtensions()
	if len(exts) != len(want) {
		t.Fatalf("expected %d extensions, got %d", len(want), len(exts))
	}
	for i, ext := range exts {
		if got := postgres.QuoteIdentifier(ext); got != want[i] {
			t.Fatalf("extension %d (%q) quoted = %q, want %q", i, ext, got, want[i])
		}
	}
}
