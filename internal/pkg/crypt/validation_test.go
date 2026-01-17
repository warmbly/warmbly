package crypt

import "testing"

func TestIsValidUUID(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		// Valid UUIDs (v1, v4, etc.)
		{"550e8400-e29b-41d4-a716-446655440000", true},
		{"123e4567-e89b-12d3-a456-426614174000", true},

		// Invalid: wrong format or missing parts
		{"550e8400e29b41d4a716446655440000", false},      // missing dashes
		{"550e8400-e29b-41d4-a716-44665544000", false},   // too short
		{"550e8400-e29b-41d4-a716-4466554400000", false}, // too long
		{"zzze8400-e29b-41d4-a716-446655440000", false},  // invalid hex
		{"", false},           // empty string
		{"not-a-uuid", false}, // random string
	}

	for _, tt := range tests {
		got := IsValidUUID(tt.input)
		if got != tt.want {
			t.Errorf("IsValidUUID(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestIsValidHexColor(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		// ✅ Valid short hex codes
		{"#000", true},
		{"#fff", true},
		{"#abc", true},
		{"#ABC", true},

		// ✅ Valid long hex codes
		{"#000000", true},
		{"#FFFFFF", true},
		{"#123456", true},
		{"#AaBbCc", true},

		// ❌ Invalid ones
		{"000000", false},
		{"#12", false},
		{"#1234", false},
		{"#12345", false},
		{"#1234567", false},
		{"#ZZZ", false},
		{"#12G", false},
		{"#123456789", false},
		{"#", false},
		{"", false},
	}

	for _, tt := range tests {
		got := IsValidHexColor(tt.input)
		if got != tt.want {
			t.Errorf("IsValidHexColor(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
