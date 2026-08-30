package zcquit

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// 本文件以「子进程 + 真实操作系统信号」方式覆盖 listen() 的信号接收路径
// （quit.go 中 signalCH 分支、SIGHUP 忽略分支、break loop 后的 Stop/doShutdown）。
//
// 设计说明：
//   - 测试二进制自身以 -test.run=TestSignalHelperProcess 重新启动为子进程，
//     子进程内调用真实的 Listen() 阻塞等待信号，父进程向子进程投递信号；
//   - 进程内直接对自身投递控制事件（尤其 Windows 的 GenerateConsoleCtrlEvent）
//     可能波及同控制台的父级进程，故必须使用独立子进程隔离；
//   - 子进程打印 READY 后父进程再延迟投递（与 Go 官方 os/signal 的
//     signal_windows_test.go 相同策略），确保 listen goroutine 已完成 signal.Notify 注册。

// signalHelperEnv 标记子进程进入 helper 模式的环境变量。
const signalHelperEnv = "ZCQUIT_SIGNAL_HELPER"

// safeBuffer 并发安全的输出缓冲：子进程输出由 exec 内部协程写入，父测试协程轮询读取。
type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// TestSignalHelperProcess 仅在 helper 模式（子进程）下执行真实逻辑，父进程中直接 Skip。
// 声明在文件末尾的完整流程测试之前没有依赖约束：子进程是全新进程，包级状态全新。
func TestSignalHelperProcess(t *testing.T) {
	if os.Getenv(signalHelperEnv) != "1" {
		t.Skip("仅作为信号测试的子进程运行")
	}

	// 注册一个记录收到信号的 handler（收到信号即说明走了 listen 的 signalCH 分支）
	gotSig := make(chan os.Signal, 1)
	AddSigHandler(0, func(sig os.Signal) {
		gotSig <- sig
	})

	listenDone := make(chan struct{})
	go func() {
		Listen()
		close(listenDone)
	}()

	// 通知父进程可以投递信号（listen goroutine 由 AddSigHandler 触发的 doListen 启动，
	// signal.Notify 注册在 goroutine 内完成，父进程收到 READY 后仍会再延迟等待）
	fmt.Println("READY")

	select {
	case sig := <-gotSig:
		fmt.Printf("SIG=%v\n", sig)
	case <-time.After(30 * time.Second):
		fmt.Println("TIMEOUT_WAITING_SIG")
		os.Exit(1)
	}
	select {
	case <-listenDone:
		fmt.Println("LISTEN_RETURNED")
	case <-time.After(10 * time.Second):
		fmt.Println("TIMEOUT_WAITING_LISTEN")
		os.Exit(1)
	}
}

// startSignalHelper 启动测试二进制自身作为子进程进入 helper 模式，等待其打印 READY。
// 环境无法启动子进程时跳过测试（不失败）。
func startSignalHelper(t *testing.T) (*exec.Cmd, *safeBuffer) {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Skipf("无法获取测试二进制路径，跳过信号测试: %v", err)
	}
	// 转发 -test.gocoverdir：go test 经该命令行参数指定覆盖率数据目录，
	// 子进程不带此参数则其覆盖率计数不会被合并
	args := []string{"-test.run=TestSignalHelperProcess"}
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "-test.gocoverdir=") {
			args = append(args, a)
		}
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), signalHelperEnv+"=1")
	buf := &safeBuffer{}
	cmd.Stdout = buf
	cmd.Stderr = buf
	prepareChildProcess(cmd) // 平台特化：独立进程组，防止控制事件波及父进程所在控制台

	if err := cmd.Start(); err != nil {
		t.Skipf("无法启动子进程，跳过信号测试: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})

	// 等待子进程就绪（最多 15 秒）
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), "READY") {
			return cmd, buf
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	t.Skipf("子进程未能在超时内就绪，跳过信号测试，输出: %s", buf.String())
	return nil, nil
}

// waitProcessExit 等待子进程退出（带超时），返回退出错误与全部输出。
func waitProcessExit(t *testing.T, cmd *exec.Cmd, buf *safeBuffer, timeout time.Duration) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		t.Fatalf("子进程未在 %v 内退出，输出: %s", timeout, buf.String())
		return nil
	}
}

// TestListen_SignalTriggersShutdown 锁死「真实操作系统信号触发完整退出流程」：
// 子进程内 Listen 阻塞，父进程投递终止类信号后，
// handler 应收到非 nil 信号值，且 Listen 解除阻塞返回。
// 覆盖 listen() 的 signalCH 接收分支、break loop、signal.Stop 与 doShutdown(sig)。
func TestListen_SignalTriggersShutdown(t *testing.T) {
	cmd, buf := startSignalHelper(t)

	// 与 Go 官方 signal_windows_test.go 相同：延迟等待 listen goroutine 完成 signal.Notify 注册
	time.Sleep(time.Second)

	if err := deliverSignal(cmd.Process.Pid, helperTermSignal); err != nil {
		t.Skipf("当前环境无法向子进程投递信号，跳过: %v", err)
	}

	if err := waitProcessExit(t, cmd, buf, 30*time.Second); err != nil {
		t.Fatalf("子进程应正常退出（退出码 0），实际: %v，输出: %s", err, buf.String())
	}

	out := buf.String()
	if !strings.Contains(out, "SIG=") || strings.Contains(out, "SIG=<nil>") {
		t.Fatalf("handler 应收到非 nil 的真实信号，输出: %s", out)
	}
	if !strings.Contains(out, "LISTEN_RETURNED") {
		t.Fatalf("收到信号后 Listen 应解除阻塞返回，输出: %s", out)
	}
}
