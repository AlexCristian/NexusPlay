package auth

// [ValidationError] is sign-up input the user can fix, returned by
// [Accounts.Register].
type ValidationError struct {
	// [Field] names the form field at fault: "email", "nickname" or
	// "password".
	Field string
	// [Message] tells the user what to fix, as a full sentence to show next to
	// the field, such as "Enter a valid e-mail address.".
	Message string
}

// [Error] describes the problem for logs.
//
// Returns:
//   - [string]: the field and message, such as "email: Enter a valid e-mail
//     address.".
func (validationError *ValidationError) Error() string {
	return validationError.Field + ": " + validationError.Message
}
