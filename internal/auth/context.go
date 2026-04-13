package auth

import (
	"context"

	"github.com/google/uuid"
)

type ctxKeyUser struct{}

type ctxKeySessionID struct{}

// ContextWithUser attaches the authenticated user for downstream handlers.
func ContextWithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, ctxKeyUser{}, u)
}

// UserFromContext returns the user set by bearer auth middleware.
func UserFromContext(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(ctxKeyUser{}).(User)
	return u, ok
}

// ContextWithSessionID attaches the auth_sessions id from JWT claim sid (uuid.Nil if absent).
func ContextWithSessionID(ctx context.Context, sessionID uuid.UUID) context.Context {
	return context.WithValue(ctx, ctxKeySessionID{}, sessionID)
}

// SessionIDFromContext returns the sid from the access token when present and non-nil.
func SessionIDFromContext(ctx context.Context) (sessionID uuid.UUID, ok bool) {
	v, typed := ctx.Value(ctxKeySessionID{}).(uuid.UUID)
	if !typed {
		return uuid.Nil, false
	}
	if v == uuid.Nil {
		return uuid.Nil, false
	}
	return v, true
}
