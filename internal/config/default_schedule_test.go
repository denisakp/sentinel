package config

import "testing"

func TestLoadConfig_DefaultScheduleFixtures(t *testing.T) {
	t.Setenv("PG_PASSWORD", "secret")

	tests := []struct {
		name         string
		fixturePath  string
		jobName      string
		wantSchedule string
	}{
		{
			name:         "inherits defaults.schedule when local schedule is empty",
			fixturePath:  "../../tests/config/default_schedule_inheritance.yaml",
			jobName:      "pg-inherit",
			wantSchedule: "*/10 * * * *",
		},
		{
			name:         "keeps explicit local schedule",
			fixturePath:  "../../tests/config/default_schedule_override.yaml",
			jobName:      "pg-override",
			wantSchedule: "0 * * * *",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := LoadConfig(tt.fixturePath)
			if err != nil {
				t.Fatalf("LoadConfig() error = %v", err)
			}

			job, ok := cfg.Databases[tt.jobName]
			if !ok {
				t.Fatalf("job %q not found", tt.jobName)
			}
			if job.Schedule != tt.wantSchedule {
				t.Fatalf("job.Schedule = %q, want %q", job.Schedule, tt.wantSchedule)
			}
		})
	}
}

func TestLoadConfig_DefaultSchedule_NoScheduleStaysEmpty(t *testing.T) {
	t.Setenv("PG_PASSWORD", "secret")
	cfgPath := writeTempConfig(t, `version: "1.0"
defaults:
  storage:
    type: local
    local_path: ./backups

databases:
  pg:
    type: postgres
    host: localhost
    username: sentinel
    password_env: PG_PASSWORD
    database: app
`)

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if got := cfg.Databases["pg"].Schedule; got != "" {
		t.Fatalf("job.Schedule = %q, want empty", got)
	}
}
