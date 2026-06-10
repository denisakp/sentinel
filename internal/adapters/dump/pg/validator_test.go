package pg

import (
	"fmt"
	"testing"
	"time"
)

func TestValidatePgCompressionAlgorithm(t *testing.T) {
	tests := []struct {
		algorithm string
		wantErr   bool
	}{
		{"gzip", false},
		{"lz4", false},
		{"none", false},
		{"zstd", false},
		{"invalid", true},
		{"", true},
	}
	for _, tt := range tests {
		t.Run(tt.algorithm, func(t *testing.T) {
			if err := validatePgCompressionAlgorithm(tt.algorithm); (err != nil) != tt.wantErr {
				t.Errorf("validatePgCompressionAlgorithm(%s) error = %v, wantErr %v", tt.algorithm, err, tt.wantErr)
			}
		})
	}
}

func TestValidatePgCompressionLevel(t *testing.T) {
	tests := []struct {
		level   int
		wantErr bool
	}{
		{0, false}, // minimum valid compression level
		{9, false}, // maximum valid compression level
		{-1, true}, // below minimum valid compression level
		{10, true}, // above maximum valid compression level
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("Level%d", tt.level), func(t *testing.T) {
			if err := validatePgCompressionLevel(tt.level); (err != nil) != tt.wantErr {
				t.Errorf("validatePgCompressionLevel(%d) error = %v, wantErr %v", tt.level, err, tt.wantErr)
			}
		})
	}
}

func TestValidatePgOutFormat(t *testing.T) {
	tests := []struct {
		format  string
		wantErr bool
	}{
		{"c", false},      // custom format
		{"d", false},      // directory format
		{"t", false},      // tar format
		{"p", false},      // plain format
		{"invalid", true}, // unsupported format
		{"x", true},       // unsupported format
		{"", true},        // empty format
	}
	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			if err := validatePgOutFormat(tt.format); (err != nil) != tt.wantErr {
				t.Errorf("validatePgOutFormat(%s) error = %v, wantErr %v", tt.format, err, tt.wantErr)
			}
		})
	}
}

func Test_validateRequiredArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    *PgDumpArgs
		wantErr bool
	}{
		{
			name:    "Missing required args",
			args:    &PgDumpArgs{},
			wantErr: true,
		},
		{
			name:    "Missing database name",
			args:    &PgDumpArgs{Username: "test"},
			wantErr: true,
		},
		{
			name:    "Missing username",
			args:    &PgDumpArgs{Database: "test"},
			wantErr: true,
		},
		{
			name:    "Valid args",
			args:    &PgDumpArgs{Username: "test", Database: "test"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateRequiredArgs(tt.args); (err != nil) != tt.wantErr {
				t.Errorf("validateRequiredArgs() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidatePITRMetadataArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    *PgDumpArgs
		wantErr bool
	}{
		{
			name:    "nil args",
			args:    nil,
			wantErr: true,
		},
		{
			name:    "pitr disabled",
			args:    &PgDumpArgs{PITREnabled: false},
			wantErr: false,
		},
		{
			name:    "missing wal archive prefix",
			args:    &PgDumpArgs{PITREnabled: true},
			wantErr: true,
		},
		{
			name: "window end before start",
			args: &PgDumpArgs{
				PITREnabled:        true,
				WALArchivePrefix:   "wal/archive",
				PITRWindowStartUTC: nowUTC(2026, 3, 20, 22, 0),
				PITRWindowEndUTC:   nowUTC(2026, 3, 20, 21, 0),
			},
			wantErr: true,
		},
		{
			name: "valid pitr metadata",
			args: &PgDumpArgs{
				PITREnabled:        true,
				WALArchivePrefix:   "wal/archive",
				PITRWindowStartUTC: nowUTC(2026, 3, 20, 20, 0),
				PITRWindowEndUTC:   nowUTC(2026, 3, 20, 21, 0),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePITRMetadataArgs(tt.args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validatePITRMetadataArgs() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func nowUTC(year, month, day, hour, minute int) time.Time {
	return time.Date(year, time.Month(month), day, hour, minute, 0, 0, time.UTC)
}
