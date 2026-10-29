package auth

// [Registration] is what the sign-up form submits, before validation.
type Registration struct {
	// [Email] is the e-mail address the account signs in with, as typed.
	Email string
	// [Nickname] is the name shown to others, as typed.
	Nickname string
	// [Password] is the chosen password, in plain text; it is hashed before
	// it is stored.
	Password string
}
