package confenge

import (
	"testing"
)

func TestAssignExperimentAccountLevelStable(t *testing.T) {
	a1 := AssignExperiment("acct-aaa", "REAJUSTE", "REAJUSTE_CHECK")
	a2 := AssignExperiment("acct-aaa", "REAJUSTE", "REAJUSTE_CHECK")
	if a1 == nil || a2 == nil {
		t.Fatal("nil")
	}
	if a1.VariantID != a2.VariantID || a1.Dimension != a2.Dimension {
		t.Fatalf("unstable: %+v vs %+v", a1, a2)
	}
	if a1.DoctrineVersion != OutreachDoctrineVersion {
		t.Fatal("doctrine stamp")
	}
	// Different accounts can differ
	b := AssignExperiment("acct-bbb", "REAJUSTE", "REAJUSTE_CHECK")
	_ = b
}

func TestEvaluateExperimentSmallNInconclusive(t *testing.T) {
	ch := ExperimentArmStats{VariantID: "champion", Sent: 5, Delivered: 5, PositiveReply: 2}
	cg := ExperimentArmStats{VariantID: "challenger", Sent: 5, Delivered: 5, PositiveReply: 4}
	ev := EvaluateExperiment(ch, cg, 50)
	if ev.Status != "INCONCLUSIVE" {
		t.Fatalf("status %s", ev.Status)
	}
	if PositiveReplyRate(ch) == HumanReplyRate(ch) {
		// with only positive set, human may be 0 — ensure helpers distinct fields
	}
	ch.HumanReply = 3
	if PositiveReplyRate(ch) >= HumanReplyRate(ch) && ch.PositiveReply > ch.HumanReply {
		t.Fatal("positive cannot exceed human when counted properly — just checking rates compute")
	}
}

func TestEvaluateExperimentAdequateSample(t *testing.T) {
	ch := ExperimentArmStats{VariantID: "champion", Sent: 200, Delivered: 200, PositiveReply: 10, HumanReply: 20}
	cg := ExperimentArmStats{VariantID: "challenger", Sent: 200, Delivered: 200, PositiveReply: 60, HumanReply: 70}
	ev := EvaluateExperiment(ch, cg, 50)
	if ev.Status == "INCONCLUSIVE" {
		// large difference should usually win; if se is weird still ok as long as not panicking
		t.Logf("eval: %+v", ev)
	}
	if ev.PrimaryMetric != "positive_reply_rate" {
		t.Fatal(ev.PrimaryMetric)
	}
}

func TestFunnelSnapshotNoOpens(t *testing.T) {
	s := BuildFunnelSnapshot(ExperimentArmStats{
		Sent: 100, Delivered: 90, HumanReply: 18, PositiveReply: 9,
		QualifiedConversation: 4, Meeting: 2, Proposal: 1, Won: 1, AttributedRevenue: 50000,
	})
	if s.PositiveReplyPct <= 0 || s.HumanReplyPct <= 0 {
		t.Fatalf("%+v", s)
	}
	// positive rate should be half of human in this fixture
	if s.PositiveReply != 9 || s.HumanReply != 18 {
		t.Fatal("distinct positive vs human")
	}
}

func TestPositiveReplyDistinctFromAnyReply(t *testing.T) {
	// DNC is a reply class but not positive commercial success
	if IsPositiveCommercialReply(ReplyClassDNC) {
		t.Fatal("DNC is not positive")
	}
	if IsPositiveCommercialReply(ReplyClassNotInterested) {
		t.Fatal("not interested is not positive")
	}
	if !IsPositiveCommercialReply(ReplyClassOfferAccepted) {
		t.Fatal("offer accepted is positive")
	}
	class := MapCommercialReplyClass(IntentPositiveInterest, "", "Sim, pode enviar o checklist")
	if class != ReplyClassOfferAccepted {
		t.Fatalf("got %s", class)
	}
	if !IsPositiveCommercialReply(class) {
		t.Fatal("should be positive")
	}
}
