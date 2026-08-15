package sessioncontrol

import "testing"

func TestStreamIDLess(t *testing.T) {
	tests := []struct {
		name        string
		left, right string
		want        bool
	}{
		{name: "empty cursor", left: "", right: "10-0", want: true},
		{name: "milliseconds", left: "9-9", right: "10-0", want: true},
		{name: "sequence", left: "10-1", right: "10-2", want: true},
		{name: "equal", left: "10-2", right: "10-2", want: false},
		{name: "newer", left: "11-0", right: "10-9", want: false},
		{name: "missing right", left: "10-0", right: "", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := streamIDLess(test.left, test.right); got != test.want {
				t.Fatalf("streamIDLess(%q, %q) = %t, want %t", test.left, test.right, got, test.want)
			}
		})
	}
}
