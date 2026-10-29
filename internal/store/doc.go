// Package [store] is NexusPlay's data layer: a SQLite database with embedded
// migrations and typed query methods. It is the only package that writes SQL.
//
// Rules that must hold across several rows (publishing and notifying
// subscribers, applying a subtitle correction once enough people vote,
// replacing a converted file) are single transactions here, not sequences
// of calls by the caller.
//
// Errors: a lookup of one row returns [ErrNotFound] when there is none; a
// write that breaks a uniqueness rule returns [ErrDuplicate]. Callers test
// them with [errors.Is]. A [Store] is safe for concurrent use.
package store
