package exec

import (
	"context"
	"os/exec"
)

type CmdRunner func(ctx context.Context, name string, arg ...string) (string, error)

func RunCmd(ctx context.Context, name string, arg ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, arg...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}
