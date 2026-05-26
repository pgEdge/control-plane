package paths

var (
	Home       string
	ConfigHome string
	DataHome   string
	CacheHome  string
)

func init() {
	initPaths()
}
