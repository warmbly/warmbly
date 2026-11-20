package repository

import (
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

// A stored theme that does not fit the type reads as no theme, and an absent one binds as NULL so the row keeps its own.
func TestPortalThemeColumnCodec(t *testing.T) {
	for _, raw := range []string{"", "{}", "null", `{"font": 5}`, `[]`, `not json`} {
		if got := decodePortalTheme([]byte(raw)); !got.Empty() {
			t.Errorf("decodePortalTheme(%q) = %+v", raw, got)
		}
	}
	if got := decodePortalTheme([]byte(`{"font":"serif","sign_in_heading":"Welcome","later_field":true}`)); got.Font != models.PortalFontSerif || got.SignInHeading != "Welcome" {
		t.Fatalf("decode = %+v", got)
	}
	if raw, err := portalThemeParam(nil); err != nil || raw != nil {
		t.Fatalf("absent theme binds %q, %v", raw, err)
	}
	if raw, err := portalThemeParam(&models.PortalTheme{}); err != nil || string(raw) != "{}" {
		t.Fatalf("empty theme binds %q, %v", raw, err)
	}
	if raw, err := portalThemeParam(&models.PortalTheme{Corners: models.PortalCornersRound}); err != nil || string(raw) != `{"corners":"round"}` {
		t.Fatalf("theme binds %q, %v", raw, err)
	}
}
