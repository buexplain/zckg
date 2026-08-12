package zchttp

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"strings"
)

// routeEntry 是单条路由在注册表中的存储单元，包含 handler、该路由作用域内的中间件链快照，
// 以及注册阶段预计算的反射信息（避免请求阶段重复反射）
type routeEntry struct {
	handler     any
	middlewares []MiddlewareHandler

	// 以下字段在注册阶段通过反射预计算，请求阶段直接使用
	handlerVal  reflect.Value // reflect.ValueOf(handler)，用于反射调用
	reqType     reflect.Type  // handler 第二个参数的类型（Req）
	resType     reflect.Type  // handler 第一个返回值的类型（Res）
	reqIsPtr    bool          // Req 是否为指针类型（*struct）
	resIsPtr    bool          // Res 是否为指针类型（*struct）
	reqElemType reflect.Type  // Req 结构体的具体类型（已解引用指针），用于 reflect.New 创建实例

	// handler 的位置信息，用于路由冲突提示与 OpenAPI 摘要
	handlerName string // 全限定函数名
	handlerFile string // 定义文件路径
	handlerLine int    // 定义行号

	// Req 结构体的反射元信息，注册阶段预计算，供 binding/validation/defaults 及 OpenAPI 生成使用
	reqMeta structMeta
	// Res 结构体的反射元信息，注册阶段预计算，供 OpenAPI 生成使用
	resMeta structMeta
	// Req 的 OpenAPIMeta 操作级元信息，注册阶段预计算
	opMeta operationMeta
	// 预填充默认值的 Req 模板，请求时浅拷贝后直接绑定
	defaultReq reflect.Value
	// 是否需要深拷贝（模板中存在非 nil 的指针/切片/map 字段）
	needsDeepCopy bool
	// 请求阶段是否需要执行 applyDefaults（结构体树中存在带 default 的指针字段）
	needsRequestPhaseDefaults bool
	// 请求阶段是否需要执行 validateNonzero（类型树任意深度存在 nonzero 字段，传递性标记）
	needsNonzeroValidation bool
}

// buildEntry 校验 handler 签名、构建中间件链、预计算反射信息，返回完整的 routeEntry。
// handler 必须是 func(ctx context.Context, req Req) (Res, error) 的形式，
// 其中 Req 和 Res 必须是结构体或结构体指针。
// globalMiddlewares 与 groupMiddlewares 合并后作为该路由的中间件链快照。
func buildEntry(handler any, globalMiddlewares, groupMiddlewares []MiddlewareHandler) (*routeEntry, error) {
	if handler == nil {
		return nil, fmt.Errorf("handler must not be nil")
	}
	t := reflect.TypeOf(handler)
	if t.Kind() != reflect.Func {
		return nil, fmt.Errorf("handler must be a function")
	}
	if t.NumIn() != 2 {
		return nil, fmt.Errorf("handler must have exactly two arguments: context.Context and a struct")
	}
	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()
	if t.In(0) != contextType {
		return nil, fmt.Errorf("the first argument of handler must be context.Context")
	}
	if t.In(1).Kind() != reflect.Struct && !isStructPtr(t.In(1)) {
		return nil, fmt.Errorf("the second argument of handler must be a struct or a pointer to struct")
	}
	if t.NumOut() != 2 {
		return nil, fmt.Errorf("handler must have exactly two return values: a struct and an error")
	}
	if t.Out(0).Kind() != reflect.Struct && !isStructPtr(t.Out(0)) {
		return nil, fmt.Errorf("the first return value of handler must be a struct or a pointer to struct")
	}
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	if !t.Out(1).Implements(errorType) {
		return nil, fmt.Errorf("the second return value of handler must be an error")
	}

	// 预计算反射信息：请求阶段直接使用，避免重复 reflect.TypeOf / reflect.ValueOf
	reqType := t.In(1)
	resType := t.Out(0)
	reqIsPtr := isStructPtr(reqType)
	resIsPtr := isStructPtr(resType)
	var reqElemType reflect.Type
	if reqIsPtr {
		reqElemType = reqType.Elem()
	} else {
		reqElemType = reqType
	}

	// 预计算 Req 结构体的字段元信息（binding/validation/defaults 及 OpenAPI 生成使用）
	reqMeta := buildStructMeta(reqElemType)

	// 预计算 Res 结构体的字段元信息（OpenAPI 生成使用，避免重复反射遍历结构体字段）
	var resElemType reflect.Type
	if resIsPtr {
		resElemType = resType.Elem()
	} else {
		resElemType = resType
	}
	resMeta := buildStructMeta(resElemType)

	// 获取 handler 的位置信息（用于路由冲突提示、OpenAPI 摘要、default 误用警告）
	handlerName, handlerFile, handlerLine := "unknown", "unknown", 0
	pc := reflect.ValueOf(handler).Pointer()
	if fn := runtime.FuncForPC(pc); fn != nil {
		handlerName = fn.Name()
		handlerFile, handlerLine = fn.FileLine(pc)
	}

	// 预计算 Req 的 OpenAPIMeta 操作级元信息（OpenAPI 生成使用）
	opMeta := buildOperationMeta(reqElemType)

	// 预计算带默认值的 Req 模板：注册阶段填充默认值，请求时浅拷贝复用
	defaultReqPtr := reflect.New(reqElemType)
	_ = applyDefaults(defaultReqPtr, reqMeta)
	defaultReq := defaultReqPtr.Elem()

	// 扫描模板中是否存在非 nil 引用类型字段（指针/切片/map），
	// 若存在则请求阶段需要深拷贝以断开共享（string/int 等不可变值类型无需深拷贝）
	needsDeepCopy := hasRefFields(defaultReq)

	// 预计算请求阶段是否需要执行 applyDefaults：
	// 仅当结构体树中存在带 default 的指针字段时才需要（值类型在请求阶段不填充）
	needsRequestPhaseDefaults := hasRequestPhaseDefaults(reqElemType, nil)

	// 预计算请求阶段是否需要执行 validateNonzero：
	// 仅当类型树任意深度存在 nonzero 字段时才需要（传递性扫描，嵌套层 nonzero 也计入）
	needsNonzeroValidation := hasNonzeroInTree(reqElemType, nil)

	// 快照中间件链
	mws := make([]MiddlewareHandler, 0, len(globalMiddlewares)+len(groupMiddlewares))
	mws = append(mws, globalMiddlewares...)
	mws = append(mws, groupMiddlewares...)

	return &routeEntry{
		handler:                   handler,
		middlewares:               mws,
		handlerVal:                reflect.ValueOf(handler),
		reqType:                   reqType,
		resType:                   resType,
		reqIsPtr:                  reqIsPtr,
		resIsPtr:                  resIsPtr,
		reqElemType:               reqElemType,
		reqMeta:                   reqMeta,
		resMeta:                   resMeta,
		opMeta:                    opMeta,
		defaultReq:                defaultReq,
		needsDeepCopy:             needsDeepCopy,
		needsRequestPhaseDefaults: needsRequestPhaseDefaults,
		needsNonzeroValidation:    needsNonzeroValidation,
		handlerName:               handlerName,
		handlerFile:               handlerFile,
		handlerLine:               handlerLine,
	}, nil
}

// buildOperationMeta 从 Req 结构体中查找嵌入的 OpenAPIMeta 字段并解析其标签，
// 提取 tags（以 "/" 分隔）、summary 和 description 操作级元信息。
// 若未嵌入 OpenAPIMeta 或未设置对应标签，则返回零值。
func buildOperationMeta(reqType reflect.Type) operationMeta {
	var m operationMeta
	if reqType.Kind() != reflect.Struct {
		return m
	}
	for i := 0; i < reqType.NumField(); i++ {
		f := reqType.Field(i)
		if !(f.Anonymous && f.Type == metaType) {
			continue
		}
		if v := f.Tag.Get("tags"); v != "" {
			for _, p := range strings.Split(v, "/") {
				if p = strings.TrimSpace(p); p != "" {
					m.tags = append(m.tags, p)
				}
			}
		}
		m.summary = f.Tag.Get("summary")
		m.description = f.Tag.Get("description")
		return m
	}
	return m
}
