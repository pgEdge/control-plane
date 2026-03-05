package swarm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/pgEdge/control-plane/server/internal/database"
	"golang.org/x/crypto/bcrypt"
)

// mcpTokenStore mirrors the MCP server's TokenStore YAML format.
type mcpTokenStore struct {
	Tokens map[string]*mcpToken `yaml:"tokens"`
}

// mcpToken mirrors the MCP server's Token struct.
type mcpToken struct {
	Hash       string     `yaml:"hash"`
	ExpiresAt  *time.Time `yaml:"expires_at"`
	Annotation string     `yaml:"annotation"`
	CreatedAt  time.Time  `yaml:"created_at"`
	Database   string     `yaml:"database,omitempty"`
}

// mcpUserStore mirrors the MCP server's UserStore YAML format.
type mcpUserStore struct {
	Users map[string]*mcpUser `yaml:"users"`
}

// mcpUser mirrors the MCP server's User struct.
type mcpUser struct {
	Username       string     `yaml:"username"`
	PasswordHash   string     `yaml:"password_hash"`
	CreatedAt      time.Time  `yaml:"created_at"`
	LastLogin      *time.Time `yaml:"last_login,omitempty"`
	Enabled        bool       `yaml:"enabled"`
	Annotation     string     `yaml:"annotation"`
	FailedAttempts int        `yaml:"failed_attempts"`
}

// GenerateTokenFile generates a tokens.yaml file from the given init token.
// The token is SHA256-hashed to match the MCP server's auth.HashToken() format.
func GenerateTokenFile(initToken string) ([]byte, error) {
	hash := sha256.Sum256([]byte(initToken))
	hashHex := hex.EncodeToString(hash[:])

	store := &mcpTokenStore{
		Tokens: map[string]*mcpToken{
			"bootstrap-token": {
				Hash:       hashHex,
				CreatedAt:  time.Now().UTC(),
				Annotation: "Bootstrap token from control plane",
			},
		},
	}

	data, err := yaml.Marshal(store)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal token store to YAML: %w", err)
	}
	return data, nil
}

// GenerateUserFile generates a users.yaml file from the given bootstrap users.
// Passwords are bcrypt-hashed with cost 12 to match the MCP server's auth.HashPassword() format.
func GenerateUserFile(users []database.MCPServiceUser) ([]byte, error) {
	store := &mcpUserStore{
		Users: make(map[string]*mcpUser, len(users)),
	}

	now := time.Now().UTC()
	for _, u := range users {
		hash, err := bcrypt.GenerateFromPassword([]byte(u.Password), 12)
		if err != nil {
			return nil, fmt.Errorf("failed to hash password for user %q: %w", u.Username, err)
		}
		store.Users[u.Username] = &mcpUser{
			Username:       u.Username,
			PasswordHash:   string(hash),
			CreatedAt:      now,
			Enabled:        true,
			Annotation:     "Bootstrap user from control plane",
			FailedAttempts: 0,
		}
	}

	data, err := yaml.Marshal(store)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal user store to YAML: %w", err)
	}
	return data, nil
}

// GenerateEmptyTokenFile generates an empty but valid tokens.yaml file.
func GenerateEmptyTokenFile() ([]byte, error) {
	store := &mcpTokenStore{
		Tokens: make(map[string]*mcpToken),
	}
	data, err := yaml.Marshal(store)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal empty token store to YAML: %w", err)
	}
	return data, nil
}

// GenerateEmptyUserFile generates an empty but valid users.yaml file.
func GenerateEmptyUserFile() ([]byte, error) {
	store := &mcpUserStore{
		Users: make(map[string]*mcpUser),
	}
	data, err := yaml.Marshal(store)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal empty user store to YAML: %w", err)
	}
	return data, nil
}
