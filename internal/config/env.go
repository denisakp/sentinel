package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var envNameRegex = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
var envInterpolationRegex = regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*)\}`)

func validateEnvName(name string) error {
	if name == "" {
		return nil
	}
	if !envNameRegex.MatchString(name) {
		return fmt.Errorf("invalid env var name '%s'", name)
	}
	return nil
}

func resolveEnvOverride(target *string, envName string) error {
	if envName == "" {
		return nil
	}
	if err := validateEnvName(envName); err != nil {
		return err
	}
	value := os.Getenv(envName)
	if value == "" {
		return fmt.Errorf("environment variable '%s' is not set", envName)
	}
	*target = value
	return nil
}

func requireEnvValue(envName string) error {
	if envName == "" {
		return nil
	}
	if err := validateEnvName(envName); err != nil {
		return err
	}
	if os.Getenv(envName) == "" {
		return fmt.Errorf("environment variable '%s' is not set", envName)
	}
	return nil
}

func interpolateEnvVars(value string) (string, error) {
	if value == "" {
		return value, nil
	}
	matches := envInterpolationRegex.FindAllStringSubmatchIndex(value, -1)
	if len(matches) == 0 {
		return value, nil
	}

	var builder strings.Builder
	lastIndex := 0
	for _, match := range matches {
		start := match[0]
		end := match[1]
		nameStart := match[2]
		nameEnd := match[3]

		builder.WriteString(value[lastIndex:start])
		envName := value[nameStart:nameEnd]
		if err := validateEnvName(envName); err != nil {
			return "", err
		}
		resolved := os.Getenv(envName)
		if resolved == "" {
			return "", fmt.Errorf("environment variable '%s' is not set", envName)
		}
		builder.WriteString(resolved)
		lastIndex = end
	}
	builder.WriteString(value[lastIndex:])
	return builder.String(), nil
}
