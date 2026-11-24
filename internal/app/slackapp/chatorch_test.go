package slackapp

import (
	"context"
	"testing"

	"github.com/warmbly/warmbly/internal/app/aiagent"
)

func TestRendererDropsInternalEvents(t *testing.T) {
	r := &renderer{}
	r.emit(aiagent.StreamEvent{Type: "text_delta", Text: "Hel"})
	r.emit(aiagent.StreamEvent{Type: "text", Text: "Hello"})
	r.emit(aiagent.StreamEvent{Type: "tool_secret", Secrets: map[string]string{"token": "sekret"}})
	r.emit(aiagent.StreamEvent{Type: "iteration", Iteration: 1})
	r.emit(aiagent.StreamEvent{Type: "done"})
	r.emit(aiagent.StreamEvent{Type: "error", Code: "provider_error", Message: "Remie hit an error. Please try again."})
	if len(r.done) != 1 || r.done[0] != "Hello" || r.cur != "" || len(r.steps) != 0 || r.errMsg != "Remie hit an error. Please try again." {
		t.Fatalf("done=%v cur=%q steps=%d err=%q", r.done, r.cur, len(r.steps), r.errMsg)
	}
	msg, skip := settleReply(r.done, r.cur, len(r.steps), r.errMsg, r.appr != nil, r.ts == "")
	if skip || msg != "Remie hit an error. Please try again." {
		t.Fatalf("msg=%q skip=%v", msg, skip)
	}
}

func TestRendererEmptySuccessAsksToRephrase(t *testing.T) {
	msg, skip := settleReply(nil, "", 0, "", false, true)
	if skip || msg != "I don't have an answer for that. Try asking another way." {
		t.Fatalf("msg=%q skip=%v", msg, skip)
	}
}

func TestRendererSkipsEmptyApprovalPost(t *testing.T) {
	msg, skip := settleReply(nil, "", 0, "", true, true)
	if !skip || msg != "" {
		t.Fatalf("msg=%q skip=%v", msg, skip)
	}
}

func TestResumeTurnIsAlwaysStrict(t *testing.T) {
	ctx := resumeApprovalContext(context.Background())
	if !aiagent.StrictApproval(ctx) {
		t.Fatal("resume context is not strict")
	}
}
