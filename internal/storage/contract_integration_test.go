//go:build integration

package storage_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/denisakp/sentinel/internal/config"
	storage "github.com/denisakp/sentinel/internal/storage"
	"github.com/denisakp/sentinel/internal/storage/azure"
	"github.com/denisakp/sentinel/internal/storage/gcs"
	"github.com/denisakp/sentinel/internal/storage/sentinel_s3"
	"github.com/denisakp/sentinel/internal/storage/storagetesting"
)

// TestStorageContract_Integration runs the same contract case table against
// MinIO, fake-gcs-server, and Azurite. Wiring only — adding a backend never
// requires modifying contract_test.go (FR-012 / SC-004).
func TestStorageContract_Integration(t *testing.T) {
	runContractSuite(t, "s3_minio", setupMinIO(t))
	runContractSuite(t, "gcs_fake", setupFakeGCS(t))
	runContractSuite(t, "azure", setupAzurite(t))
}

// ────────────────────────────────────────────────────────────────────────────
// MinIO (S3)
// ────────────────────────────────────────────────────────────────────────────

func setupMinIO(t *testing.T) storage.StorageBackend {
	t.Helper()
	storagetesting.DockerAvailable(t)
	ctx := context.Background()

	const accessKey, secretKey = "minioadmin", "minioadmin"
	req := testcontainers.ContainerRequest{
		Image:        storagetesting.MinIOImage,
		ExposedPorts: []string{"9000/tcp"},
		Env: map[string]string{
			"MINIO_ROOT_USER":     accessKey,
			"MINIO_ROOT_PASSWORD": secretKey,
		},
		Cmd:        []string{"server", "/data"},
		WaitingFor: wait.ForHTTP("/minio/health/live").WithPort("9000/tcp").WithStartupTimeout(60 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("MinIO start: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("MinIO host: %v", err)
	}
	port, err := container.MappedPort(ctx, "9000")
	if err != nil {
		t.Fatalf("MinIO port: %v", err)
	}
	endpoint := fmt.Sprintf("http://%s:%s", host, port.Port())
	bucket := fmt.Sprintf("sentinel-contract-%d", time.Now().UnixNano())

	client, err := sentinel_s3.NewS3Storage(&sentinel_s3.AmazonS3Storage{
		Bucket:    bucket,
		Region:    "us-east-1",
		EndPoint:  endpoint,
		AccessKey: accessKey,
		SecretKey: secretKey,
	})
	if err != nil {
		t.Fatalf("NewS3Storage: %v", err)
	}
	if _, err := client.Client.CreateBucket(ctx, &awss3.CreateBucketInput{Bucket: aws.String(bucket)}); err != nil {
		t.Fatalf("CreateBucket: %v", err)
	}
	return sentinel_s3.NewS3Backend(client)
}

// ────────────────────────────────────────────────────────────────────────────
// fake-gcs-server (GCS)
// ────────────────────────────────────────────────────────────────────────────

func setupFakeGCS(t *testing.T) storage.StorageBackend {
	t.Helper()
	storagetesting.DockerAvailable(t)
	ctx := context.Background()

	bucket := fmt.Sprintf("sentinel-contract-%d", time.Now().UnixNano())
	req := testcontainers.ContainerRequest{
		Image:        storagetesting.FakeGCSImage,
		ExposedPorts: []string{"4443/tcp"},
		Cmd:          []string{"-scheme", "http", "-public-host", "127.0.0.1"},
		WaitingFor:   wait.ForLog("server started").WithStartupTimeout(60 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("fake-gcs start: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("fake-gcs host: %v", err)
	}
	port, err := container.MappedPort(ctx, "4443")
	if err != nil {
		t.Fatalf("fake-gcs port: %v", err)
	}
	// STORAGE_EMULATOR_HOST is the documented env var for the Google
	// Cloud Storage Go SDK to redirect API calls at an emulator.
	t.Setenv("STORAGE_EMULATOR_HOST", fmt.Sprintf("%s:%s", host, port.Port()))

	backend, err := gcs.NewGCSBackend(gcs.Config{Bucket: bucket})
	if err != nil {
		t.Fatalf("NewGCSBackend: %v", err)
	}
	return backend
}

// ────────────────────────────────────────────────────────────────────────────
// Azurite (Azure Blob)
// ────────────────────────────────────────────────────────────────────────────

// azuriteAccountKey is the well-known devstoreaccount1 key shipped with the
// Azurite image — public, not a secret.
const azuriteAccountKey = "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw=="

func setupAzurite(t *testing.T) storage.StorageBackend {
	t.Helper()
	storagetesting.DockerAvailable(t)
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        storagetesting.AzuriteImage,
		ExposedPorts: []string{"10000/tcp"},
		Cmd:          []string{"azurite-blob", "--blobHost", "0.0.0.0", "--loose"},
		WaitingFor:   wait.ForLog("Azurite Blob service is successfully listening").WithStartupTimeout(60 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("Azurite start: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("Azurite host: %v", err)
	}
	port, err := container.MappedPort(ctx, "10000")
	if err != nil {
		t.Fatalf("Azurite port: %v", err)
	}
	connStr := fmt.Sprintf(
		"DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;"+
			"AccountKey=%s;BlobEndpoint=http://%s:%s/devstoreaccount1;",
		azuriteAccountKey, host, port.Port(),
	)
	containerName := fmt.Sprintf("sentinel-contract-%d", time.Now().UnixNano())

	// Pre-create the blob container; Azure SDK does not create it on first
	// Upload.
	rawClient, err := azblob.NewClientFromConnectionString(connStr, nil)
	if err != nil {
		t.Fatalf("azblob client: %v", err)
	}
	if _, err := rawClient.CreateContainer(ctx, containerName, nil); err != nil {
		t.Fatalf("CreateContainer: %v", err)
	}

	cfg := config.AzureConfig{
		AccountName: "devstoreaccount1",
		Container:   containerName,
		Tier:        "Hot",
		Auth: config.AzureAuthConfig{
			Type:             "connection_string",
			ConnectionString: connStr,
		},
	}
	backend, err := azure.NewAzureBlobBackend(cfg)
	if err != nil {
		t.Fatalf("NewAzureBlobBackend: %v", err)
	}
	return backend
}
