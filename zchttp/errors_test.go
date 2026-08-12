package zchttp

import (
	"fmt"
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

// TestValidationError_Error 验证 ValidationError 的 Error 方法
func TestValidationError_Error(t *testing.T) {
	ve := &ValidationError{Field: "age", Message: "must be positive"}
	expected := `field "age" must be positive`
	if ve.Error() != expected {
		t.Fatalf("Error() = %q, want %q", ve.Error(), expected)
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
