package restore

import (
	"errors"
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/ports"
)

// objectsFrom builds a listing from bare paths; sizes are irrelevant here.
func objectsFrom(paths ...string) []ports.StorageObject {
	objects := make([]ports.StorageObject, 0, len(paths))
	for _, p := range paths {
		objects = append(objects, ports.StorageObject{Path: p})
	}
	return objects
}

// A backup records its artifact as <local_path>/<out_name>, while a restore
// source lists relative to its own local_path. The two are the same file
// described from different roots, and before #150 the mismatch surfaced as
// ErrSourceObjectNotFound with both halves of the pair pointing at a file
// that was sitting right there.
func TestChainObjectResolvesWhenTheIDIsRootedDeeperThanTheListing(t *testing.T) {
	got, err := ResolveChainObject("backups/shop.sql", objectsFrom("shop.sql"))
	if err != nil {
		t.Fatalf("ResolveChainObject() error = %v, want the listed object", err)
	}
	if got.Path != "shop.sql" {
		t.Fatalf("ResolveChainObject() path = %q, want %q", got.Path, "shop.sql")
	}
}

// The same mismatch, one level further out: a nested prefix on the recorded
// id must not defeat the match either.
func TestChainObjectResolvesThroughAMultiSegmentPrefix(t *testing.T) {
	got, err := ResolveChainObject("/var/backups/shop/shop.sql", objectsFrom("shop.sql"))
	if err != nil {
		t.Fatalf("ResolveChainObject() error = %v, want the listed object", err)
	}
	if got.Path != "shop.sql" {
		t.Fatalf("ResolveChainObject() path = %q, want %q", got.Path, "shop.sql")
	}
}

// Matching on the final path element must not silently pick one of two
// files that merely share a name in different directories. Ambiguity is the
// honest answer; a guess would restore the wrong backup.
func TestChainObjectRejectsTheSameNameInTwoDirectories(t *testing.T) {
	_, err := ResolveChainObject("backups/shop.sql", objectsFrom("daily/shop.sql", "weekly/shop.sql"))
	if !errors.Is(err, ErrAmbiguousBackupID) {
		t.Fatalf("ResolveChainObject() error = %v, want ErrAmbiguousBackupID", err)
	}
	for _, want := range []string{"daily/shop.sql", "weekly/shop.sql"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ambiguous error %q does not name %q", err.Error(), want)
		}
	}
}

// In a flattened layout the id "shop" boundary-matched both the artifact and
// its manifest sidecar, so a chain that resolved its baseline correctly still
// failed to stage it. The sidecar is not a candidate artifact.
func TestChainObjectIgnoresTheManifestSidecar(t *testing.T) {
	got, err := ResolveChainObject("shop", objectsFrom("shop.sql", "shop.sql.manifest.json"))
	if err != nil {
		t.Fatalf("ResolveChainObject() error = %v, want the artifact", err)
	}
	if got.Path != "shop.sql" {
		t.Fatalf("ResolveChainObject() path = %q, want %q", got.Path, "shop.sql")
	}
}

// Excluding sidecars from the candidate set must not make a manifest
// unreachable when the caller asked for the manifest by name.
func TestChainObjectStillResolvesAManifestAskedForByName(t *testing.T) {
	got, err := ResolveChainObject("shop.sql.manifest.json", objectsFrom("shop.sql", "shop.sql.manifest.json"))
	if err != nil {
		t.Fatalf("ResolveChainObject() error = %v, want the manifest", err)
	}
	if got.Path != "shop.sql.manifest.json" {
		t.Fatalf("ResolveChainObject() path = %q, want %q", got.Path, "shop.sql.manifest.json")
	}
}

// A manifest requested from a different root resolves the same way the
// artifact does, without the exact-path shortcut carrying it.
func TestChainObjectResolvesARootedManifestByName(t *testing.T) {
	got, err := ResolveChainObject("backups/shop.sql.manifest.json", objectsFrom("shop.sql", "shop.sql.manifest.json"))
	if err != nil {
		t.Fatalf("ResolveChainObject() error = %v, want the manifest", err)
	}
	if got.Path != "shop.sql.manifest.json" {
		t.Fatalf("ResolveChainObject() path = %q, want %q", got.Path, "shop.sql.manifest.json")
	}
}

// The encrypted sibling is a real artifact, not a sidecar: two plausible
// candidates must stay ambiguous rather than resolve by luck.
func TestChainObjectKeepsEncryptedSiblingAmbiguous(t *testing.T) {
	_, err := ResolveChainObject("shop", objectsFrom("shop.sql", "shop.sql.enc"))
	if !errors.Is(err, ErrAmbiguousBackupID) {
		t.Fatalf("ResolveChainObject() error = %v, want ErrAmbiguousBackupID", err)
	}
}
