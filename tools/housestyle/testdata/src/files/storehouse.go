package files

// [Stock] is in a file that starts with the type's file name, but without
// the underscore that ends it.
func (store *Store) Stock() {} // want "method \\[Store.Stock\\] should be in store.go or a store_\\*.go file, with \\[Store\\]"
