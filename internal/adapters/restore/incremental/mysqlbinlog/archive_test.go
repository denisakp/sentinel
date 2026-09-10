package mysqlbinlog

import (
	"archive/tar"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// writeBinlogDir lays out a binlog directory with the given file names.
func writeBinlogDir(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
	}
	return dir
}

// archivedNames returns the entry names inside a tar artifact.
func archivedNames(t *testing.T, archivePath string) []string {
	t.Helper()
	f, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("Open(%s) error = %v", archivePath, err)
	}
	defer f.Close()

	var names []string
	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		names = append(names, hdr.Name)
	}
	return names
}

// Stock MySQL 8 names its binary logs from log_bin_basename, which defaults
// to "binlog", not "mysql-bin". Before #190 discovery matched only the
// mysql-bin. and mariadb-bin. prefixes, so a default MySQL 8 install
// archived nothing and said nothing about it.
func TestArchiveFindsStockMySQL8Binlogs(t *testing.T) {
	dir := writeBinlogDir(t, "binlog.000001", "binlog.000002", "binlog.index")
	out := t.TempDir()

	result, err := Archive(context.Background(), &ArchiveArgs{BinlogDir: dir, OutputDir: out, ArchiveName: "binlogs.tar"})
	if err != nil {
		t.Fatalf("Archive() error = %v, want the two segments archived", err)
	}
	if len(result.BinlogFiles) != 2 {
		t.Fatalf("Archive() archived %v, want binlog.000001 and binlog.000002", result.BinlogFiles)
	}
	if result.StartFile != "binlog.000001" || result.EndFile != "binlog.000002" {
		t.Fatalf("Archive() range = %s..%s, want binlog.000001..binlog.000002", result.StartFile, result.EndFile)
	}
	for _, name := range archivedNames(t, result.ArchivePath) {
		if name == "binlog.index" {
			t.Fatal("Archive() packed the index file; only log segments belong in the artifact")
		}
	}
}

// A custom log_bin_basename is just as valid and just as invisible to a
// prefix match.
func TestArchiveFindsBinlogsUnderACustomBasename(t *testing.T) {
	dir := writeBinlogDir(t, "shop-binlog.000007", "shop-binlog.index")
	out := t.TempDir()

	result, err := Archive(context.Background(), &ArchiveArgs{BinlogDir: dir, OutputDir: out, ArchiveName: "binlogs.tar"})
	if err != nil {
		t.Fatalf("Archive() error = %v, want the segment archived", err)
	}
	if len(result.BinlogFiles) != 1 || result.BinlogFiles[0] != "shop-binlog.000007" {
		t.Fatalf("Archive() archived %v, want [shop-binlog.000007]", result.BinlogFiles)
	}
}

// The legacy names must keep working: this fix widens discovery, it does not
// move it.
func TestArchiveStillFindsLegacyBinlogNames(t *testing.T) {
	dir := writeBinlogDir(t, "mysql-bin.000001", "mariadb-bin.000001", "mysql-bin.index")
	out := t.TempDir()

	result, err := Archive(context.Background(), &ArchiveArgs{BinlogDir: dir, OutputDir: out, ArchiveName: "binlogs.tar"})
	if err != nil {
		t.Fatalf("Archive() error = %v, want both legacy segments archived", err)
	}
	if len(result.BinlogFiles) != 2 {
		t.Fatalf("Archive() archived %v, want both legacy segments", result.BinlogFiles)
	}
}

// Unrelated files in the directory are not log segments. The sequence suffix
// is what identifies one, so a data file or a config file stays out.
func TestArchiveIgnoresNonSegmentFiles(t *testing.T) {
	dir := writeBinlogDir(t, "binlog.000001", "ibdata1", "my.cnf", "binlog.index", "relay-log.info")
	out := t.TempDir()

	result, err := Archive(context.Background(), &ArchiveArgs{BinlogDir: dir, OutputDir: out, ArchiveName: "binlogs.tar"})
	if err != nil {
		t.Fatalf("Archive() error = %v", err)
	}
	if len(result.BinlogFiles) != 1 || result.BinlogFiles[0] != "binlog.000001" {
		t.Fatalf("Archive() archived %v, want [binlog.000001]", result.BinlogFiles)
	}
}

// A directory with no log segments at all is still an error, and the error
// still names the directory. Archiving zero files silently is the defect.
func TestArchiveReportsADirectoryWithNoSegments(t *testing.T) {
	dir := writeBinlogDir(t, "ibdata1", "my.cnf")
	out := t.TempDir()

	_, err := Archive(context.Background(), &ArchiveArgs{BinlogDir: dir, OutputDir: out})
	if err == nil {
		t.Fatal("Archive() error = nil, want no_binlog_files_found")
	}
}

// Replay reads whatever Archive wrote, so its filter has to recognise the
// same set of names. It carried an identical copy of the prefix check.
func TestReplayFilterAcceptsStockMySQL8Binlogs(t *testing.T) {
	dir := writeBinlogDir(t, "binlog.000001", "binlog.000002", "binlog.index", "my.cnf")

	files, cleanup, err := expandReplaySources([]string{
		filepath.Join(dir, "binlog.000001"),
		filepath.Join(dir, "binlog.000002"),
		filepath.Join(dir, "binlog.index"),
		filepath.Join(dir, "my.cnf"),
	})
	if err != nil {
		t.Fatalf("expandReplaySources() error = %v", err)
	}
	defer cleanup()

	if len(files) != 2 {
		t.Fatalf("expandReplaySources() kept %v, want the two binlog segments", files)
	}
	for _, f := range files {
		if filepath.Base(f) == "binlog.index" || filepath.Base(f) == "my.cnf" {
			t.Fatalf("expandReplaySources() kept %s, which is not a log segment", f)
		}
	}
}
