// Package stream runs on-demand HLS transcodes for files browsers cannot
// play directly, with a cap on how many run at once, reuse while a file is
// being watched, and eviction once nobody is watching.
//
// Output is an HLS "event" playlist (-hls_playlist_type event): ffmpeg
// appends segments while it runs and writes #EXT-X-ENDLIST only at the end,
// so playback can start after the first segment instead of after the whole
// transcode. Players treat an open event playlist like live TV and would
// join near its end, so player.js sets hls.js startPosition explicitly; that
// is also why Playlist waits for the first segment before answering.
// Keyframes are forced every 6 s (= -hls_time) when re-encoding, so segments
// split cleanly.
package stream
