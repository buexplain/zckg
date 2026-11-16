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
}

// TestConfig_IntermediateNonMapPanics 验证路径中途已存在标量值时 panic：
// key 为调用方传入的完整 key，期望类型为 map[string]any，实际类型为中途标量类型。
func TestConfig_IntermediateNonMapPanics(t *testing.T) {
	resetForTest(t)
	Register("app", func() map[string]any {
		return map[string]any{"name": "myapp"}
	})
	assertTypeMismatchPanic(t, "app.name.sub", reflect.TypeFor[map[string]any](), reflect.TypeFor[string](), func() {
		Config("app.name.sub", "default")
	})
}

// TestConfig_TypeMismatchPanics 验证 key 存在但值无法转换为 T 时 panic，
// 诊断信息包含完整 key、目标类型 T 与实际存储类型；结构体目标同样适用。
func TestConfig_TypeMismatchPanics(t *testing.T) {
	t.Run("string_to_int", func(t *testing.T) {
		resetForTest(t)
		Register("app", func() map[string]any {
			return map[string]any{"port": "abc"}
		})
		assertTypeMismatchPanic(t, "app.port", reflect.TypeFor[int](), reflect.TypeFor[string](), func() {
			Config("app.port", 3306)
		})
	})
	t.Run("string_to_struct", func(t *testing.T) {
		resetForTest(t)
		Register("app", func() map[string]any {
			return map[string]any{"db": "not-a-struct"}
		})
		assertTypeMismatchPanic(t, "app.db", reflect.TypeFor[DBConfig](), reflect.TypeFor[string](), func() {
			Config("app.db", DBConfig{})
		})
	})
	t.Run("map_to_struct", func(t *testing.T) {
		// Config 不做 JSON 式解码：注册值是 map[string]any 时不能读成结构体。
		resetForTest(t)
		Register("app", func() map[string]any {
			return map[string]any{"db": map[string]any{"host": "localhost"}}
		})
		assertTypeMismatchPanic(t, "app.db", reflect.TypeFor[DBConfig](), reflect.TypeFor[map[string]any](), func() {
			Config("app.db", DBConfig{})
		})
	})
	t.Run("numeric_to_string", func(t *testing.T) {
		resetForTest(t)
		Register("app", func() map[string]any {
			return map[string]any{"name": 42}
		})
		assertTypeMismatchPanic(t, "app.name", reflect.TypeFor[string](), reflect.TypeFor[int](), func() {
			Config("app.name", "default")
		})
	})
}

// TestConfig_NilValueReturnsDefault 验证存储值为 nil 视为未配置：
// 叶子节点与路径中途节点均返回 def，不 panic。
func TestConfig_NilValueReturnsDefault(t *testing.T) {
	resetForTest(t)
	Register("app", func() map[string]any {
		return map[string]any{"leaf": nil, "child": nil}
	})
	if got := Config("app.leaf", "def"); got != "def" {
		t.Errorf("nil 叶子节点期望默认值 def，实际 %q", got)
	}
	if got := Config("app.child.sub", "def"); got != "def" {
		t.Errorf("nil 中间节点期望默认值 def，实际 %q", got)
	}
}

// TestConfig_DotSeparatorNotEscapable 锁死 "." 是唯一层级分隔符且不可转义：
// map 中字面含 "." 的 key 无法按路径读取；空路径段按空字符串键查找。
func TestConfig_DotSeparatorNotEscapable(t *testing.T) {
	resetForTest(t)
	Register("app", func() map[string]any {
		return map[string]any{
			"a.b": 42,
			"":    "empty-segment",
		}
	})
	if got := Config("app.a.b", "def"); got != "def" {
		t.Errorf(`字面 key "a.b" 不可按路径读取，期望默认值 def，实际 %v`, got)
	}
	if got := Config("app.", "def"); got != "empty-segment" {
		t.Errorf("空路径段应按空字符串键查找，期望 empty-segment，实际 %v", got)
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

// TestConfig_NilInterfaceDefKeyExistsPanics 验证业务配置键存在、目标为接口类型且默认值为 nil 时，
// 值未实现该接口属于类型不符：panic 的期望类型报接口本身，而不是 def 的动态类型。
func TestConfig_NilInterfaceDefKeyExistsPanics(t *testing.T) {
	resetForTest(t)
	Register("app", func() map[string]any {
		return map[string]any{"value": "some-value"}
	})

	assertTypeMismatchPanic(t, "app.value", reflect.TypeFor[fmt.Stringer](), reflect.TypeFor[string](), func() {
		Config[fmt.Stringer]("app.value", nil)
	})
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

// TestConfig_SliceToArrayConversion 验证长度足够的切片转换为数组及数组指针；长度不足时 panic，
// 期望类型分别是 [2]int 与 *[2]int，实际类型为 []int。
func TestConfig_SliceToArrayConversion(t *testing.T) {
	for _, target := range []string{"array", "pointer"} {
		for _, tt := range []struct {
			name  string
			value []int
			want  [2]int
			valid bool
		}{
			{"nil", nil, [2]int{8, 9}, false},
			{"empty", []int{}, [2]int{8, 9}, false},
			{"short", []int{1}, [2]int{8, 9}, false},
			{"exact", []int{1, 2}, [2]int{1, 2}, true},
			{"long", []int{1, 2, 3}, [2]int{1, 2}, true},
		} {
			t.Run(target+"/"+tt.name, func(t *testing.T) {
				resetForTest(t)
				Register("app", func() map[string]any { return map[string]any{"values": tt.value} })
				def := [2]int{8, 9}
				if target == "array" {
					if tt.valid {
						if got := Config("app.values", def); got != tt.want {
							t.Errorf("Config 数组期望 %v，实际 %v", tt.want, got)
						}
						return
					}
					assertTypeMismatchPanic(t, "app.values", reflect.TypeFor[[2]int](), reflect.TypeFor[[]int](), func() {
						Config("app.values", def)
					})
					return
				}
				if tt.valid {
					got := Config("app.values", &def)
					if got == nil || *got != tt.want {
						t.Fatalf("Config 数组指针期望指向 %v，实际 %#v", tt.want, got)
					}
					return
				}
				assertTypeMismatchPanic(t, "app.values", reflect.TypeFor[*[2]int](), reflect.TypeFor[[]int](), func() {
					Config("app.values", &def)
				})
			})
		}
	}
}

// TestConfig_UintptrConversion 验证 uintptr 的码点转 string 被拒绝（panic），零与非零可转 bool，
// 字符串可解析为 uintptr。
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
			assertTypeMismatchPanic(t, "app.value", reflect.TypeFor[string](), reflect.TypeFor[uintptr](), func() {
				Config("app.value", "fallback")
			})
			assertTypeMismatchPanic(t, "app.value", reflect.TypeFor[Label](), reflect.TypeFor[uintptr](), func() {
				Config("app.value", Label("fallback"))
			})
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

// TestConfig_ComplexBoolPanics 验证 complex64/128 的零与非零值不支持转 bool，
// 均 panic 且期望类型为 bool / 具名 Flag，实际类型为复数类型。
func TestConfig_ComplexBoolPanics(t *testing.T) {
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
				assertTypeMismatchPanic(t, "app.value", reflect.TypeFor[bool](), reflect.TypeOf(tt.value), func() {
					Config("app.value", def)
				})
				assertTypeMismatchPanic(t, "app.value", reflect.TypeFor[Flag](), reflect.TypeOf(tt.value), func() {
					Config("app.value", Flag(def))
				})
			})
		}
	}
}

// TestConfig_DurationUnitlessStrings 验证无单位字符串 0、+0、-0 可解析，
// 非零数字字符串缺少时间单位、无法解析转为 Duration 时 panic。
func TestConfig_DurationUnitlessStrings(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  time.Duration
		valid bool
	}{
		{"0", 0, true},
		{"+0", 0, true},
		{"-0", 0, true},
		{"10", 5 * time.Second, false},
	} {
		t.Run(tt.input, func(t *testing.T) {
			resetForTest(t)
			Register("app", func() map[string]any { return map[string]any{"timeout": tt.input} })
			if !tt.valid {
				assertTypeMismatchPanic(t, "app.timeout", reflect.TypeFor[time.Duration](), reflect.TypeFor[string](), func() {
					Config("app.timeout", 5*time.Second)
				})
				return
			}
			if got := Config("app.timeout", 5*time.Second); got != tt.want {
				t.Errorf("Config 字符串 %q 配默认 5s 期望 %v，实际 %v", tt.input, tt.want, got)
			}
		})
	}
}

// TestConfig_NumericConversionRejectsLossy 锁死 Config 通道的数值转换只允许静态无损子集：
// Register 提供的 typed 数值在窄化、有符号→无符号、浮点→整数、整数→浮点超尾数位、
// 数值→time.Duration 时 panic(*TypeMismatchError)；同族加宽、允许的整型→浮点与数值→bool 正常读取。
// 对应 cast 层规则见 cast_test.go 的 TestCast_NumericSafeSubsetAllowed/Rejected。
func TestConfig_NumericConversionRejectsLossy(t *testing.T) {
	resetForTest(t)
	Register("app", func() map[string]any {
		return map[string]any{
			"price":    99.99,
			"count":    200,
			"negative": -1,
			"flag":     2,
			"small":    int16(3306),
			"bits":     uint8(255),
			"ratio":    float64(3.14),
			"rate":     float32(2.5),
			"big":      int64(3306),
			"timeout":  int64(30),
			"dur":      time.Duration(30),
		}
	})

	rejected := []struct {
		name     string
		key      string
		expected reflect.Type
		actual   reflect.Type
		read     func()
	}{
		{"float_to_int", "app.price", reflect.TypeFor[int](), reflect.TypeFor[float64](), func() { Config("app.price", 0) }},
		{"int_narrow", "app.count", reflect.TypeFor[int8](), reflect.TypeFor[int](), func() { Config("app.count", int8(0)) }},
		{"signed_to_unsigned", "app.negative", reflect.TypeFor[uint](), reflect.TypeFor[int](), func() { Config("app.negative", uint(0)) }},
		{"int64_to_float64", "app.big", reflect.TypeFor[float64](), reflect.TypeFor[int64](), func() { Config("app.big", 0.0) }},
		{"float_narrow", "app.ratio", reflect.TypeFor[float32](), reflect.TypeFor[float64](), func() { Config("app.ratio", float32(0)) }},
		{"numeric_to_duration", "app.timeout", reflect.TypeFor[time.Duration](), reflect.TypeFor[int64](), func() { Config("app.timeout", time.Duration(0)) }},
	}
	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			assertTypeMismatchPanic(t, tt.key, tt.expected, tt.actual, tt.read)
		})
	}

	allowed := []struct {
		name string
		run  func(*testing.T)
	}{
		{"int_to_int64", func(t *testing.T) {
			if got := Config("app.count", int64(0)); got != int64(200) {
				t.Errorf("Config int(200) -> int64 期望 200，实际 %d", got)
			}
		}},
		{"int16_to_float64", func(t *testing.T) {
			if got := Config("app.small", 0.0); got != 3306.0 {
				t.Errorf("Config int16(3306) -> float64 期望 3306，实际 %v", got)
			}
		}},
		{"uint8_to_uint32", func(t *testing.T) {
			if got := Config("app.bits", uint32(0)); got != uint32(255) {
				t.Errorf("Config uint8(255) -> uint32 期望 255，实际 %d", got)
			}
		}},
		{"float32_to_float64", func(t *testing.T) {
			if got := Config("app.rate", 0.0); got != 2.5 {
				t.Errorf("Config float32(2.5) -> float64 期望 2.5，实际 %v", got)
			}
		}},
		{"numeric_to_bool", func(t *testing.T) {
			if got := Config("app.flag", false); got != true {
				t.Errorf("Config int(2) -> bool 期望按 C 惯例为 true，实际 %v", got)
			}
		}},
		{"duration_self", func(t *testing.T) {
			if got := Config("app.dur", time.Duration(0)); got != 30 {
				t.Errorf("Config Duration(30) -> Duration 期望 30，实际 %v", got)
			}
		}},
		{"duration_to_int64", func(t *testing.T) {
			if got := Config("app.dur", int64(0)); got != int64(30) {
				t.Errorf("Config Duration(30) -> int64 期望 30，实际 %d", got)
			}
		}},
	}
	for _, tt := range allowed {
		t.Run(tt.name, tt.run)
	}
}
