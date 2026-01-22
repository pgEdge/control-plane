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

// NodeInfo holds public and private IP for a node
type NodeInfo struct {
	PublicIP  string `json:"public_ip"`
	PrivateIP string `json:"private_ip"`
}

// NodeConfig holds the configuration for cluster nodes.
// Supports both new format (objects with public_ip/private_ip) and legacy format (string array)
type NodeConfig struct {
	Nodes json.RawMessage `json:"nodes"`
}

// getNodeInfos returns node information from config file
// Returns array of NodeInfo with public and private IPs
func getNodeInfos() []NodeInfo {
	// Try config file
	configPath := filepath.Join(".", "nodes_config.json")
	if data, err := os.ReadFile(configPath); err == nil {
		var config NodeConfig
		if err := json.Unmarshal(data, &config); err == nil && len(config.Nodes) > 0 {
			// Try new format first (array of objects with public_ip/private_ip)
			var nodeInfos []NodeInfo
			if err := json.Unmarshal(config.Nodes, &nodeInfos); err == nil && len(nodeInfos) > 0 {
				return nodeInfos
			}

			// Fallback to legacy format (array of strings) - use same IP for public and private
			var legacyNodes []string
			if err := json.Unmarshal(config.Nodes, &legacyNodes); err == nil && len(legacyNodes) > 0 {
				nodeInfos := make([]NodeInfo, len(legacyNodes))
				for i, ip := range legacyNodes {
					nodeInfos[i] = NodeInfo{PublicIP: ip, PrivateIP: ip}
				}
				return nodeInfos
			}
		}
	}

	// Default to localhost
	return []NodeInfo{
		{PublicIP: "localhost", PrivateIP: "localhost"},
		{PublicIP: "localhost", PrivateIP: "localhost"},
		{PublicIP: "localhost", PrivateIP: "localhost"},
	}
}

// get host ips from environment variable or config file
// Returns public IPs for connecting to nodes (HTTP requests go to public IP)
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

	// Get from node infos (public IPs)
	nodeInfos := getNodeInfos()
	ips := make([]string, len(nodeInfos))
	for i, node := range nodeInfos {
		ips[i] = node.PublicIP
	}
	return ips
}

// getNodeUrl returns the URL for a specific node using public IP
func getNodeUrl(ip string) string {
	return fmt.Sprintf("http://%s:3000", ip)
}

// Endpoint: GET /v1/cluster/init and POST /v1/cluster/join
func TestClusterInitAndJoinAll(t *testing.T) {
	ctx := context.Background()

	// Get all available nodes with both public and private IPs
	nodeInfos := getNodeInfos()
	if len(nodeInfos) == 0 {
		t.Fatal("No nodes configured")
	}
	if len(nodeInfos) > 11 {
		t.Logf("Warning: More than 11 nodes configured, using first 11 only")
		nodeInfos = nodeInfos[:11]
	}

	t.Logf("Cluster setup with %d nodes:", len(nodeInfos))
	for i, node := range nodeInfos {
		t.Logf("  Node %d: public=%s, private=%s", i, node.PublicIP, node.PrivateIP)
	}
	t.Log("")

	// Step 1: Initialize the cluster on the first node
	// Connect via public IP, but CP is configured with private IP internally
	node0 := httpapitest.NewClient(t, getNodeUrl(nodeInfos[0].PublicIP))
	t.Logf("Step 1: Initializing cluster on node 0 (public: %s, private: %s)...",
		nodeInfos[0].PublicIP, nodeInfos[0].PrivateIP)

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
	t.Logf("  Server URL: %s (should contain private IP)", initResult.ServerURL)
	t.Log("")

	// Step 2: Join all remaining nodes to the cluster
	if len(nodeInfos) == 1 {
		t.Log("Only 1 node configured, skipping join operations")
		return
	}

	// Join request uses the server_url from init which contains the private IP
	joinReq := map[string]interface{}{
		"token":      initResult.Token,
		"server_url": initResult.ServerURL,
	}

	for i := 1; i < len(nodeInfos); i++ {
		node := nodeInfos[i]
		t.Logf("Step %d: Joining node %d (public: %s, private: %s) to cluster...",
			i+1, i, node.PublicIP, node.PrivateIP)

		// Connect to node via public IP
		nodeClient := httpapitest.NewClient(t, getNodeUrl(node.PublicIP))
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
	t.Logf("✓ All %d nodes successfully joined the cluster!", len(nodeInfos))
}
