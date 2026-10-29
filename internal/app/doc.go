// Package app is the composition root: the only place that knows every
// concrete type. It checks the environment, builds each component with its
// dependencies, and runs the HTTP server and the background workers. The
// command and the integration tests both start the server through it.
package app
