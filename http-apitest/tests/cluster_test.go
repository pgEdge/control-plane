//go:build http_apitest

package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	httpapitest "github.com/pgEdge/control-plane/http-apitest"
)

// NodeConfig holds the configuration for cluster nodes.
type NodeConfig struct {
	Nodes []string `json:"nodes"`
}

// get host ips from environment variable or config file
func getHostIps() []string {
	// Try environment variable first
	if nodeIPs := os.Getenv("CP_NODE_IPS"); nodeIPs != "" {
		hostIps := strings.Split(nodeIPs, ",")
		// Trim whitespace from each URL
		for i := range hostIps {
			hostIps[i] = strings.TrimSpace(hostIps[i])
		}
		return hostIps
	}

	// Try config file
	configPath := filepath.Join(".", "nodes_config.json")
	if data, err := os.ReadFile(configPath); err == nil {
		var config NodeConfig
		if err := json.Unmarshal(data, &config); err == nil && len(config.Nodes) > 0 {
			return config.Nodes
		}
	}

	// Default to localhost URLs, need to tackle for random number of hosts
	return []string{
		"localhost",
		"localhost",
		"localhost",
	}
}

// getNodeUrl returns the URL for a specific node
func getNodeUrl(ip string) string {
	return fmt.Sprintf("http://%s:3000", ip)
}

// Endpoint: GET /v1/cluster/init and POST /v1/cluster/join
func TestClusterInitAndJoinAll(t *testing.T) {
	ctx := context.Background()

	// Get all available node URLs
	hostIps := getHostIps()
	if len(hostIps) == 0 {
		t.Fatal("No node URLs configured")
	}
	if len(hostIps) > 11 {
		t.Logf("Warning: More than 11 nodes configured, using first 11 only")
		hostIps = hostIps[:11]
	}

	t.Logf("Cluster setup with %d nodes:", len(hostIps))
	for i, url := range hostIps {
		t.Logf("  Node %d: %s", i, url)
	}
	t.Log("")

	// Step 1: Initialize the cluster on the first node
	node0 := httpapitest.NewClient(t, getNodeUrl(hostIps[0]))
	t.Logf("Step 1: Initializing cluster on node 0 (%s)...", hostIps[0])

	initResp := node0.GET(ctx, "/v1/cluster/init").AssertSuccess()

	var initResult struct {
		Token     string `json:"token"`
		ServerURL string `json:"server_url"`
	}
	initResp.UnmarshalJSON(&initResult)

	if initResult.Token == "" {
		t.Fatal("Expected token in init response")
	}

	t.Logf("✓ Cluster initialized successfully")
	t.Logf("  Token: %s", initResult.Token)
	t.Logf("  Server URL: %s", initResult.ServerURL)
	t.Log("")

	// Step 2: Join all remaining nodes to the cluster
	if len(hostIps) == 1 {
		t.Log("Only 1 node configured, skipping join operations")
		return
	}

	joinReq := map[string]interface{}{
		"token":      initResult.Token,
		"server_url": initResult.ServerURL,
	}

	for i := 1; i < len(hostIps); i++ {
		hostIp := hostIps[i]
		t.Logf("Step %d: Joining node %d (%s) to cluster...", i+1, i, hostIp)

		nodeClient := httpapitest.NewClient(t, getNodeUrl(hostIp))
		joinResp := nodeClient.POST(ctx, "/v1/cluster/join", joinReq).AssertSuccess()

		// Only parse response body if it's not empty (204 No Content has no body)
		if len(joinResp.Body) > 0 {
			var joinResult struct {
				Message string `json:"message,omitempty"`
				Status  string `json:"status,omitempty"`
			}
			joinResp.UnmarshalJSON(&joinResult)

			t.Logf("✓ Node %d joined cluster successfully", i)
			if joinResult.Message != "" {
				t.Logf("  Message: %s", joinResult.Message)
			}
			if joinResult.Status != "" {
				t.Logf("  Status: %s", joinResult.Status)
			}
		} else {
			t.Logf("✓ Node %d joined cluster successfully", i)
		}
		t.Log("")
	}
	t.Logf("✓ All %d nodes successfully joined the cluster!", len(hostIps))
}
