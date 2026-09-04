package zcconfig

import "testing"

// TestRegister_NilDataNoop 验证 Register 的 fn 返回 nil 时不做任何合并。
func TestRegister_NilDataNoop(t *testing.T) {
	reset()
	Register("app", func() map[string]any { return nil })
	if v := Config("app.name", "fallback"); v != "fallback" {
		t.Errorf("nil 数据注册后 Config 应返回默认值，实际 %q", v)
	}
}

// TestRegister_NestedMapRecursiveMerge 验证 mergeMap 的递归合并分支：
// 两次 Register 到同一 key，且值均含同名 map 子节点时，深层 map 递归合并而非整块覆盖。
func TestRegister_NestedMapRecursiveMerge(t *testing.T) {
	reset()
	Register("app", func() map[string]any {
		return map[string]any{"db": map[string]any{"host": "localhost", "port": 3306}}
	})
	Register("app", func() map[string]any {
		return map[string]any{"db": map[string]any{"port": 5432, "user": "root"}}
	})
	if v := Config("app.db.host", ""); v != "localhost" {
		t.Errorf("递归合并后旧节点应保留，实际 %v", v)
	}
	if v := Config("app.db.port", 0); v != 5432 {
		t.Errorf("递归合并后同名节点应覆盖，实际 %v", v)
	}
	if v := Config("app.db.user", ""); v != "root" {
		t.Errorf("递归合并后新节点应可读，实际 %v", v)
	}
}

// TestRegister_ScalarThenSubtreeOverwrite 验证 mergeMap 的覆盖分支：
// 先注册标量值，再在同一路径下注册子 map，标量应被覆盖为 map（文档约定的行为）。
func TestRegister_ScalarThenSubtreeOverwrite(t *testing.T) {
	reset()
	// 先注册 "app.name" 为标量
	Register("app", func() map[string]any {
		return map[string]any{"name": "myapp"}
	})
	if v := Config("app.name", ""); v != "myapp" {
		t.Fatalf("预期 app.name 为标量 myapp，实际 %v", v)
	}
	// 再在 "app.name" 路径下注册子节点，触发 mergeMap 中 dst 标量被 src map 覆盖
	Register("app.name", func() map[string]any {
		return map[string]any{"sub": "deep"}
	})
	if v := Config("app.name.sub", "fallback"); v != "deep" {
		t.Errorf("标量被 map 覆盖后应能读取子节点，实际 %v", v)
	}
	// 递归合并分支：同名 map 深层合并
	Register("app.name", func() map[string]any {
		return map[string]any{"sub2": "deep2"}
	})
	if v := Config("app.name.sub", "fallback"); v != "deep" {
		t.Errorf("深层合并后原节点应保留，实际 %v", v)
	}
	if v := Config("app.name.sub2", "fallback"); v != "deep2" {
		t.Errorf("深层合并后新节点应可读，实际 %v", v)
	}
}
