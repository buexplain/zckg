package zchttp

import "fmt"

// ValidationError 表示请求参数校验失败，由 validateNonzero、Validate() 等校验逻辑产生。
// HttpEngine 在 ServeHTTP 中通过 errors.As 识别该类型，并路由到 OnValidationError
// 回调处理（默认 DefaultValidationErrorHandler，返回 400）。
type ValidationError struct {
	Field   string // 校验失败的字段名（绑定名），业务校验可留空
	Message string // 失败原因
	Err     error  // 可选：包装底层错误（如 Validate() 返回的业务错误），支持 errors.Is/As 穿透
}

// NewValidationError 创建一个字段级校验错误，用于在自定义校验逻辑中返回。
func NewValidationError(Field string, Message string, Err error) *ValidationError {
	return &ValidationError{Field: Field, Message: Message, Err: Err}
}

// Error 实现 error 接口：包装了底层错误（Err 非 nil）时直接返回底层错误文案
// （不叠加 Field/Message 前缀）；否则返回 "field \"<Field>\" <Message>" 形式。
func (e *ValidationError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("field %q %s", e.Field, e.Message)
}

// Unwrap 返回被包装的底层错误，使 errors.Is/As 可穿透到原始错误
func (e *ValidationError) Unwrap() error { return e.Err }

// BindingError 表示请求数据绑定失败（如 JSON 解析错误、类型转换失败等）。
// HttpEngine 在 ServeHTTP 中通过 errors.As 识别该类型，同样路由到 OnValidationError
// 回调处理（默认 400）。与 ValidationError 的区别在于它发生在绑定阶段而非校验阶段。
type BindingError struct {
	Message string // 失败原因
	Err     error  // 包装底层错误（如 *json.SyntaxError），支持 errors.Is/As 穿透
}

// NewBindingError 创建一个绑定错误。
func NewBindingError(err error) *BindingError {
	return &BindingError{Message: err.Error(), Err: err}
}

// Error 实现 error 接口：包装了底层错误（Err 非 nil）时直接返回底层错误文案；否则返回 Message。
func (e *BindingError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Message
}

// Unwrap 返回被包装的底层错误，使 errors.Is/As 可穿透到原始错误
func (e *BindingError) Unwrap() error { return e.Err }
