package zcdb

import (
	"errors"
	"testing"
)

// TestExtractInsertData_EmptyStructSlice 验证切片元素为无字段结构体时返回 ErrNoFields。
func TestExtractInsertData_EmptyStructSlice(t *testing.T) {
	type Empty struct{}
	_, _, err := extractInsertData([]Empty{{}, {}}, "")
	if !errors.Is(err, ErrNoFields) {
		t.Fatalf("expected ErrNoFields for empty struct slice, got %v", err)
	}
}

// TestExtractInsertData_NilPtrSliceHead 验证切片首元素为 nil 指针时返回 ErrInvalidStruct。
func TestExtractInsertData_NilPtrSliceHead(t *testing.T) {
	type User struct {
		Name string `db:"name"`
	}
	_, _, err := extractInsertData([]*User{nil, {Name: "alice"}}, "")
	if !errors.Is(err, ErrInvalidStruct) {
		t.Fatalf("expected ErrInvalidStruct for nil head element, got %v", err)
	}
}

// TestExtractInsertData_AllNilPtrFields 验证切片所有字段均为 nil 指针时返回 ErrNoFields。
func TestExtractInsertData_AllNilPtrFields(t *testing.T) {
	type User struct {
		Name  *string `db:"name"`
		Email *string `db:"email"`
	}
	_, _, err := extractInsertData([]*User{{}}, "")
	if !errors.Is(err, ErrNoFields) {
		t.Fatalf("expected ErrNoFields for all-nil fields, got %v", err)
	}
}

// TestExtractUpdateData_EmptyStruct 验证空结构体（无字段）update 返回 ErrNoFields。
func TestExtractUpdateData_EmptyStruct(t *testing.T) {
	type Empty struct{}
	_, _, err := extractUpdateData(Empty{}, "")
	if !errors.Is(err, ErrNoFields) {
		t.Fatalf("expected ErrNoFields for empty struct, got %v", err)
	}
}

// TestExtractUpdateData_AllNilPtrFields 验证所有字段均为 nil 指针时 update 返回 ErrNoFields。
func TestExtractUpdateData_AllNilPtrFields(t *testing.T) {
	type User struct {
		Name  *string `db:"name"`
		Email *string `db:"email"`
	}
	_, _, err := extractUpdateData(User{}, "")
	if !errors.Is(err, ErrNoFields) {
		t.Fatalf("expected ErrNoFields for all-nil update fields, got %v", err)
	}
}
