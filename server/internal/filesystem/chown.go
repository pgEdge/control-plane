package filesystem

import (
	"fmt"
	"io/fs"

	"github.com/spf13/afero"
)

func ChownRecursive(fsys afero.Fs, path string, uid, gid int) error {
	fi, err := fsys.Stat(path)
	if err != nil {
		return err
	}
	if err := fsys.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("failed to change owner for %q: %w", path, err)
	}
	if !fi.IsDir() {
		return nil
	}
	err = afero.Walk(fsys, path, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("failed to walk %q: %w", path, err)
		}
		if err := fsys.Chown(path, uid, gid); err != nil {
			return fmt.Errorf("failed to change owner for %q: %w", path, err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to recursively chown %q: %w", path, err)
	}

	return nil
}
