package state

import (
	"testing"
)

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"My Task", "my-task"},
		{"hello world 123", "hello-world-123"},
		{"---", "unnamed"},
		{"", "unnamed"},
		{"UPPERCASE", "uppercase"},
		{"a!@#b", "a-b"},
	}

	for _, tt := range tests {
		got := SanitizeName(tt.input)
		if got != tt.expected {
			t.Errorf("SanitizeName(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
