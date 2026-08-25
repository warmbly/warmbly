package warmlint

import "testing"

func TestScoreWithOptionsMarksSevereCampaignContentHard(t *testing.T) {
	result := ScoreWithOptions(
		"FREE WINNER URGENT BONUS CASH",
		"<img><img><img><img><img>",
		"",
		ScoreOptions{AttachmentCount: 3},
	)
	if !result.Hard {
		t.Fatalf("ScoreWithOptions() score = %d, want hard result", result.Score)
	}
	if result.Score >= 25 {
		t.Fatalf("ScoreWithOptions() score = %d, want below 25", result.Score)
	}
}

func TestScoreWithOptionsKeepsOrdinaryCopyAdvisory(t *testing.T) {
	result := ScoreWithOptions("Quick question", "", "Would Tuesday work for a short call?", ScoreOptions{})
	if result.Hard || result.Score != 100 {
		t.Fatalf("ScoreWithOptions() = %+v, want clean advisory result", result)
	}
}
