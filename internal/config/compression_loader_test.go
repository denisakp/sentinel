package config

import "testing"

func TestApplyCompressionDefaults(t *testing.T) {
	cases := []struct {
		name      string
		in        *CompressionConfig
		wantAlgo  string
		wantLevel int
	}{
		{"nil untouched", nil, "", 0},
		{"disabled untouched", &CompressionConfig{Enabled: false}, "", 0},
		{"enabled empty -> zstd default", &CompressionConfig{Enabled: true}, "zstd", 3},
		{"enabled gzip default level", &CompressionConfig{Enabled: true, Algorithm: "gzip"}, "gzip", 6},
		{"explicit level preserved", &CompressionConfig{Enabled: true, Algorithm: "gzip", Level: 2}, "gzip", 2},
		{"explicit zstd level preserved", &CompressionConfig{Enabled: true, Algorithm: "zstd", Level: 12}, "zstd", 12},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			applyCompressionDefaults(tc.in)
			if tc.in == nil {
				return
			}
			if tc.in.Algorithm != tc.wantAlgo {
				t.Errorf("Algorithm = %q, want %q", tc.in.Algorithm, tc.wantAlgo)
			}
			if tc.in.Level != tc.wantLevel {
				t.Errorf("Level = %d, want %d", tc.in.Level, tc.wantLevel)
			}
		})
	}
}

// TestCompressionInheritance verifies a job without its own compression block
// inherits (a copy of) defaults.compression, while a job with its own block
// does not — mirroring the retention inheritance contract.
func TestCompressionInheritance(t *testing.T) {
	enabled := true
	cfg := &Configuration{
		MaxConcurrentBackups: 1,
		Defaults: GlobalDefaults{
			Compression: &CompressionConfig{Enabled: true, Algorithm: "gzip", Level: 4},
		},
		Databases: map[string]BackupJob{
			"inherits": {Type: "postgres", Enabled: &enabled},
			"overrides": {
				Type:        "postgres",
				Enabled:     &enabled,
				Compression: &CompressionConfig{Enabled: false},
			},
		},
	}

	applyDefaults(cfg)

	inherits := cfg.Databases["inherits"]
	if inherits.Compression == nil {
		t.Fatal("inherits.Compression = nil, want inherited defaults")
	}
	if !inherits.Compression.Enabled || inherits.Compression.Algorithm != "gzip" || inherits.Compression.Level != 4 {
		t.Fatalf("inherited compression = %#v", inherits.Compression)
	}

	// Inheritance must be a copy: mutating the job must not touch defaults.
	inherits.Compression.Level = 9
	if cfg.Defaults.Compression.Level != 4 {
		t.Fatalf("defaults mutated via inherited copy: level = %d", cfg.Defaults.Compression.Level)
	}

	overrides := cfg.Databases["overrides"]
	if overrides.Compression == nil || overrides.Compression.Enabled {
		t.Fatalf("overrides.Compression = %#v, want explicit disabled block", overrides.Compression)
	}
}
