package handlers

import "testing"

func TestValidateDBExportOptions(t *testing.T) {
	tests := []struct {
		name         string
		format       string
		includeLogs  bool
		includeStats bool
		wantErr      bool
	}{
		{name: "plain JSON", format: "json"},
		{name: "plain ZIP", format: "zip"},
		{name: "ZIP with logs", format: "zip", includeLogs: true},
		{name: "ZIP with stats", format: "zip", includeStats: true},
		{name: "JSON with logs", format: "json", includeLogs: true, wantErr: true},
		{name: "JSON with stats", format: "json", includeStats: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDBExportOptions(tt.format, tt.includeLogs, tt.includeStats)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateDBExportOptions(%q, %t, %t) error = %v, wantErr %t", tt.format, tt.includeLogs, tt.includeStats, err, tt.wantErr)
			}
		})
	}
}
