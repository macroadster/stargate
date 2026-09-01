package services

// StatusError is a domain error with an HTTP-oriented status code.
type StatusError struct {
	Status  int
	Message string
}

func (e *StatusError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// Fail constructs a StatusError.
func Fail(status int, message string) *StatusError {
	return &StatusError{Status: status, Message: message}
}

// Failf constructs a StatusError with formatting.

// AsStatus extracts StatusError from err, if present.
func AsStatus(err error) *StatusError {
	if err == nil {
		return nil
	}
	if se, ok := err.(*StatusError); ok {
		return se
	}
	return nil
}
