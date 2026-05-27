package storagetesting

// Pinned emulator image versions for the integration-tagged contract suite.
// Bump these together with the matching make e2e references to keep
// developer and CI runs deterministic.
const (
	MinIOImage   = "minio/minio:RELEASE.2024-12-18T13-15-44Z"
	FakeGCSImage = "fsouza/fake-gcs-server:1.50"
	AzuriteImage = "mcr.microsoft.com/azure-storage/azurite:3.33.0"
)
