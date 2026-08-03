package cli

import (
	"fmt"
	"os"

	"github.com/denisakp/sentinel/internal/config"
)

// DefaultConfigFileName is the conventional configuration file name looked up
// in the current working directory when --config is not provided.
const DefaultConfigFileName = "./sentinel-config.yaml"

// ResolveConfigPath returns the configuration file path to use.
//
// Resolution order:
//  1. Explicit --config flag value (non-empty flagValue wins unconditionally).
//  2. ./sentinel-config.yaml in the current working directory.
//
// If neither is available a descriptive, actionable error is returned so the
// operator knows exactly what was searched and how to fix it.
func ResolveConfigPath(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}

	if _, err := os.Stat(DefaultConfigFileName); err == nil {
		return DefaultConfigFileName, nil
	}

	return "", fmt.Errorf(
		"no configuration file found: %q does not exist in the current directory.\n"+
			"Use --config to specify a path, e.g.: sentinel --config /path/to/sentinel.yaml",
		DefaultConfigFileName,
	)
}

// LoadAndValidateConfig resolves the configuration path, loads the YAML
// configuration, and performs full schema validation.  It is the standard
// helper for commands that require a fully-validated config (backup, monitor,
// schedule, retention, restore, …).
func LoadAndValidateConfig(flagValue string) (*config.Configuration, error) {
	path, err := ResolveConfigPath(flagValue)
	if err != nil {
		return nil, err
	}

	cfg, err := config.LoadConfig(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load config %q: %w", path, err)
	}

	if err := config.ValidateConfig(cfg); err != nil {
		return nil, fmt.Errorf("invalid config %q: %w", path, err)
	}

	return cfg, nil
}

// LoadConfigMinimal resolves the configuration path and loads the YAML
// configuration without running full schema validation.  Use this for commands
// that only need a subset of the configuration (e.g. db migrate status which
// only needs history_db_path).
func LoadConfigMinimal(flagValue string) (*config.Configuration, error) {
	path, err := ResolveConfigPath(flagValue)
	if err != nil {
		return nil, err
	}

	cfg, err := config.LoadConfig(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load config %q: %w", path, err)
	}

	return cfg, nil
}
