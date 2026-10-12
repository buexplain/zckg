package zcconfig

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestConfig_BasicLookup(t *testing.T) {
	resetForTest(t)
	Register("app", func() map[string]any {
		return map[string]any{
			"name": "myapp",
			"port": 3000,
			"database": map[string]any{
				"host": "localhost",
				"port": 5432,
			},
		}
	})
	Register("", func() map[string]any {
		return map[string]any{
			"debug": true,
		}
	})

	if v := Config("app.name", "default"); v != "myapp" {
		t.Errorf("期望 'myapp'，实际 %s", v)
	}

	if v := Config("app.port", 0); v != 3000 {
		t.Errorf("期望 3000，实际 %d", v)
	}

	if v := Config("app.database.host", "default"); v != "localhost" {
		t.Errorf("期望 'localhost'，实际 %s", v)
	}

	if v := Config("app.database.port", 0); v != 5432 {
		t.Errorf("期望 5432，实际 %d", v)
	}

	if v := Config("debug", false); v != true {
		t.Errorf("期望 true，实际 %v", v)
	}
}

func TestConfig_DefaultValues(t *testing.T) {
	resetForTest(t)
	Register("app", func() map[string]any {
		return map[string]any{
			"name": "myapp",
		}
	})

	// 不存在的顶层 key
	if v := Config("notexist", "default"); v != "default" {
		t.Errorf("期望 'default'，实际 %s", v)
	}

	// 不存在的嵌套 key
	if v := Config("app.notexist", "default"); v != "default" {
		t.Errorf("期望 'default'，实际 %s", v)
	}

	// 路径中间层不是 map
	if v := Config("app.name.sub", "default"); v != "default" {
		t.Errorf("期望 'default'，实际 %s", v)
	}
}

func TestConfig_NestedRegistration(t *testing.T) {
	resetForTest(t)
	Register("app", func() map[string]any {
		return map[string]any{
			"name": "myapp",
			"port": 3000,
		}
	})
	Register("app.database", func() map[string]any {
		return map[string]any{
			"host": "localhost",
			"port": 5432,
		}
	})

	// app 下已有数据
	if v := Config("app.name", ""); v != "myapp" {
		t.Errorf("期望 'myapp'，实际 %s", v)
	}
	if v := Config("app.port", 0); v != 3000 {
		t.Errorf("期望 3000，实际 %d", v)
	}

	// app.database 子节点数据
	if v := Config("app.database.host", ""); v != "localhost" {
		t.Errorf("期望 'localhost'，实际 %s", v)
	}
	if v := Config("app.database.port", 0); v != 5432 {
		t.Errorf("期望 5432，实际 %d", v)
	}
}

func TestConfig_ImmediateExecution(t *testing.T) {
	resetForTest(t)
	callCount := 0
	Register("app", func() map[string]any {
		callCount++
		return map[string]any{
			"name": "myapp",
		}
	})

	// fn 在 Register 时立即执行
	if callCount != 1 {
		t.Fatalf("期望 fn 执行 1 次，实际 %d 次", callCount)
	}

	// Config 不会再调用 fn
	_ = Config("app.name", "")
	if callCount != 1 {
		t.Errorf("期望 fn 仍执行 1 次，实际 %d 次", callCount)
	}
}

func TestConfig_MergeRegistration(t *testing.T) {
	resetForTest(t)
	Register("app", func() map[string]any {
		return map[string]any{
			"name": "first",
			"port": 3000,
		}
	})
	Register("app", func() map[string]any {
		return map[string]any{
			"port":  8080,
			"debug": true,
		}
	})

	// 第一次注册的值保留
	if v := Config("app.name", ""); v != "first" {
		t.Errorf("期望 'first'，实际 %s", v)
	}

	// 第二次注册覆盖
	if v := Config("app.port", 0); v != 8080 {
		t.Errorf("期望 8080，实际 %d", v)
	}

	// 第二次注册新增
	if v := Config("app.debug", false); v != true {
		t.Errorf("期望 true，实际 %v", v)
	}
}

func TestConfig_TypeConversion(t *testing.T) {
	resetForTest(t)
	Register("db", func() map[string]any {
		return map[string]any{
			"port":  "3306",
			"ratio": "0.95",
		}
	})

	// string -> int
	if v := Config("db.port", 0); v != 3306 {
		t.Errorf("期望 3306，实际 %d", v)
	}

	// string -> float64
	if v := Config("db.ratio", 0.0); v != 0.95 {
		t.Errorf("期望 0.95，实际 %f", v)
	}
}

func TestConfigAll(t *testing.T) {
	resetForTest(t)
	Register("app", func() map[string]any {
		return map[string]any{
			"name": "test",
			"database": map[string]any{
				"host": "localhost",
				"port": 5432,
			},
			"cache": map[string]any{
				"driver": "memory",
			},
		}
	})
	Register("", func() map[string]any {
		return map[string]any{"debug": true}
	})

	want := map[string]any{
		"app": map[string]any{
			"name": "test",
			"database": map[string]any{
				"host": "localhost",
				"port": 5432,
			},
			"cache": map[string]any{
				"driver": "memory",
			},
		},
		"debug": true,
	}
	all := ConfigAll()
	if !reflect.DeepEqual(all, want) {
		t.Errorf("ConfigAll 与期望的完整树不一致:\n期望: %v\n实际: %v", want, all)
	}
}

// 并发读写压力测试：并发 Register + Config 不应产生数据竞争（需以 `go test -race` 运行验证）
func TestConcurrent_RegisterAndConfig(t *testing.T) {
	resetForTest(t)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := fmt.Sprintf("key_%d", n)
			Register(key, func() map[string]any {
				return map[string]any{"value": n}
			})
			if v := Config(key+".value", 0); v != n {
				t.Errorf("期望 %d，实际 %d", n, v)
			}
		}(i)
	}
	wg.Wait()
	// 全部 goroutine 结束后验证 100 个注册结果均存在且值正确，
	// 排除"单次读成功但并发合并丢数据"的场景
	all := ConfigAll()
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key_%d", i)
		node, ok := all[key].(map[string]any)
		if !ok {
			t.Fatalf("ConfigAll 缺少 key %q，实际 %v", key, all)
		}
		if v, ok := node["value"]; !ok || v != i {
			t.Errorf("key_%d.value 期望 %d，实际 %v", i, i, v)
		}
	}
}

// ConfigAll 返回内部存储的直接引用（零拷贝）：写入返回值会穿透到内部
// 注意：此写操作仅在测试单 goroutine 下安全；生产代码中写返回值会产生
// 数据竞争，文档已警示仅限只读场景。
func TestConfigAll_ZeroCopy(t *testing.T) {
	resetForTest(t)
	Register("app", func() map[string]any {
		return map[string]any{"name": "test"}
	})

	all := ConfigAll()
	all["app"].(map[string]any)["name"] = "modified"

	if v := Config("app.name", ""); v != "modified" {
		t.Errorf("零拷贝语义：期望 'modified'（写入返回值穿透到内部），实际 %q", v)
	}
}

// reset 后再读取：两条通道均应清空，仅剩 OS 环境变量回退
func TestReset_ThenRead(t *testing.T) {
	resetForTest(t)
	Register("app", func() map[string]any {
		return map[string]any{"name": "test"}
	})
	loadTestEnv(t, "RESET_ENV_KEY=reset_val\n")

	// 测试中途显式 reset：本用例专门验证 reset 的清空语义
	reset()

	if v := Config("app.name", "fallback"); v != "fallback" {
		t.Errorf("Reset 后 Config 期望默认值，实际 %q", v)
	}
	if v := Env("RESET_ENV_KEY", "fallback"); v != "fallback" {
		t.Errorf("Reset 后 Env 期望默认值，实际 %q", v)
	}
	if all := ConfigAll(); len(all) != 0 {
		t.Errorf("Reset 后 ConfigAll 期望空 map，实际 %v", all)
	}
	if all := EnvAll(); len(all) != 0 {
		t.Errorf("Reset 后 EnvAll 期望空 map，实际 %v", all)
	}
}

// TestConfig_NilInterfaceDefKeyExists 验证业务配置键存在、目标为接口类型且默认值为 nil 时，
// Config 返回 nil 而非 panic。
func TestConfig_NilInterfaceDefKeyExists(t *testing.T) {
	resetForTest(t)
	Register("app", func() map[string]any {
		return map[string]any{"value": "some-value"}
	})

	if v := Config[fmt.Stringer]("app.value", nil); v != nil {
		t.Errorf("Config[fmt.Stringer] 期望 nil，实际 %v", v)
	}
}

// TestConfig_NamedBoolConversion 验证有符号、无符号、浮点数的零与非零值以及字符串转换到具名 bool。
func TestConfig_NamedBoolConversion(t *testing.T) {
	type Flag bool
	for _, tt := range []struct {
		name  string
		value any
		want  Flag
	}{
		{"int_zero", int(0), false},
		{"int_nonzero", int(-2), true},
		{"uint_zero", uint(0), false},
		{"uint_nonzero", uint(2), true},
		{"float_zero", float64(0), false},
		{"float_nonzero", float64(-0.25), true},
		{"string_zero", "0", false},
		{"string_one", "1", true},
		{"string_false", "false", false},
		{"string_true", "true", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resetForTest(t)
			Register("app", func() map[string]any { return map[string]any{"flag": tt.value} })
			if got := Config("app.flag", !tt.want); got != tt.want {
				t.Errorf("Config 源 %T(%v) 期望 %T(%v)，实际 %T(%v)", tt.value, tt.value, tt.want, tt.want, got, got)
			}
		})
	}
}

// TestConfig_NamedStringConversion 验证普通 string 与两种具名 string 在 Config 中互转，且同类型直读保持值。
func TestConfig_NamedStringConversion(t *testing.T) {
	type Label string
	type Other string
	for _, tt := range []struct {
		name  string
		value any
	}{
		{"string", "hello"},
		{"label", Label("hello")},
		{"other", Other("hello")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resetForTest(t)
			Register("app", func() map[string]any { return map[string]any{"label": tt.value} })
			if got := Config("app.label", "fallback"); got != "hello" {
				t.Errorf("源 %T 转 string 期望 hello，实际 %q", tt.value, got)
			}
			if got := Config("app.label", Label("fallback")); got != Label("hello") {
				t.Errorf("源 %T 转 Label 期望 hello，实际 %q", tt.value, got)
			}
			if got := Config("app.label", Other("fallback")); got != Other("hello") {
				t.Errorf("源 %T 转 Other 期望 hello，实际 %q", tt.value, got)
			}
		})
	}
}

// TestConfig_SliceToArrayConversion 验证 nil、空、短切片转数组及数组指针返回非零默认值，长度足够时读取前两个元素。
func TestConfig_SliceToArrayConversion(t *testing.T) {
	for _, target := range []string{"array", "pointer"} {
		for _, tt := range []struct {
			name  string
			value []int
			valid bool
		}{
			{"nil", nil, false},
			{"empty", []int{}, false},
			{"short", []int{1}, false},
			{"exact", []int{1, 2}, true},
			{"long", []int{1, 2, 3}, true},
		} {
			t.Run(target+"/"+tt.name, func(t *testing.T) {
				resetForTest(t)
				Register("app", func() map[string]any { return map[string]any{"values": tt.value} })
				def := [2]int{8, 9}
				want := def
				if tt.valid {
					want = [2]int{1, 2}
				}
				if target == "array" {
					if got := Config("app.values", def); got != want {
						t.Errorf("Config 数组期望 %v，实际 %v", want, got)
					}
				} else {
					got := Config("app.values", &def)
					if got == nil || *got != want {
						t.Fatalf("Config 数组指针期望指向 %v，实际 %#v", want, got)
					}
					if !tt.valid && got != &def {
						t.Error("转换失败应返回原默认值指针")
					}
				}
			})
		}
	}
}

// TestConfig_UintptrConversion 验证 uintptr 的码点转 string 被拒绝，零与非零可转 bool，字符串可解析为 uintptr。
func TestConfig_UintptrConversion(t *testing.T) {
	type Label string
	type Flag bool
	for _, tt := range []struct {
		name  string
		value uintptr
		want  bool
	}{
		{"zero", 0, false},
		{"codepoint", 65, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resetForTest(t)
			Register("app", func() map[string]any { return map[string]any{"value": tt.value} })
			if got := Config("app.value", "fallback"); got != "fallback" {
				t.Errorf("uintptr 不应转换为码点字符串，实际 %q", got)
			}
			if got := Config("app.value", Label("fallback")); got != Label("fallback") {
				t.Errorf("uintptr 不应转换为具名码点字符串，实际 %q", got)
			}
			if got := Config("app.value", !tt.want); got != tt.want {
				t.Errorf("uintptr(%d) 转 bool 期望 %v，实际 %v", tt.value, tt.want, got)
			}
			if got := Config("app.value", Flag(!tt.want)); got != Flag(tt.want) {
				t.Errorf("uintptr(%d) 转 Flag 期望 %v，实际 %v", tt.value, tt.want, got)
			}
		})
	}
	t.Run("string_to_uintptr", func(t *testing.T) {
		resetForTest(t)
		Register("app", func() map[string]any { return map[string]any{"value": "65"} })
		if got := Config("app.value", uintptr(9)); got != uintptr(65) {
			t.Errorf("字符串 65 转 uintptr 期望 65，实际 %d", got)
		}
	})
}

// TestConfig_ComplexBoolDefaults 验证 complex64/128 的零与非零值不支持转 bool，始终返回指定默认值。
func TestConfig_ComplexBoolDefaults(t *testing.T) {
	type Flag bool
	for _, tt := range []struct {
		name  string
		value any
	}{
		{"complex64_zero", complex64(0)},
		{"complex64_nonzero", complex64(1 + 2i)},
		{"complex128_zero", complex128(0)},
		{"complex128_nonzero", complex128(1 + 2i)},
	} {
		for _, def := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/default_%v", tt.name, def), func(t *testing.T) {
				resetForTest(t)
				Register("app", func() map[string]any { return map[string]any{"value": tt.value} })
				if got := Config("app.value", def); got != def {
					t.Errorf("%T(%v) 转 bool 期望默认值 %v，实际 %v", tt.value, tt.value, def, got)
				}
				if got := Config("app.value", Flag(def)); got != Flag(def) {
					t.Errorf("%T(%v) 转 Flag 期望默认值 %v，实际 %v", tt.value, tt.value, def, got)
				}
			})
		}
	}
}

// TestConfig_DurationUnitlessStrings 验证无单位字符串 0、+0、-0 可解析，10 返回 5s 默认值。
func TestConfig_DurationUnitlessStrings(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  time.Duration
	}{
		{"0", 0},
		{"+0", 0},
		{"-0", 0},
		{"10", 5 * time.Second},
	} {
		t.Run(tt.input, func(t *testing.T) {
			resetForTest(t)
			Register("app", func() map[string]any { return map[string]any{"timeout": tt.input} })
			if got := Config("app.timeout", 5*time.Second); got != tt.want {
				t.Errorf("Config 字符串 %q 配默认 5s 期望 %v，实际 %v", tt.input, tt.want, got)
			}
		})
	}
}
