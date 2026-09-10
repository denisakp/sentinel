package cli

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/adapters/dump"
	dumpmariadb "github.com/denisakp/sentinel/internal/adapters/dump/mariadb"
	dumpmongo "github.com/denisakp/sentinel/internal/adapters/dump/mongo"
	dumpmysql "github.com/denisakp/sentinel/internal/adapters/dump/mysql"
	dumppg "github.com/denisakp/sentinel/internal/adapters/dump/pg"
	"github.com/denisakp/sentinel/internal/config"
	"github.com/denisakp/sentinel/internal/ports"
)

// tlsJob returns a backup job for engine with a populated tls: block.
func tlsJob(engine string) config.BackupJob {
	return config.BackupJob{
		Name:        "job",
		Type:        engine,
		Host:        "db.internal",
		Port:        5432,
		Database:    "appdb",
		Username:    "app",
		PasswordEnv: "SENTINEL_TEST_TLS_PW",
		URI:         "mongodb://db.internal:27017/appdb",
		Storage:     config.StorageConfig{Type: "local", LocalPath: "/tmp"},
		TLS: &config.TLSConfig{
			Enabled:    true,
			Mode:       "verify-full",
			CACertPath: "/etc/ssl/ca.pem",
			ClientCert: "/etc/ssl/client.pem",
			ClientKey:  "/etc/ssl/client-key.pem",
		},
	}
}

// argsTLS extracts the TLS field from whichever engine args value was built.
func argsTLS(t *testing.T, opts ports.EngineOptions) *ports.Config {
	t.Helper()
	switch v := opts.(type) {
	case *dumppg.PgDumpArgs:
		return v.TLS
	case *dumpmysql.MySqlDumpArgs:
		return v.TLS
	case *dumpmariadb.MariaDBDumpArgs:
		return v.TLS
	case *dumpmongo.DumpMongoArgs:
		return v.TLS
	default:
		t.Fatalf("unexpected engine options type %T", opts)
		return nil
	}
}

// TestTLSConfigReachesEveryDumpEngine is the regression guard for #189.
//
// Every engine's args builder consumed a TLS field and nothing ever populated
// one. The configuration validator built the port type from the YAML block,
// validated it, and threw it away; no other code built one at all. So a `tls:`
// block was checked for correctness and then had no effect on any connection,
// for any engine.
//
// The issue reported this for MongoDB. It was true of all four: PostgreSQL,
// MySQL and MariaDB were equally unwired, and their args builders equally called
// BuildTLSArgs on a nil config.
//
// The compounding half is what makes it a security defect rather than a missing
// feature: the validator warns `tls_not_configured` only when the block is
// ABSENT, so adding the block silenced the one diagnostic that would have said
// the connection was in plaintext. Someone hardening a deployment saw the warning
// disappear and concluded it had worked.
func TestTLSConfigReachesEveryDumpEngine(t *testing.T) {
	for _, engine := range []string{"postgres", "mysql", "mariadb", "mongodb"} {
		t.Run(engine, func(t *testing.T) {
			spec := config.BuildDumpJobSpec(tlsJob(engine), "pw", "")
			if spec.TLS == nil {
				t.Fatal("BuildDumpJobSpec dropped the tls: block, so no engine can receive it")
			}

			factory, err := dump.NewArgsFactory(engine)
			if err != nil {
				t.Fatalf("NewArgsFactory(%s) error = %v", engine, err)
			}
			opts, err := factory.BuildDumpArgs(spec)
			if err != nil {
				t.Fatalf("BuildDumpArgs error = %v", err)
			}

			got := argsTLS(t, opts)
			if got == nil {
				t.Fatalf("%s dump args carry no TLS configuration, so the connection is made "+
					"in plaintext while the validator's tls_not_configured warning stays silent", engine)
			}
			if !got.Enabled || got.Mode != "verify-full" {
				t.Errorf("%s received a TLS config but not the configured one: %+v", engine, got)
			}
			if got.CACertPath != "/etc/ssl/ca.pem" {
				t.Errorf("%s lost the CA path: %q", engine, got.CACertPath)
			}
		})
	}
}

// TestNoTLSBlockStaysNil: the mapping must not invent a config where the job
// declares none, or every job would look TLS-configured and the absent-block
// warning would stop firing for the opposite reason.
func TestNoTLSBlockStaysNil(t *testing.T) {
	job := tlsJob("postgres")
	job.TLS = nil

	spec := config.BuildDumpJobSpec(job, "pw", "")
	if spec.TLS != nil {
		t.Fatalf("a job with no tls: block produced a TLS config: %+v", spec.TLS)
	}
}

// TestTLSDisabledStillWarns is the second half of #189, and the half that made
// it a security defect rather than a missing feature.
//
// The validator warned `tls_not_configured` only when the `tls:` block was
// ABSENT. Writing `tls: {enabled: false}` therefore silenced the warning while
// leaving the connection in plaintext, and so did writing a fully populated block
// back when nothing consumed it. Someone hardening a deployment saw the warning
// stop and reasonably concluded it had worked.
//
// The warning now depends on whether the connection will be encrypted, not on
// whether a key exists in the file.
func TestTLSDisabledStillWarns(t *testing.T) {
	t.Setenv("SENTINEL_TEST_TLS_PW", "pw")
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	cases := map[string]*config.TLSConfig{
		"no block at all":     nil,
		"block with disabled": {Enabled: false, Mode: "prefer"},
	}
	for name, tlsCfg := range cases {
		t.Run(name, func(t *testing.T) {
			buf.Reset()
			job := tlsJob("postgres")
			job.TLS = tlsCfg
			cfg := &config.Configuration{
				Version:              "1.0",
				HistoryDBPath:        filepath.Join(t.TempDir(), "history.db"),
				MaxConcurrentBackups: 1,
				Databases:            map[string]config.BackupJob{"job": job},
			}
			if vErr := config.ValidateConfig(cfg); vErr != nil {
				t.Logf("ValidateConfig error (may stop before the TLS check): %v", vErr)
			}

			if !strings.Contains(buf.String(), "tls_not_configured") {
				t.Errorf("no tls_not_configured warning for a connection that will be in "+
					"plaintext.\nlog: %s", buf.String())
			}
		})
	}

	t.Run("enabled block does not warn", func(t *testing.T) {
		buf.Reset()
		job := tlsJob("postgres")
		cfg := &config.Configuration{
			Version:              "1.0",
			HistoryDBPath:        filepath.Join(t.TempDir(), "history.db"),
			MaxConcurrentBackups: 1,
			Databases:            map[string]config.BackupJob{"job": job},
		}
		_ = config.ValidateConfig(cfg)

		if strings.Contains(buf.String(), "tls_not_configured") {
			t.Errorf("warned about a job whose TLS is enabled and wired.\nlog: %s", buf.String())
		}
	})
}
