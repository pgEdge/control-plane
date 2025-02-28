package filesystem

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"

	"github.com/spf13/afero"
)

type CmdRunner func(ctx context.Context, name string, arg ...string) (string, error)

func RunCmd(ctx context.Context, name string, arg ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, arg...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

type LoopDeviceManager interface {
	MakeLoopDevice(ctx context.Context, opts MakeLoopDeviceOptions) error
	RemoveLoopDevice(ctx context.Context, path string) error
}

type LoopDeviceManagerOptions struct {
	CmdRunner    CmdRunner
	FSTabManager FSTabManager
	FS           afero.Fs
}

func NewLoopDeviceManager(opts LoopDeviceManagerOptions) LoopDeviceManager {
	runner := opts.CmdRunner
	if runner == nil {
		runner = RunCmd
	}

	fs := opts.FS
	if fs == nil {
		fs = afero.NewOsFs()
	}

	return &loopDeviceManager{
		run:   runner,
		fstab: opts.FSTabManager,
		fs:    fs,
	}
}

type loopDeviceManager struct {
	run   CmdRunner
	fstab FSTabManager
	fs    afero.Fs
}

type Owner struct {
	User  string
	Group string
}

func (o Owner) String() string {
	return fmt.Sprintf("%s:%s", o.User, o.Group)
}

type MakeLoopDeviceOptions struct {
	SizeSpec  string
	MountPath string
	Owner     Owner
}

// Excerpt from truncate man page:
// The  SIZE  argument  is  an  integer  and optional unit (example: 10K is
// 10*1024).  Units are K,M,G,T,P,E,Z,Y (powers of 1024) or KB,MB,... (powers of
// 1000).  Binary prefixes can be used, too: KiB=K, MiB=M, and so on.
var sizeSpecPattern = regexp.MustCompile(`^\d+([KMGTPEZY](iB|B)?)?$`)

func (m MakeLoopDeviceOptions) Validate() error {
	var errs []error
	if !sizeSpecPattern.MatchString(m.SizeSpec) {
		errs = append(errs, fmt.Errorf("invalid size spec: %q", m.SizeSpec))
	}
	if m.MountPath == "" {
		errs = append(errs, errors.New("mount path cannot be empty"))
	}
	return errors.Join(errs...)
}

func (l *loopDeviceManager) MakeLoopDevice(ctx context.Context, opts MakeLoopDeviceOptions) error {
	if err := opts.Validate(); err != nil {
		return err
	}

	// Make the mount directory and parent directories if they don't exist
	if err := l.fs.MkdirAll(opts.MountPath, 0750); err != nil {
		return fmt.Errorf("failed to create mount path %q: %w", opts.MountPath, err)
	}

	// Initialize the .fs file adjacent to the mount path.
	fsFile := fmt.Sprintf("%s.fs", opts.MountPath)
	exists, err := afero.Exists(l.fs, fsFile)
	if err != nil {
		return fmt.Errorf("failed to check existence of %q: %w", fsFile, err)
	}
	if !exists {
		if _, err := l.run(ctx, "truncate", "-s", opts.SizeSpec, fsFile); err != nil {
			return fmt.Errorf("failed to truncate %q: %w", fsFile, err)
		}
		if _, err := l.run(ctx, "mkfs.ext4", "-m", "0", fsFile); err != nil {
			return fmt.Errorf("failed to format %q: %w", fsFile, err)
		}
	}

	// Add an fstab entry for the new device
	if err := l.fstab.Add(&FSTabEntry{
		Spec:     fsFile,
		File:     opts.MountPath,
		VFSType:  "ext4",
		MountOps: []string{MountOptLoop, MountOptNoFail},
		Freq:     0,
		PassNo:   0,
	}); err != nil {
		return fmt.Errorf("failed to add device to fstab: %w", err)
	}

	// Mount the device. This will use the settings defined in the fstab and
	// automatically create the loop device.
	if _, err := l.run(ctx, "sudo", "mount", opts.MountPath); err != nil {
		return fmt.Errorf("failed to mount device: %w", err)
	}

	// Change ownership of mount path. This is safe to run every time because Owner.String() returns
	// ":" if neither group nor user are specified.
	if _, err := l.run(ctx, "sudo", "chown", "-R", opts.Owner.String(), opts.MountPath); err != nil {
		return fmt.Errorf("failed to change mount path ownership: %w", err)
	}

	return nil
}

func (l *loopDeviceManager) RemoveLoopDevice(ctx context.Context, mountPath string) error {
	// Unmount the filesystem if present
	exists, err := afero.Exists(l.fs, mountPath)
	if err != nil {
		return fmt.Errorf("failed to determine existence of mount path %q: %w", mountPath, err)
	}
	if exists {
		// Unmounting also removes the loop device
		if _, err := l.run(ctx, "sudo", "umount", mountPath); err != nil {
			return fmt.Errorf("failed to unmount %q: %w", mountPath, err)
		}
		if err := l.fs.RemoveAll(mountPath); err != nil {
			return fmt.Errorf("failed to remove mount %q: %w", mountPath, err)
		}
	}

	// Remove fstab entry if present
	fsFile := fmt.Sprintf("%s.fs", mountPath)
	err = l.fstab.Filter(func(fe FSTabEntry) bool {
		return fe.Spec != fsFile && fe.File != mountPath
	})
	if err != nil {
		return fmt.Errorf("failed to remove fstab entry for %q: %w", mountPath, err)
	}

	// Remove the filesystem file if present
	exists, err = afero.Exists(l.fs, fsFile)
	if err != nil {
		return fmt.Errorf("failed to determine existence of filesystem file %q: %w", fsFile, err)
	}
	if exists {
		if err := l.fs.Remove(fsFile); err != nil {
			return fmt.Errorf("failed to remove filesystem file %q: %w", fsFile, err)
		}
	}
	return nil
}
