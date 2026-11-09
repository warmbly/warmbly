package handler

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/utils/paging"
)

func TestSubmissionsPositionReadsBothCursorForms(t *testing.T) {
	at := time.Date(2026, 3, 1, 12, 0, 0, 123456000, time.UTC)
	id := uuid.New()

	pos, xerr := submissionsPosition(*paging.EncodeTime(at, id), "")
	if xerr != nil || pos == nil || !pos.At.Equal(at) || pos.ID != id {
		t.Fatalf("opaque cursor: %+v %v", pos, xerr)
	}

	pos, xerr = submissionsPosition("", at.Format(time.RFC3339Nano))
	if xerr != nil || pos == nil || !pos.At.Equal(at) || pos.ID != uuid.Nil {
		t.Fatalf("legacy before: %+v %v", pos, xerr)
	}

	if pos, xerr = submissionsPosition("", ""); xerr != nil || pos != nil {
		t.Fatalf("first page: %+v %v", pos, xerr)
	}

	for name, args := range map[string][2]string{
		"garbage cursor":    {"not-a-cursor", ""},
		"uuid cursor":       {*paging.EncodeUUID(id), ""},
		"garbage before":    {"", "yesterday"},
		"cursor and before": {*paging.EncodeTime(at, id), at.Format(time.RFC3339Nano)},
	} {
		if _, xerr := submissionsPosition(args[0], args[1]); xerr == nil || xerr.Code != errx.BadRequest {
			t.Errorf("%s: want 400, got %v", name, xerr)
		}
	}
}
