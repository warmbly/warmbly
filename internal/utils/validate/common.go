package validate

import (
	"strconv"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
)

func Limit(limit string) (int32, *errx.Error) {
	i, err := strconv.ParseInt(limit, 10, 32)
	if err != nil || i > config.LimitMax || i < config.LimitMin {
		return 0, errx.ErrLimit
	}

	return int32(i), nil
}
