package pg_dump

import (
	"fmt"
	"testing"
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
			if err := ValidatePgCompressionAlgorithm(tt.algorithm); (err != nil) != tt.wantErr {
				t.Errorf("ValidatePgCompressionAlgorithm(%s) error = %v, wantErr %v", tt.algorithm, err, tt.wantErr)
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
			if err := ValidatePgCompressionLevel(tt.level); (err != nil) != tt.wantErr {
				t.Errorf("ValidatePgCompressionLevel(%d) error = %v, wantErr %v", tt.level, err, tt.wantErr)
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
			if err := ValidatePgOutFormat(tt.format); (err != nil) != tt.wantErr {
				t.Errorf("ValidatePgOutFormat(%s) error = %v, wantErr %v", tt.format, err, tt.wantErr)
			}
		})
	}
}
