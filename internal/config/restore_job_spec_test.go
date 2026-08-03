package config

import (
	"testing"

	"github.com/denisakp/sentinel/internal/ports"
)

// TestBuildRestoreJobSpec covers the YAML RestoreJob -> ports.RestoreJobSpec
// translation, incl. conflict default/explicit, cascade, gzip/archive
// resolution. Uses zero/one-flag RestoreOptions to keep
// BuildRestoreAdditionalArgs (map iteration) deterministic.
func TestBuildRestoreJobSpec(t *testing.T) {
	tests := []struct {
		name string
		job  RestoreJob
		want ports.RestoreJobSpec
	}{
		{
			name: "postgres conflict default + cascade",
			job:  RestoreJob{Type: "postgres", Host: "h", Port: 5432, Username: "u", Database: "db", AllowCascade: true},
			want: ports.RestoreJobSpec{
				Engine: "postgres", Host: "h", Port: 5432, Username: "u", Password: "pw",
				Database: "db", BackupPath: "/staged", OnConflict: "error", AllowCascade: true,
			},
		},
		{
			name: "mongodb explicit conflict + gzip flag",
			job: RestoreJob{Type: "mongodb", URI: "mongodb://h", Database: "db",
				ConflictStrategy: "replace", RestoreOptions: map[string]interface{}{"gzip": true}},
			want: ports.RestoreJobSpec{
				Engine: "mongodb", URI: "mongodb://h", Password: "pw", Database: "db",
				BackupPath: "/staged", OnConflict: "replace", Gzip: true, AdditionalArgs: "--gzip",
			},
		},
		{
			name: "archive flag",
			job: RestoreJob{Type: "mongodb", URI: "mongodb://h",
				RestoreOptions: map[string]interface{}{"archive": true}},
			want: ports.RestoreJobSpec{
				Engine: "mongodb", URI: "mongodb://h", Password: "pw",
				BackupPath: "/staged", OnConflict: "error", Archive: true,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildRestoreJobSpec(tc.job, "pw", "/staged", "")
			if got != tc.want {
				t.Errorf("mismatch:\n got  %+v\n want %+v", got, tc.want)
			}
		})
	}
}

func TestBuildRestoreJobSpec_ArchivePath(t *testing.T) {
	got := BuildRestoreJobSpec(RestoreJob{Type: "mongodb", URI: "mongodb://h"}, "", "", "/oplog.bson")
	if got.ArchivePath != "/oplog.bson" || got.BackupPath != "" {
		t.Errorf("archive path mapping: got %+v", got)
	}
}
