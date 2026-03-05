package swarm

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func TestGenerateTokenFile(t *testing.T) {
	t.Run("valid YAML with tokens top-level key", func(t *testing.T) {
		data, err := GenerateTokenFile("my-secret-token")
		require.NoError(t, err)
		require.NotEmpty(t, data)

		var store mcpTokenStore
		err = yaml.Unmarshal(data, &store)
		require.NoError(t, err)
		assert.NotNil(t, store.Tokens)
	})

	t.Run("bootstrap-token entry exists", func(t *testing.T) {
		data, err := GenerateTokenFile("my-secret-token")
		require.NoError(t, err)

		var store mcpTokenStore
		require.NoError(t, yaml.Unmarshal(data, &store))

		token, ok := store.Tokens["bootstrap-token"]
		require.True(t, ok, "expected 'bootstrap-token' key in tokens map")
		assert.NotNil(t, token)
	})

	t.Run("hash matches SHA256 of input", func(t *testing.T) {
		input := "super-secret-init-token"
		data, err := GenerateTokenFile(input)
		require.NoError(t, err)

		var store mcpTokenStore
		require.NoError(t, yaml.Unmarshal(data, &store))

		token := store.Tokens["bootstrap-token"]
		require.NotNil(t, token)

		sum := sha256.Sum256([]byte(input))
		expected := hex.EncodeToString(sum[:])
		assert.Equal(t, expected, token.Hash)
	})

	t.Run("annotation is set correctly", func(t *testing.T) {
		data, err := GenerateTokenFile("tok")
		require.NoError(t, err)

		var store mcpTokenStore
		require.NoError(t, yaml.Unmarshal(data, &store))

		token := store.Tokens["bootstrap-token"]
		require.NotNil(t, token)
		assert.Equal(t, "Bootstrap token from control plane", token.Annotation)
	})

	t.Run("created_at is set to a recent time", func(t *testing.T) {
		before := time.Now().UTC().Add(-2 * time.Second)
		data, err := GenerateTokenFile("tok")
		require.NoError(t, err)
		after := time.Now().UTC().Add(2 * time.Second)

		var store mcpTokenStore
		require.NoError(t, yaml.Unmarshal(data, &store))

		token := store.Tokens["bootstrap-token"]
		require.NotNil(t, token)
		assert.True(t, token.CreatedAt.After(before), "created_at should be after test start")
		assert.True(t, token.CreatedAt.Before(after), "created_at should be before test end")
	})

	t.Run("expires_at is nil", func(t *testing.T) {
		data, err := GenerateTokenFile("tok")
		require.NoError(t, err)

		var store mcpTokenStore
		require.NoError(t, yaml.Unmarshal(data, &store))

		token := store.Tokens["bootstrap-token"]
		require.NotNil(t, token)
		assert.Nil(t, token.ExpiresAt)
	})

	t.Run("different tokens produce different hashes", func(t *testing.T) {
		data1, err := GenerateTokenFile("token-a")
		require.NoError(t, err)
		data2, err := GenerateTokenFile("token-b")
		require.NoError(t, err)

		var store1, store2 mcpTokenStore
		require.NoError(t, yaml.Unmarshal(data1, &store1))
		require.NoError(t, yaml.Unmarshal(data2, &store2))

		assert.NotEqual(t, store1.Tokens["bootstrap-token"].Hash, store2.Tokens["bootstrap-token"].Hash)
	})
}

func TestGenerateUserFile(t *testing.T) {
	t.Run("single user: valid YAML with users top-level key", func(t *testing.T) {
		users := []database.MCPServiceUser{
			{Username: "alice", Password: "password123"},
		}
		data, err := GenerateUserFile(users)
		require.NoError(t, err)
		require.NotEmpty(t, data)

		var store mcpUserStore
		err = yaml.Unmarshal(data, &store)
		require.NoError(t, err)
		assert.NotNil(t, store.Users)
	})

	t.Run("single user: correct username in output", func(t *testing.T) {
		users := []database.MCPServiceUser{
			{Username: "alice", Password: "password123"},
		}
		data, err := GenerateUserFile(users)
		require.NoError(t, err)

		var store mcpUserStore
		require.NoError(t, yaml.Unmarshal(data, &store))

		user, ok := store.Users["alice"]
		require.True(t, ok, "expected 'alice' key in users map")
		assert.Equal(t, "alice", user.Username)
	})

	t.Run("single user: password_hash is valid bcrypt matching input", func(t *testing.T) {
		password := "s3cr3tP@ssword"
		users := []database.MCPServiceUser{
			{Username: "bob", Password: password},
		}
		data, err := GenerateUserFile(users)
		require.NoError(t, err)

		var store mcpUserStore
		require.NoError(t, yaml.Unmarshal(data, &store))

		user := store.Users["bob"]
		require.NotNil(t, user)
		require.NotEmpty(t, user.PasswordHash)

		err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
		assert.NoError(t, err, "bcrypt hash should match original password")
	})

	t.Run("single user: enabled is true", func(t *testing.T) {
		users := []database.MCPServiceUser{
			{Username: "carol", Password: "pass"},
		}
		data, err := GenerateUserFile(users)
		require.NoError(t, err)

		var store mcpUserStore
		require.NoError(t, yaml.Unmarshal(data, &store))

		user := store.Users["carol"]
		require.NotNil(t, user)
		assert.True(t, user.Enabled)
	})

	t.Run("single user: annotation is set correctly", func(t *testing.T) {
		users := []database.MCPServiceUser{
			{Username: "dave", Password: "pass"},
		}
		data, err := GenerateUserFile(users)
		require.NoError(t, err)

		var store mcpUserStore
		require.NoError(t, yaml.Unmarshal(data, &store))

		user := store.Users["dave"]
		require.NotNil(t, user)
		assert.Equal(t, "Bootstrap user from control plane", user.Annotation)
	})

	t.Run("single user: created_at is set to a recent time", func(t *testing.T) {
		users := []database.MCPServiceUser{
			{Username: "eve", Password: "pass"},
		}
		before := time.Now().UTC().Add(-2 * time.Second)
		data, err := GenerateUserFile(users)
		require.NoError(t, err)
		after := time.Now().UTC().Add(2 * time.Second)

		var store mcpUserStore
		require.NoError(t, yaml.Unmarshal(data, &store))

		user := store.Users["eve"]
		require.NotNil(t, user)
		assert.True(t, user.CreatedAt.After(before), "created_at should be after test start")
		assert.True(t, user.CreatedAt.Before(after), "created_at should be before test end")
	})

	t.Run("single user: failed_attempts is zero", func(t *testing.T) {
		users := []database.MCPServiceUser{
			{Username: "frank", Password: "pass"},
		}
		data, err := GenerateUserFile(users)
		require.NoError(t, err)

		var store mcpUserStore
		require.NoError(t, yaml.Unmarshal(data, &store))

		user := store.Users["frank"]
		require.NotNil(t, user)
		assert.Equal(t, 0, user.FailedAttempts)
	})

	t.Run("multiple users: all appear in output", func(t *testing.T) {
		users := []database.MCPServiceUser{
			{Username: "user1", Password: "pass1"},
			{Username: "user2", Password: "pass2"},
			{Username: "user3", Password: "pass3"},
		}
		data, err := GenerateUserFile(users)
		require.NoError(t, err)

		var store mcpUserStore
		require.NoError(t, yaml.Unmarshal(data, &store))

		assert.Len(t, store.Users, 3)
		for _, u := range users {
			entry, ok := store.Users[u.Username]
			require.True(t, ok, "expected user %q in output", u.Username)
			assert.Equal(t, u.Username, entry.Username)

			err = bcrypt.CompareHashAndPassword([]byte(entry.PasswordHash), []byte(u.Password))
			assert.NoError(t, err, "bcrypt hash should match password for user %q", u.Username)
		}
	})

	t.Run("multiple users: each has unique hash", func(t *testing.T) {
		users := []database.MCPServiceUser{
			{Username: "x", Password: "passX"},
			{Username: "y", Password: "passY"},
		}
		data, err := GenerateUserFile(users)
		require.NoError(t, err)

		var store mcpUserStore
		require.NoError(t, yaml.Unmarshal(data, &store))

		assert.NotEqual(t, store.Users["x"].PasswordHash, store.Users["y"].PasswordHash)
	})

	t.Run("empty user list produces empty users map", func(t *testing.T) {
		data, err := GenerateUserFile([]database.MCPServiceUser{})
		require.NoError(t, err)

		var store mcpUserStore
		require.NoError(t, yaml.Unmarshal(data, &store))
		assert.Empty(t, store.Users)
	})
}

func TestGenerateEmptyTokenFile(t *testing.T) {
	t.Run("returns no error", func(t *testing.T) {
		_, err := GenerateEmptyTokenFile()
		assert.NoError(t, err)
	})

	t.Run("valid YAML", func(t *testing.T) {
		data, err := GenerateEmptyTokenFile()
		require.NoError(t, err)
		require.NotEmpty(t, data)

		var raw map[string]any
		err = yaml.Unmarshal(data, &raw)
		assert.NoError(t, err)
	})

	t.Run("tokens key is present and empty", func(t *testing.T) {
		data, err := GenerateEmptyTokenFile()
		require.NoError(t, err)

		var store mcpTokenStore
		require.NoError(t, yaml.Unmarshal(data, &store))
		require.NotNil(t, store.Tokens)
		assert.Empty(t, store.Tokens)
	})
}

func TestGenerateEmptyUserFile(t *testing.T) {
	t.Run("returns no error", func(t *testing.T) {
		_, err := GenerateEmptyUserFile()
		assert.NoError(t, err)
	})

	t.Run("valid YAML", func(t *testing.T) {
		data, err := GenerateEmptyUserFile()
		require.NoError(t, err)
		require.NotEmpty(t, data)

		var raw map[string]any
		err = yaml.Unmarshal(data, &raw)
		assert.NoError(t, err)
	})

	t.Run("users key is present and empty", func(t *testing.T) {
		data, err := GenerateEmptyUserFile()
		require.NoError(t, err)

		var store mcpUserStore
		require.NoError(t, yaml.Unmarshal(data, &store))
		require.NotNil(t, store.Users)
		assert.Empty(t, store.Users)
	})
}
