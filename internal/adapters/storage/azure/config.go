package azure

// Config holds Azure Blob Storage settings used by this package, mirroring
// internal/config.AzureConfig but kept here so the azure package has no
// dependency on internal/config (which would induce an import cycle when
// internal/storage/factory wires this backend).
type Config struct {
	AccountName string
	Container   string
	Tier        string
	Auth        AuthConfig
}

// AuthConfig specifies authentication for Azure Blob Storage.
type AuthConfig struct {
	Type                string
	ConnectionString    string
	ConnectionStringEnv string
	SASToken            string
	SASTokenEnv         string
}
