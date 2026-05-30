// Package ports — compile-time conformance assertions.
//
// This file declares one `var _ <Port> = (*<concrete>)(nil)` per port
// interface, per spec 028 FR-011. Any drift between a port and its current
// concrete implementation fails the next `go test ./internal/ports/...`
// with a "does not implement" message before any downstream adapter spec
// can lift the implementation into a new path.
//
// The file is in package ports_test (not ports) so it can import the
// implementation packages without violating FR-003 (which forbids
// production-code imports of impl from ports).
package ports_test

import (
	"github.com/denisakp/sentinel/internal/adapters/crypto"
	"github.com/denisakp/sentinel/internal/adapters/lock"
	"github.com/denisakp/sentinel/internal/adapters/manifest_store"
	"github.com/denisakp/sentinel/internal/adapters/monitor"
	"github.com/denisakp/sentinel/internal/adapters/notifier"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/adapters/storage/azure"
	"github.com/denisakp/sentinel/internal/adapters/storage/gcs"
	"github.com/denisakp/sentinel/internal/adapters/storage/gdrive"
	"github.com/denisakp/sentinel/internal/adapters/storage/local"
	"github.com/denisakp/sentinel/internal/adapters/storage/s3"
	"github.com/denisakp/sentinel/internal/adapters/tls"
	"github.com/denisakp/sentinel/internal/adapters/dump/mariadb"
	"github.com/denisakp/sentinel/internal/adapters/dump/mongo"
	"github.com/denisakp/sentinel/internal/adapters/dump/mysql"
	"github.com/denisakp/sentinel/internal/adapters/dump/pg"
	mariadbrestore "github.com/denisakp/sentinel/internal/adapters/restore/mariadb"
	mongorestore "github.com/denisakp/sentinel/internal/adapters/restore/mongo"
	mysqlrestore "github.com/denisakp/sentinel/internal/adapters/restore/mysql"
	pgrestore "github.com/denisakp/sentinel/internal/adapters/restore/pg"
)

// Conformance assertions. Each line is one verbatim claim:
//
//	"<concrete> satisfies <port>"
//
// Add a new line whenever a port or adapter changes shape.
var (
	// storage.go — StorageBackend port (5 methods).
	_ ports.StorageBackend = (*local.LocalBackend)(nil)
	_ ports.StorageBackend = (*s3.S3Backend)(nil)
	_ ports.StorageBackend = (*gcs.GCSBackend)(nil)
	_ ports.StorageBackend = (*gdrive.GDriveBackend)(nil)
	_ ports.StorageBackend = (*azure.AzureBlobBackend)(nil)

	// storage.go — StatusReporter sibling port (1 method). Spec 029 T007.
	_ ports.StatusReporter = (*local.LocalBackend)(nil)
	_ ports.StatusReporter = (*s3.S3Backend)(nil)
	_ ports.StatusReporter = (*gcs.GCSBackend)(nil)
	_ ports.StatusReporter = (*gdrive.GDriveBackend)(nil)
	_ ports.StatusReporter = (*azure.AzureBlobBackend)(nil)

	// hasher.go — Hasher port (io.Writer + Sum() string).
	_ ports.Hasher = (*crypto.HashingWriter)(nil)

	// encryption.go — EncryptWriter + DecryptReader ports.
	_ ports.EncryptWriter = (*crypto.ChunkEncryptWriter)(nil)
	_ ports.DecryptReader = (*crypto.ChunkDecryptReader)(nil)

	// manifest.go — ManifestStore port (5 methods on the thin Adapter wrapper).
	_ ports.ManifestStore = manifest_store.Adapter{}

	// lock.go — LockManager port (8 methods on *lock.Manager).
	_ ports.LockManager = (*lock.Manager)(nil)

	// recorder.go — Recorder port (10 methods on *monitor.Monitor).
	_ ports.Recorder = (*monitor.Monitor)(nil)

	// notifier.go — Dispatcher port (9 methods on *notifier.Dispatcher).
	_ ports.Dispatcher = (*notifier.Dispatcher)(nil)

	// crypto.go — KeyProvider port (1 method on *crypto.FileKeyProvider).
	_ ports.KeyProvider = (*crypto.FileKeyProvider)(nil)

	// tls.go — Prober port (1 method on tls.Adapter).
	_ ports.Prober = tls.Adapter{}

	// dump.go — EngineOptions marker. One assertion per engine confirms
	// every current *DumpArgs satisfies the port's marker contract.
	_ ports.EngineOptions = (*pg.PgDumpArgs)(nil)
	_ ports.EngineOptions = (*mysql.MySqlDumpArgs)(nil)
	_ ports.EngineOptions = (*mariadb.MariaDBDumpArgs)(nil)
	_ ports.EngineOptions = (*mongo.DumpMongoArgs)(nil)

	// dump.go — DumpBuilder unified port (spec 035 FR-013).
	_ ports.DumpBuilder = (*pg.Builder)(nil)
	_ ports.DumpBuilder = (*mysql.Builder)(nil)
	_ ports.DumpBuilder = (*mariadb.Builder)(nil)
	_ ports.DumpBuilder = (*mongo.Builder)(nil)

	// restore.go — RestoreOptions marker (spec 036 FR-002).
	_ ports.RestoreOptions = (*pgrestore.RestoreArgs)(nil)
	_ ports.RestoreOptions = (*mysqlrestore.RestoreArgs)(nil)
	_ ports.RestoreOptions = (*mariadbrestore.RestoreArgs)(nil)
	_ ports.RestoreOptions = (*mongorestore.RestoreArgs)(nil)
	_ ports.RestoreOptions = (*mongorestore.OplogReplayArgs)(nil)

	// restore.go — RestoreBuilder unified port (spec 036 FR-005).
	_ ports.RestoreBuilder = (*pgrestore.Builder)(nil)
	_ ports.RestoreBuilder = (*mysqlrestore.Builder)(nil)
	_ ports.RestoreBuilder = (*mariadbrestore.Builder)(nil)
	_ ports.RestoreBuilder = (*mongorestore.Builder)(nil)
)
