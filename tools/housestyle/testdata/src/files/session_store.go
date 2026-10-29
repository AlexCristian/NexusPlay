package files

// [Sessions] is in a file that ends with, but does not start with, its
// type's file name.
func (store *Store) Sessions() {} // want "method \\[Store.Sessions\\] should be in store.go or a store_\\*.go file, with \\[Store\\]"
