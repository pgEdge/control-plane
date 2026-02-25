package patroni

import (
	"context"
	"fmt"
	"net/url"

	clientv3 "go.etcd.io/etcd/client/v3"
)

func EtcdHosts(ctx context.Context, client *clientv3.Client) ([]string, error) {
	members, err := client.MemberList(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list etcd cluster members: %w", err)
	}
	var hosts []string
	for _, member := range members.Members {
		for _, endpoint := range member.GetClientURLs() {
			u, err := url.Parse(endpoint)
			if err != nil {
				return nil, fmt.Errorf("failed to parse etcd client url '%s': %w", endpoint, err)
			}
			hosts = append(hosts, u.Host)
		}
	}

	return hosts, nil
}
