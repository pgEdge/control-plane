package filesystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
)

type TreeNode interface {
	Create(ctx context.Context, fs afero.Fs, parent string, ownerUID int) error
}

type Directory struct {
	Path     string      `json:"path"`
	Mode     os.FileMode `json:"mode"`
	Children []TreeNode  `json:"children"`
}

func (d *Directory) Create(ctx context.Context, fs afero.Fs, parent string, ownerUID int) error {
	mode := d.Mode
	if mode == 0 {
		mode = 0o700
	}
	path := filepath.Join(parent, d.Path)
	if err := fs.MkdirAll(path, mode); err != nil {
		return fmt.Errorf("failed to create directory %q: %w", path, err)
	}
	for _, c := range d.Children {
		if err := c.Create(ctx, fs, path, ownerUID); err != nil {
			return err
		}
	}
	if ownerUID != 0 {
		// Default user GID is the same as UID. This simpler API supports our
		// current use cases.
		if err := ChownRecursive(fs, path, ownerUID, ownerUID); err != nil {
			return fmt.Errorf("failed to change ownership for directory %q: %w", path, err)
		}
	}
	return nil
}

type File struct {
	Path     string      `json:"path"`
	Mode     os.FileMode `json:"mode"`
	Contents []byte      `json:"contents"`
}

func (f *File) Create(ctx context.Context, fs afero.Fs, parent string, ownerUID int) error {
	mode := f.Mode
	if mode == 0 {
		mode = 0o644
	}
	path := filepath.Join(parent, f.Path)
	if err := afero.WriteFile(fs, path, f.Contents, mode); err != nil {
		return fmt.Errorf("failed to write file %q: %w", path, err)
	}
	if ownerUID != 0 {
		if err := ChownRecursive(fs, path, ownerUID, ownerUID); err != nil {
			return fmt.Errorf("failed to change ownership for file %q: %w", path, err)
		}
	}
	return nil
}

// func ReadDirectory(ctx context.Context, fs afero.Fs, path string) (*Directory, error) {
// 	fi, err := fs.Stat(path)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to stat file %q: %w", path, err)
// 	}
// 	if !fi.IsDir() {
// 		return nil, fmt.Errorf("path %q is a file, not a directory", path)
// 	}
// 	contents, err := afero.ReadDir(fs, path)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to read directory %q: %w", path, err)
// 	}
// 	var children []TreeNode
// 	for _, childInfo := range contents {
// 		childPath := filepath.Join(path, childInfo.Name())
// 		if childInfo.IsDir() {
// 			child, err := ReadDirectory(ctx, fs, childPath)
// 			if err != nil {
// 				return nil, fmt.Errorf("failed to read directory %q: %w", childPath, err)
// 			}
// 			children = append(children, child)
// 		} else {
// 			child, err := ReadFile(ctx, fs, childPath)
// 			if err != nil {
// 				return nil, fmt.Errorf("failed to read file %q: %w", childPath, err)
// 			}
// 			children = append(children, child)
// 		}
// 	}
// }

// func ReadFile(ctx context.Context, fs afero.Fs, path string) (*File, error) {
// 	fi, err := fs.Stat(path)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to stat file %q: %w", path, err)
// 	}
// 	if fi.IsDir() {
// 		return nil, fmt.Errorf("path %q is a directory, not a file", path)
// 	}
// 	contents, err := afero.ReadFile(fs, path)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to read file %q: %w", path, err)
// 	}
// 	return &File{
// 		Path:     filepath.Base(path),
// 		Mode:     fi.Mode(),
// 		Contents: contents,
// 	}, nil
// }
