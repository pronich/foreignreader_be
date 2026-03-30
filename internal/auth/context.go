package auth

import "context"

type ctxKeyUser struct{}

// ContextWithUser attaches the authenticated user for downstream handlers.
func ContextWithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, ctxKeyUser{}, u)
}

// UserFromContext returns the user set by bearer auth middleware.
func UserFromContext(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(ctxKeyUser{}).(User)
	return u, ok
}
