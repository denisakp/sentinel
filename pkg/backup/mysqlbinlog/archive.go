package mysqlbinlog

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ArchiveArgs defines binlog archival inputs.
type ArchiveArgs struct {
	BinlogDir   string
	OutputDir   string
	ArchiveName string
}

// ArchiveResult contains generated archive metadata.
type ArchiveResult struct {
	ArchivePath string
	StartFile   string
	EndFile     string
	BinlogFiles []string
}

// Archive packs local mysql/mariadb binlog files into a tar artifact.
func Archive(ctx context.Context, args *ArchiveArgs) (*ArchiveResult, error) {
	if args == nil {
		return nil, fmt.Errorf("archive args are required")
	}
	if strings.TrimSpace(args.BinlogDir) == "" {
		return nil, fmt.Errorf("binlog_dir is required")
	}
	if strings.TrimSpace(args.OutputDir) == "" {
		return nil, fmt.Errorf("output_dir is required")
	}
	if err := os.MkdirAll(args.OutputDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create output dir: %w", err)
	}

	binlogFiles, err := discoverBinlogFiles(args.BinlogDir)
	if err != nil {
		return nil, err
	}
	if len(binlogFiles) == 0 {
		return nil, fmt.Errorf("no_binlog_files_found in %s", args.BinlogDir)
	}

	archiveName := strings.TrimSpace(args.ArchiveName)
	if archiveName == "" {
		archiveName = fmt.Sprintf("binlogs_%d.tar", time.Now().UTC().UnixNano())
	}
	archivePath := filepath.Join(args.OutputDir, archiveName)

	f, err := os.OpenFile(archivePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("failed to create binlog archive: %w", err)
	}
	defer f.Close()

	tw := tar.NewWriter(f)
	defer tw.Close()

	entries := make([]string, 0, len(binlogFiles))
	for _, path := range binlogFiles {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if err := addFileToTar(tw, path); err != nil {
			return nil, err
		}
		entries = append(entries, filepath.Base(path))
	}

	return &ArchiveResult{
		ArchivePath: archivePath,
		StartFile:   entries[0],
		EndFile:     entries[len(entries)-1],
		BinlogFiles: entries,
	}, nil
}

func discoverBinlogFiles(binlogDir string) ([]string, error) {
	entries, err := os.ReadDir(binlogDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read binlog dir: %w", err)
	}

	matches := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".index") {
			continue
		}
		if !strings.HasPrefix(name, "mysql-bin.") && !strings.HasPrefix(name, "mariadb-bin.") {
			continue
		}
		matches = append(matches, filepath.Join(binlogDir, name))
	}

	sort.Strings(matches)
	return matches, nil
}

func addFileToTar(tw *tar.Writer, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat binlog file %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil
	}

	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return fmt.Errorf("failed to build tar header for %s: %w", path, err)
	}
	hdr.Name = filepath.Base(path)
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("failed to write tar header for %s: %w", path, err)
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open binlog file %s: %w", path, err)
	}
	defer file.Close()

	if _, err := io.Copy(tw, file); err != nil {
		return fmt.Errorf("failed to archive binlog file %s: %w", path, err)
	}
	return nil
}
