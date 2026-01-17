package db

import (
	"fmt"

	"github.com/getsentry/sentry-go"
	"github.com/jackc/pgx/v5/pgconn"
)

func CaptureError(err error, query string, params []any, operation string) {
	if err == nil {
		return
	}
	wrappedErr := fmt.Errorf("%s failed: %w (query: %s, params: %v)", operation, err, query, params)
	sentry.CaptureException(wrappedErr)
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetTag("db.operation", operation)
		scope.SetTag("db.query", query) // Sanitize sensitive params in prod
		if pgErr, ok := err.(*pgconn.PgError); ok {
			scope.SetExtra("pg.code", pgErr.Code) // e.g., "23505" for unique violation
			scope.SetExtra("pg.detail", pgErr.Detail)
			scope.SetExtra("pg.hint", pgErr.Hint)
		}

		scope.SetExtra("db.params", params) // Redact if sensitive
	})
}
