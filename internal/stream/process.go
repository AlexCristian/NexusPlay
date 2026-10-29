package stream

// [Process] is a running ffmpeg, as [Starter.Start] returns it.
type Process interface {
	// [Wait] blocks until the process exits. Call it once.
	//
	// Returns:
	//   - [error]: [nil] if ffmpeg succeeded; the context's error if the
	//     process was killed through the context given to [Starter.Start];
	//     otherwise why ffmpeg failed.
	Wait() error
}
