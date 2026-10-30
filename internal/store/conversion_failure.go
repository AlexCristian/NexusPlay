package store

// [ConversionFailure] is a file the converter gave up on, for the control
// panel.
type ConversionFailure struct {
	// [Media] is the media item.
	Media Media
	// [Error] is why the conversion failed, at most [maximumConversionErrorBytes].
	Error string
}
