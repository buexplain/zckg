package zcconfig

import (
	"reflect"
	"testing"
)

// TestRegister_NilDataNoop 验证 Register 的 fn 返回 nil 时不做任何合并。
func TestRegister_NilDataNoop(t *testing.T) {
	for _, key := range []string{"", "app", "app.db", "missing.child"} {
		t.Run("key="+key, func(t *testing.T) {
			resetForTest(t)
			Register("app", func() map[string]any {
				return map[string]any{"name": "kept", "db": map[string]any(nil)}
			})
			Register(key, func() map[string]any { return nil })
			assertConfigTree(t, map[string]any{
				"app": map[string]any{"name": "kept", "db": map[string]any(nil)},
			})
		})
	}
}

func assertConfigTree(t *testing.T, want map[string]any) {
	t.Helper()
	if got := ConfigAll(); !reflect.DeepEqual(got, want) {
		t.Errorf("ConfigAll 完整树不一致：\n期望: %#v\n实际: %#v", want, got)
	}
}

// TestRegister_NilMapNodeInitialization 验证 nil map 目标在直接注册、递归合并和路径途经时可初始化，且保留完整树中的其他节点。
func TestRegister_NilMapNodeInitialization(t *testing.T) {
	for _, tt := range []struct {
		name string
		key  string
		data map[string]any
		want map[string]any
	}{
		{
			name: "direct",
			key:  "app.db",
			data: map[string]any{"host": "localhost"},
			want: map[string]any{"host": "localhost"},
		},
		{
			name: "recursive",
			key:  "app",
			data: map[string]any{"db": map[string]any{"host": "localhost"}},
			want: map[string]any{"host": "localhost"},
		},
		{
			name: "root_recursive",
			key:  "",
			data: map[string]any{"app": map[string]any{"db": map[string]any{"host": "localhost"}}},
			want: map[string]any{"host": "localhost"},
		},
		{
			name: "path_through_nil",
			key:  "app.db.child",
			data: map[string]any{"host": "localhost"},
			want: map[string]any{"child": map[string]any{"host": "localhost"}},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resetForTest(t)
			Register("", func() map[string]any {
				return map[string]any{
					"peer": "kept",
					"app":  map[string]any{"name": "kept", "db": map[string]any(nil)},
				}
			})
			Register(tt.key, func() map[string]any { return tt.data })
			assertConfigTree(t, map[string]any{
				"peer": "kept",
				"app":  map[string]any{"name": "kept", "db": tt.want},
			})
		})
	}
}

// TestRegister_MapMergeBoundaries 验证 nil 源不清空非 nil 目标，双方 nil、空 map、标量和缺失节点按深合并规则处理。
func TestRegister_MapMergeBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name    string
		dst     any
		src     any
		want    any
		missing bool
	}{
		{
			name: "nil_source_keeps_existing",
			dst:  map[string]any{"host": "kept"},
			src:  map[string]any(nil),
			want: map[string]any{"host": "kept"},
		},
		{
			name: "both_nil",
			dst:  map[string]any(nil),
			src:  map[string]any(nil),
			want: map[string]any(nil),
		},
		{
			name: "nil_target_empty_source",
			dst:  map[string]any(nil),
			src:  map[string]any{},
			want: map[string]any{},
		},
		{
			name: "empty_target_nil_source",
			dst:  map[string]any{},
			src:  map[string]any(nil),
			want: map[string]any{},
		},
		{
			name: "both_empty",
			dst:  map[string]any{},
			src:  map[string]any{},
			want: map[string]any{},
		},
		{
			name: "empty_source_keeps_existing",
			dst:  map[string]any{"host": "kept"},
			src:  map[string]any{},
			want: map[string]any{"host": "kept"},
		},
		{
			name: "scalar_target_map_source",
			dst:  "old",
			src:  map[string]any{"host": "new"},
			want: map[string]any{"host": "new"},
		},
		{
			name: "scalar_target_nil_map_source",
			dst:  "old",
			src:  map[string]any(nil),
			want: map[string]any(nil),
		},
		{
			name: "nil_target_scalar_source",
			dst:  map[string]any(nil),
			src:  "new",
			want: "new",
		},
		{
			name: "map_target_scalar_source",
			dst:  map[string]any{"host": "old"},
			src:  "new",
			want: "new",
		},
		{
			name:    "missing_target_map_source",
			missing: true,
			src:     map[string]any{"host": "new"},
			want:    map[string]any{"host": "new"},
		},
		{
			name:    "missing_target_nil_map_source",
			missing: true,
			src:     map[string]any(nil),
			want:    map[string]any(nil),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resetForTest(t)
			Register("", func() map[string]any {
				app := map[string]any{"name": "kept"}
				if !tt.missing {
					app["db"] = tt.dst
				}
				return map[string]any{"peer": "kept", "app": app}
			})
			Register("app", func() map[string]any { return map[string]any{"db": tt.src} })
			assertConfigTree(t, map[string]any{
				"peer": "kept",
				"app":  map[string]any{"name": "kept", "db": tt.want},
			})
		})
	}
}

// TestRegister_NestedMapRecursiveMerge 验证 mergeMap 的递归合并分支：
// 两次 Register 到同一 key，且值均含同名 map 子节点时，深层 map 递归合并而非整块覆盖。
func TestRegister_NestedMapRecursiveMerge(t *testing.T) {
	resetForTest(t)
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
	resetForTest(t)
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
