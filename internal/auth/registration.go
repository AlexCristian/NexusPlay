package auth

// [Registration] is what the sign-up form submits, before validation.
type Registration struct {
	// [Email] is the e-mail address the account signs in with.
	Email, Nickname, Password string
}
