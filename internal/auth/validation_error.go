package auth

// ValidationError is input the user can fix; Msg is shown to them.
type ValidationError struct {
	Field   string
	Message string
}

func (validationError *ValidationError) Error() string {
	return validationError.Field + ": " + validationError.Message
}
