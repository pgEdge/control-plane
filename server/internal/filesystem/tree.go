package filesystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pgEdge/control-plane/server/internal/exec"
	"github.com/spf13/afero"
)

type TreeNode interface {
	Create(ctx context.Context, fs afero.Fs, run exec.CmdRunner, parent string) error
}

type Directory struct {
	Path     string
	Mode     os.FileMode
	Owner    *Owner
	Children []TreeNode
}

func (d *Directory) Create(ctx context.Context, fs afero.Fs, run exec.CmdRunner, parent string) error {
	mode := d.Mode
	if mode == 0 {
		mode = 0o700
	}
	path := filepath.Join(parent, d.Path)
	if err := fs.MkdirAll(path, mode); err != nil {
		return fmt.Errorf("failed to create directory %q: %w", path, err)
	}
	for _, c := range d.Children {
		if err := c.Create(ctx, fs, run, path); err != nil {
			return err
		}
	}
	if d.Owner != nil {
		if _, err := run(ctx, "sudo", "chown", "-R", d.Owner.String(), path); err != nil {
			return fmt.Errorf("failed to change owner for directory %q: %w", path, err)
		}
	}
	return nil
}

type File struct {
	Path     string
	Mode     os.FileMode
	Owner    *Owner
	Contents []byte
}

func (f *File) Create(ctx context.Context, fs afero.Fs, run exec.CmdRunner, parent string) error {
	mode := f.Mode
	if mode == 0 {
		mode = 0o644
	}
	path := filepath.Join(parent, f.Path)
	if err := afero.WriteFile(fs, path, f.Contents, mode); err != nil {
		return fmt.Errorf("failed to write file %q: %w", path, err)
	}
	if f.Owner != nil {
		if _, err := run(ctx, "sudo", "chown", f.Owner.String(), path); err != nil {
			return fmt.Errorf("failed to change owner for file %q: %w", path, err)
		}
	}
	return nil
}
