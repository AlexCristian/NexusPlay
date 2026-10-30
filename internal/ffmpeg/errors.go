package ffmpeg

import "errors"

// [ErrNoVideo] means a file has no video stream other than cover art.
var ErrNoVideo = errors.New("no video stream")
