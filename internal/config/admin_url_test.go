package config

import "testing"

func TestAdminURL(t *testing.T) {
	for _, tt := range []struct{ env, cors, want string }{
		{"https://ops.example.com/", "https://admin.example.com", "https://ops.example.com"},
		{"", "https://app.example.com, https://admin.example.com", "https://admin.example.com"},
		{"", "http://65.0.0.1:5173,http://65.0.0.1:5174", "http://65.0.0.1:5174"},
		{"", "https://app.example.com", ""},
		{"", "*", ""},
	} {
		t.Setenv("ADMIN_URL", tt.env)
		t.Setenv("CORS_ALLOW_ORIGINS", tt.cors)
		if got := AdminURL(); got != tt.want {
			t.Errorf("AdminURL(%q, %q) = %q, want %q", tt.env, tt.cors, got, tt.want)
		}
	}
}
