package agencyhome

import (
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/utils/paging"
)

// encodeCursor names the last thread of a page by its position, so a page boundary holds while mail arrives.
func encodeCursor(at time.Time, id uuid.UUID) string {
	if tok := paging.EncodeTime(at, id); tok != nil {
		return *tok
	}
	return ""
}

func decodeCursor(token string) (time.Time, uuid.UUID, *errx.Error) {
	at, id, xerr := paging.DecodeTimeCursor(token)
	if xerr != nil {
		return time.Time{}, uuid.Nil, errInvalidCursor
	}
	return at, id, nil
}
