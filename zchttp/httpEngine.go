package zchttp

import (
	"encoding/json"
	"errors"
	"html"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"runtime/debug"
	"strings"
)

// Response 统一的 JSON 响应结构
type Response struct {
	Data    any    `json:"data"`
	Code    int    `json:"code"`
	Message string `json:"message"`
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
}

func NewEngine() *HttpEngine {
	return &HttpEngine{
		Router:            NewRouter(),
		OnNotFound:        DefaultNotFoundHandler,
		OnResponse:        DefaultResponseHandler,
		OnError:           DefaultErrorHandler,
		OnValidationError: DefaultValidationErrorHandler,
		OnPanic:           DefaultPanicHandler,
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
	if err := json.NewEncoder(w).Encode(Response{Data: res, Code: 0, Message: "success"}); err != nil {
		slog.Error("json encode failed in DefaultResponseHandler", "error", err, "path", r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	}
}

// DefaultErrorHandler 默认错误响应：若响应已写入则跳过（避免重复写头）；
// 否则根据 WantHtml 决定返回 HTML 或统一 JSON 结构，状态码统一为 500
func DefaultErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	if IsResponseWritten(w) {
		return
	}
	if WantHtml(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("<h1>500 Internal Server Error</h1><p>" + html.EscapeString(err.Error()) + "</p>"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	if encErr := json.NewEncoder(w).Encode(Response{Data: nil, Code: http.StatusInternalServerError, Message: err.Error()}); encErr != nil {
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
	if encErr := json.NewEncoder(w).Encode(Response{Data: nil, Code: http.StatusBadRequest, Message: err.Error()}); encErr != nil {
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
	if encErr := json.NewEncoder(w).Encode(Response{Data: nil, Code: http.StatusInternalServerError, Message: "internal server error"}); encErr != nil {
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
	if encErr := json.NewEncoder(w).Encode(Response{Data: nil, Code: http.StatusNotFound, Message: "not found"}); encErr != nil {
		slog.Error("json encode failed in DefaultNotFoundHandler", "error", encErr, "path", r.URL.Path)
	}
}

func (e *HttpEngine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 包装 ResponseWriter 以跟踪响应是否已被写入（提前包装，以便 panic 恢复时也能使用）
	rw := &responseWriter{ResponseWriter: w}

	// panic 捕获：防止单个请求的 panic 导致整个进程崩溃
	defer func() {
		if recovered := recover(); recovered != nil {
			e.OnPanic(rw, r, recovered)
		}
	}()

	// 1. 根据 method 查找路由表
	methodRoutes, ok := e.Router.routes[r.Method]
	if !ok {
		e.OnNotFound(rw, r)
		return
	}

	// 2. 根据 path 查找具体路由（末尾斜杠归一化，使 /hello 与 /hello/ 等价）
	entry, ok := methodRoutes[normalizePath(r.URL.Path)]
	if !ok {
		e.OnNotFound(rw, r)
		return
	}

	ctx := r.Context()
	// 将 *HttpEngine、*http.Request 与（包装后的）ResponseWriter 注入 ctx，供 handler 通过
	// EngineFromContext / RequestFromContext / ResponseWriterFromContext 获取
	ctx = withEngine(ctx, e)
	ctx = withRequestResponse(ctx, r, rw)

	if r.Body != nil {
		defer func() {
			_, _ = io.Copy(io.Discard, r.Body)
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

	if err := bindRequestData(r, reqPtr, entry.reqMeta); err != nil {
		// 绑定失败不提前返回，将错误存入 ctx，随中间件链穿透到 core 层再处理
		ctx = withBindingErr(ctx, NewBindingError(err))
	}

	// 请求阶段重新应用默认值：补填 JSON/表单绑定后动态创建的子元素（切片/数组/map/nested struct ptr）中的默认值。
	// requestPhase=true：仅填充 nil 指针字段，值类型（int/string/bool）跳过以避免覆盖用户显式传入的零值。
	// 仅当注册阶段预计算存在带 default 的指针字段时才执行，避免无意义的递归遍历。
	if entry.needsRequestPhaseDefaults {
		_ = applyDefaults(reqPtr, entry.reqMeta, true)
	}

	// 4. 将解析后的 Req 注入 ctx，供中间件与 core 层获取
	//    （即使绑定失败也注入，中间件可通过 BoundReqFromContext 拿到错误）
	ctx = withBoundReq(ctx, reqPtr.Interface())

	// 将 Res 共享容器注入 ctx，使 core 层写入的 Res 对所有中间件层（包括后置阶段）可见
	ctx = withBoundResContainer(ctx)

	// 5. 洋葱模型核心层：参数校验 + 反射调用 handler
	core := func() error {
		boundAny, err := BoundReqFromContext[any](ctx)
		if err != nil {
			return err
		}
		reqPtr := reflect.ValueOf(boundAny)

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
		errVal := results[1].Interface()
		if errVal != nil {
			return errVal.(error)
		}

		// 成功：将 Res 写入共享容器，再交由响应回调处理（默认 JSON，可自定义为文件等）
		setBoundRes(ctx, results[0].Interface())
		e.OnResponse(rw, r, results[0].Interface())
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
