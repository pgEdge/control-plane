package version

import (
	"errors"
	"runtime/debug"
)

type Info struct {
	Arch         string `json:"arch"`
	Revision     string `json:"revision"`
	RevisionTime string `json:"revision_time"`
	Version      string `json:"version"`
}

func GetInfo() (*Info, error) {
	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		return nil, errors.New("could not read build info")
	}
	var revision, revisionTime, arch string
	for _, setting := range buildInfo.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.time":
			revisionTime = setting.Value
		case "vcs.modified":
			if setting.Value == "true" {
				revision += "+dirty"
			}
		case "GOARCH":
			arch = setting.Value
		}
	}
	return &Info{
		Version:      buildInfo.Main.Version,
		Revision:     revision,
		RevisionTime: revisionTime,
		Arch:         arch,
	}, nil
}
