package paths

import (
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
)

func initPaths() {
	Home = xdg.Home
	// The xdg library wants to put everything in ~/Library/Application Support
	// on macOS. We want to follow the linux conventions instead, which is more
	// common and ergonomic for CLI tools.
	ConfigHome = envOrDefault("XDG_CONFIG_HOME", filepath.Join(Home, ".config"))
	DataHome = envOrDefault("XDG_DATA_HOME", filepath.Join(Home, ".local", "share"))
	CacheHome = envOrDefault("XDG_CACHE_HOME", filepath.Join(Home, ".cache"))
}

func envOrDefault(envName string, defaultVal string) string {
	if envVal := os.Getenv(envName); envVal != "" {
		return envVal
	}
	return defaultVal
}
