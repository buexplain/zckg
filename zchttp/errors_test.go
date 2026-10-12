package zchttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// ========== 错误类型工具方法测试 ==========

// TestNewValidationError 验证 NewValidationError 构造函数
func TestNewValidationError(t *testing.T) {
	err := NewValidationError("name", "is required", nil)
	if err == nil {
		t.Fatal("NewValidationError returned nil")
	}
	if err.Field != "name" {
		t.Fatalf("field = %q, want 'name'", err.Field)
	}
	if err.Message != "is required" {
		t.Fatalf("message = %q, want 'is required'", err.Message)
	}
}

// TestBindingError_Unwrap 验证 BindingError 的 Unwrap 方法
func TestBindingError_Unwrap(t *testing.T) {
	inner := fmt.Errorf("underlying error")
	be := NewBindingError(inner)
	unwrapped := be.Unwrap()
	if unwrapped != inner {
		t.Fatalf("Unwrap() = %v, want %v", unwrapped, inner)
	}
}

// TestValidationError_Error 验证 ValidationError 的 Error 方法：
// Field 非空输出 `field "<Field>" <Message>`；Field 为空（跨字段业务校验）仅输出 Message，不出现 `field ""`
func TestValidationError_Error(t *testing.T) {
	ve := &ValidationError{Field: "age", Message: "must be positive"}
	expected := `field "age" must be positive`
	if ve.Error() != expected {
		t.Fatalf("Error() = %q, want %q", ve.Error(), expected)
	}
	empty := &ValidationError{Message: "start must be before end"}
	if got := empty.Error(); got != "start must be before end" {
		t.Fatalf("empty Field Error() = %q, want %q", got, "start must be before end")
	}
}

// emptyFieldValidatorReq 的 Validate 返回 Field 为空的跨字段校验错误
type emptyFieldValidatorReq struct{}

func (emptyFieldValidatorReq) Validate() error {
	return &ValidationError{Message: "start must be before end"}
}

// TestValidationError_EmptyFieldResponseMessage 验证经默认校验回调输出时，空 Field 的响应 message 仅为 Message
func TestValidationError_EmptyFieldResponseMessage(t *testing.T) {
	router := NewRouter()
	router.GET("/ef", func(_ context.Context, _ emptyFieldValidatorReq) (struct{}, error) { return struct{}{}, nil })
	rec := serveRequest(t, router, http.MethodGet, "/ef", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var resp Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	if resp.Message != "start must be before end" {
		t.Fatalf("message = %q, want %q", resp.Message, "start must be before end")
	}
}

type mapErr map[string]string

func (mapErr) Error() string { return "map err" }

type valErr struct{}

func (valErr) Error() string { return "val err" }

// TestIsNilError 锁死 typed-nil 归一化判定：nil 与 nilable kind（Ptr/Map）的 nil 值视为无错误，
// 非 nil 指针、非 nil map 与值类型错误均视为错误
func TestIsNilError(t *testing.T) {
	var nilVE *ValidationError
	var nilMap mapErr
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"untyped nil", nil, true},
		{"typed-nil ptr", nilVE, true},
		{"typed-nil map", nilMap, true},
		{"non-nil ptr", &ValidationError{Message: "x"}, false},
		{"non-nil map", mapErr{"a": "b"}, false},
		{"value type", valErr{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isNilError(c.err); got != c.want {
				t.Fatalf("isNilError = %v, want %v", got, c.want)
			}
		})
	}
}

// TestValidationError_Unwrap 验证 ValidationError 的 Unwrap 方法
func TestValidationError_Unwrap(t *testing.T) {
	inner := fmt.Errorf("underlying cause")
	ve := NewValidationError("email", "invalid", inner)
	if ve.Unwrap() != inner {
		t.Fatalf("Unwrap() = %v, want %v", ve.Unwrap(), inner)
	}
	// Err 为 nil 时 Unwrap 返回 nil
	ve2 := NewValidationError("name", "required", nil)
	if ve2.Unwrap() != nil {
		t.Fatalf("Unwrap() should be nil when Err is nil, got %v", ve2.Unwrap())
	}
}

// TestBindingError_Error 验证 BindingError 的 Error 方法
func TestBindingError_Error(t *testing.T) {
	be := NewBindingError(fmt.Errorf("read body failed"))
	// 存在底层错误时透传其文本
	if be.Error() != "read body failed" {
		t.Fatalf("Error() = %q, want %q", be.Error(), "read body failed")
	}
	// 底层错误为 nil 时降级返回 Message
	be2 := &BindingError{Message: "fallback message"}
	if be2.Error() != "fallback message" {
		t.Fatalf("Error() = %q, want 'fallback message'", be2.Error())
	}
}
