//go:build !darwin

package paths

import "github.com/adrg/xdg"

func initPaths() {
	Home = xdg.Home
	ConfigHome = xdg.ConfigHome
	DataHome = xdg.DataHome
	CacheHome = xdg.CacheHome
}
