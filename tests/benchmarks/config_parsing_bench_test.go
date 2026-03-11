package benchmarks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/config"
)

const configBenchJobCount = 25

func BenchmarkConfigLoadAndValidate(b *testing.B) {
	b.Setenv("PG_PASSWORD", "dummy")

	configPath := filepath.Join(b.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(buildConfigYAML(configBenchJobCount)), 0o600); err != nil {
		b.Fatalf("write config: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			b.Fatalf("load config: %v", err)
		}
		if err := config.ValidateConfig(cfg); err != nil {
			b.Fatalf("validate config: %v", err)
		}
	}
}

func buildConfigYAML(jobCount int) string {
	var builder strings.Builder
	builder.WriteString("version: \"1.0\"\n")
	builder.WriteString("defaults:\n")
	builder.WriteString("  storage:\n")
	builder.WriteString("    type: local\n")
	builder.WriteString("    local_path: \"./backups\"\n")
	builder.WriteString("max_concurrent_backups: 3\n")
	builder.WriteString("databases:\n")

	for i := 1; i <= jobCount; i++ {
		name := fmt.Sprintf("job-%02d", i)
		dbName := fmt.Sprintf("db_%02d", i)
		builder.WriteString("  " + name + ":\n")
		builder.WriteString("    type: postgres\n")
		builder.WriteString("    host: \"localhost\"\n")
		builder.WriteString("    port: 5432\n")
		builder.WriteString("    username: \"postgres\"\n")
		builder.WriteString("    password_env: \"PG_PASSWORD\"\n")
		builder.WriteString("    database: \"" + dbName + "\"\n")
		builder.WriteString("    schedule: \"0 2 * * *\"\n")
		builder.WriteString("    enabled: true\n")
	}

	return builder.String()
}
