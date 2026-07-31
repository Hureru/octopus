package handlers

import "testing"

func TestValidateDBExportOptions(t *testing.T) {
	tests := []struct {
		name        string
		format      string
		includeLogs bool
		wantErr     bool
	}{
		{name: "plain JSON", format: "json"},
		{name: "plain ZIP", format: "zip"},
		{name: "ZIP with logs", format: "zip", includeLogs: true},
		{name: "JSON with logs", format: "json", includeLogs: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDBExportOptions(tt.format, tt.includeLogs)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateDBExportOptions(%q, %t) error = %v, wantErr %t", tt.format, tt.includeLogs, err, tt.wantErr)
			}
		})
	}
}
