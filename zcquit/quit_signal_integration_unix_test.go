//go:build !windows

package zcquit

import (
	"errors"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// helperTermSignal 为 Unix 上投递的终止类信号。
var helperTermSignal = syscall.SIGTERM

// prepareChildProcess 使子进程进入独立进程组，防止信号投递波及父进程的进程组。
func prepareChildProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// deliverSignal 只向本测试新建的子进程组投递指定信号。
func deliverSignal(pid int, sig syscall.Signal) error {
	return syscall.Kill(-pid, sig)
}

// assertDefaultSignalExit 精确验证 Unix 默认 SIGTERM 终止，而非普通非零退出或清理超时。
func assertDefaultSignalExit(t *testing.T, err error) {
	t.Helper()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("第二个信号应终止子进程，实际退出结果: %v", err)
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != helperTermSignal || exitErr.ExitCode() != -1 {
		t.Fatalf("应由 %v 默认终止（ExitCode=-1），实际: %v，status=%v", helperTermSignal, exitErr, exitErr.Sys())
	}
}

// TestListen_SIGHUPIgnored 验证 Unix 真实 SIGHUP 被忽略：保留 800ms 独立存活观察窗口，
// 再投递 SIGTERM 后仅执行一次清理并正常返回；READY 后无需额外就绪延迟。
func TestListen_SIGHUPIgnored(t *testing.T) {
	child := startSignalHelper(t, "single")
	if err := deliverSignal(child.cmd.Process.Pid, syscall.SIGHUP); err != nil {
		t.Skipf("当前环境无法向独立子进程组投递 SIGHUP，跳过: %v", err)
	}
	child.assertAlive(t, 800*time.Millisecond)
	child.send(t)
	if err := child.waitExit(t); err != nil {
		t.Fatalf("SIGTERM 应触发正常退出流程（退出码 0），实际: %v，输出: %s", err, child.output.String())
	}
	child.requireLine(t, "SIG="+helperTermSignal.String())
	child.requireLine(t, "HANDLER_CALLS=1")
	child.requireLine(t, "LISTEN_RETURNED")
	if strings.Count(child.output.String(), "SIG=") != 1 {
		t.Fatalf("SIGHUP 不应触发 handler，仅 SIGTERM 应触发一次，输出: %s", child.output.String())
	}
}
