package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// A lead who booked a meeting gets no more of the cold sequence when
// stop_on_meeting is on, whether or not they replied, while a reply flow they
// already entered keeps running like it does under stop_on_reply.
func TestRouteStopsTheColdSequenceForABookedLead(t *testing.T) {
	notRepliedIn3d := []models.BranchCondition{{Field: "not_replied", Operator: "within_days", Value: intPtr(3)}}
	replied := []models.BranchCondition{{Field: "replied"}}

	// first -> (unconditional) -> followUp -> (not replied in 3d) -> breakup
	// first -> (replied) -> replyStep -> (unconditional) -> replyFollowUp
	first, followUp, breakup := uuid.New(), uuid.New(), uuid.New()
	replyStep, replyFollowUp := uuid.New(), uuid.New()
	newRouter := func(stopOnReply, stopOnMeeting bool) *campaignRouter {
		now := time.Now()
		return &campaignRouter{
			steps: []routeStep{
				{id: first, kind: "email", bc: models.BranchConditions{Branches: []models.Branch{
					{BranchID: "reply", TargetSequenceID: &replyStep, Conditions: replied},
					{BranchID: "cold", TargetSequenceID: &followUp},
				}}},
				{id: followUp, kind: "email", waitAfter: 3, bc: models.BranchConditions{Branches: []models.Branch{
					{BranchID: "quiet", TargetSequenceID: &breakup, Conditions: notRepliedIn3d},
				}}},
				{id: breakup, kind: "email", waitAfter: 7},
				{id: replyStep, kind: "email", bc: models.BranchConditions{Branches: []models.Branch{
					{BranchID: "next", TargetSequenceID: &replyFollowUp},
				}}},
				{id: replyFollowUp, kind: "email", waitAfter: 2},
			},
			idxByID:           map[uuid.UUID]int{first: 0, followUp: 1, breakup: 2, replyStep: 3, replyFollowUp: 4},
			entry:             first,
			now:               now,
			dueBy:             now.Add(time.Minute),
			stopOnReply:       stopOnReply,
			stopOnMeeting:     stopOnMeeting,
			replyFlowSteps:    map[uuid.UUID]bool{replyStep: true, replyFollowUp: true},
			campaignCreatedAt: now.Add(-30 * 24 * time.Hour),
		}
	}
	at := func(daysAgo int) *time.Time {
		t := time.Now().Add(-time.Duration(daysAgo) * 24 * time.Hour)
		return &t
	}
	step := func(id uuid.UUID) *uuid.UUID { return &id }

	cases := []struct {
		name          string
		stopOnReply   bool
		stopOnMeeting bool
		in            routeInput
		wantTarget    *uuid.UUID
		wantWait      bool
		wantStoppedBy string
	}{
		{
			name:        "booked from email 1 without replying: no follow-up",
			stopOnReply: true, stopOnMeeting: true,
			in:            routeInput{lastSeq: step(first), sentAt: at(5), sentIDs: []uuid.UUID{first}, hasBooked: true},
			wantStoppedBy: StoppedByMeeting,
		},
		{
			name:        "booked while a not-replied window is open: stops instead of waiting for it",
			stopOnReply: true, stopOnMeeting: true,
			in:            routeInput{lastSeq: step(followUp), sentAt: at(1), sentIDs: []uuid.UUID{first, followUp}, hasBooked: true},
			wantStoppedBy: StoppedByMeeting,
		},
		{
			name:        "booked before the first email: nothing is sent",
			stopOnReply: true, stopOnMeeting: true,
			in:            routeInput{hasBooked: true},
			wantStoppedBy: StoppedByMeeting,
		},
		{
			name:        "stop_on_meeting off: the follow-up still goes out",
			stopOnReply: true, stopOnMeeting: false,
			in:         routeInput{lastSeq: step(first), sentAt: at(5), sentIDs: []uuid.UUID{first}, hasBooked: true},
			wantTarget: step(followUp),
		},
		{
			name:        "stop_on_meeting works with stop_on_reply off",
			stopOnReply: false, stopOnMeeting: true,
			in:            routeInput{lastSeq: step(first), sentAt: at(5), sentIDs: []uuid.UUID{first}, hasBooked: true},
			wantStoppedBy: StoppedByMeeting,
		},
		{
			name:        "no booking: the cold sequence continues",
			stopOnReply: true, stopOnMeeting: true,
			in:         routeInput{lastSeq: step(first), sentAt: at(5), sentIDs: []uuid.UUID{first}},
			wantTarget: step(followUp),
		},
		{
			name:        "booked inside the reply flow: the reply flow keeps its own steps",
			stopOnReply: false, stopOnMeeting: true,
			in: routeInput{lastSeq: step(replyStep), sentAt: at(3), sentIDs: []uuid.UUID{first, replyStep},
				repliedAt: at(4), hasReplied: true, hasBooked: true},
			wantTarget: step(replyFollowUp),
		},
		{
			name:        "replied and booked: the stop names the booking",
			stopOnReply: true, stopOnMeeting: true,
			in:            routeInput{lastSeq: step(followUp), sentAt: at(1), sentIDs: []uuid.UUID{first, followUp}, hasReplied: true, hasBooked: true},
			wantStoppedBy: StoppedByMeeting,
		},
		{
			name:        "replied without a booking: the stop names the reply",
			stopOnReply: true, stopOnMeeting: true,
			in:            routeInput{lastSeq: step(followUp), sentAt: at(1), sentIDs: []uuid.UUID{first, followUp}, hasReplied: true},
			wantStoppedBy: StoppedByReply,
		},
		{
			name:        "booked on the last step: the flow ended on its own, not by the rule",
			stopOnReply: true, stopOnMeeting: true,
			in: routeInput{lastSeq: step(breakup), sentAt: at(1), sentIDs: []uuid.UUID{first, followUp, breakup}, hasBooked: true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := newRouter(tc.stopOnReply, tc.stopOnMeeting).route(uuid.New(), uuid.New(), tc.in)
			switch {
			case tc.wantTarget == nil && got.Target != nil:
				t.Fatalf("target = %s, want none", *got.Target)
			case tc.wantTarget != nil && (got.Target == nil || *got.Target != *tc.wantTarget):
				t.Fatalf("target = %v, want %s", got.Target, *tc.wantTarget)
			}
			if (got.WaitUntil != nil) != tc.wantWait {
				t.Fatalf("WaitUntil = %v, want waiting %v", got.WaitUntil, tc.wantWait)
			}
			if got.StoppedBy != tc.wantStoppedBy {
				t.Fatalf("StoppedBy = %q, want %q", got.StoppedBy, tc.wantStoppedBy)
			}
		})
	}
}
