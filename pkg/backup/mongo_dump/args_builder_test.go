package mongo_dump

import (
	"reflect"
	"testing"
)

func Test_argsBuilder(t *testing.T) {

	tests := []struct {
		name    string
		args    *DumpMongoArgs
		want    []string
		wantErr bool
	}{
		{
			name:    "Args with default URI",
			args:    &DumpMongoArgs{Compress: false, OutName: "test.archive"},
			want:    []string{"--uri=mongodb://localhost:27017", "--out=test.archive", "--quiet"},
			wantErr: false,
		},
		{
			name: "Args with custom URI",
			args: &DumpMongoArgs{Uri: "mongodb://username@password:192.168.1.34:27017/?timeoutMS=5000", Compress: false, OutName: "test.archive"},
			want: []string{"--uri=mongodb://username@password:192.168.1.34:27017/?timeoutMS=5000", "--out=test.archive", "--quiet"},
		},
		{
			name:    "Args with compression enabled",
			args:    &DumpMongoArgs{Uri: "mongodb://localhost:27017", Compress: true, OutName: "test.archive"},
			want:    []string{"--uri=mongodb://localhost:27017", "--out=test.archive", "--quiet", "--gzip"},
			wantErr: false,
		},
		{
			name:    "Args with additional arguments",
			args:    &DumpMongoArgs{Uri: "mongodb://localhost:27017", Compress: false, AdditionalArgs: "--authenticationDatabase=admin", OutName: "test.archive"},
			want:    []string{"--uri=mongodb://localhost:27017", "--out=test.archive", "--quiet", "--authenticationDatabase=admin"},
			wantErr: false,
		},
		{
			name:    "Remove duplicate arguments",
			args:    &DumpMongoArgs{Uri: "mongodb://localhost:27017", Compress: false, AdditionalArgs: "--authenticationDatabase=admin --authenticationDatabase=admin", OutName: "test.archive"},
			want:    []string{"--uri=mongodb://localhost:27017", "--out=test.archive", "--quiet", "--authenticationDatabase=admin"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := argsBuilder(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("argsBuilder() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("argsBuilder() got = %v, want %v", got, tt.want)
			}
		})
	}
}
