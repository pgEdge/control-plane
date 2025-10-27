package etcd

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/afero"
	"go.etcd.io/etcd/api/v3/authpb"
	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/certificates"
	"github.com/pgEdge/control-plane/server/internal/config"
	"github.com/pgEdge/control-plane/server/internal/ds"
	"github.com/pgEdge/control-plane/server/internal/filesystem"
	"github.com/pgEdge/control-plane/server/internal/utils"
)

func CreateHostCredentials(
	ctx context.Context,
	client *clientv3.Client,
	certSvc *certificates.Service,
	opts HostCredentialOptions,
) (*HostCredentials, error) {
	username := hostUsername(opts.HostID)
	password, err := generatePassword()
	if err != nil {
		return nil, err
	}

	// Create a user for the peer host
	err = createUserIfNotExists(ctx, client, username, password, "root")
	if err != nil {
		return nil, fmt.Errorf("failed to create host user: %w", err)
	}

	// Create a cert for the peer user
	clientPrincipal, err := certSvc.HostEtcdUser(ctx, opts.HostID)
	if err != nil {
		return nil, fmt.Errorf("failed to create cert for etcd host user: %w", err)
	}

	creds := &HostCredentials{
		Username:   username,
		Password:   password,
		CaCert:     certSvc.CACert(),
		ClientCert: clientPrincipal.CertPEM,
		ClientKey:  clientPrincipal.KeyPEM,
	}

	if opts.EmbeddedEtcdEnabled {
		// Create a cert for the peer server
		serverPrincipal, err := certSvc.EtcdServer(ctx,
			opts.HostID,
			opts.Hostname,
			[]string{"localhost", opts.Hostname},
			[]string{"127.0.0.1", opts.IPv4Address},
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create cert for etcd server: %w", err)
		}

		creds.ServerCert = serverPrincipal.CertPEM
		creds.ServerKey = serverPrincipal.KeyPEM
	}

	return creds, nil
}

func RemoveHostCredentials(
	ctx context.Context,
	client *clientv3.Client,
	certSvc *certificates.Service,
	hostID string,
) error {
	err := removeUserIfExists(ctx, client, hostUsername(hostID))
	if err != nil {
		return fmt.Errorf("failed to remove host user: %w", err)
	}
	err = certSvc.RemoveHostEtcdUser(ctx, hostID)
	if err != nil {
		return fmt.Errorf("failed to remove host etcd user principal: %w", err)
	}
	err = certSvc.RemoveEtcdServer(ctx, hostID)
	if err != nil {
		return fmt.Errorf("failed to remove host etcd server principal: %w", err)
	}

	return nil
}

type InstanceUserOptions struct {
	InstanceID string
	KeyPrefix  string
	Password   string
}

type InstanceUserCredentials struct {
	Username   string
	Password   string
	CaCert     []byte
	ClientCert []byte
	ClientKey  []byte
}

func CreateInstanceEtcdUser(
	ctx context.Context,
	client *clientv3.Client,
	certSvc *certificates.Service,
	opts InstanceUserOptions,
) (*InstanceUserCredentials, error) {
	username := instanceUsername(opts.InstanceID)
	password := opts.Password
	if password == "" {
		pw, err := generatePassword()
		if err != nil {
			return nil, err
		}
		password = pw
	}

	if err := createRoleIfNotExists(ctx, client, username, opts.KeyPrefix); err != nil {
		return nil, fmt.Errorf("failed to create instance role: %w", err)
	}

	if err := createUserIfNotExists(ctx, client, username, password, username); err != nil {
		return nil, fmt.Errorf("failed to create instance user: %w", err)
	}

	// Create a cert for the instance user. This operation is idempotent.
	clientPrincipal, err := certSvc.InstanceEtcdUser(ctx, opts.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to create cert for etcd host user: %w", err)
	}

	return &InstanceUserCredentials{
		Username:   username,
		Password:   password,
		CaCert:     certSvc.CACert(),
		ClientCert: clientPrincipal.CertPEM,
		ClientKey:  clientPrincipal.KeyPEM,
	}, nil
}

func RemoveInstanceEtcdUser(
	ctx context.Context,
	client *clientv3.Client,
	certSvc *certificates.Service,
	instanceID string,
) error {
	username := instanceUsername(instanceID)

	if err := removeUserIfExists(ctx, client, username); err != nil {
		return fmt.Errorf("failed to remove instance user: %w", err)
	}
	if err := removeRoleIfExists(ctx, client, username); err != nil {
		return fmt.Errorf("failed to remove instance role: %w", err)
	}
	if err := certSvc.RemoveInstanceEtcdUser(ctx, instanceID); err != nil {
		return fmt.Errorf("failed to remove instance cert: %w", err)
	}

	return nil
}

func certificateService(ctx context.Context, cfg config.Config, client *clientv3.Client) (*certificates.Service, error) {
	store := certificates.NewStore(client, cfg.EtcdKeyRoot)
	svc := certificates.NewService(store)

	if err := svc.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start certificate service: %w", err)
	}

	return svc, nil
}

func instanceUsername(instanceID string) string {
	return fmt.Sprintf("instance-%s", instanceID)
}

func hostUsername(hostID string) string {
	return fmt.Sprintf("host-%s", hostID)
}

func generatePassword() (string, error) {
	password, err := utils.RandomString(32)
	if err != nil {
		return "", fmt.Errorf("failed to generate random password: %w", err)
	}

	return password, nil
}

func createRoleIfNotExists(
	ctx context.Context,
	client *clientv3.Client,
	roleName string,
	keyPrefix string,
) error {
	var perms []*authpb.Permission
	resp, err := client.RoleGet(ctx, roleName)
	switch {
	case errors.Is(err, rpctypes.ErrRoleNotFound):
		if _, err := client.RoleAdd(ctx, roleName); err != nil {
			return fmt.Errorf("failed to create role %q: %w", roleName, err)
		}
	case err != nil:
		return fmt.Errorf("failed to get role %q: %w", roleName, err)
	default:
		perms = resp.Perm
	}

	if keyPrefix == "" {
		return nil
	}

	return grantKeyPrefixToRole(ctx, client, roleName, keyPrefix, perms)
}

func grantKeyPrefixToRole(
	ctx context.Context,
	client *clientv3.Client,
	roleName string,
	keyPrefix string,
	currentPerms []*authpb.Permission,
) error {
	var hasPerm bool
	for _, perm := range currentPerms {
		if string(perm.Key) == keyPrefix {
			hasPerm = true
			break
		}
	}
	if hasPerm {
		return nil
	}
	rangeEnd := clientv3.GetPrefixRangeEnd(keyPrefix)
	permType := clientv3.PermissionType(clientv3.PermReadWrite)
	if _, err := client.RoleGrantPermission(ctx, roleName, keyPrefix, rangeEnd, permType); err != nil {
		return fmt.Errorf("failed to grant role permission: %w", err)
	}

	return nil
}

func createUserIfNotExists(
	ctx context.Context,
	client *clientv3.Client,
	username string,
	password string,
	wantRoles ...string,
) error {
	var haveRoles []string
	resp, err := client.UserGet(ctx, username)
	switch {
	case errors.Is(err, rpctypes.ErrUserNotFound):
		if _, err := client.UserAdd(ctx, username, password); err != nil {
			return fmt.Errorf("failed to create user: %w", err)
		}
	case err != nil:
		return fmt.Errorf("failed to get user %q: %w", username, err)
	default:
		// We're recovering from an error state if this user already exists, so
		// we'll update the password to the given one.
		if _, err := client.UserChangePassword(ctx, username, password); err != nil {
			return fmt.Errorf("failed to update user password: %w", err)
		}
		haveRoles = resp.Roles
	}

	for role := range ds.SetDifference(wantRoles, haveRoles) {
		if _, err := client.UserGrantRole(ctx, username, role); err != nil {
			return fmt.Errorf("failed to grant role to user: %w", err)
		}
	}

	return nil
}

func removeRoleIfExists(
	ctx context.Context,
	client *clientv3.Client,
	roleName string,
) error {
	_, err := client.RoleDelete(ctx, roleName)
	if err == nil || errors.Is(err, rpctypes.ErrRoleNotFound) {
		return nil
	}

	return fmt.Errorf("failed to delete role %q: %w", roleName, err)
}

func removeUserIfExists(
	ctx context.Context,
	client *clientv3.Client,
	username string,
) error {
	_, err := client.UserDelete(ctx, username)
	if err == nil || errors.Is(err, rpctypes.ErrUserNotFound) {
		return nil
	}

	return fmt.Errorf("failed to delete user %q: %w", username, err)
}

func writeHostCredentials(creds *HostCredentials, cfg *config.Manager) error {
	certs := &filesystem.Directory{
		Path: "certificates",
		Mode: 0o700,
		Children: []filesystem.TreeNode{
			&filesystem.File{
				Path:     "ca.crt",
				Mode:     0o644,
				Contents: creds.CaCert,
			},
			&filesystem.File{
				Path:     "etcd-user.crt",
				Mode:     0o644,
				Contents: creds.ClientCert,
			},
			&filesystem.File{
				Path:     "etcd-user.key",
				Mode:     0o600,
				Contents: creds.ClientKey,
			},
		},
	}
	if len(creds.ServerCert) > 0 {
		certs.Children = append(certs.Children,
			&filesystem.File{
				Path:     "etcd-server.crt",
				Mode:     0o644,
				Contents: creds.ServerCert,
			},
		)
	}
	if len(creds.ServerKey) > 0 {
		certs.Children = append(certs.Children,
			&filesystem.File{
				Path:     "etcd-server.key",
				Mode:     0o600,
				Contents: creds.ServerKey,
			},
		)
	}
	appCfg := cfg.Config()
	err := certs.Create(context.Background(), afero.NewOsFs(), appCfg.DataDir, 0)
	if err != nil {
		return fmt.Errorf("failed to write credentials: %w", err)
	}

	generatedCfg := cfg.GeneratedConfig()
	generatedCfg.EtcdUsername = creds.Username
	generatedCfg.EtcdPassword = creds.Password
	if err := cfg.UpdateGeneratedConfig(generatedCfg); err != nil {
		return fmt.Errorf("failed to update generated config: %w", err)
	}

	return nil
}
