package config

import (
	"strings"
	"testing"
)

// compressionJob returns a minimal valid postgres job with the given
// compression + database options applied, wrapped in a Configuration.
func compressionJob(t *testing.T, compression *CompressionConfig, dbOpts map[string]interface{}, engine string) *Configuration {
	t.Helper()
	enabled := true
	job := BackupJob{
		Name:            "job",
		Type:            engine,
		Enabled:         &enabled,
		Host:            "localhost",
		Username:        "sentinel",
		PasswordEnv:     "TEST_PASSWORD",
		Database:        "app",
		Storage:         StorageConfig{Type: "local", LocalPath: "/tmp"},
		DatabaseOptions: dbOpts,
		Compression:     compression,
	}
	if engine == "mongodb" {
		job.URI = "mongodb://localhost:27017"
		job.Host = ""
		job.Username = ""
		job.PasswordEnv = ""
	}
	return &Configuration{
		Version:              "1.0",
		MaxConcurrentBackups: 1,
		Databases:            map[string]BackupJob{"job": job},
	}
}

func TestValidateCompression_Valid(t *testing.T) {
	cases := []struct {
		name string
		c    *CompressionConfig
	}{
		{"nil", nil},
		{"disabled", &CompressionConfig{Enabled: false, Algorithm: "gzip"}},
		{"zstd default level", &CompressionConfig{Enabled: true, Algorithm: "zstd"}},
		{"gzip level 9", &CompressionConfig{Enabled: true, Algorithm: "gzip", Level: 9}},
		{"zstd level 19", &CompressionConfig{Enabled: true, Algorithm: "zstd", Level: 19}},
		{"algorithm none allowed when present", &CompressionConfig{Enabled: false, Algorithm: "none"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := compressionJob(t, tc.c, nil, "postgres")
			if err := ValidateConfig(cfg); err != nil {
				t.Fatalf("ValidateConfig() unexpected error = %v", err)
			}
		})
	}
}

func TestValidateCompression_Invalid(t *testing.T) {
	cases := []struct {
		name    string
		c       *CompressionConfig
		wantErr string
	}{
		{"bad algorithm", &CompressionConfig{Enabled: true, Algorithm: "lz4"}, "must be one of: gzip, zstd, none"},
		{"gzip level too high", &CompressionConfig{Enabled: true, Algorithm: "gzip", Level: 10}, "gzip must be between 1 and 9"},
		{"gzip level negative", &CompressionConfig{Enabled: true, Algorithm: "gzip", Level: -1}, "gzip must be between 1 and 9"},
		{"zstd level too high", &CompressionConfig{Enabled: true, Algorithm: "zstd", Level: 20}, "zstd must be between 1 and 19"},
		{"enabled with none", &CompressionConfig{Enabled: true, Algorithm: "none"}, "algorithm is 'none'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := compressionJob(t, tc.c, nil, "postgres")
			err := ValidateConfig(cfg)
			if err == nil {
				t.Fatalf("ValidateConfig() error = nil, want error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateConfig() error = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

// TestValidateCompression_RejectsDoubleCompress covers the Option A guard: a
// job may not enable pipeline compression AND engine-native compression.
func TestValidateCompression_RejectsDoubleCompress(t *testing.T) {
	cases := []struct {
		name   string
		engine string
		dbOpts map[string]interface{}
	}{
		{"pg compress level", "postgres", map[string]interface{}{"compress": 6}},
		{"pg compression algo", "postgres", map[string]interface{}{"pg_compression_algo": "zstd"}},
		{"pg compression level", "postgres", map[string]interface{}{"pg_compression_level": 5}},
		{"mongo gzip", "mongodb", map[string]interface{}{"gzip": true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := compressionJob(t, &CompressionConfig{Enabled: true, Algorithm: "zstd"}, tc.dbOpts, tc.engine)
			err := ValidateConfig(cfg)
			if err == nil {
				t.Fatal("ValidateConfig() error = nil, want double-compress rejection")
			}
			if !strings.Contains(err.Error(), "engine-native compression") {
				t.Fatalf("ValidateConfig() error = %v, want double-compress error", err)
			}
		})
	}
}

// TestValidateCompression_NativeWithoutPipelineAllowed ensures native
// compression alone (no pipeline compression) still validates — the guard only
// fires when BOTH are set.
func TestValidateCompression_NativeWithoutPipelineAllowed(t *testing.T) {
	cfg := compressionJob(t, nil, map[string]interface{}{"compress": 6}, "postgres")
	if err := ValidateConfig(cfg); err != nil {
		t.Fatalf("ValidateConfig() unexpected error = %v", err)
	}

	// pipeline compression on a PG job with pg_compression_algo=none is fine.
	cfg2 := compressionJob(t, &CompressionConfig{Enabled: true, Algorithm: "zstd"},
		map[string]interface{}{"pg_compression_algo": "none"}, "postgres")
	if err := ValidateConfig(cfg2); err != nil {
		t.Fatalf("ValidateConfig() unexpected error = %v", err)
	}
}
