package handlers

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMaskPhone(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"   ", ""},
		{"9876543210", "••••••3210"},
		{"+91 98765 43210", "••••••3210"},
		{"12345", "••••"},
		{"+91-9876543210", "••••••3210"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.in), func(t *testing.T) {
			assert.Equal(t, tt.want, maskPhone(tt.in))
		})
	}
}

func TestMaskEmail(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"  ", ""},
		{"amit.patel@example.com", "a***@example.com"},
		{"x@y.co", "x***@y.co"},
		{"no-at-sign", "•••"},
		{"@example.com", "•••"},
		{"user@", "•••"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.in), func(t *testing.T) {
			assert.Equal(t, tt.want, maskEmail(tt.in))
		})
	}
}
