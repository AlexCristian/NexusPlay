package auth

// RegistrationStatus tells the sign-up form what to say.
type RegistrationStatus struct {
	FirstUser   bool // the next account becomes the administrator
	Closed      bool // the account limit is reached
	MaxAccounts int  // the limit; 0 for none
}
