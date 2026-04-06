package auth

import (
	"database/sql"

	"github.com/google/uuid"
)

// User is a row from users.
type User struct {
	ID                       uuid.UUID
	DisplayName              sql.NullString
	AvatarURL                sql.NullString
	Email                    sql.NullString
	EmailVerified            bool
	AppStorefront            sql.NullString
	AppStorefrontUpdatedAt   sql.NullTime
}
