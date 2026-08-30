package zcdb

import (
	"reflect"
	"testing"
)

// TestNullSafeFieldScan_StringToNumeric 覆盖 string→uint/float 成功分支
// （PG/SQLite 驱动的 TEXT 列返回 string，需按目标位宽解析）。
func TestNullSafeFieldScan_StringToNumeric(t *testing.T) {
	newField := func(dst any) *nullSafeField {
		return &nullSafeField{field: reflect.ValueOf(dst).Elem()}
	}

	var u uint64
	if err := newField(&u).Scan("456"); err != nil || u != 456 {
		t.Errorf("string→uint64 失败: u=%d err=%v", u, err)
	}
	var f float64
	if err := newField(&f).Scan("1.5"); err != nil || f != 1.5 {
		t.Errorf("string→float64 失败: f=%v err=%v", f, err)
	}
	// 溢出应报错而非静默截断
	var u8 uint8
	if err := newField(&u8).Scan("300"); err == nil {
		t.Error("string→uint8 溢出应报错")
	}
}

// TestNullSafeFieldScan_NumericToBoolUintFloat 覆盖数值→bool 的 uint/float 分支。
func TestNullSafeFieldScan_NumericToBoolUintFloat(t *testing.T) {
	newField := func(dst any) *nullSafeField {
		return &nullSafeField{field: reflect.ValueOf(dst).Elem()}
	}

	var b bool
	if err := newField(&b).Scan(uint64(1)); err != nil || !b {
		t.Errorf("uint64(1)→bool 失败: %v err=%v", b, err)
	}
	if err := newField(&b).Scan(uint64(0)); err != nil || b {
		t.Errorf("uint64(0)→bool 应为 false: %v err=%v", b, err)
	}
	if err := newField(&b).Scan(float64(1.5)); err != nil || !b {
		t.Errorf("float64(1.5)→bool 失败: %v err=%v", b, err)
	}
	if err := newField(&b).Scan(float64(0)); err != nil || b {
		t.Errorf("float64(0)→bool 应为 false: %v err=%v", b, err)
	}
}

// TestNullSafeFieldScan_JSONErrorPaths 覆盖 string→slice/map JSON 反序列化失败与 string→bool 解析失败分支。
func TestNullSafeFieldScan_JSONErrorPaths(t *testing.T) {
	newField := func(dst any) *nullSafeField {
		return &nullSafeField{field: reflect.ValueOf(dst).Elem()}
	}

	var strs []string
	if err := newField(&strs).Scan("not-json"); err == nil {
		t.Error("string→[]string 非法 JSON 应报错")
	}
	var m map[string]any
	if err := newField(&m).Scan("not-json"); err == nil {
		t.Error("string→map 非法 JSON 应报错")
	}
	var b bool
	if err := newField(&b).Scan("not-a-bool"); err == nil {
		t.Error("string→bool 非法输入应报错")
	}
}
