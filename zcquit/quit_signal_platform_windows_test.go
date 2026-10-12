//go:build windows

package zcquit

import (
	"errors"
	"os/exec"
	"syscall"
	"testing"
)

// helperTermSignal 仅表示本 helper 可发送的 Windows 控制台信号：
// GenerateConsoleCtrlEvent 的 Ctrl+C / Ctrl+Break 由 Go 运行时映射为 SIGINT，
// 本 helper 使用可定向到独立进程组的 Ctrl+Break，不会发送 SIGTERM。
// Windows SIGTERM 可来自关闭控制台、注销或关机事件，不能由本 helper 投递；
// 这些系统终止事件只提供有限清理机会，本测试不触发它们，也不将 taskkill 当作 SIGTERM。
var helperTermSignal = syscall.SIGINT

// prepareChildProcess 使子进程成为新进程组的组长：
// GenerateConsoleCtrlEvent 按进程组 ID 投递，独立进程组可确保
// 控制事件仅发给子进程，不波及父进程所在的控制台/进程组。
func prepareChildProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

// deliverSignal 向子进程投递信号：以 CTRL_BREAK_EVENT 形式投递（映射为 SIGINT）。
// 环境无控制台等场景下 GenerateConsoleCtrlEvent 会失败，由调用方据此跳过测试。
func deliverSignal(pid int, sig syscall.Signal) (retErr error) {
	if sig != syscall.SIGINT {
		// 本 helper 仅支持投递映射到 SIGINT 的控制事件。
		return syscall.EWINDOWS
	}
	dll, err := syscall.LoadDLL("kernel32.dll")
	if err != nil {
		return err
	}
	defer func() {
		retErr = errors.Join(retErr, dll.Release())
	}()
	proc, err := dll.FindProc("GenerateConsoleCtrlEvent")
	if err != nil {
		return err
	}
	r, _, callErr := proc.Call(uintptr(syscall.CTRL_BREAK_EVENT), uintptr(pid))
	if r == 0 {
		return callErr
	}
	return nil
}

// assertDefaultSignalExit 精确验证 Ctrl+Break 的默认退出码，而非任意非零退出或清理超时。
func assertDefaultSignalExit(t *testing.T, err error) {
	t.Helper()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("第二个控制事件应终止子进程，实际退出结果: %v", err)
	}
	const statusControlCExit = uint32(0xC000013A)
	if got := uint32(exitErr.ExitCode()); got != statusControlCExit {
		t.Fatalf("Ctrl+Break 默认退出码应为 %#x，实际: %#x（%v）", statusControlCExit, got, exitErr)
	}
}
