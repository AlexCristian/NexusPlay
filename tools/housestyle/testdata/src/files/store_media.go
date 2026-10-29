package files

// [Media] is in a file that starts with its type's file name.
func (store *Store) Media() {}

// [StoreView] is declared in a file named after another type, which is
// still wrong: an exported type must be in its own file exactly.
type StoreView struct{} // want "exported type \\[StoreView\\] should be in its own file, store_view.go"
