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
