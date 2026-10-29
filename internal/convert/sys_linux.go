package convert

import "syscall"

// freeBytes reports the space available in the folder's file system.
func freeBytes(directory string) (uint64, error) {
	var fileSystemStats syscall.Statfs_t
	if err := syscall.Statfs(directory, &fileSystemStats); err != nil {
		return 0, err
	}
	return fileSystemStats.Bavail * uint64(fileSystemStats.Bsize), nil
}
