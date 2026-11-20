package pgbackrest_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/pgEdge/control-plane/server/internal/pgbackrest"
)

func TestWriteConfig(t *testing.T) {
	for _, tc := range []struct {
		name          string
		opts          pgbackrest.ConfigOptions
		expectedLines []string
	}{
		{
			name: "s3 auto",
			opts: pgbackrest.ConfigOptions{
				DatabaseID: "706fd161-8df5-4ddd-b25a-f85d8b7bb033",
				NodeName:   "n1",
				InstanceID: "706fd161-8df5-4ddd-b25a-f85d8b7bb033-n1-n5fe2mcy",
				PgDataPath: "/opt/pgedge/data",
				HostUser:   "pgedge-db",
				User:       "pgedge",
				Repositories: []*pgbackrest.Repository{
					{
						ID:       "0c28ad6e-233b-4ca7-ae60-4214e3a5356d",
						Type:     pgbackrest.RepositoryTypeS3,
						S3Bucket: "backups",
						S3Region: "us-east-1",
					},
				},
			},
			expectedLines: []string{
				"[global]",
				"lock-path                 = /tmp/pgbackrest/706fd161-8df5-4ddd-b25a-f85d8b7bb033-n1-n5fe2mcy",
				"log-level-console         = info",
				"repo1-cipher-type         = none",
				"repo1-path                = /databases/706fd161-8df5-4ddd-b25a-f85d8b7bb033/0c28ad6e-233b-4ca7-ae60-4214e3a5356d/n1",
				"repo1-retention-full      = 7",
				"repo1-retention-full-type = time",
				"repo1-s3-bucket           = backups",
				"repo1-s3-endpoint         = s3.us-east-1.amazonaws.com",
				"repo1-s3-key-type         = auto",
				"repo1-s3-region           = us-east-1",
				"repo1-type                = s3",
				"start-fast                = y",
				"",
				"[db]",
				"pg1-host-user = pgedge-db",
				"pg1-path      = /opt/pgedge/data",
				"pg1-user      = pgedge",
				"",
			},
		},
		{
			name: "s3 shared",
			opts: pgbackrest.ConfigOptions{
				DatabaseID: "706fd161-8df5-4ddd-b25a-f85d8b7bb033",
				NodeName:   "n1",
				InstanceID: "706fd161-8df5-4ddd-b25a-f85d8b7bb033-n1-n5fe2mcy",
				PgDataPath: "/opt/pgedge/data",
				HostUser:   "pgedge-db",
				User:       "pgedge",
				Repositories: []*pgbackrest.Repository{
					{
						ID:                "0c28ad6e-233b-4ca7-ae60-4214e3a5356d",
						Type:              pgbackrest.RepositoryTypeS3,
						S3Bucket:          "backups",
						S3Endpoint:        "minio.cluster.local",
						S3Key:             "minio-key",
						S3KeySecret:       "minio-secret",
						BasePath:          "/backups",
						RetentionFullType: pgbackrest.RetentionFullTypeCount,
						RetentionFull:     5,
					},
				},
			},
			expectedLines: []string{
				"[global]",
				"lock-path                 = /tmp/pgbackrest/706fd161-8df5-4ddd-b25a-f85d8b7bb033-n1-n5fe2mcy",
				"log-level-console         = info",
				"repo1-cipher-type         = none",
				"repo1-path                = /backups/databases/706fd161-8df5-4ddd-b25a-f85d8b7bb033/0c28ad6e-233b-4ca7-ae60-4214e3a5356d/n1",
				"repo1-retention-full      = 5",
				"repo1-retention-full-type = count",
				"repo1-s3-bucket           = backups",
				"repo1-s3-endpoint         = minio.cluster.local",
				"repo1-s3-key              = minio-key",
				"repo1-s3-key-secret       = minio-secret",
				"repo1-s3-key-type         = shared",
				"repo1-type                = s3",
				"start-fast                = y",
				"",
				"[db]",
				"pg1-host-user = pgedge-db",
				"pg1-path      = /opt/pgedge/data",
				"pg1-user      = pgedge",
				"",
			},
		},
		{
			name: "s3 custom",
			opts: pgbackrest.ConfigOptions{
				DatabaseID: "706fd161-8df5-4ddd-b25a-f85d8b7bb033",
				NodeName:   "n1",
				InstanceID: "706fd161-8df5-4ddd-b25a-f85d8b7bb033-n1-n5fe2mcy",
				PgDataPath: "/opt/pgedge/data",
				HostUser:   "pgedge-db",
				User:       "pgedge",
				Repositories: []*pgbackrest.Repository{
					{
						ID:       "0c28ad6e-233b-4ca7-ae60-4214e3a5356d",
						Type:     pgbackrest.RepositoryTypeS3,
						S3Bucket: "backups",
						S3Region: "us-east-1",
						CustomOptions: map[string]string{
							"cipher-type": "aes-256-cbc",
							// The repo9 prefix will be sanitized when we're computing the key.
							"repo9-cipher-pass": "zWaf6XtpjIVZC5444yXB+cgFDFl7MxGlgkZSaoPvTGirhPygu4jOKOXf9LO4vjfO",
						},
					},
				},
			},
			expectedLines: []string{
				"[global]",
				"lock-path                 = /tmp/pgbackrest/706fd161-8df5-4ddd-b25a-f85d8b7bb033-n1-n5fe2mcy",
				"log-level-console         = info",
				"repo1-cipher-pass         = zWaf6XtpjIVZC5444yXB+cgFDFl7MxGlgkZSaoPvTGirhPygu4jOKOXf9LO4vjfO",
				"repo1-cipher-type         = aes-256-cbc",
				"repo1-path                = /databases/706fd161-8df5-4ddd-b25a-f85d8b7bb033/0c28ad6e-233b-4ca7-ae60-4214e3a5356d/n1",
				"repo1-retention-full      = 7",
				"repo1-retention-full-type = time",
				"repo1-s3-bucket           = backups",
				"repo1-s3-endpoint         = s3.us-east-1.amazonaws.com",
				"repo1-s3-key-type         = auto",
				"repo1-s3-region           = us-east-1",
				"repo1-type                = s3",
				"start-fast                = y",
				"",
				"[db]",
				"pg1-host-user = pgedge-db",
				"pg1-path      = /opt/pgedge/data",
				"pg1-user      = pgedge",
				"",
			},
		},
		{
			name: "s3 multi",
			opts: pgbackrest.ConfigOptions{
				DatabaseID: "706fd161-8df5-4ddd-b25a-f85d8b7bb033",
				NodeName:   "n1",
				InstanceID: "706fd161-8df5-4ddd-b25a-f85d8b7bb033-n1-n5fe2mcy",
				PgDataPath: "/opt/pgedge/data",
				HostUser:   "pgedge-db",
				User:       "pgedge",
				Repositories: []*pgbackrest.Repository{
					{
						ID:       "time-retention",
						Type:     pgbackrest.RepositoryTypeS3,
						S3Bucket: "backups",
						S3Region: "us-east-1",
					},
					{
						ID:                "count-retention",
						Type:              pgbackrest.RepositoryTypeS3,
						S3Bucket:          "backups",
						S3Region:          "us-east-1",
						RetentionFullType: pgbackrest.RetentionFullTypeCount,
						RetentionFull:     30,
					},
				},
			},
			expectedLines: []string{
				"[global]",
				"lock-path                 = /tmp/pgbackrest/706fd161-8df5-4ddd-b25a-f85d8b7bb033-n1-n5fe2mcy",
				"log-level-console         = info",
				"repo1-cipher-type         = none",
				"repo1-path                = /databases/706fd161-8df5-4ddd-b25a-f85d8b7bb033/time-retention/n1",
				"repo1-retention-full      = 7",
				"repo1-retention-full-type = time",
				"repo1-s3-bucket           = backups",
				"repo1-s3-endpoint         = s3.us-east-1.amazonaws.com",
				"repo1-s3-key-type         = auto",
				"repo1-s3-region           = us-east-1",
				"repo1-type                = s3",
				"repo2-cipher-type         = none",
				"repo2-path                = /databases/706fd161-8df5-4ddd-b25a-f85d8b7bb033/count-retention/n1",
				"repo2-retention-full      = 30",
				"repo2-retention-full-type = count",
				"repo2-s3-bucket           = backups",
				"repo2-s3-endpoint         = s3.us-east-1.amazonaws.com",
				"repo2-s3-key-type         = auto",
				"repo2-s3-region           = us-east-1",
				"repo2-type                = s3",
				"start-fast                = y",
				"",
				"[db]",
				"pg1-host-user = pgedge-db",
				"pg1-path      = /opt/pgedge/data",
				"pg1-user      = pgedge",
				"",
			},
		},
		{
			name: "gcs auto",
			opts: pgbackrest.ConfigOptions{
				DatabaseID: "706fd161-8df5-4ddd-b25a-f85d8b7bb033",
				NodeName:   "n1",
				InstanceID: "706fd161-8df5-4ddd-b25a-f85d8b7bb033-n1-n5fe2mcy",
				PgDataPath: "/opt/pgedge/data",
				HostUser:   "pgedge-db",
				User:       "pgedge",
				Repositories: []*pgbackrest.Repository{
					{
						ID:        "0c28ad6e-233b-4ca7-ae60-4214e3a5356d",
						Type:      pgbackrest.RepositoryTypeGCS,
						GCSBucket: "backups",
					},
				},
			},
			expectedLines: []string{
				"[global]",
				"lock-path                 = /tmp/pgbackrest/706fd161-8df5-4ddd-b25a-f85d8b7bb033-n1-n5fe2mcy",
				"log-level-console         = info",
				"repo1-cipher-type         = none",
				"repo1-gcs-bucket          = backups",
				"repo1-gcs-endpoint        = storage.googleapis.com",
				"repo1-gcs-key-type        = auto",
				"repo1-path                = /databases/706fd161-8df5-4ddd-b25a-f85d8b7bb033/0c28ad6e-233b-4ca7-ae60-4214e3a5356d/n1",
				"repo1-retention-full      = 7",
				"repo1-retention-full-type = time",
				"repo1-type                = gcs",
				"start-fast                = y",
				"",
				"[db]",
				"pg1-host-user = pgedge-db",
				"pg1-path      = /opt/pgedge/data",
				"pg1-user      = pgedge",
				"",
			},
		},
		{
			name: "gcs service",
			opts: pgbackrest.ConfigOptions{
				DatabaseID: "706fd161-8df5-4ddd-b25a-f85d8b7bb033",
				NodeName:   "n1",
				InstanceID: "706fd161-8df5-4ddd-b25a-f85d8b7bb033-n1-n5fe2mcy",
				PgDataPath: "/opt/pgedge/data",
				HostUser:   "pgedge-db",
				User:       "pgedge",
				Repositories: []*pgbackrest.Repository{
					{
						ID:        "0c28ad6e-233b-4ca7-ae60-4214e3a5356d",
						Type:      pgbackrest.RepositoryTypeGCS,
						GCSBucket: "backups",
						GCSKey:    "gcs-key",
					},
				},
			},
			expectedLines: []string{
				"[global]",
				"lock-path                 = /tmp/pgbackrest/706fd161-8df5-4ddd-b25a-f85d8b7bb033-n1-n5fe2mcy",
				"log-level-console         = info",
				"repo1-cipher-type         = none",
				"repo1-gcs-bucket          = backups",
				"repo1-gcs-endpoint        = storage.googleapis.com",
				"repo1-gcs-key             = gcs-key",
				"repo1-gcs-key-type        = service",
				"repo1-path                = /databases/706fd161-8df5-4ddd-b25a-f85d8b7bb033/0c28ad6e-233b-4ca7-ae60-4214e3a5356d/n1",
				"repo1-retention-full      = 7",
				"repo1-retention-full-type = time",
				"repo1-type                = gcs",
				"start-fast                = y",
				"",
				"[db]",
				"pg1-host-user = pgedge-db",
				"pg1-path      = /opt/pgedge/data",
				"pg1-user      = pgedge",
				"",
			},
		},
		//{ Pending: support for "auto" hasn't been added to pgbackrest yet
		//	name: "azure auto",
		//	opts: pgbackrest.ConfigOptions{
		//		DatabaseID: "706fd161-8df5-4ddd-b25a-f85d8b7bb033",
		//		NodeName:   "n1",
		//		PgDataPath: "/opt/pgedge/data",
		//		HostUser:   "pgedge-db",
		//		User:       "pgedge",
		//		Repositories: []*pgbackrest.Repository{
		//			{
		//				ID:             "0c28ad6e-233b-4ca7-ae60-4214e3a5356d",
		//				Type:           pgbackrest.RepositoryTypeAzure,
		//				AzureAccount:   "backups-account",
		//				AzureContainer: "backups",
		//			},
		//		},
		//	},
		//	expectedLines: []string{
		//		"[global]",
		//		"log-level-console         = info",
		//		"repo1-azure-account       = backups-account",
		//		"repo1-azure-container     = backups",
		//		"repo1-azure-endpoint      = blob.core.windows.net",
		//		"repo1-azure-key-type      = auto",
		//		"repo1-azure-uri-style     = host",
		//		"repo1-cipher-type         = none",
		//		"repo1-path                = /databases/706fd161-8df5-4ddd-b25a-f85d8b7bb033/0c28ad6e-233b-4ca7-ae60-4214e3a5356d/n1",
		//		"repo1-retention-full      = 7",
		//		"repo1-retention-full-type = time",
		//		"repo1-type                = azure",
		//		"start-fast                = y",
		//		"",
		//		"[db]",
		//		"pg1-host-user = pgedge-db",
		//		"pg1-path      = /opt/pgedge/data",
		//		"pg1-user      = pgedge",
		//		"",
		//	},
		//},
		{
			name: "azure shared",
			opts: pgbackrest.ConfigOptions{
				DatabaseID: "706fd161-8df5-4ddd-b25a-f85d8b7bb033",
				NodeName:   "n1",
				InstanceID: "706fd161-8df5-4ddd-b25a-f85d8b7bb033-n1-n5fe2mcy",
				PgDataPath: "/opt/pgedge/data",
				HostUser:   "pgedge-db",
				User:       "pgedge",
				Repositories: []*pgbackrest.Repository{
					{
						ID:             "0c28ad6e-233b-4ca7-ae60-4214e3a5356d",
						Type:           pgbackrest.RepositoryTypeAzure,
						AzureAccount:   "backups-account",
						AzureContainer: "backups",
						AzureKey:       "azure-key",
					},
				},
			},
			expectedLines: []string{
				"[global]",
				"lock-path                 = /tmp/pgbackrest/706fd161-8df5-4ddd-b25a-f85d8b7bb033-n1-n5fe2mcy",
				"log-level-console         = info",
				"repo1-azure-account       = backups-account",
				"repo1-azure-container     = backups",
				"repo1-azure-endpoint      = blob.core.windows.net",
				"repo1-azure-key           = azure-key",
				"repo1-azure-key-type      = shared",
				"repo1-azure-uri-style     = host",
				"repo1-cipher-type         = none",
				"repo1-path                = /databases/706fd161-8df5-4ddd-b25a-f85d8b7bb033/0c28ad6e-233b-4ca7-ae60-4214e3a5356d/n1",
				"repo1-retention-full      = 7",
				"repo1-retention-full-type = time",
				"repo1-type                = azure",
				"start-fast                = y",
				"",
				"[db]",
				"pg1-host-user = pgedge-db",
				"pg1-path      = /opt/pgedge/data",
				"pg1-user      = pgedge",
				"",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b strings.Builder
			err := pgbackrest.WriteConfig(&b, tc.opts)
			assert.NoError(t, err)
			assert.Equal(t, strings.Join(tc.expectedLines, "\n"), b.String())
		})
	}
}

func TestIsSensitiveCustomOption(t *testing.T) {
	sensitive := []string{
		"cipher-pass", "repo1-cipher-pass", "REPO2-CIPHER-PASS",
		"s3-key", "s3-key-secret", "s3-token", "repo1-s3-key",
		"gcs-key", "azure-key", "sftp-private-key-passphrase",
		"s3-sse-customer-key", "repo1-s3-sse-customer-key",
	}
	for _, key := range sensitive {
		assert.True(t, pgbackrest.IsSensitiveCustomOption(key), key)
	}

	notSensitive := []string{
		"cipher-type", "repo1-cipher-type", "s3-key-type", "s3-uri-style",
		"gcs-key-type", "azure-key-type", "s3-kms-key-id", "storage-verify-tls",
	}
	for _, key := range notSensitive {
		assert.False(t, pgbackrest.IsSensitiveCustomOption(key), key)
	}
}

func TestRepositoryDefaultOptionalFieldsFrom(t *testing.T) {
	other := &pgbackrest.Repository{
		S3Key:       "key",
		S3KeySecret: "secret",
		CustomOptions: map[string]string{
			"cipher-type":   "aes-256-cbc",
			"cipher-pass":   "passphrase",
			"s3-token":      "token",
			"s3-uri-style":  "path",
			"s3-key-secret": "",
		},
	}

	t.Run("restores omitted sensitive options", func(t *testing.T) {
		r := &pgbackrest.Repository{
			CustomOptions: map[string]string{
				"cipher-type": "aes-256-cbc",
			},
		}
		r.DefaultOptionalFieldsFrom(other)
		assert.Equal(t, "key", r.S3Key)
		assert.Equal(t, "secret", r.S3KeySecret)
		assert.Equal(t, map[string]string{
			"cipher-type": "aes-256-cbc",
			"cipher-pass": "passphrase",
			"s3-token":    "token",
		}, r.CustomOptions)
	})

	t.Run("restores into nil options", func(t *testing.T) {
		r := &pgbackrest.Repository{}
		r.DefaultOptionalFieldsFrom(other)
		assert.Equal(t, map[string]string{
			"cipher-pass": "passphrase",
			"s3-token":    "token",
		}, r.CustomOptions)
	})

	t.Run("keeps submitted sensitive options", func(t *testing.T) {
		r := &pgbackrest.Repository{
			CustomOptions: map[string]string{
				"cipher-pass": "new-passphrase",
				"s3-token":    "",
			},
		}
		r.DefaultOptionalFieldsFrom(other)
		assert.Equal(t, map[string]string{
			"cipher-pass": "new-passphrase",
			"s3-token":    "",
		}, r.CustomOptions)
	})

	t.Run("restores omitted s3-sse-customer-key", func(t *testing.T) {
		other := &pgbackrest.Repository{
			CustomOptions: map[string]string{
				"s3-sse-customer-key": "customer-key",
				"s3-key-type":         "shared",
			},
		}
		r := &pgbackrest.Repository{
			CustomOptions: map[string]string{
				"s3-key-type": "shared",
			},
		}
		r.DefaultOptionalFieldsFrom(other)
		assert.Equal(t, map[string]string{
			"s3-sse-customer-key": "customer-key",
			"s3-key-type":         "shared",
		}, r.CustomOptions)
	})

	t.Run("treats prefixed and unprefixed keys as equivalent", func(t *testing.T) {
		other := &pgbackrest.Repository{
			CustomOptions: map[string]string{
				"repo1-cipher-pass":         "passphrase",
				"s3-token":                  "token",
				"repo1-s3-sse-customer-key": "customer-key",
			},
		}
		r := &pgbackrest.Repository{
			CustomOptions: map[string]string{
				"cipher-pass":    "new-passphrase",
				"repo1-s3-token": "",
			},
		}
		r.DefaultOptionalFieldsFrom(other)
		assert.Equal(t, map[string]string{
			"cipher-pass":               "new-passphrase",
			"repo1-s3-token":            "",
			"repo1-s3-sse-customer-key": "customer-key",
		}, r.CustomOptions)
	})
}
