package commands

import (
	"context"
	"os/exec"
	"syscall"
	"time"
)

// waitDelay bounds how long Wait blocks on output pipes after a process is
// killed, in case something it started still holds them.
const waitDelay = 5 * time.Second

// groupCommand runs name in its own process group and, on cancel, kills the
// whole group. The fifo pipeline depends on this: a killed process must not
// leave a child behind blocked opening a fifo.
func groupCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = waitDelay
	return cmd
}
