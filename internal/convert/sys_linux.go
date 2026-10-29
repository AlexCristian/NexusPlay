package convert

import "syscall"

// [freeBytes] reports the space available to this program in a folder's
// file system.
//
// Parameters:
//   - [directory]: any folder on the file system to check.
//
// Returns:
//   - [uint64]: the free bytes available to unprivileged users, or 0 if
//     [error] is not [nil].
//   - [error]: [nil] on success, or the error from statfs.
func freeBytes(directory string) (uint64, error) {
	var fileSystemStats syscall.Statfs_t
	if err := syscall.Statfs(directory, &fileSystemStats); err != nil {
		return 0, err
	}
	return fileSystemStats.Bavail * uint64(fileSystemStats.Bsize), nil
}
