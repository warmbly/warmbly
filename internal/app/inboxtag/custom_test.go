package inboxtag

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func laterMaybe(action models.InboxTagQuestionAction) models.InboxTagQuestion {
	return models.InboxTagQuestion{
		ID:       "q1",
		Type:     models.InboxTagQuestionYesNo,
		Question: "The sender says they have no suitable position right now but will get back later.",
		Label:    "later-maybe",
		Action:   action,
	}
}

func roleQuestion() models.InboxTagQuestion {
	return models.InboxTagQuestion{
		ID:       "q2",
		Type:     models.InboxTagQuestionChoice,
		Question: "Who is replying?",
		Choices: []models.InboxTagChoice{
			{Label: "recruiter", Description: "A recruiter or staffing agency"},
			{Label: "hiring-manager", Description: "The person who would hire", Action: models.InboxTagQuestionAction{Type: models.InboxTagActionTask}},
		},
	}
}

func confidentReply() map[string]Answer {
	return map[string]Answer{
		"kind":   {Choice: KindHumanReply, Confidence: 0.95},
		"intent": {Choice: IntentNotNow, Confidence: 0.9},
	}
}

func TestQuestionsForAddsWorkspaceQuestions(t *testing.T) {
	q := QuestionsFor([]models.InboxTagQuestion{laterMaybe(models.InboxTagQuestionAction{}), roleQuestion()})
	if len(q) != len(Questions())+2 {
		t.Fatalf("got %d questions, want the built-in set plus two", len(q))
	}
	yn := q["custom_q1"]
	if yn.Type != QuestionNoul || yn.Instructions == "" {
		t.Fatalf("yes/no question = %+v", yn)
	}
	choice := q["custom_q2"]
	criteria, ok := choice.Criteria.(map[string]string)
	if choice.Type != QuestionChoice || !ok {
		t.Fatalf("choice question = %+v", choice)
	}
	if _, ok := criteria[noneOfThese]; !ok || len(criteria) != 3 {
		t.Fatalf("a choice question needs its options plus a way out: %v", criteria)
	}
}

func TestWorkspaceYesNoLabelsAtYesAndActsAtStrong(t *testing.T) {
	custom := []models.InboxTagQuestion{laterMaybe(models.InboxTagQuestionAction{Type: models.InboxTagActionHold, HoldDays: 60})}

	weak := confidentReply()
	weak["custom_q1"] = Answer{Noul: 0.65}
	d := DecideWith(weak, Facts{}, custom)
	if !slices.Contains(d.Labels, "later-maybe") {
		t.Fatalf("a yes should label: %v", d.Labels)
	}
	if len(d.Custom) != 1 || d.Custom[0].Acts {
		t.Fatalf("a yes below Strong labels but does not act: %+v", d.Custom)
	}

	none := confidentReply()
	none["custom_q1"] = Answer{Noul: 0.4}
	if d := DecideWith(none, Facts{}, custom); slices.Contains(d.Labels, "later-maybe") {
		t.Fatalf("a no labelled: %v", d.Labels)
	}

	strong := confidentReply()
	strong["custom_q1"] = Answer{Noul: 0.9}
	if d := DecideWith(strong, Facts{}, custom); len(d.Custom) != 1 || !d.Custom[0].Acts {
		t.Fatalf("a strong yes should act: %+v", d.Custom)
	}
}

func TestWorkspaceChoicePicksItsOptionLabel(t *testing.T) {
	custom := []models.InboxTagQuestion{roleQuestion()}

	a := confidentReply()
	a["custom_q2"] = Answer{Choice: "option_2", Confidence: 0.8}
	d := DecideWith(a, Facts{}, custom)
	if !slices.Contains(d.Labels, "hiring-manager") || slices.Contains(d.Labels, "recruiter") {
		t.Fatalf("labels = %v", d.Labels)
	}

	for name, ans := range map[string]Answer{
		"none of these":   {Choice: noneOfThese, Confidence: 0.99},
		"below the floor": {Choice: "option_1", Confidence: 0.5},
	} {
		a := confidentReply()
		a["custom_q2"] = ans
		if d := DecideWith(a, Facts{}, custom); len(d.Custom) != 0 {
			t.Errorf("%s matched %+v", name, d.Custom)
		}
	}
}

// A workspace question is about what a person said; a mail server said
// nothing, and an untrusted kind is not read at all.
func TestWorkspaceQuestionsSkipAutomatedAndUntrustedKinds(t *testing.T) {
	custom := []models.InboxTagQuestion{laterMaybe(models.InboxTagQuestionAction{})}
	for name, kind := range map[string]Answer{
		"bounce":     {Choice: KindBounceHard, Confidence: 0.95},
		"ooo":        {Choice: KindAutoReplyOOO, Confidence: 0.95},
		"notice":     {Choice: KindNotification, Confidence: 0.95},
		"kind floor": {Choice: KindHumanReply, Confidence: 0.4},
	} {
		d := DecideWith(map[string]Answer{"kind": kind, "custom_q1": {Noul: 0.99}}, Facts{}, custom)
		if slices.Contains(d.Labels, "later-maybe") || len(d.Custom) != 0 {
			t.Errorf("%s: labels %v, custom %+v", name, d.Labels, d.Custom)
		}
	}
	// A cold pitch is still a person writing, so it can be labelled.
	d := DecideWith(map[string]Answer{"kind": {Choice: KindColdInbound, Confidence: 0.9}, "custom_q1": {Noul: 0.9}}, Facts{}, custom)
	if !slices.Contains(d.Labels, "later-maybe") {
		t.Fatalf("cold inbound: %v", d.Labels)
	}
}

// Workspace questions add labels; they never move the calibrated score.
func TestWorkspaceQuestionsDoNotMoveRelevance(t *testing.T) {
	custom := []models.InboxTagQuestion{laterMaybe(models.InboxTagQuestionAction{})}
	a := confidentReply()
	base := Decide(a, Facts{})
	a["custom_q1"] = Answer{Noul: 0.99}
	if got := DecideWith(a, Facts{}, custom); got.Relevance != base.Relevance || got.Kind != base.Kind || got.Intent != base.Intent {
		t.Fatalf("custom answer changed the verdict: %+v vs %+v", got, base)
	}
}

func TestPlanActionsFoldsWorkspaceActions(t *testing.T) {
	hold := []models.InboxTagQuestion{laterMaybe(models.InboxTagQuestionAction{Type: models.InboxTagActionHold, HoldDays: 60})}
	a := confidentReply()
	a["custom_q1"] = Answer{Noul: 0.9}

	p := PlanActions(DecideWith(a, Facts{}, hold), allOn())
	if p.HoldDays != 60 || p.HoldReason == "" {
		t.Fatalf("the longer workspace hold should win with its reason: %+v", p)
	}

	short := []models.InboxTagQuestion{laterMaybe(models.InboxTagQuestionAction{Type: models.InboxTagActionHold, HoldDays: 7})}
	if p := PlanActions(DecideWith(a, Facts{}, short), allOn()); p.HoldDays != 30 || p.HoldReason != "" {
		t.Fatalf("a shorter workspace hold must not cut the not-now hold: %+v", p)
	}

	// The action is the question's own switch: it runs with every built-in off.
	if p := PlanActions(DecideWith(a, Facts{}, hold), models.InboxTaggingSettings{}); p.HoldDays != 60 {
		t.Fatalf("workspace hold with built-ins off: %+v", p)
	}

	stop := []models.InboxTagQuestion{laterMaybe(models.InboxTagQuestionAction{Type: models.InboxTagActionStop})}
	if p := PlanActions(DecideWith(a, Facts{}, stop), models.InboxTaggingSettings{}); !p.Stop {
		t.Fatalf("workspace stop: %+v", p)
	}

	task := []models.InboxTagQuestion{laterMaybe(models.InboxTagQuestionAction{Type: models.InboxTagActionTask})}
	if p := PlanActions(DecideWith(a, Facts{}, task), models.InboxTaggingSettings{}); p.Task == "" {
		t.Fatalf("workspace task: %+v", p)
	}

	weak := confidentReply()
	weak["custom_q1"] = Answer{Noul: 0.65}
	if p := PlanActions(DecideWith(weak, Facts{}, stop), models.InboxTaggingSettings{}); !p.Empty() {
		t.Fatalf("a yes below Strong acted: %+v", p.Actions())
	}

	unread := confidentReply()
	unread["intent"] = Answer{Choice: IntentNotNow, Confidence: 0.4}
	unread["custom_q1"] = Answer{Noul: 0.95}
	if p := PlanActions(DecideWith(unread, Facts{}, stop), models.InboxTaggingSettings{}); !p.Empty() {
		t.Fatalf("a verdict marked needs-review acted: %+v", p.Actions())
	}

	removal := confidentReply()
	removal["custom_q1"] = Answer{Noul: 0.9}
	removal[SigRequestsRemoval] = Answer{Noul: 0.95}
	if p := PlanActions(DecideWith(removal, Facts{}, hold), allOn()); p.Suppress == "" || p.HoldDays != 0 || p.HoldReason != "" {
		t.Fatalf("a suppression ends every other action: %+v", p)
	}
}

func TestValidateQuestionsRefusesBuiltInLabels(t *testing.T) {
	for _, label := range []string{LabelNeedsReview, "needs review", "SALES PITCH", LabelGoneQuiet} {
		q := laterMaybe(models.InboxTagQuestionAction{})
		q.Label = label
		if err := ValidateQuestions([]models.InboxTagQuestion{q}); err == nil {
			t.Errorf("%q accepted", label)
		}
	}
	c := roleQuestion()
	c.Choices[0].Label = "interested"
	if err := ValidateQuestions([]models.InboxTagQuestion{c}); err == nil {
		t.Error("a built-in label accepted as a choice")
	}
	if err := ValidateQuestions([]models.InboxTagQuestion{laterMaybe(models.InboxTagQuestionAction{}), roleQuestion()}); err != nil {
		t.Fatalf("valid questions refused: %v", err)
	}
}

type capturingAsker struct {
	state     State
	questions map[string]Question
	resp      Response
}

func (c *capturingAsker) Ask(_ context.Context, state any, q map[string]Question) (*Response, error) {
	c.state, _ = state.(State)
	c.questions = q
	r := c.resp
	return &r, nil
}

type fakeSettings struct {
	s     models.InboxTaggingSettings
	langs []string
}

func (f fakeSettings) GetOutreachSettings(context.Context, uuid.UUID) (*models.AdvancedOutreachSettings, error) {
	f.s.Languages = f.langs
	return &models.AdvancedOutreachSettings{InboxTagging: f.s}, nil
}

func TestClassifyAsksWorkspaceQuestionsInTheSameCall(t *testing.T) {
	resp := Response{Answers: confidentReply()}
	resp.Answers["custom_q1"] = Answer{Noul: 0.9}
	asker := &capturingAsker{resp: resp}
	repo := &fakeRepo{}
	svc := newService(t, asker, repo)
	svc.WireSettings(fakeSettings{s: models.InboxTaggingSettings{
		Questions: []models.InboxTagQuestion{laterMaybe(models.InboxTagQuestionAction{})},
	}, langs: []string{"de", "pl"}})

	d, err := svc.Classify(context.Background(), inboundMessage())
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if _, ok := asker.questions["custom_q1"]; !ok {
		t.Fatal("the workspace question was not in the call")
	}
	if asker.state.Language != "German, Polish" {
		t.Fatalf("language hint = %q", asker.state.Language)
	}
	if !slices.Contains(d.Labels, "later-maybe") || !slices.Contains(repo.saved[0].Labels, "later-maybe") {
		t.Fatalf("labels = %v, saved %v", d.Labels, repo.saved[0].Labels)
	}
}

// No language set means no hint, so an English workspace's state is exactly
// what the fixtures were recorded against.
func TestClassifySendsNoLanguageByDefault(t *testing.T) {
	asker := &capturingAsker{resp: Response{Answers: confidentReply()}}
	svc := newService(t, asker, &fakeRepo{})
	svc.WireSettings(fakeSettings{})
	if _, err := svc.Classify(context.Background(), inboundMessage()); err != nil {
		t.Fatalf("classify: %v", err)
	}
	if asker.state.Language != "" || len(asker.questions) != len(Questions()) {
		t.Fatalf("state %+v, %d questions", asker.state, len(asker.questions))
	}
}

func TestBackfillPassesInReplyTo(t *testing.T) {
	asker := &capturingAsker{resp: Response{Answers: confidentReply()}}
	c := candidates(1)
	c[0].InReplyTo = []string{"<sent@mail.gmail.com>"}
	repo := &fakeRepo{untagged: c, campaign: "Q3 outreach"}
	svc := newService(t, asker, repo)

	if _, err := svc.Backfill(context.Background(), uuid.New(), BackfillOptions{}); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if !slices.Equal(repo.inReplyTo, c[0].InReplyTo) {
		t.Fatalf("In-Reply-To not passed: %v", repo.inReplyTo)
	}
	if asker.state.Campaign != "Q3 outreach" {
		t.Fatalf("campaign not in the state: %+v", asker.state)
	}
}

// A verdict stored before the campaign could be resolved is asked again with
// the campaign in the state, and the stale label comes off when it moved.
func TestRecheckColdInboundReclassifiesCampaignThreads(t *testing.T) {
	asker := &capturingAsker{resp: Response{Answers: confidentReply()}}
	c := candidates(2)
	repo := &fakeRepo{cold: c, campaign: "Q3 outreach"}
	cats := &fakeCategories{}
	svc := NewService(asker, repo, cats, nil, true)

	p, err := svc.Backfill(context.Background(), uuid.New(), BackfillOptions{RecheckColdInbound: true, Since: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatalf("recheck: %v", err)
	}
	if p.Classified != 2 || len(repo.reopened) != 2 {
		t.Fatalf("progress %+v, reopened %v", p, repo.reopened)
	}
	if got := cats.removed["thread-1"]; !slices.Contains(got, "cold-inbound") {
		t.Fatalf("stale label not removed: %v", cats.removed)
	}
}

// Without a campaign there is nothing new to tell the model, so nothing is
// reopened and nothing is paid for.
func TestRecheckSkipsThreadsWithNoCampaign(t *testing.T) {
	asker := &countingAsker{}
	repo := &fakeRepo{cold: candidates(1)}
	svc := newService(t, asker, repo)

	p, err := svc.Backfill(context.Background(), uuid.New(), BackfillOptions{RecheckColdInbound: true})
	if err != nil {
		t.Fatalf("recheck: %v", err)
	}
	if asker.calls != 0 || len(repo.reopened) != 0 || p.Skipped != 1 {
		t.Fatalf("calls %d, reopened %v, progress %+v", asker.calls, repo.reopened, p)
	}
}

var _ repository.InboxTagRepository = (*fakeRepo)(nil)

// The hourly sweep creates the workspace's labels too, so they are filterable
// before the first message earns one.
func TestSweepSeedsWorkspaceLabels(t *testing.T) {
	cats := &fakeCategories{}
	svc := NewService(&countingAsker{}, &fakeRepo{}, cats, nil, true)
	svc.WireSettings(fakeSettings{s: models.InboxTaggingSettings{
		Questions: []models.InboxTagQuestion{laterMaybe(models.InboxTagQuestionAction{}), roleQuestion()},
	}})
	if _, err := svc.SweepFollowUps(context.Background(), uuid.New(), time.Now().Add(-time.Hour), 10); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	for _, want := range []string{"later-maybe", "recruiter", "hiring-manager"} {
		if !slices.Contains(cats.seeded, want) {
			t.Errorf("%q not seeded: %v", want, cats.seeded)
		}
	}
}

// The workspace's languages decide which quoted history is cut before the
// call: German Outlook's block is read only once German is chosen.
func TestClassifyReadsQuotesInTheWorkspaceLanguages(t *testing.T) {
	m := inboundMessage()
	m.BodyText = "Danke, passt.\n\nVon: Anna <anna@example.com>\nGesendet: Montag, 3. März 2025 10:12\nAn: Max\n\nHallo Max"
	for _, c := range []struct {
		langs []string
		want  string
	}{
		{[]string{"de"}, "Danke, passt."},
		{nil, m.BodyText},
	} {
		asker := &capturingAsker{resp: Response{Answers: confidentReply()}}
		svc := newService(t, asker, &fakeRepo{})
		svc.WireSettings(fakeSettings{langs: c.langs})
		if _, err := svc.Classify(context.Background(), m); err != nil {
			t.Fatalf("classify: %v", err)
		}
		if asker.state.Body != c.want {
			t.Errorf("languages %v: body %q", c.langs, asker.state.Body)
		}
	}
}

// A recheck that stores no new verdict (a lost claim, an empty body) must not
// take the thread's labels away.
func TestRecheckKeepsLabelsWhenNothingWasStored(t *testing.T) {
	c := candidates(1)
	c[0].Subject, c[0].BodyText = "", ""
	repo := &fakeRepo{cold: c, campaign: "Q3 outreach"}
	cats := &fakeCategories{}
	svc := NewService(&capturingAsker{resp: Response{Answers: confidentReply()}}, repo, cats, nil, true)
	if _, err := svc.Backfill(context.Background(), uuid.New(), BackfillOptions{RecheckColdInbound: true}); err != nil {
		t.Fatalf("recheck: %v", err)
	}
	if len(cats.removed) != 0 {
		t.Fatalf("labels removed with no verdict stored: %v", cats.removed)
	}
}
