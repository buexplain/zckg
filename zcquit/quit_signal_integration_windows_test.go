//go:build windows

package zcquit

import (
	"os/exec"
	"syscall"
)

// helperTermSignal 为 Windows 上可投递的终止类信号。
// Windows 仅能经由控制台事件投递信号：GenerateConsoleCtrlEvent 的
// CTRL_C_EVENT / CTRL_BREAK_EVENT 由 Go 运行时映射为 SIGINT
// （见 runtime/os_windows.go 的 ctrlHandler），
// SIGHUP / SIGQUIT / SIGTERM 在该平台不可投递。
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
func deliverSignal(pid int, sig syscall.Signal) error {
	if sig != syscall.SIGINT {
		// Windows 平台仅支持投递映射到 SIGINT 的控制事件
		return syscall.EWINDOWS
	}
	dll, err := syscall.LoadDLL("kernel32.dll")
	if err != nil {
		return err
	}
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
