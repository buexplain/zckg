package zcquit

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// resetState 在每个测试前重置包级可变状态，使测试可重复执行。
// 测试直接调用 executeShutdown（不依赖 sync.Once）以绕过无法重置的 sync.Once。
func resetState() {
	Ctx, cancel = context.WithCancel(context.Background())
	signalHandlerMap = map[int][]SigHandler{}
	signalHandlerMux = sync.RWMutex{}
	waitChan = make(chan struct{})
}

// TestAddSigHandler 测试 AddSigHandler 注册 handler 到正确的级别
func TestAddSigHandler(t *testing.T) {
	resetState()

	called0 := false
	called1 := false

	AddSigHandler(0, func(sig os.Signal) {
		called0 = true
	})
	AddSigHandler(1, func(sig os.Signal) {
		called1 = true
	})

	// 验证 handler 已注册到正确的级别
	signalHandlerMux.RLock()

	if len(signalHandlerMap[0]) != 1 {
		signalHandlerMux.RUnlock()
		t.Fatalf("期望 level 0 有 1 个 handler，实际 %d", len(signalHandlerMap[0]))
	}
	if len(signalHandlerMap[1]) != 1 {
		signalHandlerMux.RUnlock()
		t.Fatalf("期望 level 1 有 1 个 handler，实际 %d", len(signalHandlerMap[1]))
	}

	// 执行 handler 验证可调用
	signalHandlerMap[0][0](nil)
	signalHandlerMap[1][0](nil)
	signalHandlerMux.RUnlock()

	if !called0 || !called1 {
		t.Fatal("handler 未被正确调用")
	}
}

// TestAddSigHandler_MultipleHandlersSameLevel 测试同一级别注册多个 handler
func TestAddSigHandler_MultipleHandlersSameLevel(t *testing.T) {
	resetState()

	AddSigHandler(0,
		func(sig os.Signal) {},
		func(sig os.Signal) {},
	)
	AddSigHandler(0, func(sig os.Signal) {})

	signalHandlerMux.RLock()
	defer signalHandlerMux.RUnlock()

	if len(signalHandlerMap[0]) != 3 {
		t.Fatalf("期望 level 0 有 3 个 handler，实际 %d", len(signalHandlerMap[0]))
	}
}

// TestExecuteShutdown_LevelOrder 测试 handler 按 level 升序执行，级别间串行
func TestExecuteShutdown_LevelOrder(t *testing.T) {
	resetState()

	var order []int
	var mu sync.Mutex

	AddSigHandler(2, func(sig os.Signal) {
		mu.Lock()
		order = append(order, 2)
		mu.Unlock()
	})
	AddSigHandler(0, func(sig os.Signal) {
		mu.Lock()
		order = append(order, 0)
		mu.Unlock()
	})
	AddSigHandler(1, func(sig os.Signal) {
		mu.Lock()
		order = append(order, 1)
		mu.Unlock()
	})

	executeShutdown(nil)

	if len(order) != 3 {
		t.Fatalf("期望 3 个 handler 被执行，实际 %d", len(order))
	}
	// 级别间串行，所以顺序一定是 0 → 1 → 2
	for i, expected := range []int{0, 1, 2} {
		if order[i] != expected {
			t.Fatalf("期望第 %d 个执行的 level 为 %d，实际为 %d，完整顺序: %v", i, expected, order[i], order)
		}
	}
}

// TestExecuteShutdown_ConcurrentWithinLevel 测试同级别内 handler 并发执行
func TestExecuteShutdown_ConcurrentWithinLevel(t *testing.T) {
	resetState()

	var running atomic.Int32
	var maxConcurrent atomic.Int32

	for i := 0; i < 5; i++ {
		AddSigHandler(0, func(sig os.Signal) {
			cur := running.Add(1)
			// 更新最大并发数
			for {
				old := maxConcurrent.Load()
				if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond) // 保持一段时间以检测并发
			running.Add(-1)
		})
	}

	executeShutdown(nil)

	if maxConcurrent.Load() < 2 {
		t.Fatalf("期望同级别 handler 存在并发执行（最大并发 >= 2），实际最大并发: %d", maxConcurrent.Load())
	}
}

// TestExecuteShutdown_LevelSequential 测试不同级别间严格串行
func TestExecuteShutdown_LevelSequential(t *testing.T) {
	resetState()

	var level0Done atomic.Bool
	var level1Overlap atomic.Bool

	AddSigHandler(0, func(sig os.Signal) {
		time.Sleep(50 * time.Millisecond)
		level0Done.Store(true)
	})
	AddSigHandler(1, func(sig os.Signal) {
		// 如果 level 0 还没完成，说明存在重叠（不应该发生）
		if !level0Done.Load() {
			level1Overlap.Store(true)
		}
	})

	executeShutdown(nil)

	if level1Overlap.Load() {
		t.Fatal("level 1 的 handler 在 level 0 完成前就开始执行，级别间未串行等待")
	}
}

// TestExecuteShutdown_PanicRecovery 测试单个 handler panic 不影响其他 handler
func TestExecuteShutdown_PanicRecovery(t *testing.T) {
	resetState()

	var normalCalled bool

	AddSigHandler(0,
		func(sig os.Signal) {
			panic("模拟 panic")
		},
		func(sig os.Signal) {
			normalCalled = true
		},
	)
	// 高级别的 handler 也应正常执行
	AddSigHandler(1, func(sig os.Signal) {
		normalCalled = true
	})

	executeShutdown(nil)

	if !normalCalled {
		t.Fatal("某个 handler panic 后，其他 handler 未被执行")
	}
}

// TestExecuteShutdown_ContextCancelled 测试 executeShutdown 取消全局上下文
func TestExecuteShutdown_ContextCancelled(t *testing.T) {
	resetState()

	select {
	case <-Ctx.Done():
		t.Fatal("executeShutdown 之前 Ctx 不应已被取消")
	default:
	}

	executeShutdown(nil)

	select {
	case <-Ctx.Done():
		// 期望：上下文已取消
	default:
		t.Fatal("executeShutdown 之后 Ctx 应已被取消")
	}
}

// TestExecuteShutdown_WaitChanClosed 测试 executeShutdown 关闭 waitChan 以解除 Listen 阻塞
func TestExecuteShutdown_WaitChanClosed(t *testing.T) {
	resetState()

	select {
	case <-waitChan:
		t.Fatal("executeShutdown 之前 waitChan 不应已关闭")
	default:
	}

	executeShutdown(nil)

	select {
	case <-waitChan:
		// 期望：waitChan 已关闭
	case <-time.After(time.Second):
		t.Fatal("executeShutdown 之后 waitChan 应已关闭")
	}
}

// TestExecuteShutdown_SignalPassedToHandler 测试信号值正确传递给 handler
func TestExecuteShutdown_SignalPassedToHandler(t *testing.T) {
	resetState()

	var receivedSig os.Signal

	AddSigHandler(0, func(sig os.Signal) {
		receivedSig = sig
	})

	// executeShutdown(nil) 模拟 Shutdown() 主动调用，handler 收到 nil
	executeShutdown(nil)

	if receivedSig != nil {
		t.Fatalf("Shutdown 触发时 handler 应收到 nil 信号，实际收到 %v", receivedSig)
	}
}

// TestExecuteShutdown_NegativeLevel 测试负数 level 也能正确排序执行
func TestExecuteShutdown_NegativeLevel(t *testing.T) {
	resetState()

	var order []int
	var mu sync.Mutex

	AddSigHandler(1, func(sig os.Signal) {
		mu.Lock()
		order = append(order, 1)
		mu.Unlock()
	})
	AddSigHandler(-1, func(sig os.Signal) {
		mu.Lock()
		order = append(order, -1)
		mu.Unlock()
	})
	AddSigHandler(0, func(sig os.Signal) {
		mu.Lock()
		order = append(order, 0)
		mu.Unlock()
	})

	executeShutdown(nil)

	expected := []int{-1, 0, 1}
	for i, v := range expected {
		if order[i] != v {
			t.Fatalf("期望执行顺序 %v，实际 %v", expected, order)
		}
	}
}

// TestExecuteShutdown_NoHandlers 测试没有注册 handler 时 executeShutdown 也能正常完成
func TestExecuteShutdown_NoHandlers(t *testing.T) {
	resetState()

	// 不应 panic 或死锁
	executeShutdown(nil)

	select {
	case <-Ctx.Done():
		// 上下文应已取消
	case <-time.After(time.Second):
		t.Fatal("无 handler 时 executeShutdown 应仍能正常完成")
	}

	select {
	case <-waitChan:
		// waitChan 应已关闭
	default:
		t.Fatal("无 handler 时 waitChan 也应被关闭")
	}
}

// TestCtx_InitialState 测试初始状态下 Ctx 未被取消
func TestCtx_InitialState(t *testing.T) {
	resetState()

	select {
	case <-Ctx.Done():
		t.Fatal("初始状态下 Ctx 不应已被取消")
	default:
		// 期望：未取消
	}
}

// TestExecuteShutdown_CancelBeforeHandlers 测试 cancel 在 handler 执行之前被调用
func TestExecuteShutdown_CancelBeforeHandlers(t *testing.T) {
	resetState()

	var ctxDoneDuringHandler atomic.Bool

	AddSigHandler(0, func(sig os.Signal) {
		// handler 执行时，Ctx 应该已经被取消
		select {
		case <-Ctx.Done():
			ctxDoneDuringHandler.Store(true)
		default:
		}
	})

	executeShutdown(nil)

	if !ctxDoneDuringHandler.Load() {
		t.Fatal("handler 执行时 Ctx 应已被取消（cancel 应先于 handler 执行）")
	}
}
