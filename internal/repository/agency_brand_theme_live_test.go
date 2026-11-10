package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"

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

func TestLiveAgencyPortalThemeRoundTrip(t *testing.T) {
	_, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 385)
	ctx := context.Background()
	tag := uuid.New().String()[:8]
	owner, agency, older := uuid.New(), uuid.New(), uuid.New()
	host := "portal-" + tag + ".agency.test"
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}
	exec(`INSERT INTO users (id, first_name, last_name, email, password_hash) VALUES ($1, 'Theme', 'Live', $2, 'x')`, owner, "theme-"+tag+"@test.local")
	exec(`INSERT INTO agencies (id, name, owner_user_id) VALUES ($1, 'Theme agency', $2), ($3, 'Older agency', $2)`, agency, owner, older)
	// A row written the way the previous release writes it names none of the theme columns.
	exec(`INSERT INTO agency_brands (agency_id, display_name, accent_color, website_url, hide_powered_by, portal_sign_in, updated_at, updated_by)
	      VALUES ($1, 'Older', '', '', false, 'portal', now(), $2)`, older, owner)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM agencies WHERE owner_user_id = $1`, owner)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, owner)
	})
	repo := NewBrandRepository(pool)

	if b, err := repo.GetAgencyBrand(ctx, older); err != nil || b == nil || !b.Theme.Empty() || b.IconURL != "" || b.SignInImageURL != "" {
		t.Fatalf("previous-release row = %+v, %v", b, err)
	}

	theme := models.PortalTheme{Font: models.PortalFontSerif, SignInHeading: "Welcome back", SupportURL: "https://agency.test/help"}
	b, err := repo.UpsertAgencyBrand(ctx, agency, models.BrandInput{DisplayName: "Themed", Theme: &theme}, owner)
	if err != nil || b.Theme != theme || b.PortalSignIn != models.PortalSignInPortal {
		t.Fatalf("insert with theme = %+v, %v", b, err)
	}
	// Leaving the theme out keeps it; sending one replaces it whole.
	if b, err = repo.UpsertAgencyBrand(ctx, agency, models.BrandInput{DisplayName: "Renamed"}, owner); err != nil || b.Theme != theme || b.DisplayName != "Renamed" {
		t.Fatalf("absent theme = %+v, %v", b, err)
	}
	next := models.PortalTheme{Corners: models.PortalCornersSharp}
	if b, err = repo.UpsertAgencyBrand(ctx, agency, models.BrandInput{DisplayName: "Renamed", Theme: &next}, owner); err != nil || b.Theme != next {
		t.Fatalf("replaced theme = %+v, %v", b, err)
	}
	if b, err = repo.UpsertAgencyBrand(ctx, agency, models.BrandInput{DisplayName: "Renamed", Theme: &models.PortalTheme{}}, owner); err != nil || !b.Theme.Empty() {
		t.Fatalf("cleared theme = %+v, %v", b, err)
	}
	var stored string
	if err := pool.QueryRow(ctx, `SELECT theme::text FROM agency_brands WHERE agency_id = $1`, agency).Scan(&stored); err != nil || stored != "{}" {
		t.Fatalf("an empty theme is stored as %q, %v", stored, err)
	}

	// Each image has its own column, and a write to one leaves the others and the theme alone.
	for _, image := range []models.AgencyBrandImage{models.AgencyBrandLogo, models.AgencyBrandIcon, models.AgencyBrandSignInImage} {
		first, second := "https://cdn.test/"+string(image)+"-1.png", "https://cdn.test/"+string(image)+"-2.png"
		if prev, err := repo.SetAgencyImage(ctx, agency, image, first, owner); err != nil || prev != "" {
			t.Fatalf("%s first = %q, %v", image, prev, err)
		}
		if prev, err := repo.SetAgencyImage(ctx, agency, image, second, owner); err != nil || prev != first {
			t.Fatalf("%s second = %q, %v", image, prev, err)
		}
	}
	if _, err := repo.SetAgencyImage(ctx, agency, "theme", "https://cdn.test/x.png", owner); err == nil {
		t.Fatal("an unknown image name was written")
	}
	// An image set before any brand was saved creates the row.
	fresh := uuid.New()
	exec(`INSERT INTO agencies (id, name, owner_user_id) VALUES ($1, 'Fresh agency', $2)`, fresh, owner)
	if prev, err := repo.SetAgencyImage(ctx, fresh, models.AgencyBrandIcon, "https://cdn.test/fresh.png", owner); err != nil || prev != "" {
		t.Fatalf("icon on a new row = %q, %v", prev, err)
	}
	if b, err := repo.GetAgencyBrand(ctx, fresh); err != nil || b == nil || b.IconURL != "https://cdn.test/fresh.png" || !b.Theme.Empty() {
		t.Fatalf("new row = %+v, %v", b, err)
	}

	if _, err = repo.UpsertAgencyBrand(ctx, agency, models.BrandInput{DisplayName: "Renamed", Theme: &theme}, owner); err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE agency_brands SET portal_domain = $2, portal_domain_verified = true WHERE agency_id = $1`, agency, host)
	got, name, err := repo.VerifiedPortal(ctx, host)
	if err != nil || got == nil || name != "Theme agency" || got.Theme != theme ||
		got.LogoURL != "https://cdn.test/logo-2.png" || got.IconURL != "https://cdn.test/icon-2.png" || got.SignInImageURL != "https://cdn.test/sign_in_image-2.png" {
		t.Fatalf("verified portal = %+v, %q, %v", got, name, err)
	}

	// A theme that does not fit the type is read as none, not as a failed brand read.
	exec(`UPDATE agency_brands SET theme = '{"font": 5}'::jsonb WHERE agency_id = $1`, agency)
	if b, err := repo.GetAgencyBrand(ctx, agency); err != nil || b == nil || !b.Theme.Empty() || b.IconURL == "" {
		t.Fatalf("malformed theme = %+v, %v", b, err)
	}
}
