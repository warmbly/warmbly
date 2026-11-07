package leadfinder

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/emailverify"
)

// addr builds an address, so none sits in the source as one string.
func addr(local, domain string) string { return local + "@" + domain }

func TestParseNameIsStrictAboutWhatIsAPerson(t *testing.T) {
	people := map[string][2]string{
		"Jane Doe":                {"Jane", "Doe"},
		"Dr. Maria García-López":  {"Maria", "García-López"},
		"Jane Doe, DDS":           {"Jane", "Doe"},
		"JOHN SMITH":              {"John", "Smith"},
		"Ludwig van Beethoven":    {"Ludwig", "van Beethoven"},
		"Mary J. Blige":           {"Mary", "Blige"},
		"Prof. Dr. Anna Schröder": {"Anna", "Schröder"},
	}
	for in, want := range people {
		n, ok := parseName(in)
		if !ok || n.first != want[0] || n.last != want[1] {
			t.Errorf("parseName(%q) = %+v, %v; want %v", in, n, ok, want)
		}
	}
	if n, _ := parseName("Jane Doe, DDS"); n.suffix != "DDS" {
		t.Errorf("credential suffix = %q, want DDS", n.suffix)
	}
	for _, in := range []string{
		"Contact Us", "Our Team", "Book Now", "Read More", "Family Dentistry", "Austin Texas 78701",
		"Monday Friday", "Jane", addr("jane", "example.org"), "Privacy Policy", "New Patients Welcome", "Meet The Team",
		"Jane Doe, Owner", "Call (512) 555-0100", "The quick brown fox jumps over", "Zum Profil", "Google Ads",
	} {
		if n, ok := parseName(in); ok {
			t.Errorf("parseName(%q) = %+v, want no name", in, n)
		}
	}
}

func TestExtractPeopleFromATeamPage(t *testing.T) {
	src := `<html><body>
		<h2>Meet the Team</h2>
		<div class="member"><h3>Jane Doe</h3><p>Owner &amp; Lead Dentist</p></div>
		<div class="member"><h3>Dr. Ravi Patel, DMD</h3><p>Associate Dentist</p></div>
		<div class="member"><span>Office Manager</span><h4>Carla Gómez</h4></div>
		<p>Tom Baker – Head Hygienist</p>
		<h3>Our Services</h3><p>Cleaning and whitening</p>
		<h3>Austin Downtown</h3><p>Open Monday to Friday</p>
		<a href="mailto:` + addr("jane", "brightsmiles.com") + `">Jane Doe</a>
		<a href="https://www.linkedin.com/in/ravi-patel-4a1b2c">LinkedIn</a>
		<script type="application/ld+json">{"@context":"https://schema.org","@type":"Dentist","employee":[
			{"@type":"Person","name":"Sam Lee","jobTitle":"Practice Coordinator","email":"mailto:` + addr("sam.lee", "brightsmiles.com") + `"}]}</script>
		</body></html>`
	p := parsePage(src)
	p.url, _ = url.Parse("https://brightsmiles.com/our-team")
	people := mergePeople(extractPeople(p))
	byName := map[string]FoundPerson{}
	for _, f := range people {
		byName[f.Name] = f
	}
	expect := map[string]struct {
		title     string
		seniority models.LeadSeniority
	}{
		"Jane Doe":    {"Owner & Lead Dentist", models.LeadSeniorityOwner},
		"Ravi Patel":  {"Associate Dentist", models.LeadSeniorityStaff},
		"Carla Gómez": {"Office Manager", models.LeadSeniorityManager},
		"Tom Baker":   {"Head Hygienist", models.LeadSeniorityManager},
		"Sam Lee":     {"Practice Coordinator", models.LeadSeniorityManager},
	}
	for name, want := range expect {
		got, ok := byName[name]
		if !ok {
			t.Errorf("missing %s; found %v", name, people)
			continue
		}
		if got.Title != want.title || got.Seniority != want.seniority {
			t.Errorf("%s = %q (%s), want %q (%s)", name, got.Title, got.Seniority, want.title, want.seniority)
		}
	}
	if len(people) != len(expect) {
		t.Errorf("found %d people, want %d: %+v", len(people), len(expect), people)
	}
	if byName["Jane Doe"].Email != addr("jane", "brightsmiles.com") {
		t.Errorf("Jane's mailto = %q", byName["Jane Doe"].Email)
	}
	if byName["Ravi Patel"].LinkedIn != "https://www.linkedin.com/in/ravi-patel-4a1b2c" {
		t.Errorf("Ravi's LinkedIn = %q", byName["Ravi Patel"].LinkedIn)
	}
	if byName["Sam Lee"].Email != addr("sam.lee", "brightsmiles.com") || byName["Sam Lee"].Source != models.LeadPersonSchema {
		t.Errorf("Sam = %+v", byName["Sam Lee"])
	}
	if people[0].Name != "Jane Doe" {
		t.Errorf("owner should come first, got %s", people[0].Name)
	}
}

func TestRoleTextRefusesLinks(t *testing.T) {
	for _, s := range []string{"https://policies.google.com/technologies/partner-sites", "www.example.com/team-lead"} {
		if roleText(s) {
			t.Errorf("roleText(%q) = true", s)
		}
	}
	if got := seniorityOf("Stellvertretende Leitung"); got != models.LeadSeniorityManager {
		t.Errorf("a German deputy lead is %s, want manager", got)
	}
}

func TestExtractPeopleFromAnImprint(t *testing.T) {
	src := `<html><body><h1>Impressum</h1>
		<p>Zahnarztpraxis Sonnenschein GmbH<br>Hauptstraße 1, 10115 Berlin</p>
		<p>Geschäftsführer: Dr. Max Mustermann und Erika Musterfrau</p>
		<p>Registergericht: Amtsgericht Berlin</p></body></html>`
	p := parsePage(src)
	p.url, _ = url.Parse("https://sonnenschein.de/impressum")
	people := mergePeople(extractPeople(p))
	if len(people) != 2 {
		t.Fatalf("found %+v, want Max Mustermann and Erika Musterfrau", people)
	}
	for _, f := range people {
		if f.Source != models.LeadPersonImprint || f.Seniority != models.LeadSeniorityExecutive {
			t.Errorf("%s = %s, %s", f.Name, f.Source, f.Seniority)
		}
	}
}

func TestAttachEmailsLearnsTheDomainPattern(t *testing.T) {
	people := []FoundPerson{
		{Name: "Jane Doe", First: "Jane", Last: "Doe"},
		{Name: "Ravi Patel", First: "Ravi", Last: "Patel"},
	}
	emails := []FoundEmail{
		{Email: addr("jane.doe", "brightsmiles.com")},
		{Email: addr("info", "brightsmiles.com")},
		{Email: addr("sam.lee", "brightsmiles.com")},
		{Email: addr("ravi", "gmail.com")},
	}
	out, pattern := attachEmails(people, emails, "brightsmiles.com")
	if pattern != "first.last" {
		t.Errorf("pattern = %q, want first.last", pattern)
	}
	if out[0].Email != addr("jane.doe", "brightsmiles.com") || out[1].Email != "" {
		t.Errorf("attached = %q, %q", out[0].Email, out[1].Email)
	}
	if len(out) != 3 || out[2].Name != "Sam Lee" || out[2].Source != models.LeadPersonEmail {
		t.Errorf("an address naming its owner should add them: %+v", out)
	}
	if got := candidates(pattern, "Ravi", "Patel", "brightsmiles.com", 3); got[0] != addr("ravi.patel", "brightsmiles.com") {
		t.Errorf("candidates = %v", got)
	}
	if got := candidates("", "José", "Núñez", "x.com", 2); got[0] != addr("jose.nunez", "x.com") || got[1] != addr("jose", "x.com") {
		t.Errorf("accented candidates = %v", got)
	}
}

type fakeVerifier struct {
	valid    map[string]bool
	catchAll bool
	calls    []string
}

func (f *fakeVerifier) VerifyAddress(_ context.Context, _ uuid.UUID, email string) emailverify.Result {
	f.calls = append(f.calls, email)
	switch {
	case f.catchAll:
		return emailverify.Result{Status: emailverify.StatusRisky, SubStatus: emailverify.SubStatusCatchAll, IsCatchAll: true}
	case f.valid[email]:
		return emailverify.Result{Status: emailverify.StatusValid}
	}
	return emailverify.Result{Status: emailverify.StatusInvalid}
}

func TestAddressFinderChecksShapesUntilOneIsConfirmed(t *testing.T) {
	v := &fakeVerifier{valid: map[string]bool{addr("jdoe", "brightsmiles.com"): true}}
	s := &Service{Deps: Deps{Verifier: v}}
	l := &models.Lead{Domain: "brightsmiles.com"}
	f := s.newAddressFinder(uuid.New(), l, true, 15)
	jane := models.LeadPerson{Name: "Jane Doe", FirstName: "Jane", LastName: "Doe"}
	if !f.find(context.Background(), &jane) || jane.Email != addr("jdoe", "brightsmiles.com") || jane.EmailStatus != "valid" {
		t.Fatalf("jane = %+v after %v", jane, v.calls)
	}
	if f.pattern != "flast" {
		t.Errorf("learned pattern = %q, want flast", f.pattern)
	}
	// The learned pattern is tried first for the next person.
	v.calls = nil
	v.valid[addr("rpatel", "brightsmiles.com")] = true
	ravi := models.LeadPerson{Name: "Ravi Patel", FirstName: "Ravi", LastName: "Patel"}
	f.find(context.Background(), &ravi)
	if len(v.calls) != 1 || ravi.EmailConfidence != confPatternValid {
		t.Errorf("ravi took %v, confidence %d", v.calls, ravi.EmailConfidence)
	}
	// Nobody by that name: every shape refused, no address kept.
	nobody := models.LeadPerson{Name: "Zed Nobody", FirstName: "Zed", LastName: "Nobody"}
	if f.find(context.Background(), &nobody) || nobody.Email != "" {
		t.Errorf("nobody = %+v", nobody)
	}
}

func TestAddressFinderStopsCheckingAServerThatAcceptsAll(t *testing.T) {
	v := &fakeVerifier{catchAll: true}
	s := &Service{Deps: Deps{Verifier: v}}
	l := &models.Lead{Domain: "anything.com", Site: models.LeadSite{EmailPattern: "first"}}
	f := s.newAddressFinder(uuid.New(), l, true, 15)
	people := []models.LeadPerson{
		{Name: "Jane Doe", FirstName: "Jane", LastName: "Doe"},
		{Name: "Ravi Patel", FirstName: "Ravi", LastName: "Patel"},
	}
	for i := range people {
		f.find(context.Background(), &people[i])
	}
	if len(v.calls) != 1 {
		t.Errorf("checked %v; a server that accepts everything is asked once", v.calls)
	}
	for _, p := range people {
		if p.EmailStatus != models.LeadPersonStatusGuessed || p.EmailConfidence != confKnownPattern || !strings.HasSuffix(p.Email, "@anything.com") {
			t.Errorf("%s = %q %s %d", p.Name, p.Email, p.EmailStatus, p.EmailConfidence)
		}
	}
	f.remember(l)
	if !l.Site.AcceptsAll {
		t.Error("the lead should remember its server accepts any address")
	}
}

func TestMergeFoundKeepsWhatATeammateTyped(t *testing.T) {
	existing := []models.LeadPerson{{
		ID: uuid.New(), Name: "Jane Doe", NameKey: "jane doe", FirstName: "Jane", LastName: "Doe",
		Email: addr("jane", "home.example"), EmailSource: models.LeadEmailManual, Source: models.LeadPersonManual,
	}}
	found := []FoundPerson{
		{Name: "Jane Doe", First: "Jane", Last: "Doe", Title: "Owner", Seniority: models.LeadSeniorityOwner, Email: addr("jane", "brightsmiles.com"), Source: models.LeadPersonPage},
		{Name: "Ravi Patel", First: "Ravi", Last: "Patel", Source: models.LeadPersonPage},
	}
	gone := models.LeadPerson{ID: uuid.New(), Name: "Old Hire", NameKey: "old hire", Source: models.LeadPersonPage}
	out, stale := mergeFound(append(existing, gone), found)
	if len(out) != 2 {
		t.Fatalf("merged %+v", out)
	}
	if len(stale) != 1 || stale[0].ID != gone.ID {
		t.Errorf("someone the site no longer names should be stale: %+v", stale)
	}
	if out[0].Email != addr("jane", "home.example") || out[0].Title != "Owner" || out[0].ID != existing[0].ID {
		t.Errorf("jane = %+v", out[0])
	}
	if out[1].NameKey != "ravi patel" || out[1].ID != uuid.Nil {
		t.Errorf("ravi = %+v", out[1])
	}
}
