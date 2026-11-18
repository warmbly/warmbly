package leadreview

import (
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

func TestCursorRoundTrip(t *testing.T) {
	in := models.CampaignReviewCursor{Email: "a+b@example.com", ContactID: uuid.New()}
	out, err := DecodeCursor(EncodeCursor(in))
	if err != nil || *out != in {
		t.Fatalf("round trip = %+v, %v", out, err)
	}
	for _, bad := range []string{"%%%", "bm90LWEtY3Vyc29y", EncodeCursor(in) + "x"} {
		if _, err := DecodeCursor(bad); err == nil {
			t.Errorf("DecodeCursor(%q) accepted a cursor it did not issue", bad)
		}
	}
}

func TestGapsSkipFieldsWithAFallback(t *testing.T) {
	steps := []models.Sequence{
		{Subject: "Quick question, {{.FirstName}}", BodyHTML: `<p>{{if .Company}}{{.Company}}{{end}}</p>`},
		{Kind: "email", BodyPlain: `{{.Phone | default "you"}}`},
	}
	got := gaps(emailSteps(steps))
	if len(got) != 1 || got[0] != "FirstName" {
		t.Fatalf("gaps = %v, want [FirstName]", got)
	}
}

func TestCheckBatch(t *testing.T) {
	id := uuid.New()
	cases := []struct {
		ids  []uuid.UUID
		all  bool
		fail bool
	}{
		{nil, true, false},
		{[]uuid.UUID{id}, false, false},
		{nil, false, true},
		{[]uuid.UUID{id}, true, true},
		{make([]uuid.UUID, MaxBatch+1), false, true},
	}
	for i, c := range cases {
		if got := checkBatch(c.ids, c.all) != nil; got != c.fail {
			t.Errorf("case %d: refused = %v, want %v", i, got, c.fail)
		}
	}
}
