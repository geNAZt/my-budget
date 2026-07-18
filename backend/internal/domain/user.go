package domain

import (
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

// User represents the system user for authentication.
type User struct {
	ID                     string
	Username               string
	Authenticators         []webauthn.Credential
	AuthenticatorNames     map[string]string    // base64(ID) -> Name
	AuthenticatorCreatedAt map[string]time.Time // base64(ID) -> CreatedAt
	DashboardScenarioID    string
	DashboardMonthOffset   int
	RecoveryHash           string
	Timezone               string
}

// WebAuthnID returns the user's ID as a byte slice.
func (u *User) WebAuthnID() []byte {
	return []byte(u.ID)
}

// WebAuthnName returns the user's username.
func (u *User) WebAuthnName() string {
	return u.Username
}

// WebAuthnDisplayName returns the user's username.
func (u *User) WebAuthnDisplayName() string {
	return u.Username
}

// WebAuthnIcon is not used.
func (u *User) WebAuthnIcon() string {
	return ""
}

// WebAuthnCredentials returns the user's credentials.
func (u *User) WebAuthnCredentials() []webauthn.Credential {
	return u.Authenticators
}
