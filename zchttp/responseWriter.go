package zchttp

import (
	"bufio"
	"net"
	"net/http"
	"sync"
)

// IsResponseWritten 判断响应是否已经写入（WriteHeader 或 Write 被调用过）
func IsResponseWritten(w http.ResponseWriter) bool {
	rw, ok := w.(interface{ Written() bool })
	return ok && rw.Written()
}

// responseWriter 包装 http.ResponseWriter，记录响应是否已被写入
type responseWriter struct {
	http.ResponseWriter
	written bool
}

// responseWriterPool 复用 responseWriter 包装器，减少每请求分配
var responseWriterPool = sync.Pool{
	New: func() any { return &responseWriter{} },
}

// acquireResponseWriter 从池中取出包装器并绑定底层 ResponseWriter
func acquireResponseWriter(w http.ResponseWriter) *responseWriter {
	rw := responseWriterPool.Get().(*responseWriter)
	rw.ResponseWriter = w
	rw.written = false
	return rw
}

// releaseResponseWriter 重置状态并归还包装器；必须在请求处理完全结束后调用
// （含 panic 处理完成后），并清空对底层 writer 的引用避免经池长时间持有
func releaseResponseWriter(rw *responseWriter) {
	rw.ResponseWriter = nil
	rw.written = false
	responseWriterPool.Put(rw)
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

// Flush 透传 http.Flusher，支持 SSE 等流式响应场景；底层不支持时静默忽略
func (w *responseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		w.written = true
		f.Flush()
	}
}

// Hijack 透传 http.Hijacker，支持 WebSocket 等协议升级场景；底层不支持时返回错误
func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		w.written = true
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// Push 透传 http.Pusher，支持 HTTP/2 服务端推送；底层不支持时返回 ErrNotSupported
func (w *responseWriter) Push(target string, opts *http.PushOptions) error {
	if p, ok := w.ResponseWriter.(http.Pusher); ok {
		return p.Push(target, opts)
	}
	return http.ErrNotSupported
}
