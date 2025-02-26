package general

import (
	"context"
	"errors"
	"fmt"

	"github.com/cschleiden/go-workflows/core"
	"github.com/cschleiden/go-workflows/workflow"
	"github.com/google/uuid"
	"github.com/pgEdge/control-plane/server/internal/etcd"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
)

type CreateEtcdInstanceUserInput struct {
	InstanceID uuid.UUID `json:"instance_id"`
	KeyPrefix  string    `json:"key_space"`
	CertDir    string    `json:"cert_dir"`
	// CaCertPath     string    `json:"ca_cert_path"`
	// ClientCertPath string    `json:"client_cert_path"`
	// ClientKeyPath  string    `json:"client_key_path"`
	Owner *Owner `json:"owner,omitempty"`
}

func (i *CreateEtcdInstanceUserInput) Validate() error {
	var errs []error
	if i.InstanceID == uuid.Nil {
		errs = append(errs, errors.New("username: cannot be empty"))
	}
	if i.KeyPrefix == "" {
		errs = append(errs, errors.New("key_space: cannot be empty"))
	}
	return errors.Join(errs...)
}

type CreateEtcdUserOutput struct {
	Username string `json:"username"`
}

func (a *Activities) ExecuteCreateEtcdUser(
	ctx workflow.Context,
	hostID uuid.UUID,
	input *CreateEtcdInstanceUserInput,
) workflow.Future[*CreateEtcdUserOutput] {
	options := workflow.ActivityOptions{
		Queue: core.Queue(hostID.String()),
	}
	return workflow.ExecuteActivity[*CreateEtcdUserOutput](ctx, options, a.CreateEtcdUser, input)
}

// type CreateEtcdUser func(ctx context.Context, input *CreateEtcdUserInput) error

func (a *Activities) CreateEtcdUser(ctx context.Context, input *CreateEtcdInstanceUserInput) (*CreateEtcdUserOutput, error) {
	if err := input.Validate(); err != nil {
		return nil, fmt.Errorf("invalid input: %w", err)
	}
	creds, err := a.Etcd.AddInstanceUser(ctx, etcd.InstanceUserOptions{
		InstanceID: input.InstanceID,
		KeyPrefix:  input.KeyPrefix,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to add instance user: %w", err)
	}

	var owner *filesystem.Owner
	if input.Owner != nil {
		owner = &filesystem.Owner{
			User:  input.Owner.User,
			Group: input.Owner.Group,
		}
	}

	certsDir := &filesystem.Directory{
		Path:  input.CertDir,
		Mode:  0o700,
		Owner: owner,
		Children: []filesystem.TreeNode{
			&filesystem.File{
				Path:     "ca.crt",
				Mode:     0o644,
				Contents: creds.CaCert,
			},
			&filesystem.File{
				Path:     "client.crt",
				Mode:     0o644,
				Contents: creds.ClientCert,
			},
			&filesystem.File{
				Path:     "client.key",
				Mode:     0o600,
				Contents: creds.ClientKey,
			},
		},
	}

	if err := certsDir.Create(ctx, a.Fs, a.Run, ""); err != nil {
		return nil, fmt.Errorf("failed to create etcd certs directory: %w", err)
	}

	// parents := ds.NewSet[string]()
	// parents.Add(filepath.Dir(input.CaCertPath))
	// parents.Add(filepath.Dir(input.ClientCertPath))
	// parents.Add(filepath.Dir(input.ClientKeyPath))
	// for _, p := range parents.ToSlice() {
	// 	if err := a.Fs.MkdirAll(p, 0o700); err != nil {
	// 		return nil, fmt.Errorf("failed to make directory: %w", err)
	// 	}
	// }
	// if err := afero.WriteFile(a.Fs, input.CaCertPath, creds.CaCert, 0o644); err != nil {
	// 	return nil, fmt.Errorf("failed to write CA cert: %w", err)
	// }
	// if err := afero.WriteFile(a.Fs, input.ClientCertPath, creds.ClientCert, 0o600); err != nil {
	// 	return nil, fmt.Errorf("failed to write client cert: %w", err)
	// }
	// if err := afero.WriteFile(a.Fs, input.ClientKeyPath, creds.ClientKey, 0o600); err != nil {
	// 	return nil, fmt.Errorf("failed to write client key: %w", err)
	// }

	// allPaths := append(parents.ToSlice(), input.CaCertPath, input.ClientCertPath, input.ClientKeyPath)
	// args := append([]string{"chown", input.Owner.String()}, allPaths...)

	// if _, err := a.Run(ctx, "sudo", args...); err != nil {
	// 	return fmt.Errorf("failed to change mount path ownership: %w", err)
	// }
	// role, err := client.RoleGet(ctx, input.Username)
	// if err == nil {
	// 	var hasPerm bool
	// 	for _, p := range role.Perm {
	// 		if string(p.Key) == input.KeySpace && p.PermType == clientv3.PermReadWrite {
	// 			hasPerm = true
	// 			break
	// 		}
	// 	}
	// 	if !hasPerm {
	// 		if _, err := client.RoleGrantPermission(ctx, input.Username, input.KeySpace, "", clientv3.PermissionType(clientv3.PermReadWrite)); err != nil {
	// 			return fmt.Errorf("failed to grant permission to existing role: %w", err)
	// 		}
	// 	}
	// } else if strings.Contains(err.Error(), "role not found") { // using strings because error comes from RPC
	// 	if _, err := client.RoleAdd(ctx, input.Username); err != nil {
	// 		return fmt.Errorf("failed to create role: %w", err)
	// 	}
	// 	if _, err := client.RoleGrantPermission(ctx, input.Username, input.KeySpace, "", clientv3.PermissionType(clientv3.PermReadWrite)); err != nil {
	// 		return fmt.Errorf("failed to grant permission to role: %w", err)
	// 	}
	// } else {
	// 	return fmt.Errorf("failed to check for existing role: %w", err)
	// }

	// user, err := client.UserGet(ctx, input.Username)
	// if err == nil {
	// 	var hasRole bool
	// 	for _, r := range user.Roles {
	// 		if r == input.Username {
	// 			hasRole = true
	// 			break
	// 		}
	// 	}
	// 	if !hasRole {
	// 		if _, err := client.UserGrantRole(ctx, input.Username, input.Username); err != nil {
	// 			return fmt.Errorf("failed to grant role to existing user: %w", err)
	// 		}
	// 	}
	// } else if strings.Contains(err.Error(), "user not found") {
	// 	if _, err := client.UserAdd(ctx, input.Username, ""); err != nil { // using cert auth instead of password
	// 		return fmt.Errorf("failed to create role: %w", err)
	// 	}
	// 	if _, err := client.UserGrantRole(ctx, input.Username, input.Username); err != nil {
	// 		return fmt.Errorf("failed to grant role to user: %w", err)
	// 	}
	// } else {
	// 	return fmt.Errorf("failed to check for existing user: %w", err)
	// }

	return &CreateEtcdUserOutput{
		Username: creds.Username,
	}, nil
}
