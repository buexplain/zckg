package zchttp

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"sync"
)

// IsResponseWritten 判断响应是否已经写入（WriteHeader 或 Write 被调用过）
func IsResponseWritten(w http.ResponseWriter) bool {
	rw, ok := w.(interface{ Written() bool })
	return ok && rw.Written()
}

// responseWriter 包装 http.ResponseWriter，记录响应是否已被写入。
// 基础类型仅实现 http.ResponseWriter 与 io.ReaderFrom；Flusher/Hijacker/Pusher
// 能力由下方组合类型按底层真实支持情况声明，接口断言严格反映底层能力。
type responseWriter struct {
	http.ResponseWriter
	written bool
}

// responseWriterPool 复用 responseWriter 基础包装器，减少每请求分配；
// 组合类型均为小结构体，栈上分配即可，只有基础包装器走池
var responseWriterPool = sync.Pool{
	New: func() any { return &responseWriter{} },
}

// responseWriterF 组合 http.Flusher 能力（仅在底层确认支持时使用）
type responseWriterF struct{ *responseWriter }

// responseWriterH 组合 http.Hijacker 能力（仅在底层确认支持时使用）
type responseWriterH struct{ *responseWriter }

// responseWriterP 组合 http.Pusher 能力（仅在底层确认支持时使用）
type responseWriterP struct{ *responseWriter }

// responseWriterFH 组合 Flusher + Hijacker
type responseWriterFH struct{ *responseWriterF }

// responseWriterFP 组合 Flusher + Pusher
type responseWriterFP struct{ *responseWriterF }

// responseWriterHP 组合 Hijacker + Pusher
type responseWriterHP struct{ *responseWriterH }

// responseWriterFHP 组合 Flusher + Hijacker + Pusher
type responseWriterFHP struct{ *responseWriterFP }

// acquireResponseWriter 从池中取出基础包装器，按底层 ResponseWriter 实际支持的
// 能力组合返回对应包装类型（最多 8 种变体），使 http.Flusher/Hijacker/Pusher
// 接口断言不会误报能力（如底层不支持 Flusher 时包装层也不再声称支持）
func acquireResponseWriter(w http.ResponseWriter) http.ResponseWriter {
	rw := responseWriterPool.Get().(*responseWriter)
	rw.ResponseWriter = w
	rw.written = false
	_, isFlusher := w.(http.Flusher)
	_, isHijacker := w.(http.Hijacker)
	_, isPusher := w.(http.Pusher)
	switch {
	case isFlusher && isHijacker && isPusher:
		return &responseWriterFHP{responseWriterFP: &responseWriterFP{responseWriterF: &responseWriterF{responseWriter: rw}}}
	case isFlusher && isHijacker:
		return &responseWriterFH{responseWriterF: &responseWriterF{responseWriter: rw}}
	case isFlusher && isPusher:
		return &responseWriterFP{responseWriterF: &responseWriterF{responseWriter: rw}}
	case isHijacker && isPusher:
		return &responseWriterHP{responseWriterH: &responseWriterH{responseWriter: rw}}
	case isFlusher:
		return &responseWriterF{responseWriter: rw}
	case isHijacker:
		return &responseWriterH{responseWriter: rw}
	case isPusher:
		return &responseWriterP{responseWriter: rw}
	default:
		return rw
	}
}

// baseResponseWriter 提取任意组合包装变体的基础包装器；非本包装器类型返回 nil
func baseResponseWriter(rw http.ResponseWriter) *responseWriter {
	switch v := rw.(type) {
	case *responseWriterFHP:
		return v.responseWriter
	case *responseWriterFH:
		return v.responseWriter
	case *responseWriterFP:
		return v.responseWriter
	case *responseWriterHP:
		return v.responseWriter
	case *responseWriterF:
		return v.responseWriter
	case *responseWriterH:
		return v.responseWriter
	case *responseWriterP:
		return v.responseWriter
	case *responseWriter:
		return v
	default:
		return nil // 非本包装器类型（防御性处理，理论上不会发生）
	}
}

// releaseResponseWriter 重置状态并归还基础包装器；必须在请求处理完全结束后调用
// （含 panic 处理完成后），并清空对底层 writer 的引用避免经池长时间持有。
// 组合类型均嵌入 *responseWriter，此处按类型还原基础包装器后归还唯一的池
func releaseResponseWriter(rw http.ResponseWriter) {
	base := baseResponseWriter(rw)
	if base == nil {
		return
	}
	base.ResponseWriter = nil
	base.written = false
	responseWriterPool.Put(base)
}

func (w *responseWriter) WriteHeader(statusCode int) {
	w.written = true
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *responseWriter) Write(b []byte) (int, error) {
	w.written = true
	return w.ResponseWriter.Write(b)
}

// Written 返回响应是否已经写入
func (w *responseWriter) Written() bool {
	return w.written
}

// ReadFrom 透传 io.ReaderFrom，使 io.Copy(w, file) 走底层零拷贝路径（如 sendfile）。
// 透传前标记 written=true；底层不支持 io.ReaderFrom 时回退到通用拷贝
func (w *responseWriter) ReadFrom(r io.Reader) (int64, error) {
	w.written = true
	if rf, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(r)
	}
	return io.Copy(w.ResponseWriter, r)
}

// flush 公共实现：标记 written 并透传底层 Flusher（调用方保证底层支持）
func (w *responseWriter) flush() {
	w.written = true
	w.ResponseWriter.(http.Flusher).Flush()
}

// hijack 公共实现：标记 written 并透传底层 Hijacker（调用方保证底层支持）
func (w *responseWriter) hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.written = true
	return w.ResponseWriter.(http.Hijacker).Hijack()
}

// push 公共实现：透传底层 Pusher（调用方保证底层支持）。
// Push 推送的是独立流，不影响本响应的 written 状态
func (w *responseWriter) push(target string, opts *http.PushOptions) error {
	return w.ResponseWriter.(http.Pusher).Push(target, opts)
}

// Flush 透传 http.Flusher，支持 SSE 等流式响应场景
func (w *responseWriterF) Flush() { w.flush() }

// Hijack 透传 http.Hijacker，支持 WebSocket 等协议升级场景
func (w *responseWriterH) Hijack() (net.Conn, *bufio.ReadWriter, error) { return w.hijack() }

// Hijack 透传 http.Hijacker（Flusher + Hijacker 组合）
func (w *responseWriterFH) Hijack() (net.Conn, *bufio.ReadWriter, error) { return w.hijack() }

// Hijack 透传 http.Hijacker（Flusher + Hijacker + Pusher 组合）
func (w *responseWriterFHP) Hijack() (net.Conn, *bufio.ReadWriter, error) { return w.hijack() }

// Push 透传 http.Pusher，支持 HTTP/2 服务端推送
func (w *responseWriterP) Push(target string, opts *http.PushOptions) error {
	return w.push(target, opts)
}

// Push 透传 http.Pusher（Flusher + Pusher 组合）
func (w *responseWriterFP) Push(target string, opts *http.PushOptions) error {
	return w.push(target, opts)
}

// Push 透传 http.Pusher（Hijacker + Pusher 组合）
func (w *responseWriterHP) Push(target string, opts *http.PushOptions) error {
	return w.push(target, opts)
}
