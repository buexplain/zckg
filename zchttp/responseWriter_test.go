package zchttp

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ========== IsResponseWritten 测试 ==========

// TestIsResponseWritten 验证 IsResponseWritten 对写入状态的判定
func TestIsResponseWritten(t *testing.T) {
	// 未包装的 ResponseWriter 无 Written() 能力 → 恒为 false
	rec := httptest.NewRecorder()
	if IsResponseWritten(rec) {
		t.Fatal("plain recorder should report not written")
	}

	// 新包装未写入 → false
	rw := &responseWriter{ResponseWriter: rec}
	if IsResponseWritten(rw) {
		t.Fatal("fresh responseWriter should report not written")
	}

	// Write 后 → true
	_, _ = rw.Write([]byte("x"))
	if !IsResponseWritten(rw) {
		t.Fatal("responseWriter should report written after Write")
	}

	// 仅 WriteHeader 也视为已写入
	rw2 := &responseWriter{ResponseWriter: httptest.NewRecorder()}
	rw2.WriteHeader(http.StatusTeapot)
	if !IsResponseWritten(rw2) {
		t.Fatal("responseWriter should report written after WriteHeader")
	}
}

// ========== Flusher / Hijacker / Pusher 透传测试 ==========

// TestResponseWriter_FlusherPassThrough 验证 responseWriter 包装器透传 http.Flusher：
// SSE 等流式场景依赖 w.(http.Flusher) 断言，包装后不应丢失底层能力
func TestResponseWriter_FlusherPassThrough(t *testing.T) {
	rec := httptest.NewRecorder() // ResponseRecorder 实现了 http.Flusher
	rw := &responseWriter{ResponseWriter: rec}

	var w http.ResponseWriter = rw
	f, ok := w.(http.Flusher)
	if !ok {
		t.Fatal("responseWriter does not pass through http.Flusher, SSE streaming broken")
	}
	f.Flush()
	if !rec.Flushed {
		t.Fatal("Flush not delegated to underlying ResponseWriter")
	}
	if !rw.Written() {
		t.Fatal("Flush should mark responseWriter as written")
	}
}

// TestResponseWriter_HijackerNotSupported 验证底层不支持 Hijack 时返回错误而非 panic
func TestResponseWriter_HijackerNotSupported(t *testing.T) {
	rec := httptest.NewRecorder() // ResponseRecorder 未实现 http.Hijacker
	rw := &responseWriter{ResponseWriter: rec}

	_, _, err := rw.Hijack()
	if err != http.ErrNotSupported {
		t.Fatalf("Hijack on non-hijackable underlying writer should return ErrNotSupported, got %v", err)
	}
}

// fakeHijacker 为 ResponseRecorder 补充 Hijack 能力，模拟支持 WebSocket 升级的底层 writer
type fakeHijacker struct {
	*httptest.ResponseRecorder
	hijacked bool
}

func (f *fakeHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	f.hijacked = true
	return nil, nil, nil
}

// TestResponseWriter_HijackPassThrough 验证底层支持 Hijack 时透传调用并标记已写入
func TestResponseWriter_HijackPassThrough(t *testing.T) {
	fh := &fakeHijacker{ResponseRecorder: httptest.NewRecorder()}
	rw := &responseWriter{ResponseWriter: fh}

	if _, _, err := rw.Hijack(); err != nil {
		t.Fatalf("Hijack should delegate to underlying Hijacker, got error: %v", err)
	}
	if !fh.hijacked {
		t.Fatal("Hijack not delegated to underlying writer")
	}
	if !rw.Written() {
		t.Fatal("Hijack should mark responseWriter as written")
	}
}

// TestResponseWriter_PushNotSupported 验证底层不支持 HTTP/2 Push 时返回 ErrNotSupported
func TestResponseWriter_PushNotSupported(t *testing.T) {
	rec := httptest.NewRecorder() // ResponseRecorder 未实现 http.Pusher
	rw := &responseWriter{ResponseWriter: rec}

	if err := rw.Push("/app.js", nil); err != http.ErrNotSupported {
		t.Fatalf("Push on non-pusher underlying writer should return ErrNotSupported, got %v", err)
	}
}

// fakePusher 为 ResponseRecorder 补充 Push 能力，模拟支持 HTTP/2 推送的底层 writer
type fakePusher struct {
	*httptest.ResponseRecorder
	pushedTarget string
}

func (f *fakePusher) Push(target string, _ *http.PushOptions) error {
	f.pushedTarget = target
	return nil
}

// TestResponseWriter_PushPassThrough 验证底层支持 Push 时透传调用
func TestResponseWriter_PushPassThrough(t *testing.T) {
	fp := &fakePusher{ResponseRecorder: httptest.NewRecorder()}
	rw := &responseWriter{ResponseWriter: fp}

	if err := rw.Push("/app.js", nil); err != nil {
		t.Fatalf("Push should delegate to underlying Pusher, got error: %v", err)
	}
	if fp.pushedTarget != "/app.js" {
		t.Fatalf("Push target = %q, want '/app.js'", fp.pushedTarget)
	}
}
