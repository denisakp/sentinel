package azure

import (
	"fmt"
	"os"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"

	"github.com/denisakp/sentinel/internal/config"
)

// NewAzureClient creates an azblob.Client based on the authentication config.
func NewAzureClient(cfg config.AzureConfig) (*azblob.Client, error) {
	serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/", cfg.AccountName)

	switch cfg.Auth.Type {
	case "managed_identity":
		cred, err := azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			return nil, fmt.Errorf("azure: failed to create managed identity credential: %w", err)
		}
		client, err := azblob.NewClient(serviceURL, cred, nil)
		if err != nil {
			return nil, fmt.Errorf("azure: failed to create client with managed identity: %w", err)
		}
		return client, nil

	case "connection_string":
		connStr := cfg.Auth.ConnectionString
		if connStr == "" && cfg.Auth.ConnectionStringEnv != "" {
			connStr = os.Getenv(cfg.Auth.ConnectionStringEnv)
		}
		if connStr == "" {
			return nil, fmt.Errorf("azure: connection_string auth requires connection_string or connection_string_env")
		}
		client, err := azblob.NewClientFromConnectionString(connStr, nil)
		if err != nil {
			return nil, fmt.Errorf("azure: failed to create client from connection string: %w", err)
		}
		return client, nil

	case "sas_token":
		sasToken := cfg.Auth.SASToken
		if sasToken == "" {
			return nil, fmt.Errorf("azure: sas_token auth requires sas_token")
		}
		sasURL := serviceURL + "?" + sasToken
		client, err := azblob.NewClientWithNoCredential(sasURL, nil)
		if err != nil {
			return nil, fmt.Errorf("azure: failed to create client with SAS token: %w", err)
		}
		return client, nil

	default:
		return nil, fmt.Errorf("azure: unsupported auth type %q (must be managed_identity, connection_string, or sas_token)", cfg.Auth.Type)
	}
}
