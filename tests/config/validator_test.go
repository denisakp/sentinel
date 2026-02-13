package config_test

import (
	"path/filepath"
	"testing"

	"github.com/denisakp/sentinel/internal/config"
)

func TestValidateConfigFixtures(t *testing.T) {
	fixtures := []struct {
		name    string
		file    string
		env     map[string]string
		wantErr bool
	}{
		{
			name: "valid postgres",
			file: "valid_postgres.yaml",
			env: map[string]string{
				"PG_PASSWORD": "secret",
			},
			wantErr: false,
		},
		{
			name: "valid auto discovery",
			file: "valid_auto_discovery.yaml",
			env: map[string]string{
				"MYSQL_PASSWORD": "secret",
			},
			wantErr: false,
		},
		{
			name: "valid mongo",
			file: "valid_mongo.yaml",
			env: map[string]string{
				"MONGO_URI": "mongodb://localhost:27017",
			},
			wantErr: false,
		},
		{
			name: "invalid env name",
			file: "invalid_env_name.yaml",
			env: map[string]string{
				"PG_PASSWORD": "secret",
			},
			wantErr: true,
		},
		{
			name: "invalid cron",
			file: "invalid_cron.yaml",
			env: map[string]string{
				"PG_PASSWORD": "secret",
			},
			wantErr: true,
		},
		{
			name: "invalid postgres option",
			file: "invalid_pg_option.yaml",
			env: map[string]string{
				"PG_PASSWORD": "secret",
			},
			wantErr: true,
		},
	}

	for _, tt := range fixtures {
		t.Run(tt.name, func(t *testing.T) {
			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			path := filepath.Join("fixtures", tt.file)
			cfg, err := config.LoadConfig(path)
			if err != nil {
				if !tt.wantErr {
					t.Fatalf("LoadConfig() error = %v", err)
				}
				return
			}

			err = config.ValidateConfig(cfg)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
