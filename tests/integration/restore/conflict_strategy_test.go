package integration_test

import (
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/config"
	mariadbrestore "github.com/denisakp/sentinel/pkg/restore/mariadb_restore"
	mongorestore "github.com/denisakp/sentinel/pkg/restore/mongo_restore"
	mysqlrestore "github.com/denisakp/sentinel/pkg/restore/mysql_restore"
	pgrestore "github.com/denisakp/sentinel/pkg/restore/pg_restore"
)

// --- Config-layer validation tests ---

func TestConflictStrategy_Config_ValidStrategies(t *testing.T) {
	for _, strategy := range []string{"error", "ignore", "replace"} {
		t.Run(strategy, func(t *testing.T) {
			job := validPostgresJob()
			job.ConflictStrategy = strategy
			if strategy == "replace" {
				job.AllowCascade = true
			}
			if err := config.ValidateRestoreJob(&job); err != nil {
				t.Errorf("strategy=%q should be valid, got error: %v", strategy, err)
			}
		})
	}
}

func TestConflictStrategy_Config_InvalidStrategy_Rejected(t *testing.T) {
	job := validPostgresJob()
	job.ConflictStrategy = "upsert"
	err := config.ValidateRestoreJob(&job)
	if err == nil {
		t.Fatal("expected error for invalid conflict_strategy=upsert")
	}
	if !strings.Contains(err.Error(), "conflict_strategy") {
		t.Errorf("expected error mentioning conflict_strategy, got %q", err.Error())
	}
}

func TestConflictStrategy_Config_PGReplace_RequiresAllowCascade(t *testing.T) {
	job := validPostgresJob()
	job.ConflictStrategy = "replace"
	job.AllowCascade = false // not set

	err := config.ValidateRestoreJob(&job)
	if err == nil {
		t.Fatal("expected error: postgres replace requires allow_cascade: true")
	}
	if !strings.Contains(err.Error(), "allow_cascade") {
		t.Errorf("expected error mentioning allow_cascade, got %q", err.Error())
	}
}

func TestConflictStrategy_Config_AllowCascade_NonPostgres_Rejected(t *testing.T) {
	for _, dbType := range []string{"mysql", "mariadb", "mongodb"} {
		t.Run(dbType, func(t *testing.T) {
			job := validJobForType(dbType)
			job.AllowCascade = true

			err := config.ValidateRestoreJob(&job)
			if err == nil {
				t.Fatalf("expected error: allow_cascade is only supported for postgres, not %q", dbType)
			}
			if !strings.Contains(err.Error(), "allow_cascade") {
				t.Errorf("expected error mentioning allow_cascade, got %q", err.Error())
			}
		})
	}
}

func TestConflictStrategy_Config_EmptyStrategy_DefaultsToError(t *testing.T) {
	job := validPostgresJob()
	job.ConflictStrategy = ""
	if err := config.ValidateRestoreJob(&job); err != nil {
		t.Errorf("empty conflict_strategy should be valid (defaults to error): %v", err)
	}
}

// --- PostgreSQL flag mapping tests ---

func TestConflictStrategy_PG_Error_NoFlags(t *testing.T) {
	ra := &pgrestore.RestoreArgs{
		Host: "localhost", Port: 5432, Username: "user",
		Database: "db", BackupPath: "/tmp/b.sql",
		OnConflict: "error",
	}
	if err := pgrestore.ValidateOnConflict(ra.OnConflict); err != nil {
		t.Fatalf("ValidateOnConflict(error) = %v", err)
	}
}

func TestConflictStrategy_PG_Ignore_ValidatesOK(t *testing.T) {
	if err := pgrestore.ValidateOnConflict("ignore"); err != nil {
		t.Fatalf("ValidateOnConflict(ignore) = %v", err)
	}
}

func TestConflictStrategy_PG_Replace_ValidatesOK(t *testing.T) {
	if err := pgrestore.ValidateOnConflict("replace"); err != nil {
		t.Fatalf("ValidateOnConflict(replace) = %v", err)
	}
}

func TestConflictStrategy_PG_Invalid_Rejected(t *testing.T) {
	if err := pgrestore.ValidateOnConflict("overwrite"); err == nil {
		t.Fatal("expected error for invalid pg conflict strategy")
	}
}

// --- MySQL flag mapping tests ---

func TestConflictStrategy_MySQL_Error_ValidatesOK(t *testing.T) {
	if err := mysqlrestore.ValidateOnConflict("error"); err != nil {
		t.Fatalf("ValidateOnConflict(error) = %v", err)
	}
}

func TestConflictStrategy_MySQL_Ignore_ValidatesOK(t *testing.T) {
	if err := mysqlrestore.ValidateOnConflict("ignore"); err != nil {
		t.Fatalf("ValidateOnConflict(ignore) = %v", err)
	}
}

func TestConflictStrategy_MySQL_Replace_ValidatesOK(t *testing.T) {
	if err := mysqlrestore.ValidateOnConflict("replace"); err != nil {
		t.Fatalf("ValidateOnConflict(replace) = %v", err)
	}
}

func TestConflictStrategy_MySQL_Invalid_Rejected(t *testing.T) {
	if err := mysqlrestore.ValidateOnConflict("overwrite"); err == nil {
		t.Fatal("expected error for invalid mysql conflict strategy")
	}
}

// --- MariaDB flag mapping tests ---

func TestConflictStrategy_MariaDB_Error_ValidatesOK(t *testing.T) {
	if err := mariadbrestore.ValidateOnConflict("error"); err != nil {
		t.Fatalf("ValidateOnConflict(error) = %v", err)
	}
}

func TestConflictStrategy_MariaDB_Ignore_ValidatesOK(t *testing.T) {
	if err := mariadbrestore.ValidateOnConflict("ignore"); err != nil {
		t.Fatalf("ValidateOnConflict(ignore) = %v", err)
	}
}

func TestConflictStrategy_MariaDB_Replace_ValidatesOK(t *testing.T) {
	if err := mariadbrestore.ValidateOnConflict("replace"); err != nil {
		t.Fatalf("ValidateOnConflict(replace) = %v", err)
	}
}

// --- MongoDB flag mapping tests ---

func TestConflictStrategy_MongoDB_Ignore_ValidatesOK(t *testing.T) {
	if err := mongorestore.ValidateOnConflict("ignore"); err != nil {
		t.Fatalf("ValidateOnConflict(ignore) = %v", err)
	}
}

func TestConflictStrategy_MongoDB_Replace_ValidatesOK(t *testing.T) {
	if err := mongorestore.ValidateOnConflict("replace"); err != nil {
		t.Fatalf("ValidateOnConflict(replace) = %v", err)
	}
}

func TestConflictStrategy_MongoDB_Invalid_Rejected(t *testing.T) {
	if err := mongorestore.ValidateOnConflict("drop"); err == nil {
		t.Fatal("expected error for invalid mongodb conflict strategy")
	}
}

// --- PostgreSQL cascade safety (config layer) ---

func TestCascadeSafety_PGReplace_WithoutAllowCascade_FailsValidation(t *testing.T) {
	job := validPostgresJob()
	job.ConflictStrategy = "replace"
	job.AllowCascade = false

	err := config.ValidateRestoreJob(&job)
	if err == nil {
		t.Fatal("expected validation error for replace without allow_cascade")
	}
}

func TestCascadeSafety_PGReplace_WithAllowCascade_PassesValidation(t *testing.T) {
	job := validPostgresJob()
	job.ConflictStrategy = "replace"
	job.AllowCascade = true

	if err := config.ValidateRestoreJob(&job); err != nil {
		t.Errorf("expected no validation error with allow_cascade=true, got: %v", err)
	}
}

func TestCascadeSafety_PGIgnore_NoCascadeRequired(t *testing.T) {
	job := validPostgresJob()
	job.ConflictStrategy = "ignore"
	job.AllowCascade = false

	if err := config.ValidateRestoreJob(&job); err != nil {
		t.Errorf("ignore strategy should not require allow_cascade, got: %v", err)
	}
}

// --- Helpers ---

func validPostgresJob() config.RestoreJob {
	return config.RestoreJob{
		Name:       "test-pg",
		Type:       "postgres",
		Schedule:   "0 2 * * *",
		StagingDir: "/tmp/sentinel",
		Host:       "localhost",
		Port:       5432,
		Username:   "user",
		Database:   "testdb",
		BackupSource: config.RestoreBackupSource{
			Type:       "local",
			LocalPath:  "/backups",
			BackupPath: "prod.sql",
		},
	}
}

func validJobForType(dbType string) config.RestoreJob {
	switch dbType {
	case "mongodb":
		return config.RestoreJob{
			Name:       "test-mongo",
			Type:       "mongodb",
			Schedule:   "0 2 * * *",
			StagingDir: "/tmp/sentinel",
			URI:        "mongodb://localhost/testdb",
			Database:   "testdb",
			BackupSource: config.RestoreBackupSource{
				Type:       "local",
				LocalPath:  "/backups",
				BackupPath: "backup.archive",
			},
		}
	default:
		return config.RestoreJob{
			Name:       "test-" + dbType,
			Type:       dbType,
			Schedule:   "0 2 * * *",
			StagingDir: "/tmp/sentinel",
			Host:       "localhost",
			Port:       3306,
			Username:   "user",
			Database:   "testdb",
			BackupSource: config.RestoreBackupSource{
				Type:       "local",
				LocalPath:  "/backups",
				BackupPath: "prod.sql",
			},
		}
	}
}
