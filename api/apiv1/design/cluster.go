package design

import (
	g "goa.design/goa/v3/dsl"
)

var ClusterStatus = g.Type("ClusterStatus", func() {
	g.Attribute("state", g.String, func() {
		g.Description("The current state of the cluster.")
		g.Enum("available", "error")
		g.Meta("struct:tag:json", "state")
	})

	g.Required("state")
})

var Cluster = g.Type("Cluster", func() {
	g.Attribute("id", Identifier, func() {
		g.Description("Unique identifier for the cluster.")
		g.Example("production")
		g.Meta("struct:tag:json", "id")
	})
	g.Attribute("status", ClusterStatus, func() {
		g.Description("Current status of the cluster.")
		g.Meta("struct:tag:json", "status")
	})
	g.Attribute("hosts", g.ArrayOf(Host), func() {
		g.Description("All of the hosts in the cluster.")
		g.Example(HostsArrayExample)
		g.Meta("struct:tag:json", "hosts")
	})

	g.Required("id", "status", "hosts")
})

var InitClusterRequest = g.Type("InitClusterRequest", func() {
	g.Description("Request to initialize a cluster")
	g.Attribute("cluster_id", Identifier, func() {
		g.Description("Optional id for the cluster, omit for default generated id")
		g.Meta("struct:tag:json", "cluster_id,omitempty")
	})
})

var ClusterJoinToken = g.Type("ClusterJoinToken", func() {
	g.Attribute("token", g.String, func() {
		g.Description("Token to join an existing cluster.")
		g.Example("PGEDGE-dd440afcf5de20ef8e8cf54f6cb9f125fd55f90e64faa94b906130b31235e730-41e975f41d7ea61058f2fe2572cb52dd")
		g.Meta("struct:tag:json", "token")
	})
	g.Attribute("server_url", g.String, func() {
		g.Format(g.FormatURI)
		g.Description("Existing server to join")
		g.Example("http://192.168.1.1:3000")
		g.Meta("struct:tag:json", "server_url")
	})

	g.Required("token", "server_url")
})

var ClusterJoinRequest = g.Type("ClusterJoinRequest", func() {
	g.Attribute("token", g.String, func() {
		g.Description("Token to join the cluster.")
		g.Pattern(`^PGEDGE-[\w]{64}-[\w]{32}$`)
		g.Example("PGEDGE-dd440afcf5de20ef8e8cf54f6cb9f125fd55f90e64faa94b906130b31235e730-41e975f41d7ea61058f2fe2572cb52dd")
		g.Meta("struct:tag:json", "token")
	})
	g.Attribute("host_id", Identifier, func() {
		g.Description("The unique identifier for the host that's joining the cluster.")
		g.Example("host-1")
		g.Meta("struct:tag:json", "host_id")
	})
	g.Attribute("hostname", g.String, func() {
		g.Description("The hostname of the host that's joining the cluster.")
		g.MinLength(3)
		g.MaxLength(128)
		g.Example("ip-10-1-0-113.ec2.internal")
		g.Meta("struct:tag:json", "hostname")
	})
	g.Attribute("ipv4_address", g.String, func() {
		g.Format(g.FormatIPv4)
		g.Description("The IPv4 address of the host that's joining the cluster.")
		g.Example("10.1.0.113")
		g.Meta("struct:tag:json", "ipv4_address")
	})
	g.Attribute("embedded_etcd_enabled", g.Boolean, func() {
		g.Description("True if the joining member is configured to run an embedded an etcd server.")
		g.Example(true)
		g.Meta("struct:tag:json", "embedded_etcd_enabled")
	})

	g.Required("embedded_etcd_enabled", "token", "host_id", "hostname", "ipv4_address")
})

var EtcdClusterMember = g.Type("EtcdClusterMember", func() {
	g.Attribute("name", g.String, func() {
		g.Description("The name of the Etcd cluster member.")
		g.Example("host-1")
		g.Meta("struct:tag:json", "name")
	})
	g.Attribute("peer_urls", g.ArrayOf(g.String), func() {
		g.Description("The Etcd peer endpoint for this cluster member.")
		g.Example([]string{"http://192.168.1.1:2380"})
		g.Meta("struct:tag:json", "peer_urls")
	})
	g.Attribute("client_urls", g.ArrayOf(g.String), func() {
		g.Description("The Etcd client endpoint for this cluster member.")
		g.Example([]string{"http://192.168.1.1:2379"})
		g.Meta("struct:tag:json", "client_urls")
	})

	g.Required("name", "peer_urls", "client_urls")
})

var ClusterCredentials = g.Type("ClusterCredentials", func() {
	g.Attribute("username", g.String, func() {
		g.Description("The Etcd username for the new host.")
		g.Example("host-2")
		g.Meta("struct:tag:json", "username")
	})
	g.Attribute("password", g.String, func() {
		g.Description("The Etcd password for the new host.")
		g.Example("a78v2x866zirk4o737gjdssfi")
		g.Meta("struct:tag:json", "password")
	})
	g.Attribute("ca_cert", g.String, func() {
		g.Description("The base64-encoded CA certificate for the cluster.")
		g.Example("ZGE4NDdkMzMtM2FiYi00YzE2LTkzOGQtNDRkODU2ZDFlZWZlCg==")
		g.Meta("struct:tag:json", "ca_cert")
	})
	g.Attribute("client_cert", g.String, func() {
		g.Description("The base64-encoded etcd client certificate for the new cluster member.")
		g.Example("NWM0MGMyZTAtYjAyYS00NzkxLTk0YjAtMjMyN2EyZGQ4ZDc3Cg==")
		g.Meta("struct:tag:json", "client_cert")
	})
	g.Attribute("client_key", g.String, func() {
		g.Description("The base64-encoded etcd client key for the new cluster member.")
		g.Example("Y2FlNjhmODQtYjE1Ni00YWYyLWFhMWEtM2FhNzI2MmVhYTM0Cg==")
		g.Meta("struct:tag:json", "client_key")
	})
	g.Attribute("server_cert", g.String, func() {
		g.Description("The base64-encoded etcd server certificate for the new cluster member.")
		g.Example("Nzc1OGQyY2UtZjdjOC00YmE4LTk2ZmQtOWE3MjVmYmY3NDdiCg==")
		g.Meta("struct:tag:json", "server_cert")
	})
	g.Attribute("server_key", g.String, func() {
		g.Description("The base64-encoded etcd server key for the new cluster member.")
		g.Example("NWRhNzY1ZGUtNzJkMi00OTU3LTk4ODUtOWRiZThjOGE5MGQ3Cg==")
		g.Meta("struct:tag:json", "server_key")
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
	g.Attribute("leader", EtcdClusterMember, func() {
		g.Description("Connection information for the etcd cluster leader")
		g.Meta("struct:tag:json", "leader")
	})
	g.Attribute("credentials", ClusterCredentials, func() {
		g.Description("Credentials for the new host joining the cluster.")
		g.Meta("struct:tag:json", "credentials")
	})

	g.Required("leader", "credentials")
})
