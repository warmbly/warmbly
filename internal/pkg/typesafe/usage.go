package typesafe

import "context"

type featureKey struct{}
type organizationKey struct{}

func WithUsage(ctx context.Context, feature, organizationID string) context.Context {
	ctx = context.WithValue(ctx, featureKey{}, feature)
	if organizationID != "" {
		ctx = context.WithValue(ctx, organizationKey{}, organizationID)
	}
	return ctx
}

func featureFrom(ctx context.Context) string {
	if feature, ok := ctx.Value(featureKey{}).(string); ok && feature != "" {
		return feature
	}
	return "unattributed"
}
