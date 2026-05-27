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
	"github.com/denisakp/sentinel/internal/crypto"
	"github.com/denisakp/sentinel/internal/lock"
	"github.com/denisakp/sentinel/internal/manifest"
	"github.com/denisakp/sentinel/internal/monitor"
	"github.com/denisakp/sentinel/internal/notifier"
	"github.com/denisakp/sentinel/internal/ports"
	"github.com/denisakp/sentinel/internal/storage/azure"
	"github.com/denisakp/sentinel/internal/storage/gcs"
	"github.com/denisakp/sentinel/internal/storage/gdrive"
	"github.com/denisakp/sentinel/internal/storage/local"
	"github.com/denisakp/sentinel/internal/storage/sentinel_s3"
	"github.com/denisakp/sentinel/internal/tls"
	"github.com/denisakp/sentinel/pkg/backup/mariadb_dump"
	"github.com/denisakp/sentinel/pkg/backup/mongo_dump"
	"github.com/denisakp/sentinel/pkg/backup/mysql_dump"
	"github.com/denisakp/sentinel/pkg/backup/pg_dump"
)

// Conformance assertions. Each line is one verbatim claim:
//   "<concrete> satisfies <port>"
// Add a new line whenever a port or adapter changes shape.
var (
	// storage.go — StorageBackend port (5 methods).
	_ ports.StorageBackend = (*local.LocalBackend)(nil)
	_ ports.StorageBackend = (*sentinel_s3.S3Backend)(nil)
	_ ports.StorageBackend = (*gcs.GCSBackend)(nil)
	_ ports.StorageBackend = (*gdrive.GDriveBackend)(nil)
	_ ports.StorageBackend = (*azure.AzureBlobBackend)(nil)

	// hasher.go — Hasher port (io.Writer + Sum() string).
	_ ports.Hasher = (*crypto.HashingWriter)(nil)

	// encryption.go — EncryptWriter + DecryptReader ports.
	_ ports.EncryptWriter = (*crypto.ChunkEncryptWriter)(nil)
	_ ports.DecryptReader = (*crypto.ChunkDecryptReader)(nil)

	// manifest.go — ManifestStore port (5 methods on the thin Adapter wrapper).
	_ ports.ManifestStore = manifest.Adapter{}

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
	_ ports.EngineOptions = (*pg_dump.PgDumpArgs)(nil)
	_ ports.EngineOptions = (*mysql_dump.MySqlDumpArgs)(nil)
	_ ports.EngineOptions = (*mariadb_dump.MariaDBDumpArgs)(nil)
	_ ports.EngineOptions = (*mongo_dump.DumpMongoArgs)(nil)
)
