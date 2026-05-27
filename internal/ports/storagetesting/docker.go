package storagetesting

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
)

// DockerAvailable skips the calling test with an actionable message when no
// Docker daemon is reachable. Integration-tagged contract cases call this
// before attempting to start an emulator container.
func DockerAvailable(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		t.Skipf("docker unavailable: %v (start Docker Desktop / dockerd to run integration tier)", err)
		return
	}
	defer provider.Close()
	if err := provider.Health(ctx); err != nil {
		t.Skipf("docker unhealthy: %v (start Docker Desktop / dockerd to run integration tier)", err)
	}
}
