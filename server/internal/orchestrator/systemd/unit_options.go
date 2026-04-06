package systemd

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/coreos/go-systemd/v22/unit"
)

type ServiceType string

func (s ServiceType) String() string {
	return string(s)
}

const (
	ServiceTypeSimple       ServiceType = "simple"
	ServiceTypeExec         ServiceType = "exec"
	ServiceTypeForking      ServiceType = "forking"
	ServiceTypeOneShot      ServiceType = "oneshot"
	ServiceTypeNotify       ServiceType = "notify"
	ServiceTypeDBus         ServiceType = "dbus"
	ServiceTypeIdle         ServiceType = "idle"
	ServiceTypeNotifyReload ServiceType = "notify-reload"
)

type ServiceKillMode string

func (s ServiceKillMode) String() string {
	return string(s)
}

const (
	ServiceKillModeControlGroup ServiceKillMode = "control-group"
	ServiceKillModeMixed        ServiceKillMode = "mixed"
	ServiceKillModeProcess      ServiceKillMode = "process"
	ServiceKillModeNone         ServiceKillMode = "none"
)

type ServiceRestart string

func (s ServiceRestart) String() string {
	return string(s)
}

const (
	ServiceRestartNo         ServiceRestart = "no"
	ServiceRestartAlways     ServiceRestart = "always"
	ServiceRestartOnSuccess  ServiceRestart = "on-success"
	ServiceRestartOnFailure  ServiceRestart = "on-failure"
	ServiceRestartOnAbnormal ServiceRestart = "on-abnormal"
	ServiceRestartOnAbort    ServiceRestart = "on-abort"
	ServiceRestartOnWatchdog ServiceRestart = "on-watchdog"
)

type UnitFile struct {
	Unit    UnitSection
	Service ServiceSection
	Install InstallSection
}

func (f UnitFile) Options() []*unit.UnitOption {
	return slices.Concat(
		f.Unit.Options(),
		f.Service.Options(),
		f.Install.Options(),
	)
}

type UnitSection struct {
	After []string
}

func (u UnitSection) Options() []*unit.UnitOption {
	var opts []*unit.UnitOption
	if len(u.After) != 0 {
		opts = append(opts, UnitAfterOption(u.After...))
	}
	return opts
}

type ServiceSection struct {
	Type        ServiceType
	User        string
	ExecStart   string
	ExecReload  string
	KillMode    ServiceKillMode
	TimeoutSec  int
	CPUs        float64
	MemoryBytes uint64
	Restart     ServiceRestart
	Environment map[string]string
}

func (s ServiceSection) Options() []*unit.UnitOption {
	var opts []*unit.UnitOption
	if s.Type != "" {
		opts = append(opts, ServiceTypeOption(s.Type))
	}
	if s.User != "" {
		opts = append(opts, ServiceUserOption(s.User))
	}
	if s.ExecStart != "" {
		opts = append(opts, ServiceExecStartOption(s.ExecStart))
	}
	if s.ExecReload != "" {
		opts = append(opts, ServiceExecReloadOption(s.ExecReload))
	}
	if s.KillMode != "" {
		opts = append(opts, ServiceKillModeOption(s.KillMode))
	}
	if s.TimeoutSec != 0 {
		opts = append(opts, ServiceTimeoutSecOption(s.TimeoutSec))
	}
	if s.CPUs > 0 {
		opts = append(opts, ServiceCPUQuotaOption(s.CPUs))
	}
	if s.MemoryBytes != 0 {
		opts = append(opts, ServiceMemoryMaxOption(s.MemoryBytes))
	}
	if s.Restart != "" {
		opts = append(opts, ServiceRestartOption(s.Restart))
	}
	for _, name := range slices.Sorted(maps.Keys(s.Environment)) {
		opts = append(opts, ServiceEnvironmentOption(name, s.Environment[name]))
	}
	return opts
}

type InstallSection struct {
	WantedBy []string
}

func (s InstallSection) Options() []*unit.UnitOption {
	var opts []*unit.UnitOption
	if len(s.WantedBy) != 0 {
		opts = append(opts, InstallWantedByOption(s.WantedBy...))
	}
	return opts
}

const (
	sectionNameUnit    = "Unit"
	sectionNameService = "Service"
	sectionNameInstall = "Install"
)

func UnitAfterOption(values ...string) *unit.UnitOption {
	return &unit.UnitOption{
		Section: sectionNameUnit,
		Name:    "After",
		Value:   strings.Join(values, " "),
	}
}

func ServiceTypeOption(value ServiceType) *unit.UnitOption {
	return &unit.UnitOption{
		Section: sectionNameService,
		Name:    "Type",
		Value:   value.String(),
	}
}

func ServiceUserOption(value string) *unit.UnitOption {
	return &unit.UnitOption{
		Section: sectionNameService,
		Name:    "User",
		Value:   value,
	}
}

func ServiceExecStartOption(value string) *unit.UnitOption {
	return &unit.UnitOption{
		Section: sectionNameService,
		Name:    "ExecStart",
		Value:   value,
	}
}

func ServiceExecReloadOption(value string) *unit.UnitOption {
	return &unit.UnitOption{
		Section: sectionNameService,
		Name:    "ExecReload",
		Value:   value,
	}
}

func ServiceKillModeOption(value ServiceKillMode) *unit.UnitOption {
	return &unit.UnitOption{
		Section: sectionNameService,
		Name:    "KillMode",
		Value:   value.String(),
	}
}

func ServiceTimeoutSecOption(value int) *unit.UnitOption {
	return &unit.UnitOption{
		Section: sectionNameService,
		Name:    "TimeoutSec",
		Value:   strconv.Itoa(value),
	}
}

func ServiceRestartOption(value ServiceRestart) *unit.UnitOption {
	return &unit.UnitOption{
		Section: sectionNameService,
		Name:    "Restart",
		Value:   value.String(),
	}
}

func ServiceEnvironmentOption(name, value string) *unit.UnitOption {
	return &unit.UnitOption{
		Section: sectionNameService,
		Name:    "Environment",
		Value:   fmt.Sprintf("%q", name+"="+value),
	}
}

func ServiceCPUQuotaOption(cpus float64) *unit.UnitOption {
	return &unit.UnitOption{
		Section: sectionNameService,
		Name:    "CPUQuota",
		Value:   fmt.Sprintf("%.f%%", cpus*100),
	}
}

func ServiceMemoryMaxOption(memoryBytes uint64) *unit.UnitOption {
	return &unit.UnitOption{
		Section: sectionNameService,
		Name:    "MemoryMax",
		Value:   strconv.FormatUint(memoryBytes, 10),
	}
}

func InstallWantedByOption(value ...string) *unit.UnitOption {
	return &unit.UnitOption{
		Section: sectionNameInstall,
		Name:    "WantedBy",
		Value:   strings.Join(value, " "),
	}
}
