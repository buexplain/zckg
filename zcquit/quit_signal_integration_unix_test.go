//go:build !windows

package zcquit

import (
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

// deliverSignal 向子进程投递指定信号。
func deliverSignal(pid int, sig syscall.Signal) error {
	return syscall.Kill(pid, sig)
}

// TestListen_SIGHUPIgnored 锁死「SIGHUP 不触发退出流程」：
// 先投递 SIGHUP，子进程应继续存活（listen 循环内 continue 忽略）；
// 再投递 SIGTERM，子进程正常完成退出流程。
// 覆盖 listen() 的 SIGHUP 忽略分支（该分支在 Windows 上不可投递，仅 Unix 可覆盖）。
func TestListen_SIGHUPIgnored(t *testing.T) {
	cmd, buf := startSignalHelper(t)
	time.Sleep(time.Second)

	if err := deliverSignal(cmd.Process.Pid, syscall.SIGHUP); err != nil {
		t.Skipf("当前环境无法投递 SIGHUP，跳过: %v", err)
	}

	// SIGHUP 应被忽略：子进程在 800ms 内不应退出
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		t.Fatalf("SIGHUP 不应触发退出流程，子进程却提前退出: %v，输出: %s", err, buf.String())
	case <-time.After(800 * time.Millisecond):
		// 期望：子进程仍在监听
	}

	if err := deliverSignal(cmd.Process.Pid, syscall.SIGTERM); err != nil {
		_ = cmd.Process.Kill()
		t.Skipf("当前环境无法投递 SIGTERM，跳过: %v", err)
	}

	select {
	case err := <-done:
		if err != nil && !strings.Contains(buf.String(), "LISTEN_RETURNED") {
			t.Fatalf("SIGTERM 应触发正常退出流程: %v，输出: %s", err, buf.String())
		}
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("SIGHUP 忽略后 SIGTERM 应能使 Listen 返回，输出: %s", buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "LISTEN_RETURNED") {
		t.Fatalf("SIGTERM 后 Listen 应返回，输出: %s", out)
	}
	if strings.Count(out, "SIG=") != 1 {
		t.Fatalf("SIGHUP 不应触发 handler，仅 SIGTERM 应触发一次，输出: %s", out)
	}
}
