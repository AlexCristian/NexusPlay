package auth

// [RegistrationStatus] tells the sign-up form what to say, as returned by
// [Accounts.RegistrationStatus].
type RegistrationStatus struct {
	// [FirstUser] is [true] when there are no accounts yet, so the next one
	// becomes the administrator.
	FirstUser bool
	// [Closed] is [true] when the account limit is reached and nobody else can
	// register.
	Closed bool
	// [MaxAccounts] is the account limit, or 0 for no limit.
	MaxAccounts int
}
