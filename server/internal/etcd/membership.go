package etcd

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"

	"go.etcd.io/etcd/api/v3/etcdserverpb"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/pgEdge/control-plane/server/internal/certificates"
)

var (
	ErrMinimumClusterSize = errors.New("cannot remove a member from a cluster with less than three nodes")
	ErrCannotRemoveSelf   = errors.New("cannot remove self from cluster")
	ErrInvalidJoinToken   = errors.New("invalid join token")
)

func RemoveHost(
	ctx context.Context,
	client *clientv3.Client,
	certSvc *certificates.Service,
	hostID string,
) error {
	if err := RemoveMember(ctx, client, hostID); err != nil {
		return err
	}

	return RemoveHostCredentials(ctx, client, certSvc, hostID)
}

func RemoveMember(ctx context.Context, client *clientv3.Client, hostID string) error {
	resp, err := client.MemberList(ctx)
	if err != nil {
		return fmt.Errorf("failed to list members: %w", err)
	}
	member := findMember(resp.Members, hostID)
	if member == nil {
		return nil
	}
	if len(resp.Members) < 3 {
		return ErrMinimumClusterSize
	}
	_, err = client.MemberRemove(ctx, member.ID)
	if err != nil {
		return fmt.Errorf("failed to remove member: %w", err)
	}

	return nil
}

func VerifyJoinToken(certSvc *certificates.Service, in string) error {
	token := certSvc.JoinToken()
	if subtle.ConstantTimeCompare([]byte(in), []byte(token)) != 1 {
		return ErrInvalidJoinToken
	}
	return nil
}

func GetClusterLeader(ctx context.Context, client *clientv3.Client) (*ClusterMember, error) {
	leader, err := getLeaderMember(ctx, client)
	if err != nil {
		return nil, err
	}

	return &ClusterMember{
		Name:       leader.Name,
		PeerURLs:   leader.PeerURLs,
		ClientURLs: leader.ClientURLs,
	}, nil
}

func findMember(members []*etcdserverpb.Member, memberName string) *etcdserverpb.Member {
	for _, m := range members {
		if m.Name == memberName {
			return m
		}
	}
	return nil
}

func getLeaderMember(ctx context.Context, client *clientv3.Client) (*etcdserverpb.Member, error) {
	endpoints := client.Endpoints()
	if len(endpoints) == 0 {
		return nil, errors.New("client has no endpoints")
	}

	status, err := client.Status(ctx, endpoints[0])
	if err != nil {
		return nil, fmt.Errorf("failed to get initial endpoint status: %w", err)
	}

	members, err := client.MemberList(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list cluster members: %w", err)
	}

	for _, member := range members.Members {
		if member.ID == status.Leader {
			return member, nil
		}
	}

	return nil, errors.New("cluster has no leader")
}

func UpdateMemberPeerURLs(ctx context.Context, client *clientv3.Client, memberName string, newPeerURLs []string) (bool, error) {
	resp, err := client.MemberList(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to list members: %w", err)
	}

	member := findMember(resp.Members, memberName)
	if member == nil {
		return false, fmt.Errorf("member %s not found", memberName)
	}

	if UrlsEqual(member.PeerURLs, newPeerURLs) {
		return false, nil // No change needed
	}

	_, err = client.MemberUpdate(ctx, member.ID, newPeerURLs)
	if err != nil {
		return false, fmt.Errorf("failed to update member peer URLs: %w", err)
	}

	return true, nil
}

func UrlsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aSet := make(map[string]struct{}, len(a))
	for _, url := range a {
		aSet[url] = struct{}{}
	}
	for _, url := range b {
		if _, ok := aSet[url]; !ok {
			return false
		}
	}
	return true
}
