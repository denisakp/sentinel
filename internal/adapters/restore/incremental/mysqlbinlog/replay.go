package mysqlbinlog

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// BinlogPosition identifies a replay stop point.
type BinlogPosition struct {
	File string
	Pos  int64
}

// ReplayArgs defines mysqlbinlog replay inputs.
type ReplayArgs struct {
	Engine         string
	Host           string
	Port           int
	Username       string
	Password       string
	Database       string
	BinlogSources  []string
	TargetTime     string
	TargetPosition *BinlogPosition
}

// Replay replays archived or raw binlog sources into a MySQL/MariaDB target.
func Replay(ctx context.Context, args *ReplayArgs) error {
	if args == nil {
		return fmt.Errorf("replay args are required")
	}
	if len(args.BinlogSources) == 0 {
		return fmt.Errorf("at least one binlog source is required")
	}
	if strings.TrimSpace(args.Database) == "" {
		return fmt.Errorf("database is required")
	}
	if strings.TrimSpace(args.Username) == "" {
		return fmt.Errorf("username is required")
	}
	if strings.TrimSpace(args.TargetTime) != "" && args.TargetPosition != nil {
		return fmt.Errorf("target_time and target_position are mutually exclusive")
	}

	expandedFiles, cleanup, err := expandReplaySources(args.BinlogSources)
	if err != nil {
		return err
	}
	defer cleanup()
	if len(expandedFiles) == 0 {
		return fmt.Errorf("no replayable binlog files found")
	}

	selectedFiles := expandedFiles
	if args.TargetPosition != nil {
		targetFile := strings.TrimSpace(args.TargetPosition.File)
		if targetFile == "" || args.TargetPosition.Pos <= 0 {
			return fmt.Errorf("target_position requires both file and pos > 0")
		}
		filtered := make([]string, 0, len(expandedFiles))
		for _, f := range expandedFiles {
			if filepath.Base(f) <= targetFile {
				filtered = append(filtered, f)
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("target_position file %s not found in replay sources", targetFile)
		}
		selectedFiles = filtered
	}

	tool := "mysqlbinlog"
	if _, err := exec.LookPath(tool); err != nil {
		return fmt.Errorf("required_tool_missing: mysqlbinlog")
	}

	binlogArgs := make([]string, 0, len(selectedFiles)+2)
	if strings.TrimSpace(args.TargetTime) != "" {
		binlogArgs = append(binlogArgs, fmt.Sprintf("--stop-datetime=%s", args.TargetTime))
	}
	if args.TargetPosition != nil {
		binlogArgs = append(binlogArgs, fmt.Sprintf("--stop-position=%d", args.TargetPosition.Pos))
	}
	binlogArgs = append(binlogArgs, selectedFiles...)

	clientTool := "mysql"
	if strings.EqualFold(args.Engine, "mariadb") {
		clientTool = "mariadb"
	}
	if _, err := exec.LookPath(clientTool); err != nil {
		return fmt.Errorf("required_tool_missing: %s", clientTool)
	}

	port := args.Port
	if port == 0 {
		port = 3306
	}
	clientArgs := []string{
		fmt.Sprintf("--host=%s", defaultIfEmpty(args.Host, "127.0.0.1")),
		fmt.Sprintf("--port=%d", port),
		fmt.Sprintf("--user=%s", args.Username),
		args.Database,
	}

	binlogCmd := exec.CommandContext(ctx, tool, binlogArgs...)
	clientCmd := exec.CommandContext(ctx, clientTool, clientArgs...)

	env := os.Environ()
	if strings.TrimSpace(args.Password) != "" {
		env = append(env, fmt.Sprintf("MYSQL_PWD=%s", args.Password))
	}
	binlogCmd.Env = env
	clientCmd.Env = env

	pipe, err := binlogCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create mysqlbinlog stdout pipe: %w", err)
	}
	clientCmd.Stdin = pipe

	var binlogErr bytes.Buffer
	var clientErr bytes.Buffer
	binlogCmd.Stderr = &binlogErr
	clientCmd.Stderr = &clientErr

	if err := clientCmd.Start(); err != nil {
		return fmt.Errorf("failed to start %s: %w", clientTool, err)
	}
	if err := binlogCmd.Start(); err != nil {
		_ = clientCmd.Process.Kill()
		_ = clientCmd.Wait()
		return fmt.Errorf("failed to start mysqlbinlog: %w", err)
	}

	binlogRunErr := binlogCmd.Wait()
	_ = pipe.Close()
	clientRunErr := clientCmd.Wait()

	if binlogRunErr != nil {
		return fmt.Errorf("mysqlbinlog replay failed: %w: %s", binlogRunErr, strings.TrimSpace(binlogErr.String()))
	}
	if clientRunErr != nil {
		return fmt.Errorf("%s apply failed: %w: %s", clientTool, clientRunErr, strings.TrimSpace(clientErr.String()))
	}

	return nil
}

func expandReplaySources(sources []string) ([]string, func(), error) {
	files := make([]string, 0)
	tempDirs := make([]string, 0)
	cleanup := func() {
		for _, dir := range tempDirs {
			_ = os.RemoveAll(dir)
		}
	}

	for _, source := range sources {
		path := strings.TrimSpace(source)
		if path == "" {
			continue
		}
		if strings.HasSuffix(strings.ToLower(path), ".tar") {
			dir, err := os.MkdirTemp("", "sentinel-binlog-replay-*")
			if err != nil {
				cleanup()
				return nil, func() {}, fmt.Errorf("failed to create replay temp dir: %w", err)
			}
			tempDirs = append(tempDirs, dir)
			extracted, err := extractTar(path, dir)
			if err != nil {
				cleanup()
				return nil, func() {}, err
			}
			files = append(files, extracted...)
			continue
		}
		files = append(files, path)
	}

	filtered := make([]string, 0, len(files))
	for _, f := range files {
		base := filepath.Base(f)
		if strings.HasSuffix(base, ".index") {
			continue
		}
		if !strings.HasPrefix(base, "mysql-bin.") && !strings.HasPrefix(base, "mariadb-bin.") {
			continue
		}
		filtered = append(filtered, f)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filepath.Base(filtered[i]) < filepath.Base(filtered[j])
	})

	return filtered, cleanup, nil
}

func extractTar(archivePath, destDir string) ([]string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open binlog archive %s: %w", archivePath, err)
	}
	defer f.Close()

	tr := tar.NewReader(f)
	extracted := make([]string, 0)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read binlog archive %s: %w", archivePath, err)
		}
		if hdr.FileInfo().IsDir() {
			continue
		}

		target := filepath.Join(destDir, filepath.Base(hdr.Name))
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return nil, fmt.Errorf("failed to extract %s: %w", hdr.Name, err)
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return nil, fmt.Errorf("failed to extract %s: %w", hdr.Name, err)
		}
		if err := out.Close(); err != nil {
			return nil, fmt.Errorf("failed to finalize extracted file %s: %w", hdr.Name, err)
		}
		extracted = append(extracted, target)
	}
	return extracted, nil
}

func defaultIfEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
