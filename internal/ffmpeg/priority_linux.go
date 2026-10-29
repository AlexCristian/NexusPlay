package ffmpeg

import "syscall"

// [lowerPriority] makes a process yield the CPU to playback by raising its
// nice value to 10. It is best effort: if the priority cannot be changed,
// the process simply runs at normal priority.
//
// Parameters:
//   - [pid]: the process's ID.
func lowerPriority(pid int) {
	_ = syscall.Setpriority(syscall.PRIO_PROCESS, pid, 10)
}
