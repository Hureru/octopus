package cmd

import "testing"

func TestShouldRunDesktopByDefault(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "installed desktop executable", args: []string{`C:\\Program Files\\Octopus\\octopus-desktop.exe`}, want: true},
		{name: "release desktop executable", args: []string{"./octopus-desktop-x86_64.exe"}, want: true},
		{name: "explicit desktop command", args: []string{"octopus-desktop.exe", "desktop"}, want: false},
		{name: "server executable", args: []string{"octopus.exe"}, want: false},
		{name: "missing argv", args: nil, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldRunDesktopByDefault(test.args); got != test.want {
				t.Fatalf("shouldRunDesktopByDefault(%q) = %v, want %v", test.args, got, test.want)
			}
		})
	}
}

func TestNormalizeAddrPort(t *testing.T) {
	tests := map[string]string{
		"127.0.0.1:8080":   ":8080",
		"[::1]:8080":       ":8080",
		"::1:8080":         ":8080",
		"2001:db8::1:9090": ":9090",
		":8080":            ":8080",
		"8080":             ":8080",
	}
	for input, want := range tests {
		if got := normalizeAddrPort(input); got != want {
			t.Errorf("normalizeAddrPort(%q) = %q, want %q", input, got, want)
		}
	}
}
