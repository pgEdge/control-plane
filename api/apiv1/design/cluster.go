package design

import (
	g "goa.design/goa/v3/dsl"
)

var ClusterStatus = g.Type("ClusterStatus", func() {
	g.Attribute("state", g.String, func() {
		g.Description("The current state of the cluster.")
		g.Enum("available", "error")
	})

	g.Required("state")
})

var Cluster = g.Type("Cluster", func() {
	g.Attribute("id", Identifier, func() {
		g.Description("Unique identifier for the cluster.")
		g.Example("production")
	})
	g.Attribute("tenant_id", Identifier, func() {
		g.Description("Unique identifier for the cluster's owner.")
		g.Example("engineering")
	})
	g.Attribute("status", ClusterStatus, func() {
		g.Description("Current status of the cluster.")
	})
	g.Attribute("hosts", g.ArrayOf(Host), func() {
		g.Description("All of the hosts in the cluster.")
		g.Example(HostsArrayExample)
	})

	g.Required("id", "tenant_id", "status", "hosts")
})

var InitClusterRequest = g.Type("InitClusterRequest", func() {
	g.Description("Request to initialize a cluster")
	g.Attribute("cluster_id", Identifier, func() {
		g.Description("Optional id for the cluster, omit for default generated id")
	})
})

var ClusterJoinToken = g.Type("ClusterJoinToken", func() {
	g.Attribute("token", g.String, func() {
		g.Description("Token to join an existing cluster.")
		g.Example("PGEDGE-dd440afcf5de20ef8e8cf54f6cb9f125fd55f90e64faa94b906130b31235e730-41e975f41d7ea61058f2fe2572cb52dd")
	})
	g.Attribute("server_url", g.String, func() {
		g.Format(g.FormatURI)
		g.Description("Existing server to join")
		g.Example("http://192.168.1.1:3000")
	})

	g.Required("token", "server_url")
})

var ClusterJoinRequest = g.Type("ClusterJoinRequest", func() {
	g.Attribute("token", g.String, func() {
		g.Description("Token to join the cluster.")
		g.Pattern(`^PGEDGE-[\w]{64}-[\w]{32}$`)
		g.Example("PGEDGE-dd440afcf5de20ef8e8cf54f6cb9f125fd55f90e64faa94b906130b31235e730-41e975f41d7ea61058f2fe2572cb52dd")
	})
	g.Attribute("host_id", Identifier, func() {
		g.Description("The unique identifier for the host that's joining the cluster.")
		g.Example("host-1")
	})
	g.Attribute("hostname", g.String, func() {
		g.Description("The hostname of the host that's joining the cluster.")
		g.MinLength(3)
		g.MaxLength(128)
		g.Example("ip-10-1-0-113.ec2.internal")
	})
	g.Attribute("ipv4_address", g.String, func() {
		g.Format(g.FormatIPv4)
		g.Description("The IPv4 address of the host that's joining the cluster.")
		g.Example("10.1.0.113")
	})

	g.Required("token", "host_id", "hostname", "ipv4_address")
})

var ClusterPeer = g.Type("ClusterPeer", func() {
	g.Attribute("name", g.String, func() {
		g.Description("The name of the Etcd cluster member.")
		g.Example("host-1")
	})
	g.Attribute("peer_url", g.String, func() {
		g.Format(g.FormatURI)
		g.Description("The Etcd peer endpoint for this cluster member.")
		g.Example("http://192.168.1.1:2380")
	})
	g.Attribute("client_url", g.String, func() {
		g.Format(g.FormatURI)
		g.Description("The Etcd client endpoint for this cluster member.")
		g.Example("http://192.168.1.1:2379")
	})

	g.Required("name", "peer_url", "client_url")
})

var ClusterCredentials = g.Type("ClusterCredentials", func() {
	g.Attribute("username", g.String, func() {
		g.Description("The Etcd username for the new host.")
		g.Example("host-2")
	})
	g.Attribute("password", g.String, func() {
		g.Description("The Etcd password for the new host.")
		g.Example("a78v2x866zirk4o737gjdssfi")
	})
	g.Attribute("ca_cert", g.String, func() {
		g.Description("The base64-encoded CA certificate for the cluster.")
		g.Example("ZGE4NDdkMzMtM2FiYi00YzE2LTkzOGQtNDRkODU2ZDFlZWZlCg==")
	})
	g.Attribute("client_cert", g.String, func() {
		g.Description("The base64-encoded etcd client certificate for the new cluster member.")
		g.Example("NWM0MGMyZTAtYjAyYS00NzkxLTk0YjAtMjMyN2EyZGQ4ZDc3Cg==")
	})
	g.Attribute("client_key", g.String, func() {
		g.Description("The base64-encoded etcd client key for the new cluster member.")
		g.Example("Y2FlNjhmODQtYjE1Ni00YWYyLWFhMWEtM2FhNzI2MmVhYTM0Cg==")
	})
	g.Attribute("server_cert", g.String, func() {
		g.Description("The base64-encoded etcd server certificate for the new cluster member.")
		g.Example("Nzc1OGQyY2UtZjdjOC00YmE4LTk2ZmQtOWE3MjVmYmY3NDdiCg==")
	})
	g.Attribute("server_key", g.String, func() {
		g.Description("The base64-encoded etcd server key for the new cluster member.")
		g.Example("NWRhNzY1ZGUtNzJkMi00OTU3LTk4ODUtOWRiZThjOGE5MGQ3Cg==")
	})

	g.Required(
		"username",
		"password",
		"ca_cert",
		"client_cert",
		"client_key",
		"server_cert",
		"server_key",
	)
})

var ClusterJoinOptions = g.Type("ClusterJoinOptions", func() {
	g.Attribute("peer", ClusterPeer, func() {
		g.Description("Information about this cluster member")
	})
	g.Attribute("credentials", ClusterCredentials, func() {
		g.Description("Credentials for the new host joining the cluster.")
	})

	g.Required("peer", "credentials")
})
