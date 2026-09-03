package database

import "strings"

// IsSensitiveConfigKey returns true if the given service config key name
// likely contains a secret value that should not be returned in API
// responses, and whose omission on an update should restore the stored value
// rather than be treated as removal. Shared by the API layer (which strips
// these keys from GET responses) and DefaultOptionalFieldsFrom (which fills
// them back in from stored state when omitted from an update).
func IsSensitiveConfigKey(key string) bool {
	k := strings.ToLower(key)
	// Use suffix matching for "token" to avoid stripping non-secret keys like
	// "token_budget". Keys named exactly "token" or ending with "_token" (e.g.
	// "init_token", "auth_token") are still treated as sensitive.
	if k == "token" || strings.HasSuffix(k, "_token") {
		return true
	}
	patterns := []string{
		"password", "secret",
		"api_key", "apikey", "api-key",
		"credential", "private_key", "private-key",
		"access_key", "access-key",
		"init_users", // mcp 'init_users' contains embedded passwords and must be stripped
	}
	for _, p := range patterns {
		if strings.Contains(k, p) {
			return true
		}
	}
	return false
}

// isBlankConfigValue reports whether v represents "no value supplied" for a
// sensitive config field: either the key was omitted entirely (the API layer
// deletes rather than blanks stripped keys) or it was submitted as null/empty
// string.
func isBlankConfigValue(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && s == ""
}

// restoreSensitiveConfig is the inverse of the API layer's config scrubbing:
// it fills sensitive keys that are missing or blank in newConfig with the
// corresponding value from oldConfig, so a read-edit-write cycle on an
// unrelated field doesn't require the caller to re-supply secrets that GET
// never showed them. Nested objects inside arrays (e.g. RAG pipelines) are
// matched to their old counterpart by a "name" field when present, falling
// back to position.
func restoreSensitiveConfig(newConfig, oldConfig map[string]any) map[string]any {
	if newConfig == nil {
		return nil
	}
	out := make(map[string]any, len(newConfig))
	for k, v := range newConfig {
		if IsSensitiveConfigKey(k) && isBlankConfigValue(v) {
			if old, ok := oldConfig[k]; ok && !isBlankConfigValue(old) {
				out[k] = old
				continue
			}
		}
		out[k] = restoreSensitiveValue(v, oldConfig[k])
	}
	// Sensitive keys entirely absent from newConfig (the common case, since
	// the API layer deletes rather than blanks them) are restored here.
	for k, old := range oldConfig {
		if _, present := newConfig[k]; present {
			continue
		}
		if IsSensitiveConfigKey(k) && !isBlankConfigValue(old) {
			out[k] = old
		}
	}
	return out
}

func restoreSensitiveValue(newVal, oldVal any) any {
	switch nv := newVal.(type) {
	case map[string]any:
		ov, _ := oldVal.(map[string]any)
		return restoreSensitiveConfig(nv, ov)
	case []any:
		ov, _ := oldVal.([]any)
		oldByName := make(map[string]map[string]any, len(ov))
		for _, elem := range ov {
			if em, ok := elem.(map[string]any); ok {
				if name, ok := em["name"].(string); ok {
					oldByName[name] = em
				}
			}
		}
		out := make([]any, len(nv))
		for i, elem := range nv {
			em, ok := elem.(map[string]any)
			if !ok {
				out[i] = elem
				continue
			}
			var match map[string]any
			if name, ok := em["name"].(string); ok {
				match = oldByName[name]
			} else if i < len(ov) {
				match, _ = ov[i].(map[string]any)
			}
			out[i] = restoreSensitiveConfig(em, match)
		}
		return out
	default:
		return newVal
	}
}

// DefaultOptionalFieldsFrom will default this service's config secrets to the
// values from the given service, for any secret omitted or blank in this
// service's config. This gives service secrets (e.g. a RAG pipeline's
// api_key) the same "omitted means keep the stored value" semantics that
// User.DefaultOptionalFieldsFrom and Repository.DefaultOptionalFieldsFrom
// already provide for database user passwords and backup/restore repository
// credentials.
func (s *ServiceSpec) DefaultOptionalFieldsFrom(other *ServiceSpec) {
	if other == nil || s.Config == nil {
		return
	}
	s.Config = restoreSensitiveConfig(s.Config, other.Config)
}
