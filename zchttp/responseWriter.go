package zchttp

import (
	"bufio"
	"net"
	"net/http"
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
