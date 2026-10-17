package convert

import "syscall"

// lowerPriority makes a conversion yield the CPU to playback.
func lowerPriority(pid int) {
	// Best effort: if the priority cannot be lowered the conversion simply
	// competes with playback at normal priority.
	_ = syscall.Setpriority(syscall.PRIO_PROCESS, pid, 10)
}

// freeBytes reports the space available in the folder's file system.
func freeBytes(dir string) (uint64, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, false
	}
	return st.Bavail * uint64(st.Bsize), true
}
