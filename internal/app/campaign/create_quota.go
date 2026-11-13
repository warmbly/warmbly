package campaign

import "context"

type migrationCreateKey struct{}

// WithMigrationCreate marks campaign creates made by a tool migration, which
// holds them to its own per-workspace cap instead of the daily throttle. No
// request path sets it.
func WithMigrationCreate(ctx context.Context) context.Context {
	return context.WithValue(ctx, migrationCreateKey{}, true)
}

func migrationCreate(ctx context.Context) bool {
	v, _ := ctx.Value(migrationCreateKey{}).(bool)
	return v
}
