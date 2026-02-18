package postgres

func GetPostgresVersion() Query[string] {
	return Query[string]{
		SQL: "SHOW server_version;",
	}
}

func GetSpockVersion() Query[string] {
	return Query[string]{
		SQL: "SELECT spock.spock_version();",
	}
}

func GetSpockReadOnly() Query[string] {
	return Query[string]{
		SQL: "SHOW spock.readonly;",
	}
}

// Spock subscription statuses returned by spock.sub_show_status().
// See: https://github.com/pgEdge/spock/blob/main/src/spock_functions.c
const (
	SubStatusInitializing = "initializing" // Worker running, sync in progress
	SubStatusReplicating  = "replicating"  // Worker running, sync ready
	SubStatusUnknown      = "unknown"      // Worker running, no sync status record
	SubStatusDisabled     = "disabled"     // Worker not running, subscription disabled
	SubStatusDown         = "down"         // Worker not running, subscription enabled
)

type SubscriptionStatus struct {
	SubscriptionName string   `json:"subscription_name"`
	Status           string   `json:"status"`
	ProviderNode     string   `json:"provider_node"`
	ProviderDSN      string   `json:"provider_dsn"`
	SlotName         string   `json:"slot_name"`
	ReplicationSets  []string `json:"replication_sets"`
	ForwardOrigins   []string `json:"forward_origins"`
}

func GetSubscriptionStatuses() Query[SubscriptionStatus] {
	return Query[SubscriptionStatus]{
		SQL: "SELECT to_json(spock.sub_show_status(sub_name)) from spock.subscription;",
	}
}
