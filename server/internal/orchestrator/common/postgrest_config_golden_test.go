package common

import (
	"flag"
	"testing"

	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/testutils"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

// TestPostgRESTConfigGolden pins the rendered postgrest.conf so that changes
// to it are deliberate. The swarm orchestrator must render identical files.
func TestPostgRESTConfigGolden(t *testing.T) {
	// The update flag is registered by TestMain in the external test package.
	update := flag.Lookup("update").Value.String() == "true"
	golden := &testutils.GoldenTest[string]{
		FileExtension: ".conf",
		Marshal: func(v any) ([]byte, error) {
			return []byte(v.(string)), nil
		},
		Unmarshal: func(data []byte, v any) error {
			*v.(*string) = string(data)
			return nil
		},
	}

	for _, tc := range []struct {
		name     string
		resource *PostgRESTConfigResource
	}{
		{
			name: "minimal",
			resource: &PostgRESTConfigResource{
				Config: &database.PostgRESTServiceConfig{
					DBSchemas:  "public",
					DBAnonRole: "web_anon",
					DBPool:     10,
					MaxRows:    1000,
				},
				ConnectAsUsername: "app",
				ConnectAsPassword: "pass",
				DatabaseName:      "mydb",
				DatabaseHosts:     []database.ServiceHostEntry{{Host: "postgres-inst-1", Port: 5432}},
			},
		},
		{
			name: "jwt and cors",
			resource: &PostgRESTConfigResource{
				Config: &database.PostgRESTServiceConfig{
					DBSchemas:                "api,public",
					DBAnonRole:               "web_anon",
					DBPool:                   5,
					MaxRows:                  500,
					JWTSecret:                utils.PointerTo("a-very-long-jwt-secret-that-is-at-least-32-chars"),
					JWTAud:                   utils.PointerTo("my-audience"),
					JWTRoleClaimKey:          utils.PointerTo(".role"),
					ServerCORSAllowedOrigins: utils.PointerTo("https://example.com"),
				},
				ConnectAsUsername: "app",
				ConnectAsPassword: "pass",
				DatabaseName:      "mydb",
				DatabaseHosts:     []database.ServiceHostEntry{{Host: "postgres-inst-1", Port: 5432}},
			},
		},
		{
			name: "multi host",
			resource: &PostgRESTConfigResource{
				Config: &database.PostgRESTServiceConfig{
					DBSchemas:  "public",
					DBAnonRole: "web_anon",
					DBPool:     10,
					MaxRows:    1000,
				},
				ConnectAsUsername: "app",
				ConnectAsPassword: "p@ss:word",
				DatabaseName:      "mydb",
				DatabaseHosts: []database.ServiceHostEntry{
					{Host: "10.0.0.1", Port: 6432},
					{Host: "10.0.0.2", Port: 6432},
					{Host: "10.0.0.3", Port: 6432},
				},
				TargetSessionAttrs: "read-write",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content, err := tc.resource.generateConf()
			if err != nil {
				t.Fatal(err)
			}
			golden.Run(t, string(content), update)
		})
	}
}
