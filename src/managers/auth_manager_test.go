package managers

import "testing"

func TestValidEmail(t *testing.T) {
	tests := []struct {
		value string
		valid bool
	}{
		{value: "alice@example.com", valid: true},
		{value: "alice+cloud@example.com", valid: true},
		{value: "", valid: false},
		{value: "alice", valid: false},
		{value: "Alice <alice@example.com>", valid: false},
	}

	for _, test := range tests {
		if got := validEmail(test.value); got != test.valid {
			t.Errorf("validEmail(%q) = %t, want %t", test.value, got, test.valid)
		}
	}
}
