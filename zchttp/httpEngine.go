package zchttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"runtime/debug"
	"strings"
	"sync"
)

// Response 统一的 JSON 响应结构
type Response struct {
	Data    any    `json:"data"`
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// maxBodyDrainBytes 请求处理结束后为复用 keep-alive 连接而排空剩余请求体的字节上限；
// 超出则不再排空（连接由 net/http 关闭而非复用），防止超大未读请求体拖慢服务端 IO
const maxBodyDrainBytes = 2 << 10 // 2 KB

// jsonBufEncoder 将编码器与其专属缓冲绑定一同池化（json.Encoder 不支持重定向
// 目标 Writer，故先写入缓冲再拷到响应），避免每响应新建编码器。
// API 响应无需嵌入 HTML，关闭 HTML 转义（< > & 不再转义为 \uXXXX）以降低编码开销
type jsonBufEncoder struct {
	buf bytes.Buffer
	enc *json.Encoder
}

var jsonEncoderPool = sync.Pool{
	New: func() any {
		jb := &jsonBufEncoder{}
		jb.enc = json.NewEncoder(&jb.buf)
		jb.enc.SetEscapeHTML(false)
		return jb
	},
}

// encodeJSON 从池中取出编码器将 v 编码为 JSON 写入 w，用完归还
func encodeJSON(w io.Writer, v any) error {
	jb := jsonEncoderPool.Get().(*jsonBufEncoder)
	err := jb.enc.Encode(v)
	if err == nil {
		_, err = w.Write(jb.buf.Bytes())
	}
	jb.buf.Reset()
	jsonEncoderPool.Put(jb)
	return err
}

// ResponseHandler 自定义成功响应的写入逻辑，res 为 handler 的第一个返回值
type ResponseHandler func(w http.ResponseWriter, r *http.Request, res any)

// ErrorHandler 自定义错误响应的写入逻辑，err 为中间件链或 handler 产生的错误
type ErrorHandler func(w http.ResponseWriter, r *http.Request, err error)

// PanicHandler 自定义 panic 恢复的处理逻辑，recovered 为 recover() 捕获的值
type PanicHandler func(w http.ResponseWriter, r *http.Request, recovered any)

type HttpEngine struct {
	Router            *Router
	OnNotFound        func(w http.ResponseWriter, r *http.Request) // 未命中路由回调，默认 DefaultNotFoundHandler
	OnResponse        ResponseHandler                              // 成功响应回调，默认 DefaultResponseHandler
	OnError           ErrorHandler                                 // 错误响应回调，默认 DefaultErrorHandler
	OnValidationError ErrorHandler                                 // 参数校验失败回调，默认 DefaultValidationErrorHandler
	OnPanic           PanicHandler                                 // panic 恢复回调，默认 DefaultPanicHandler
	// MaxBodyBytes 请求体整体大小上限（字节），绑定前用 http.MaxBytesReader 包装 r.Body，
	// 超限时绑定失败并映射为 400；0 表示不限制。对 JSON/表单/multipart 等所有请求体统一生效，
	// 防止超大请求体造成的内存/磁盘 DoS。
	MaxBodyBytes int64
	// MultipartFormMaxMemory multipart/form-data 解析的内存缓冲上限（字节），
	// 超出部分写入临时文件；默认 32 MB。
	MultipartFormMaxMemory int64
}

func NewEngine() *HttpEngine {
	return &HttpEngine{
		Router:                 NewRouter(),
		OnNotFound:             DefaultNotFoundHandler,
		OnResponse:             DefaultResponseHandler,
		OnError:                DefaultErrorHandler,
		OnValidationError:      DefaultValidationErrorHandler,
		OnPanic:                DefaultPanicHandler,
		MultipartFormMaxMemory: 32 << 20, // 32 MB
	}
}

// WantHtml 判断请求头 Accept 是否包含 text/html 或 text/plain，
// 包含则返回 true（倾向于 HTML/纯文本响应），否则返回 false
func WantHtml(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "text/html") || strings.Contains(accept, "text/plain")
}

// DefaultResponseHandler 默认成功响应：若 handler 已写入响应（如文件下载、自定义 Content-Type 等）
// 则跳过；否则以统一 JSON 结构返回 res
func DefaultResponseHandler(w http.ResponseWriter, r *http.Request, res any) {
	if IsResponseWritten(w) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := encodeJSON(w, Response{Data: res, Code: 0, Message: "success"}); err != nil {
		slog.Error("json encode failed in DefaultResponseHandler", "error", err, "path", r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	}
}

// DefaultErrorHandler 默认错误响应：若响应已写入则跳过（避免重复写头）；
// 否则根据 WantHtml 决定返回 HTML 或统一 JSON 结构，状态码统一为 500。
// 客户端仅收到通用 "internal server error" 消息，错误详情只写入服务端日志，
// 防止泄露业务内部信息（与 DefaultPanicHandler 对齐）。
func DefaultErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	if IsResponseWritten(w) {
		return
	}
	slog.Error("request failed with internal error", "error", err, "method", r.Method, "path", r.URL.Path)
	if WantHtml(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("<h1>500 Internal Server Error</h1><p>internal server error</p>"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	if encErr := encodeJSON(w, Response{Data: nil, Code: http.StatusInternalServerError, Message: "internal server error"}); encErr != nil {
		slog.Error("json encode failed in DefaultErrorHandler", "error", encErr, "path", r.URL.Path)
	}
}

// DefaultValidationErrorHandler 默认参数校验失败响应：若响应已写入则跳过；
// 否则根据 WantHtml 决定返回 HTML 或统一 JSON 结构，状态码统一为 400
func DefaultValidationErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	if IsResponseWritten(w) {
		return
	}
	if WantHtml(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("<h1>400 Bad Request</h1><p>" + html.EscapeString(err.Error()) + "</p>"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	if encErr := encodeJSON(w, Response{Data: nil, Code: http.StatusBadRequest, Message: err.Error()}); encErr != nil {
		slog.Error("json encode failed in DefaultValidationErrorHandler", "error", encErr, "path", r.URL.Path)
	}
}

// DefaultPanicHandler 默认 panic 恢复处理：用 slog 将 panic 信息与堆栈输出到日志，向客户端返回 500 错误响应。
// 响应体仅包含 "internal server error" 通用消息，不向客户端暴露完整堆栈，防止泄露服务端内部结构。
func DefaultPanicHandler(w http.ResponseWriter, r *http.Request, recovered any) {
	stack := debug.Stack()
	slog.Error("panic recovered",
		"method", r.Method,
		"path", r.URL.Path,
		"error", recovered,
		"stack", string(stack),
	)
	if IsResponseWritten(w) {
		return
	}
	if WantHtml(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("<h1>500 Internal Server Error</h1><p>internal server error</p>"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	if encErr := encodeJSON(w, Response{Data: nil, Code: http.StatusInternalServerError, Message: "internal server error"}); encErr != nil {
		slog.Error("json encode failed in DefaultPanicHandler", "error", encErr, "path", r.URL.Path)
	}
}

// DefaultNotFoundHandler 默认未命中路由响应：根据 WantHtml 决定返回 HTML 或统一 JSON 结构，
// 状态码统一为 404
func DefaultNotFoundHandler(w http.ResponseWriter, r *http.Request) {
	if IsResponseWritten(w) {
		return
	}
	if WantHtml(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("<h1>404 Not Found</h1><p>" + html.EscapeString(r.URL.Path) + "</p>"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	if encErr := encodeJSON(w, Response{Data: nil, Code: http.StatusNotFound, Message: "not found"}); encErr != nil {
		slog.Error("json encode failed in DefaultNotFoundHandler", "error", encErr, "path", r.URL.Path)
	}
}

func (e *HttpEngine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 整体请求体大小限制（MaxBodyBytes > 0 时生效）：超限请求在绑定阶段收到
	// *http.MaxBytesError 并映射为 400，防止超大请求体造成内存/磁盘 DoS
	if e.MaxBodyBytes > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, e.MaxBodyBytes)
	}

	// 从池中取出 ResponseWriter 包装器以跟踪响应是否已被写入（提前获取，以便 panic 恢复时也能使用）
	rw := acquireResponseWriter(w)

	// panic 捕获：防止单个请求的 panic 导致整个进程崩溃；
	// 包装器在 panic 处理完成后才归还池，确保 OnPanic 仍可安全使用 rw
	defer func() {
		if recovered := recover(); recovered != nil {
			e.OnPanic(rw, r, recovered)
		}
		releaseResponseWriter(rw)
	}()

	// 1. 查找路由：先按 method+path 精确匹配，未命中再回退到参数路由基数树匹配
	//    （精确路由优先；末尾斜杠归一化，使 /hello 与 /hello/ 等价）
	normalizedPath := normalizePath(r.URL.Path)
	var entry *routeEntry
	var paramValues []string
	if methodRoutes, ok := e.Router.routes[r.Method]; ok {
		entry = methodRoutes[normalizedPath]
	}
	if entry == nil {
		entry, paramValues = e.Router.matchParam(r.Method, normalizedPath)
	}
	if entry == nil {
		e.OnNotFound(rw, r)
		return
	}

	ctx := r.Context()
	// 将请求作用域的全部状态（*HttpEngine、*http.Request、包装后的 ResponseWriter）
	// 合并为单个 requestState 一次性注入 ctx（仅一次 context.WithValue），
	// 供 handler 通过 EngineFromContext / RequestFromContext / ResponseWriterFromContext 获取
	st := &requestState{engine: e, req: r, w: rw}
	ctx = context.WithValue(ctx, stateKey, st)

	if r.Body != nil {
		defer func() {
			// 限量排空剩余请求体以复用 keep-alive 连接；超出上限不再排空，
			// 连接由 net/http 关闭，避免超大未读请求体占用服务端 IO
			_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, maxBodyDrainBytes))
			_ = r.Body.Close()
		}()
	}

	// 3. 命中路由后立即构造 Req 并绑定请求数据（不做参数校验），
	//    使中间件可通过 BoundReqFromContext 提前检查请求数据
	//    使用注册阶段预计算的 reqElemType 与 reqMeta，始终创建指向结构体的指针用于参数绑定
	var reqPtr reflect.Value
	reqPtr = reflect.New(entry.reqElemType)
	reqPtr.Elem().Set(entry.defaultReq)
	if entry.needsDeepCopy {
		// 模板中存在非 nil 引用类型字段（指针/切片/map），
		// 深拷贝以断开共享，确保并发请求间不共享底层内存
		deepCopyDefaults(reqPtr.Elem())
	}

	if len(entry.reqMeta.fields) > 0 {
		if err := bindRequestData(r, reqPtr, entry.reqMeta, e.MultipartFormMaxMemory); err != nil {
			// 绑定失败不提前返回，将错误存入状态，随中间件链穿透到 core 层再处理
			st.bindingErr = NewBindingError(err)
		} else if len(entry.pathParams) > 0 {
			// 路由路径参数在 query/body 之后绑定，覆盖同名参数；转换失败同样走 BindingError 通道（400）
			if err := bindPathParams(reqPtr, entry.pathParams, paramValues); err != nil {
				st.bindingErr = NewBindingError(err)
			}
		}
	}

	// 请求阶段重新应用默认值：补填 JSON/表单绑定后动态创建的子元素（切片/数组/map/nested struct ptr）中的默认值。
	// requestPhase=true：仅填充 nil 指针字段，值类型（int/string/bool）跳过以避免覆盖用户显式传入的零值。
	// 仅当注册阶段预计算存在带 default 的指针字段时才执行，避免无意义的递归遍历。
	if entry.needsRequestPhaseDefaults {
		applyDefaults(reqPtr, entry.reqMeta, true)
	}

	// 4. 将解析后的 Req 存入状态，供中间件与 core 层通过 BoundReqFromContext 获取
	//    （即使绑定失败也存入，中间件可通过 BoundReqFromContext 拿到错误）
	st.boundReq = reqPtr.Interface()

	// 5. 洋葱模型核心层：参数校验 + 反射调用 handler
	//    直接使用闭包捕获的 reqPtr（绑定阶段产物），避免再从 ctx 取值并重复反射
	core := func() error {
		// 绑定阶段失败时不提前返回，错误随中间件链穿透到 core 层再统一处理
		if st.bindingErr != nil {
			return st.bindingErr
		}
		// 校验 Req（required 字段 + 自定义 Validator）
		// 使用注册阶段预计算的 reqMeta，避免请求阶段重复反射解析；
		// needsNonzeroValidation 为传递性标记：类型树任意深度存在 nonzero 字段才执行遍历
		if err := validateRequest(reqPtr, entry.reqMeta, entry.needsNonzeroValidation); err != nil {
			return err
		}

		// 根据 Req 声明类型决定传入指针还是值（使用注册阶段预计算的结果）
		var reqArg reflect.Value
		if entry.reqIsPtr {
			reqArg = reqPtr
		} else {
			reqArg = reqPtr.Elem()
		}

		// 反射调用 handler: func(ctx context.Context, req Req) (Res, error)
		results := entry.handlerVal.Call([]reflect.Value{
			reflect.ValueOf(ctx),
			reqArg,
		})

		// 处理返回值：成功交由响应回调，失败则返回 error 交由上层处理
		// typed-nil 归一化：handler 返回 (*MyErr)(nil) 时接口非 nil，误判会走 500。
		// errVal == nil 的快速路径零反射开销；仅当接口非 nil 时用反射检测 typed-nil
		// （错误路径本就低频，反射开销可接受）
		errVal := results[1].Interface()
		if errVal != nil {
			rv := reflect.ValueOf(errVal)
			if rv.Kind() == reflect.Ptr && rv.IsNil() {
				errVal = nil // typed-nil 归一化为 nil，走成功路径
			} else {
				return errVal.(error)
			}
		}

		// 成功：将 Res 写入共享状态，再交由响应回调处理（默认 JSON，可自定义为文件等）
		// Interface() 仅装箱一次复用，避免重复装箱分配
		res := results[0].Interface()
		st.res = res
		e.OnResponse(rw, r, res)
		return nil
	}

	// 6. 构建洋葱模型中间件链并执行，错误传播到最外层时交由错误回调处理；
	// 绑定错误（*BindingError）与参数校验错误（*ValidationError）路由到 OnValidationError，
	// 其余错误路由到 OnError
	if err := runChain(entry.middlewares, ctx, rw, r, core); err != nil {
		var ve *ValidationError
		var be *BindingError
		if errors.As(err, &ve) || errors.As(err, &be) {
			e.OnValidationError(rw, r, err)
		} else {
			e.OnError(rw, r, err)
		}
	}
}

func (e *HttpEngine) Run(server *http.Server) error {
	server.Handler = e
	return server.ListenAndServe()
}
