package auth

// Registration is what the sign-up form submits.
type Registration struct {
	Email, Nickname, Password string
}
