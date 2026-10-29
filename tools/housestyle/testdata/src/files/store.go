package files

// [Store] is a type whose methods are split by topic over several files.
type Store struct{}

// [Open] is in the type's own file.
func (store *Store) Open() {}
