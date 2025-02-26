package filesystem

// type File struct {
// 	Path     string
// 	Contents []byte
// 	Owner    Owner
// 	Mode     os.FileMode
// }

// func WriteFiles(ctx context.Context, fs afero.Fs, run exec.CmdRunner, files ...File) error {
// 	for _, f := range files {
// 		parent := filepath.Dir(f.Path)
// 		if err := fs.MkdirAll(parent, 0o700); err != nil {
// 			return fmt.Errorf("failed to create parent directory %q: %w", parent, err)
// 		}
// 		if err := afero.WriteFile(fs, f.Path, f.Contents, f.Mode); err != nil {
// 			return fmt.Errorf("failed to write file %q: %w", f.Path, err)
// 		}
// 		if err := run(ctx, "")
// 	}
// 	return nil
// }
