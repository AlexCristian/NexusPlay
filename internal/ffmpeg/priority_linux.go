package ffmpeg

import "syscall"

// lowerPriority makes a process yield the CPU to playback. It is best
// effort: if the priority cannot be lowered, the process simply runs at
// normal priority.
func lowerPriority(pid int) {
	_ = syscall.Setpriority(syscall.PRIO_PROCESS, pid, 10)
}
