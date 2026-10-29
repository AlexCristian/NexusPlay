package files

// [List] is a generic type in its own file.
type List[T any] struct {
	// [items] holds the items.
	items []T
}

// [Add] is a method of a generic type in its type's file.
//
// Parameters:
//   - [item]: the item to add.
func (list *List[T]) Add(item T) { list.items = append(list.items, item) }
