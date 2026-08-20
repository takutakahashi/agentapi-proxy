package proxybinary

import "testing"

func TestFromMap(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "default", env: nil, want: Default},
		{name: "configured path", env: map[string]string{EnvName: "/opt/ccplant/bin/ccplant"}, want: "/opt/ccplant/bin/ccplant"},
		{name: "blank uses default", env: map[string]string{EnvName: "  "}, want: Default},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FromMap(tt.env); got != tt.want {
				t.Fatalf("FromMap() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	if got := Resolve(""); got != "ccplant" {
		t.Fatalf("Resolve() = %q, want ccplant", got)
	}
}

func TestShellReference(t *testing.T) {
	if got, want := ShellReference(), `"${CCPLANT_BINARY_PATH:-ccplant}"`; got != want {
		t.Fatalf("ShellReference() = %q, want %q", got, want)
	}
}
