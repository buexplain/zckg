package zcquit

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 本文件以「子进程 + 真实操作系统信号」方式覆盖 listen() 的信号接收路径，
// 包括信号触发、主动 Shutdown、其他 Notify 订阅者共存时的信号生命周期。
//
// 每个场景都启动全新测试子进程，不复用或重置包级状态；只向其独立进程组投递信号，
// 防止 Windows 控制事件波及父进程。AddSigHandler 返回前已同步完成 signal.Notify，
// 因而子进程此后打印的 READY 就是真实就绪屏障，父进程无需再延迟投递。
const signalHelperEnv = "ZCQUIT_SIGNAL_HELPER"

// safeBuffer 并发保存子进程输出，并通知等待方重新检查握手标记，无需轮询。
type safeBuffer struct {
	mu      sync.Mutex
	b       bytes.Buffer
	changed chan struct{}
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.b.Write(p)
	select {
	case s.changed <- struct{}{}:
	default:
	}
	return n, err
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// signalHelperLine 写出完整握手行；写失败必须使子进程失败，不能留下虚假的就绪状态。
func signalHelperLine(t *testing.T, format string, args ...any) {
	t.Helper()
	if _, err := fmt.Fprintf(os.Stdout, format+"\n", args...); err != nil {
		t.Fatalf("写子进程握手输出失败: %v", err)
	}
}

// waitSignalRelease 等待父进程通过 stdin 放行清理；超时或 I/O 错误均导致子进程失败。
func waitSignalRelease(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		if err := os.Stdin.Close(); err != nil {
			t.Errorf("关闭子进程 stdin 失败: %v", err)
		}
	})
	type readResult struct {
		line string
		err  error
	}
	input := make(chan readResult, 1)
	go func() {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		input <- readResult{line: line, err: err}
	}()
	select {
	case result := <-input:
		if result.err != nil {
			t.Fatalf("读取清理放行命令失败: %v", result.err)
		}
		if result.line != "RELEASE\n" {
			t.Fatalf("清理放行命令应为 RELEASE，实际: %q", result.line)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("等待父进程放行清理超时")
	}
}

// TestSignalHelperProcess 在全新子进程中注册真实信号监听，并按场景阻塞或完成清理。
// 普通父进程入口跳过；每个 handler 和 Listen/Shutdown/stdin 等待都设有超时。
func TestSignalHelperProcess(t *testing.T) {
	mode := os.Getenv(signalHelperEnv)
	if mode == "" {
		t.Skip("仅作为信号测试的子进程运行")
	}
	switch mode {
	case "single", "signal-block", "notify-block", "shutdown-block":
	default:
		t.Fatalf("未知信号测试场景: %q", mode)
	}

	// 独立订阅仅用于 notify-block 场景，不能影响默认终止/主动退出场景的信号行为。
	extraSignals := make(chan os.Signal, 2)
	if mode == "notify-block" {
		signal.Notify(extraSignals, helperTermSignal)
		defer signal.Stop(extraSignals)
	}

	type handlerResult struct {
		sig      os.Signal
		timedOut bool
	}
	started := make(chan os.Signal, 2)
	completed := make(chan handlerResult, 2)
	release := make(chan struct{})
	var handlerCalls atomic.Int32
	AddSigHandler(0, func(sig os.Signal) {
		handlerCalls.Add(1)
		started <- sig
		result := handlerResult{sig: sig}
		if mode != "single" {
			select {
			case <-release:
			case <-time.After(30 * time.Second):
				result.timedOut = true
			}
		}
		if !result.timedOut {
			signalHelperLine(t, "HANDLER_COMPLETED sig=%v", sig)
		}
		completed <- result
	})

	// 注册返回即完成 Notify；READY 后立即投递即可，不依赖 Listen goroutine 的调度。
	signalHelperLine(t, "READY")
	listenDone := make(chan struct{})
	go func() {
		Listen()
		close(listenDone)
	}()
	shutdownDone := make(chan struct{})
	if mode == "shutdown-block" {
		go func() {
			Shutdown()
			close(shutdownDone)
		}()
	}

	var initialSignal os.Signal
	select {
	case initialSignal = <-started:
	case <-time.After(30 * time.Second):
		t.Fatal("等待 handler 开始超时")
	}
	if mode == "single" {
		signalHelperLine(t, "SIG=%v", initialSignal)
	} else {
		signalHelperLine(t, "HANDLER_STARTED sig=%v", initialSignal)
	}

	if mode == "notify-block" {
		for i := 1; i <= 2; i++ {
			select {
			case sig := <-extraSignals:
				if sig != helperTermSignal {
					t.Fatalf("独立订阅收到意外信号: %v，期望: %v", sig, helperTermSignal)
				}
				signalHelperLine(t, "EXTRA_SIG_%d=%v", i, sig)
			case <-time.After(30 * time.Second):
				t.Fatalf("独立订阅等待第 %d 次信号超时", i)
			}
		}
	}
	if mode != "single" {
		waitSignalRelease(t)
		close(release)
	}
	select {
	case result := <-completed:
		if result.timedOut || result.sig != initialSignal {
			t.Fatalf("handler 清理结果异常: %+v，初始信号: %v", result, initialSignal)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("等待 handler 完成超时")
	}
	select {
	case <-listenDone:
	case <-time.After(10 * time.Second):
		t.Fatal("等待 Listen 返回超时")
	}
	if mode == "shutdown-block" {
		select {
		case <-shutdownDone:
			signalHelperLine(t, "SHUTDOWN_RETURNED")
		case <-time.After(10 * time.Second):
			t.Fatal("等待首次 Shutdown 返回超时")
		}
	}
	if calls := handlerCalls.Load(); calls != 1 {
		t.Fatalf("handler 应仅调用一次，实际: %d", calls)
	}
	signalHelperLine(t, "HANDLER_CALLS=1")
	signalHelperLine(t, "LISTEN_RETURNED")
}

// signalHelperProcess 只调用一次 Wait；关闭 exited 后才读取 waitErr，避免观察退出状态时竞争。
type signalHelperProcess struct {
	cmd     *exec.Cmd
	output  *safeBuffer
	stdin   io.WriteCloser
	exited  chan struct{}
	waitErr error
}

// startSignalHelper 启动独立进程组中的新子进程，等待注册完成的 READY 握手。
// 仅进程无法启动时跳过；已启动后的异常退出或握手超时属于真实失败。
func startSignalHelper(t *testing.T, mode string) *signalHelperProcess {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("获取测试二进制路径失败: %v", err)
	}
	args := []string{"-test.run=^TestSignalHelperProcess$", "-test.timeout=90s"}
	// 转发覆盖率目录，使子进程中的真实信号分支计入覆盖率。
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "-test.gocoverdir=") {
			args = append(args, arg)
		}
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), signalHelperEnv+"="+mode)
	buf := &safeBuffer{changed: make(chan struct{}, 1)}
	cmd.Stdout, cmd.Stderr = buf, buf
	prepareChildProcess(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("创建子进程 stdin 失败: %v", err)
	}
	if err := cmd.Start(); err != nil {
		if closeErr := stdin.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
			t.Errorf("启动失败后关闭 stdin 失败: %v", closeErr)
		}
		t.Skipf("当前环境无法启动子进程，跳过信号测试: %v", err)
	}
	child := &signalHelperProcess{cmd: cmd, output: buf, stdin: stdin, exited: make(chan struct{})}
	go func() {
		child.waitErr = cmd.Wait()
		close(child.exited)
	}()
	t.Cleanup(func() {
		// Wait 也会关闭 StdinPipe；因此仅容忍其已关闭错误，其他 I/O 错误均报告。
		if err := child.stdin.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			t.Errorf("关闭父进程 stdin 管道失败: %v", err)
		}
		select {
		case <-child.exited:
			return
		default:
		}
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			t.Errorf("清理信号子进程失败: %v", err)
		}
		select {
		case <-child.exited:
			// 提前失败/跳过时主动 Kill 的退出错误已由清理行为解释，无需再次报告。
		case <-time.After(5 * time.Second):
			t.Errorf("清理后等待信号子进程退出超时，输出: %s", buf.String())
		}
	})
	child.awaitLine(t, "READY")
	return child
}

// awaitLine 以输出通知等待精确握手行，同时监测实际进程退出与独立超时。
func (p *signalHelperProcess) awaitLine(t *testing.T, line string) {
	t.Helper()
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	for {
		if strings.Contains("\n"+p.output.String(), "\n"+line+"\n") {
			return
		}
		select {
		case <-p.output.changed:
		case <-p.exited:
			t.Fatalf("等待 %q 时子进程提前退出: %v，输出: %s", line, p.waitErr, p.output.String())
		case <-timer.C:
			t.Fatalf("等待子进程握手 %q 超时，输出: %s", line, p.output.String())
		}
	}
}

// assertAlive 的否定断言直接监测 Wait 完成，不以缺少日志代替存活证据。
func (p *signalHelperProcess) assertAlive(t *testing.T, duration time.Duration) {
	t.Helper()
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-p.exited:
		t.Fatalf("子进程应在 %v 观察窗口内保持存活，实际退出: %v，输出: %s", duration, p.waitErr, p.output.String())
	case <-timer.C:
	}
}

// waitExit 有界等待已启动的 Wait；超时时由注册的 cleanup 终止并回收子进程。
func (p *signalHelperProcess) waitExit(t *testing.T) error {
	t.Helper()
	select {
	case <-p.exited:
		return p.waitErr
	case <-time.After(15 * time.Second):
		t.Fatalf("等待子进程退出超时，输出: %s", p.output.String())
		return nil
	}
}

// send 向该测试新建的独立子进程组发送信号，不修改父进程的信号订阅。
func (p *signalHelperProcess) send(t *testing.T) {
	t.Helper()
	if err := deliverSignal(p.cmd.Process.Pid, helperTermSignal); err != nil {
		select {
		case <-p.exited:
			t.Fatalf("投递前子进程已异常退出: %v，投递错误: %v，输出: %s", p.waitErr, err, p.output.String())
		default:
			t.Skipf("当前环境无法向独立子进程组投递 %v，跳过: %v", helperTermSignal, err)
		}
	}
}

// finish 通过 stdin 释放阻塞 handler，并验证清理仅执行一次、信号未变且 Listen 正常返回。
func (p *signalHelperProcess) finish(t *testing.T, sig os.Signal) {
	t.Helper()
	const command = "RELEASE\n"
	if n, err := io.WriteString(p.stdin, command); err != nil || n != len(command) {
		t.Fatalf("写入清理放行命令失败: n=%d，err=%v，输出: %s", n, err, p.output.String())
	}
	if err := p.waitExit(t); err != nil {
		t.Fatalf("放行后子进程应正常退出，实际: %v，输出: %s", err, p.output.String())
	}
	p.requireLine(t, fmt.Sprintf("HANDLER_COMPLETED sig=%v", sig))
	p.requireLine(t, "HANDLER_CALLS=1")
	p.requireLine(t, "LISTEN_RETURNED")
}

// requireLine 对已经结束的子进程输出作完整行断言，防止短串误命中。
func (p *signalHelperProcess) requireLine(t *testing.T, line string) {
	t.Helper()
	if !strings.Contains("\n"+p.output.String(), "\n"+line+"\n") {
		t.Fatalf("子进程缺少输出行 %q，输出: %s", line, p.output.String())
	}
}

// TestListen_SignalTriggersShutdown 验证注册返回后的真实信号立即触发清理，Listen 正常返回。
func TestListen_SignalTriggersShutdown(t *testing.T) {
	child := startSignalHelper(t, "single")
	// READY 已确认同步完成 Notify，不再睡眠等待后台 goroutine。
	child.send(t)
	if err := child.waitExit(t); err != nil {
		t.Fatalf("子进程应正常退出（退出码 0），实际: %v，输出: %s", err, child.output.String())
	}
	child.requireLine(t, "SIG="+helperTermSignal.String())
	child.requireLine(t, "LISTEN_RETURNED")
}

// TestListen_SecondSignalDefaultTerminates 验证信号触发清理且 handler 阻塞时，
// 无其他 Notify 订阅的第二个真实信号按平台默认行为终止进程，清理未完成。
func TestListen_SecondSignalDefaultTerminates(t *testing.T) {
	child := startSignalHelper(t, "signal-block")
	child.send(t)
	child.awaitLine(t, "HANDLER_STARTED sig="+helperTermSignal.String())
	child.send(t)
	assertDefaultSignalExit(t, child.waitExit(t))
	out := child.output.String()
	if strings.Contains(out, "HANDLER_COMPLETED") || strings.Contains(out, "LISTEN_RETURNED") {
		t.Fatalf("第二个信号默认终止时不应完成阻塞清理或返回 Listen，输出: %s", out)
	}
}

// TestListen_SecondSignalWithIndependentNotify 验证其他 Notify 订阅不被 zcquit 的 Stop 取消：
// 两次真实信号均可观察，第二次之后另设存活观察窗口，stdin 放行后正常完成清理。
func TestListen_SecondSignalWithIndependentNotify(t *testing.T) {
	child := startSignalHelper(t, "notify-block")
	child.send(t)
	child.awaitLine(t, "HANDLER_STARTED sig="+helperTermSignal.String())
	child.awaitLine(t, "EXTRA_SIG_1="+helperTermSignal.String())
	child.send(t)
	child.awaitLine(t, "EXTRA_SIG_2="+helperTermSignal.String())
	child.assertAlive(t, 800*time.Millisecond)
	child.finish(t, helperTermSignal)
	if count := strings.Count(child.output.String(), "EXTRA_SIG_"); count != 2 {
		t.Fatalf("独立订阅应观察到两次信号，实际标记数: %d，输出: %s", count, child.output.String())
	}
}

// TestShutdown_FirstSignalDuringBlockedHandler 验证主动 Shutdown 已获执行权、handler 阻塞时，
// 首个真实信号不强退且不改变 nil 参数；独立观察存活后放行，Shutdown 和 Listen 均返回。
func TestShutdown_FirstSignalDuringBlockedHandler(t *testing.T) {
	child := startSignalHelper(t, "shutdown-block")
	child.awaitLine(t, "HANDLER_STARTED sig=<nil>")
	child.send(t)
	child.assertAlive(t, 800*time.Millisecond)
	child.finish(t, nil)
	child.requireLine(t, "SHUTDOWN_RETURNED")
}
