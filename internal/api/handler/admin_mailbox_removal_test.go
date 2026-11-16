package handler

import (
	"reflect"
	"testing"
)

func TestAdminRemovalDomains(t *testing.T) {
	got, ok := adminRemovalDomains([]string{" Example.COM ", "@example.com", "mail.example.org."})
	if !ok || !reflect.DeepEqual(got, []string{"example.com", "mail.example.org"}) {
		t.Fatalf("normalised = %v, %v", got, ok)
	}
	for _, bad := range [][]string{
		nil,
		{""},
		{"localhost"},
		{"a@b.com"},
		{"%.com"},
		{"ex_ample.com"},
		{".com"},
		{"a..com"},
		{"gmail.com"},
		{"example.com", "Outlook.com"},
		make([]string, adminRemovalMaxDomains+1),
	} {
		if got, ok := adminRemovalDomains(bad); ok {
			t.Errorf("%q accepted as %v", bad, got)
		}
	}
}
