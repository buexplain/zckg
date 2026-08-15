package zchttp

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
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
	rw := acquireResponseWriter(rec)
	if IsResponseWritten(rw) {
		t.Fatal("fresh responseWriter should report not written")
	}
	releaseResponseWriter(rw)

	// Write 后 → true
	rw2 := acquireResponseWriter(httptest.NewRecorder())
	_, _ = rw2.Write([]byte("x"))
	if !IsResponseWritten(rw2) {
		t.Fatal("responseWriter should report written after Write")
	}
	releaseResponseWriter(rw2)

	// 仅 WriteHeader 也视为已写入
	rw3 := acquireResponseWriter(httptest.NewRecorder())
	rw3.WriteHeader(http.StatusTeapot)
	if !IsResponseWritten(rw3) {
		t.Fatal("responseWriter should report written after WriteHeader")
	}
	releaseResponseWriter(rw3)
}

// ========== 能力组合包装（m2）测试 ==========

// plainWriter 仅实现 http.ResponseWriter，不提供 Flusher/Hijacker/Pusher/ReaderFrom，
// 用于验证包装层不会误报能力
type plainWriter struct {
	header     http.Header
	statusCode int
	body       []byte
}

func (p *plainWriter) Header() http.Header {
	if p.header == nil {
		p.header = http.Header{}
	}
	return p.header
}

func (p *plainWriter) Write(b []byte) (int, error) {
	p.body = append(p.body, b...)
	return len(b), nil
}

func (p *plainWriter) WriteHeader(code int) {
	p.statusCode = code
}

// TestResponseWriter_NoCapabilityMisreport 验证 m2 修复：底层不支持的能力，
// 包装层接口断言必须失败（Flusher/Hijacker/Pusher 均不得误报）
func TestResponseWriter_NoCapabilityMisreport(t *testing.T) {
	pw := &plainWriter{}
	rw := acquireResponseWriter(pw)
	defer releaseResponseWriter(rw)

	if _, ok := rw.(http.Flusher); ok {
		t.Fatal("wrapper must not implement http.Flusher when underlying writer does not")
	}
	if _, ok := rw.(http.Hijacker); ok {
		t.Fatal("wrapper must not implement http.Hijacker when underlying writer does not")
	}
	if _, ok := rw.(http.Pusher); ok {
		t.Fatal("wrapper must not implement http.Pusher when underlying writer does not")
	}

	// 基础功能不受影响
	rw.WriteHeader(http.StatusOK)
	_, _ = rw.Write([]byte("hello"))
	if !IsResponseWritten(rw) {
		t.Fatal("written should be marked after Write/WriteHeader")
	}
	if pw.statusCode != http.StatusOK || string(pw.body) != "hello" {
		t.Fatalf("write not delegated: code=%d body=%q", pw.statusCode, pw.body)
	}
}

// TestResponseWriter_FlusherPassThrough 验证底层支持 Flusher 时包装层透传：
// SSE 等流式场景依赖 w.(http.Flusher) 断言，包装后不应丢失底层能力
func TestResponseWriter_FlusherPassThrough(t *testing.T) {
	rec := httptest.NewRecorder() // ResponseRecorder 实现了 http.Flusher
	rw := acquireResponseWriter(rec)
	defer releaseResponseWriter(rw)

	f, ok := rw.(http.Flusher)
	if !ok {
		t.Fatal("wrapper must pass through http.Flusher when underlying supports it")
	}
	f.Flush()
	if !rec.Flushed {
		t.Fatal("Flush not delegated to underlying ResponseWriter")
	}
	if !IsResponseWritten(rw) {
		t.Fatal("Flush should mark responseWriter as written")
	}
}

// TestResponseWriter_HijackerNotSupported 验证底层不支持 Hijack 时包装层不实现该接口
func TestResponseWriter_HijackerNotSupported(t *testing.T) {
	rec := httptest.NewRecorder() // ResponseRecorder 未实现 http.Hijacker
	rw := acquireResponseWriter(rec)
	defer releaseResponseWriter(rw)

	if _, ok := rw.(http.Hijacker); ok {
		t.Fatal("wrapper must not implement http.Hijacker when underlying writer does not")
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
	rw := acquireResponseWriter(fh)
	defer releaseResponseWriter(rw)

	// fakeHijacker 嵌入 ResponseRecorder（含 Flusher），组合变体应为 Flusher+Hijacker
	if _, ok := rw.(http.Flusher); !ok {
		t.Fatal("Flusher+Hijacker combination should still expose Flusher")
	}
	h, ok := rw.(http.Hijacker)
	if !ok {
		t.Fatal("wrapper must pass through http.Hijacker when underlying supports it")
	}
	if _, _, err := h.Hijack(); err != nil {
		t.Fatalf("Hijack should delegate to underlying Hijacker, got error: %v", err)
	}
	if !fh.hijacked {
		t.Fatal("Hijack not delegated to underlying writer")
	}
	if !IsResponseWritten(rw) {
		t.Fatal("Hijack should mark responseWriter as written")
	}
}

// TestResponseWriter_PushNotSupported 验证底层不支持 HTTP/2 Push 时包装层不实现该接口
func TestResponseWriter_PushNotSupported(t *testing.T) {
	rec := httptest.NewRecorder() // ResponseRecorder 未实现 http.Pusher
	rw := acquireResponseWriter(rec)
	defer releaseResponseWriter(rw)

	if _, ok := rw.(http.Pusher); ok {
		t.Fatal("wrapper must not implement http.Pusher when underlying writer does not")
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

// TestResponseWriter_PushPassThrough 验证底层支持 Push 时透传调用且不影响 written 状态
func TestResponseWriter_PushPassThrough(t *testing.T) {
	fp := &fakePusher{ResponseRecorder: httptest.NewRecorder()}
	rw := acquireResponseWriter(fp)
	defer releaseResponseWriter(rw)

	p, ok := rw.(http.Pusher)
	if !ok {
		t.Fatal("wrapper must pass through http.Pusher when underlying supports it")
	}
	if err := p.Push("/app.js", nil); err != nil {
		t.Fatalf("Push should delegate to underlying Pusher, got error: %v", err)
	}
	if fp.pushedTarget != "/app.js" {
		t.Fatalf("Push target = %q, want '/app.js'", fp.pushedTarget)
	}
	// Push 推送的是独立流，不影响本响应的 written 状态
	if IsResponseWritten(rw) {
		t.Fatal("Push must not mark the response as written")
	}
}

// fakeHijackPusher 同时提供 Hijack 与 Push，配合 ResponseRecorder 的 Flusher 构成全能力底层
type fakeHijackPusher struct {
	*fakeHijacker
	pushedTarget string
}

func (f *fakeHijackPusher) Push(target string, _ *http.PushOptions) error {
	f.pushedTarget = target
	return nil
}

// TestResponseWriter_FullCapabilityCombo 验证 Flusher+Hijacker+Pusher 全组合变体
// 三个能力接口断言均成功
func TestResponseWriter_FullCapabilityCombo(t *testing.T) {
	fhp := &fakeHijackPusher{fakeHijacker: &fakeHijacker{ResponseRecorder: httptest.NewRecorder()}}
	rw := acquireResponseWriter(fhp)
	defer releaseResponseWriter(rw)

	if _, ok := rw.(http.Flusher); !ok {
		t.Fatal("full combo must expose Flusher")
	}
	if _, ok := rw.(http.Hijacker); !ok {
		t.Fatal("full combo must expose Hijacker")
	}
	if _, ok := rw.(http.Pusher); !ok {
		t.Fatal("full combo must expose Pusher")
	}
	if _, _, err := rw.(http.Hijacker).Hijack(); err != nil || !fhp.hijacked {
		t.Fatal("full combo Hijack not delegated")
	}
	if err := rw.(http.Pusher).Push("/x.js", nil); err != nil || fhp.pushedTarget != "/x.js" {
		t.Fatalf("full combo Push not delegated: err=%v target=%q", err, fhp.pushedTarget)
	}
	rw.(http.Flusher).Flush()
	if !fhp.ResponseRecorder.Flushed {
		t.Fatal("full combo Flush not delegated")
	}
}

// ========== ReadFrom（n7）测试 ==========

// readerFromWriter 实现 io.ReaderFrom 的底层 writer，用于验证零拷贝路径透传
type readerFromWriter struct {
	plainWriter
	readFromCalled bool
}

func (r *readerFromWriter) ReadFrom(src io.Reader) (int64, error) {
	r.readFromCalled = true
	return io.Copy(&r.plainWriter, src)
}

// TestResponseWriter_ReadFromPassThrough 验证底层支持 io.ReaderFrom 时透传（sendfile 优化保留）
func TestResponseWriter_ReadFromPassThrough(t *testing.T) {
	rf := &readerFromWriter{}
	rw := acquireResponseWriter(rf)
	defer releaseResponseWriter(rw)

	n, err := rw.(io.ReaderFrom).ReadFrom(strings.NewReader("sendfile-data"))
	if err != nil {
		t.Fatalf("ReadFrom error: %v", err)
	}
	if n != int64(len("sendfile-data")) {
		t.Fatalf("ReadFrom n = %d, want %d", n, len("sendfile-data"))
	}
	if !rf.readFromCalled {
		t.Fatal("ReadFrom must delegate to underlying io.ReaderFrom")
	}
	if !IsResponseWritten(rw) {
		t.Fatal("ReadFrom should mark responseWriter as written")
	}
}

// TestResponseWriter_ReadFromFallback 验证底层不支持 io.ReaderFrom 时回退通用拷贝
func TestResponseWriter_ReadFromFallback(t *testing.T) {
	pw := &plainWriter{}
	rw := acquireResponseWriter(pw)
	defer releaseResponseWriter(rw)

	rf, ok := rw.(io.ReaderFrom)
	if !ok {
		t.Fatal("wrapper must always implement io.ReaderFrom")
	}
	n, err := rf.ReadFrom(strings.NewReader("fallback-data"))
	if err != nil {
		t.Fatalf("ReadFrom error: %v", err)
	}
	if n != int64(len("fallback-data")) || string(pw.body) != "fallback-data" {
		t.Fatalf("fallback copy failed: n=%d body=%q", n, pw.body)
	}
	if !IsResponseWritten(rw) {
		t.Fatal("ReadFrom fallback should mark responseWriter as written")
	}
}
