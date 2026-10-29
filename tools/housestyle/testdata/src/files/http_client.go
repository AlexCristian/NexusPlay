package files

// [HTTPClient] is in its own file, its acronym split off as one word.
type HTTPClient struct{}

// [Get] is a method in its type's file.
func (client *HTTPClient) Get() {}
