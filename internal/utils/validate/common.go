package validate

import (
	"strconv"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
)

// LimitDefault matches the frontend's DEFAULT_PAGINATION_LIMIT. The
// client omits ?limit= when it'd be sending the default, so the empty
// string here means "use the default", not "invalid". Treating it as
// invalid is what 400'd the campaigns and contacts listings — they
// stayed in loading state forever because the page didn't handle the
// error and the client kept retrying.
const LimitDefault int32 = 50

func Limit(limit string) (int32, *errx.Error) {
	if limit == "" {
		return LimitDefault, nil
	}
	i, err := strconv.ParseInt(limit, 10, 32)
	if err != nil || i > config.LimitMax || i < config.LimitMin {
		return 0, errx.ErrLimit
	}

	return int32(i), nil
}
