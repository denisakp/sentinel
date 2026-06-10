package mongo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewStagingDir_CreatesDir(t *testing.T) {
	root := t.TempDir()
	s, err := newStagingDir(root, "job1")
	if err != nil {
		t.Fatalf("newStagingDir: %v", err)
	}
	if s.Root != filepath.Join(root, ".staging", "job1") {
		t.Errorf("unexpected Root: %s", s.Root)
	}
	if _, err := os.Stat(s.Root); err != nil {
		t.Fatalf("expected dir to exist: %v", err)
	}
}

func TestNewStagingDir_EmptyJobIDGenerates(t *testing.T) {
	root := t.TempDir()
	s, err := newStagingDir(root, "")
	if err != nil {
		t.Fatalf("newStagingDir: %v", err)
	}
	if !strings.HasPrefix(s.JobID, "mongo-") {
		t.Errorf("expected generated JobID prefix mongo-, got %q", s.JobID)
	}
}

func TestNewStagingDir_CollisionIsFatal(t *testing.T) {
	root := t.TempDir()
	if _, err := newStagingDir(root, "dup"); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := newStagingDir(root, "dup"); err == nil {
		t.Fatal("expected collision error")
	}
}

func TestNewStagingDir_EmptyBackupPath(t *testing.T) {
	if _, err := newStagingDir("", "j"); err == nil {
		t.Fatal("expected error for empty backup path")
	}
}

func TestArchivePath(t *testing.T) {
	s := &stagingDir{Root: "/x"}
	if got := s.ArchivePath(false); got != "/x/dump.archive" {
		t.Errorf("ArchivePath(false)=%q", got)
	}
	if got := s.ArchivePath(true); got != "/x/dump.archive.gz" {
		t.Errorf("ArchivePath(true)=%q", got)
	}
}

func TestCleanup_RemovesDir(t *testing.T) {
	root := t.TempDir()
	s, _ := newStagingDir(root, "j")
	if err := os.WriteFile(filepath.Join(s.Root, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.Cleanup()
	if _, err := os.Stat(s.Root); !os.IsNotExist(err) {
		t.Errorf("expected dir absent, got err=%v", err)
	}
}

func TestCleanup_Idempotent(t *testing.T) {
	root := t.TempDir()
	s, _ := newStagingDir(root, "j")
	s.Cleanup()
	s.Cleanup()
}

func TestCleanup_NilSafe(t *testing.T) {
	var s *stagingDir
	s.Cleanup()
}
