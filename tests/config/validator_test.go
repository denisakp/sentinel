package config_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/config"
)

func TestValidateConfigFixtures(t *testing.T) {
	fixtures := []struct {
		name    string
		file    string
		env     map[string]string
		wantErr bool
		errHas  []string
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
		{
			name: "invalid inherited defaults.schedule",
			file: "defaults_schedule_invalid_inherited.yaml",
			env: map[string]string{
				"PG_PASSWORD": "secret",
			},
			wantErr: true,
			errHas:  []string{"backup 'inherited-job'", "invalid cron expression"},
		},
		{
			name: "whitespace defaults.schedule is invalid",
			file: "defaults_schedule_whitespace.yaml",
			env: map[string]string{
				"PG_PASSWORD": "secret",
			},
			wantErr: true,
			errHas:  []string{"backup 'whitespace-job'", "whitespace-only"},
		},
		{
			name: "disabled jobs are still validated for inherited schedule",
			file: "defaults_schedule_disabled_job_invalid.yaml",
			env: map[string]string{
				"PG_PASSWORD": "secret",
			},
			wantErr: true,
			errHas:  []string{"backup 'disabled-invalid-job'", "invalid cron expression"},
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
			for _, want := range tt.errHas {
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("expected error to contain %q, got: %v", want, err)
				}
			}
		})
	}
}
