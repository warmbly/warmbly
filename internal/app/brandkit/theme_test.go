package brandkit

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func fullTheme() models.PortalTheme {
	return models.PortalTheme{
		Font:             models.PortalFontGeometric,
		Corners:          models.PortalCornersRound,
		Appearance:       models.PortalAppearanceDark,
		SignInLayout:     models.PortalSignInLayoutCover,
		SignInBackground: models.PortalSignInBackgroundImage,
		SignInHeading:    "Welcome back",
		SignInMessage:    "Sign in to see how your campaigns are doing.",
		SupportEmail:     "help@northwind.test",
		SupportURL:       "https://northwind.test/help",
		TermsURL:         "https://northwind.test/terms",
		PrivacyURL:       "https://northwind.test/privacy",
	}
}

func TestNormalizeThemeAcceptsEveryListedValue(t *testing.T) {
	if out, xerr := NormalizeTheme(models.PortalTheme{}); xerr != nil || !out.Empty() {
		t.Fatalf("empty theme: %+v %v", out, xerr)
	}
	if out, xerr := NormalizeTheme(fullTheme()); xerr != nil || out != fullTheme() {
		t.Fatalf("full theme: %+v %v", out, xerr)
	}
	for _, th := range []models.PortalTheme{
		{Font: models.PortalFontSystem}, {Font: models.PortalFontGeometric}, {Font: models.PortalFontSerif},
		{Corners: models.PortalCornersSharp}, {Corners: models.PortalCornersRound},
		{Appearance: models.PortalAppearanceLight}, {Appearance: models.PortalAppearanceDark},
		{SignInLayout: models.PortalSignInLayoutSplit}, {SignInLayout: models.PortalSignInLayoutCover},
		{SignInBackground: models.PortalSignInBackgroundAccent}, {SignInBackground: models.PortalSignInBackgroundDark},
		{SignInBackground: models.PortalSignInBackgroundImage},
	} {
		if out, xerr := NormalizeTheme(th); xerr != nil || out != th {
			t.Errorf("NormalizeTheme(%+v) = %+v, %v", th, out, xerr)
		}
	}
	// Text and links are stored in their normal form.
	out, xerr := NormalizeTheme(models.PortalTheme{SignInHeading: "  Welcome   back ", SupportEmail: " help@northwind.test ", SupportURL: " https://northwind.test/help "})
	if xerr != nil || out.SignInHeading != "Welcome back" || out.SupportEmail != "help@northwind.test" || out.SupportURL != "https://northwind.test/help" {
		t.Fatalf("normal form: %+v %v", out, xerr)
	}
}

func TestNormalizeThemeRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		theme models.PortalTheme
		code  string
		names string
	}{
		"font":       {models.PortalTheme{Font: "comic"}, "invalid_theme", "theme.font"},
		"corners":    {models.PortalTheme{Corners: "pill"}, "invalid_theme", "theme.corners"},
		"appearance": {models.PortalTheme{Appearance: "auto"}, "invalid_theme", "theme.appearance"},
		"layout":     {models.PortalTheme{SignInLayout: "centered"}, "invalid_theme", "theme.sign_in_layout"},
		"background": {models.PortalTheme{SignInBackground: "gradient"}, "invalid_theme", "theme.sign_in_background"},
		"enum case":  {models.PortalTheme{Font: "Serif"}, "invalid_theme", "theme.font"},

		"heading too long":  {models.PortalTheme{SignInHeading: strings.Repeat("a", 61)}, "invalid_brand_text", "sign-in heading must be 60"},
		"heading link":      {models.PortalTheme{SignInHeading: "Visit https://evil.test"}, "invalid_brand_text", "sign-in heading cannot contain a link"},
		"heading address":   {models.PortalTheme{SignInHeading: "Go to evil.test now"}, "invalid_brand_text", "sign-in heading"},
		"heading markup":    {models.PortalTheme{SignInHeading: "<b>Welcome</b>"}, "invalid_brand_text", "sign-in heading contains characters"},
		"heading bidi":      {models.PortalTheme{SignInHeading: "Welcome‮back"}, "invalid_brand_text", "sign-in heading"},
		"heading invisible": {models.PortalTheme{SignInHeading: "Welcome​back"}, "invalid_brand_text", "sign-in heading"},
		"message too long":  {models.PortalTheme{SignInMessage: strings.Repeat("a", 201)}, "invalid_brand_text", "sign-in message must be 200"},
		"message email":     {models.PortalTheme{SignInMessage: "Write to help@evil.test"}, "invalid_brand_text", "sign-in message cannot contain"},
		"message control":   {models.PortalTheme{SignInMessage: "Line one\nline two"}, "invalid_brand_text", "sign-in message"},
		"message markup":    {models.PortalTheme{SignInMessage: "a > b"}, "invalid_brand_text", "sign-in message"},

		"support email":       {models.PortalTheme{SupportEmail: "not an address"}, "invalid_brand_link", "support email"},
		"support email named": {models.PortalTheme{SupportEmail: "Help <help@northwind.test>"}, "invalid_brand_link", "support email"},
		"support email host":  {models.PortalTheme{SupportEmail: "help@localhost"}, "invalid_brand_link", "support email"},
		"support url http":    {models.PortalTheme{SupportURL: "http://northwind.test/help"}, "invalid_brand_link", "support link"},
		"support url scheme":  {models.PortalTheme{SupportURL: "javascript:alert(1)"}, "invalid_brand_link", "support link"},
		"terms url bare":      {models.PortalTheme{TermsURL: "northwind.test/terms"}, "invalid_brand_link", "terms link"},
		"privacy url creds":   {models.PortalTheme{PrivacyURL: "https://user:pw@northwind.test"}, "invalid_brand_link", "privacy link"},
	} {
		_, xerr := NormalizeTheme(tc.theme)
		if xerr == nil || xerr.Identifier != tc.code || !strings.Contains(xerr.Message, tc.names) {
			t.Errorf("%s: got %v, want %s naming %q", name, xerr, tc.code, tc.names)
		}
	}
	// Sixty and two hundred are counted in characters, not bytes.
	if _, xerr := NormalizeTheme(models.PortalTheme{SignInHeading: strings.Repeat("é", 60), SignInMessage: strings.Repeat("é", 200)}); xerr != nil {
		t.Fatalf("at the limit: %v", xerr)
	}
}

// A stored value that fails a rule reads as unset, field by field.
func TestCleanThemeDropsOnlyWhatFails(t *testing.T) {
	stored := fullTheme()
	stored.Font, stored.SignInHeading, stored.SupportURL, stored.SupportEmail = "comic", "Visit evil.test", "http://northwind.test", "<help@northwind.test>"
	want := fullTheme()
	want.Font, want.SignInHeading, want.SupportURL, want.SupportEmail = "", "", "", ""
	if got := CleanTheme(stored); got != want {
		t.Fatalf("CleanTheme = %+v, want %+v", got, want)
	}
}

func TestComposeCarriesThemeOnlyFromTheAgencyBrand(t *testing.T) {
	agency := &models.AgencyBrand{
		AgencyID:       uuid.New(),
		BrandSettings:  models.BrandSettings{DisplayName: "Agency"},
		IconURL:        "https://cdn/icon.png",
		SignInImageURL: "https://cdn/sign-in.jpg",
		Theme:          fullTheme(),
	}
	got := Compose(deployment, nil, agency, false)
	if got.Source != SourceAgency || got.Brand.IconURL != agency.IconURL || got.Brand.SignInImageURL != agency.SignInImageURL ||
		got.Brand.Theme == nil || *got.Brand.Theme != fullTheme() {
		t.Fatalf("agency brand: %+v", got.Brand)
	}

	// A workspace's own brand replaces the agency's whole, theme included.
	ws := &models.BrandSettings{DisplayName: "Client Co"}
	if got := Compose(deployment, ws, agency, false).Brand; got.Theme != nil || got.IconURL != "" || got.SignInImageURL != "" {
		t.Fatalf("workspace brand carries the agency theme: %+v", got)
	}
	// The deployment's brand carries none either.
	unnamed := &models.AgencyBrand{AgencyID: uuid.New(), IconURL: "https://cdn/icon.png", Theme: fullTheme()}
	if got := Compose(deployment, nil, unnamed, false); got.Source != SourceDeployment || got.Brand.Theme != nil || got.Brand.IconURL != "" {
		t.Fatalf("deployment brand carries a theme: %+v", got.Brand)
	}

	// An agency with no theme sends none, and a stored value that fails a rule is not shown.
	agency.Theme = models.PortalTheme{}
	if got := Compose(deployment, nil, agency, false).Brand; got.Theme != nil {
		t.Fatalf("empty theme sent: %+v", got.Theme)
	}
	agency.Theme = models.PortalTheme{SignInHeading: "<script>", Font: "comic", Corners: models.PortalCornersSharp}
	if got := Compose(deployment, nil, agency, false).Brand; got.Theme == nil || *got.Theme != (models.PortalTheme{Corners: models.PortalCornersSharp}) {
		t.Fatalf("stored theme not cleaned: %+v", got.Theme)
	}
	agency.Theme = models.PortalTheme{SignInHeading: "<script>"}
	if got := Compose(deployment, nil, agency, false).Brand; got.Theme != nil {
		t.Fatalf("a theme with nothing left was sent: %+v", got.Theme)
	}
}

func TestBrandJSONOmitsAnUnsetTheme(t *testing.T) {
	raw, _ := json.Marshal(Brand{Name: "Agency"})
	if string(raw) != `{"name":"Agency"}` {
		t.Fatalf("plain brand = %s", raw)
	}
	theme := models.PortalTheme{Font: models.PortalFontSerif}
	raw, _ = json.Marshal(Brand{Name: "Agency", IconURL: "https://cdn/icon.png", SignInImageURL: "https://cdn/s.jpg", Theme: &theme})
	if string(raw) != `{"name":"Agency","icon_url":"https://cdn/icon.png","sign_in_image_url":"https://cdn/s.jpg","theme":{"font":"serif"}}` {
		t.Fatalf("themed brand = %s", raw)
	}
	if raw, _ := json.Marshal(models.PortalTheme{}); string(raw) != `{}` {
		t.Fatalf("empty theme stores as %s", raw)
	}
}

// themeRepo is one agency's brand row, in memory.
type themeRepo struct {
	repository.BrandRepository
	brand   *models.AgencyBrand
	role    models.AgencyRole
	lookups int
}

func (r *themeRepo) AgencyRole(context.Context, uuid.UUID, uuid.UUID) (models.AgencyRole, error) {
	return r.role, nil
}

func (r *themeRepo) GetAgencyBrand(context.Context, uuid.UUID) (*models.AgencyBrand, error) {
	return r.brand, nil
}

func (r *themeRepo) UpsertAgencyBrand(_ context.Context, agencyID uuid.UUID, in models.BrandInput, _ uuid.UUID) (*models.AgencyBrand, error) {
	if r.brand == nil {
		r.brand = &models.AgencyBrand{AgencyID: agencyID, PortalSignIn: models.PortalSignInPortal}
	}
	r.brand.DisplayName, r.brand.AccentColor, r.brand.WebsiteURL, r.brand.HidePoweredBy = in.DisplayName, in.AccentColor, in.WebsiteURL, in.HidePoweredBy
	if in.PortalSignIn != nil {
		r.brand.PortalSignIn = *in.PortalSignIn
	}
	if in.Theme != nil {
		r.brand.Theme = *in.Theme
	}
	return r.brand, nil
}

func (r *themeRepo) SetAgencyImage(_ context.Context, agencyID uuid.UUID, image models.AgencyBrandImage, url string, _ uuid.UUID) (string, error) {
	if r.brand == nil {
		r.brand = &models.AgencyBrand{AgencyID: agencyID}
	}
	field := map[models.AgencyBrandImage]*string{
		models.AgencyBrandLogo: &r.brand.LogoURL, models.AgencyBrandIcon: &r.brand.IconURL, models.AgencyBrandSignInImage: &r.brand.SignInImageURL,
	}[image]
	previous := *field
	*field = url
	return previous, nil
}

func (r *themeRepo) VerifiedPortal(_ context.Context, host string) (*models.AgencyBrand, string, error) {
	r.lookups++
	if r.brand == nil || r.brand.PortalDomain != host || !r.brand.PortalDomainVerified {
		return nil, "", nil
	}
	b := *r.brand
	return &b, "Northwind Growth", nil
}

func themeService(t *testing.T, repo *themeRepo) *Service {
	t.Helper()
	t.Setenv("BILLING_PROVIDER", "none")
	t.Setenv("PORTAL_DOMAIN", "portal.warmbly.test")
	s := NewService(repo, NewDBResolver(&fakeStore{}, nil), nil, nil)
	s.WirePortals(NewPortals(repo))
	return s
}

func TestUpdateAgencyThemeIsReplacedWholeOrKept(t *testing.T) {
	agency, user := uuid.New(), uuid.New()
	repo := &themeRepo{role: models.AgencyRoleOwner}
	s := themeService(t, repo)
	ctx := context.Background()

	// A brand with no theme answers an empty object, never null.
	view, xerr := s.UpdateAgency(ctx, agency, user, models.BrandInput{DisplayName: "Northwind"})
	if xerr != nil || !view.Theme.Empty() {
		t.Fatalf("first save: %+v %v", view, xerr)
	}
	raw, _ := json.Marshal(view)
	var shape map[string]json.RawMessage
	_ = json.Unmarshal(raw, &shape)
	if string(shape["theme"]) != `{}` || string(shape["icon_url"]) != `""` || string(shape["sign_in_image_url"]) != `""` {
		t.Fatalf("view shape: theme=%s icon_url=%s sign_in_image_url=%s", shape["theme"], shape["icon_url"], shape["sign_in_image_url"])
	}

	full := fullTheme()
	warmbly := models.PortalSignInWarmbly
	view, xerr = s.UpdateAgency(ctx, agency, user, models.BrandInput{DisplayName: "Northwind", Theme: &full, PortalSignIn: &warmbly})
	if xerr != nil || view.Theme != full || view.PortalSignIn != models.PortalSignInWarmbly {
		t.Fatalf("theme save: %+v %v", view, xerr)
	}
	// Leaving the theme and the sign-in choice out keeps both.
	view, xerr = s.UpdateAgency(ctx, agency, user, models.BrandInput{DisplayName: "Northwind Growth"})
	if xerr != nil || view.Theme != full || view.PortalSignIn != models.PortalSignInWarmbly || view.Brand.DisplayName != "Northwind Growth" {
		t.Fatalf("absent theme: %+v %v", view, xerr)
	}
	// A theme in the body replaces every field, so one it leaves out is cleared.
	view, xerr = s.UpdateAgency(ctx, agency, user, models.BrandInput{DisplayName: "Northwind Growth", Theme: &models.PortalTheme{Corners: models.PortalCornersSharp}})
	if xerr != nil || view.Theme != (models.PortalTheme{Corners: models.PortalCornersSharp}) {
		t.Fatalf("replaced theme: %+v %v", view, xerr)
	}
	// A refused theme stores nothing.
	if _, xerr := s.UpdateAgency(ctx, agency, user, models.BrandInput{DisplayName: "Other", Theme: &models.PortalTheme{Font: "comic"}}); xerr == nil || xerr.Identifier != "invalid_theme" {
		t.Fatalf("invalid theme: %v", xerr)
	}
	if repo.brand.DisplayName != "Northwind Growth" || repo.brand.Theme.Corners != models.PortalCornersSharp {
		t.Fatalf("a refused write changed the brand: %+v", repo.brand)
	}

	// Staff who cannot manage the agency change neither the theme nor an image.
	repo.role = models.AgencyRoleMember
	if _, xerr := s.UpdateAgency(ctx, agency, user, models.BrandInput{Theme: &full}); xerr == nil || xerr.Identifier != "agency_forbidden" {
		t.Fatalf("member theme write: %v", xerr)
	}
	if _, xerr := s.SetAgencyImage(ctx, agency, user, models.AgencyBrandIcon, "https://cdn/icon.png"); xerr == nil || xerr.Identifier != "agency_forbidden" {
		t.Fatalf("member icon write: %v", xerr)
	}
}

func TestSetAgencyImageReturnsWhatItReplaced(t *testing.T) {
	agency, user := uuid.New(), uuid.New()
	repo := &themeRepo{role: models.AgencyRoleAdmin}
	s := themeService(t, repo)
	ctx := context.Background()
	for _, image := range []models.AgencyBrandImage{models.AgencyBrandLogo, models.AgencyBrandIcon, models.AgencyBrandSignInImage} {
		first, second := "https://cdn/"+string(image)+"-1.png", "https://cdn/"+string(image)+"-2.png"
		if prev, xerr := s.SetAgencyImage(ctx, agency, user, image, first); xerr != nil || prev != "" {
			t.Fatalf("%s first: %q %v", image, prev, xerr)
		}
		if prev, xerr := s.SetAgencyImage(ctx, agency, user, image, second); xerr != nil || prev != first {
			t.Fatalf("%s second: %q %v", image, prev, xerr)
		}
	}
	view, xerr := s.Agency(ctx, agency, user)
	if xerr != nil || view.Brand.LogoURL != "https://cdn/logo-2.png" || view.IconURL != "https://cdn/icon-2.png" || view.SignInImageURL != "https://cdn/sign_in_image-2.png" {
		t.Fatalf("view: %+v %v", view, xerr)
	}
	if prev, xerr := s.SetAgencyImage(ctx, agency, user, models.AgencyBrandIcon, ""); xerr != nil || prev != "https://cdn/icon-2.png" || repo.brand.IconURL != "" {
		t.Fatalf("remove icon: %q %v", prev, xerr)
	}
}

// The portal carries the agency's icon, sign-in image and theme whether or not the agency named or coloured its brand,
// and a theme or image write reaches a portal that was already cached.
func TestPortalViewCarriesThemeOnBothPaths(t *testing.T) {
	const host = "portal.northwind.test"
	agency, user := uuid.New(), uuid.New()
	repo := &themeRepo{role: models.AgencyRoleOwner, brand: &models.AgencyBrand{
		AgencyID: agency, PortalDomain: host, PortalDomainVerified: true, PortalSignIn: models.PortalSignInPortal,
	}}
	s := themeService(t, repo)
	ctx := context.Background()

	view, err := s.PortalView(ctx, host, false)
	if err != nil || !view.Portal || view.Brand.Name != "Northwind Growth" || view.Brand.Theme != nil || view.Brand.IconURL != "" {
		t.Fatalf("unthemed portal: %+v %v", view.Brand, err)
	}

	// The agency set no name, logo, accent or website: the fallback brand still carries the theme.
	full := fullTheme()
	if _, xerr := s.UpdateAgency(ctx, agency, user, models.BrandInput{Theme: &full}); xerr != nil {
		t.Fatal(xerr)
	}
	if _, xerr := s.SetAgencyImage(ctx, agency, user, models.AgencyBrandIcon, "https://cdn/icon.png"); xerr != nil {
		t.Fatal(xerr)
	}
	view, err = s.PortalView(ctx, host, false)
	if err != nil || view.Brand.Name != "Northwind Growth" || view.Brand.Theme == nil || *view.Brand.Theme != full ||
		view.Brand.IconURL != "https://cdn/icon.png" || view.Brand.SignInImageURL != "" || view.Brand.PoweredBy == nil && Default().PoweredBy != nil {
		t.Fatalf("fallback path: %+v %v", view.Brand, err)
	}
	if _, xerr := s.SetAgencyImage(ctx, agency, user, models.AgencyBrandSignInImage, "https://cdn/sign-in.jpg"); xerr != nil {
		t.Fatal(xerr)
	}
	if view, _ = s.PortalView(ctx, host, false); view.Brand.SignInImageURL != "https://cdn/sign-in.jpg" {
		t.Fatalf("a sign-in image write did not reach the cached portal: %+v", view.Brand)
	}

	// With a named brand the composed path carries the same three.
	if _, xerr := s.UpdateAgency(ctx, agency, user, models.BrandInput{DisplayName: "Northwind", AccentColor: "#0f766e"}); xerr != nil {
		t.Fatal(xerr)
	}
	view, err = s.PortalView(ctx, host, false)
	if err != nil || view.Brand.Name != "Northwind" || view.Brand.AccentColor != "#0f766e" || view.Brand.Theme == nil || *view.Brand.Theme != full ||
		view.Brand.IconURL != "https://cdn/icon.png" || view.Brand.SignInImageURL != "https://cdn/sign-in.jpg" {
		t.Fatalf("composed path: %+v %v", view.Brand, err)
	}

	// A stored value that fails a rule is not shown on the portal.
	repo.brand.Theme.SignInHeading = "Visit evil.test"
	s.Portals().Invalidate()
	if view, _ = s.PortalView(ctx, host, false); view.Brand.Theme == nil || view.Brand.Theme.SignInHeading != "" || view.Brand.Theme.SignInMessage != full.SignInMessage {
		t.Fatalf("stored heading not cleaned: %+v", view.Brand.Theme)
	}
}
