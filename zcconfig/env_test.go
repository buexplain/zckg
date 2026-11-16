package zcconfig

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLoadEnv_BasicTypes(t *testing.T) {
	resetForTest(t)
	content := `
# 注释行
APP_NAME="my app"
PORT=8080
DEBUG=true
PRICE=99.99
export EXPORTED_KEY=exported_value
EMPTY_KEY=
NO_QUOTE=hello world
`
	loadTestEnv(t, content)

	// 字符串（双引号去除）
	if v := Env("APP_NAME", "default"); v != "my app" {
		t.Errorf("期望 'my app'，实际 %s", v)
	}

	// 整数
	if v := Env("PORT", 0); v != 8080 {
		t.Errorf("期望 8080，实际 %d", v)
	}

	// 布尔值
	if v := Env("DEBUG", false); v != true {
		t.Errorf("期望 true，实际 %v", v)
	}

	// 浮点数
	if v := Env("PRICE", 0.0); v != 99.99 {
		t.Errorf("期望 99.99，实际 %f", v)
	}

	// export 前缀
	if v := Env("EXPORTED_KEY", "default"); v != "exported_value" {
		t.Errorf("期望 'exported_value'，实际 %s", v)
	}

	// 空值
	if v := Env("EMPTY_KEY", "default"); v != "" {
		t.Errorf("期望空字符串，实际 %s", v)
	}

	// 无引号
	if v := Env("NO_QUOTE", "default"); v != "hello world" {
		t.Errorf("期望 'hello world'，实际 %s", v)
	}

	// 不存在的 key，返回默认值
	if v := Env("NOT_EXIST", 12345); v != 12345 {
		t.Errorf("期望默认值 12345，实际 %d", v)
	}
}

// 超长单行（70KB，超过 bufio.Scanner 默认 64KB 上限）应能正常加载，
// 调大后的缓冲区（上限 1MB）避免 bufio.ErrTooLong 导致整体加载失败。
func TestLoadEnv_LongLine(t *testing.T) {
	resetForTest(t)
	longVal := strings.Repeat("a", 70*1024)
	loadTestEnv(t, "LONG_KEY="+longVal+"\n")

	if v := Env("LONG_KEY", "fallback"); v != longVal {
		t.Errorf("期望长值完整读回（长度 %d），实际长度 %d", len(longVal), len(v))
	}
}

// export 前缀识别：大小写不敏感，兼容多个空格/Tab；无空白分隔的 "exportKEY=v" 不受影响。
func TestLoadEnv_ExportPrefixVariants(t *testing.T) {
	resetForTest(t)
	content := "EXPORT KEY=v1\nexport  KEY2=v2\nexport\tKEY3=v3\nExPoRt   KEY4=v4\nexportKEY=v5\n"
	loadTestEnv(t, content)

	// 大小写不敏感
	if v := Env("KEY", "fallback"); v != "v1" {
		t.Errorf("EXPORT KEY=v 期望 'v1'，实际 %q", v)
	}
	// 多个空格
	if v := Env("KEY2", "fallback"); v != "v2" {
		t.Errorf("export  KEY2=v 期望 'v2'，实际 %q", v)
	}
	// Tab 分隔
	if v := Env("KEY3", "fallback"); v != "v3" {
		t.Errorf("export\\tKEY3=v 期望 'v3'，实际 %q", v)
	}
	// 混合大小写 + 多空格
	if v := Env("KEY4", "fallback"); v != "v4" {
		t.Errorf("ExPoRt   KEY4=v 期望 'v4'，实际 %q", v)
	}
	// 无空白分隔：exportKEY 是普通 key，不触发前缀剥离
	if v := Env("exportKEY", "fallback"); v != "v5" {
		t.Errorf("exportKEY=v 期望 'v5'，实际 %q", v)
	}
}

func TestLoadEnv_MergeMultipleFiles(t *testing.T) {
	resetForTest(t)

	loadTestEnv(t, "KEY1=value1\nSHARED=from_file1")
	loadTestEnv(t, "KEY2=value2\nSHARED=from_file2")

	if v := Env("KEY1", ""); v != "value1" {
		t.Errorf("期望 'value1'，实际 %s", v)
	}
	if v := Env("KEY2", ""); v != "value2" {
		t.Errorf("期望 'value2'，实际 %s", v)
	}
	// 后加载的覆盖先前的
	if v := Env("SHARED", ""); v != "from_file2" {
		t.Errorf("期望 'from_file2'，实际 %s", v)
	}
}

func TestEnv_TypeConversion(t *testing.T) {
	resetForTest(t)
	content := `
INT_VAL=42
FLOAT_VAL=3.14
BOOL_VAL=true
STR_VAL=hello
`
	loadTestEnv(t, content)

	// string -> int
	if v := Env("INT_VAL", 0); v != 42 {
		t.Errorf("期望 42，实际 %d", v)
	}

	// string -> int64
	if v := Env("INT_VAL", int64(0)); v != 42 {
		t.Errorf("期望 42，实际 %d", v)
	}

	// string -> float64
	if v := Env("FLOAT_VAL", 0.0); v != 3.14 {
		t.Errorf("期望 3.14，实际 %f", v)
	}

	// string -> bool
	if v := Env("BOOL_VAL", false); v != true {
		t.Errorf("期望 true，实际 %v", v)
	}

	// 无法转换时 panic：key 存在但值类型不符，不再静默返回默认值
	assertTypeMismatchPanic(t, "STR_VAL", reflect.TypeFor[int](), reflect.TypeFor[string](), func() {
		Env("STR_VAL", 0)
	})
}

func TestEnv_OsEnvFallback(t *testing.T) {
	resetForTest(t)
	const key = "MY_TEST_OS_ENV"
	// 设置前保存原值，结束后恢复而非直接删除，避免污染开发机同名环境变量
	orig, had := os.LookupEnv(key)
	_ = os.Setenv(key, "os_value")
	defer func() {
		if had {
			_ = os.Setenv(key, orig)
		} else {
			_ = os.Unsetenv(key)
		}
	}()

	// .env 中没有此 key，回退到 OS 环境变量
	if v := Env(key, "default"); v != "os_value" {
		t.Errorf("期望 'os_value'，实际 %s", v)
	}
}

// TestEnv_OsEnvFallbackConversion 验证 OS 环境变量恒为 string，但宽松转换规则仍然生效：
// "8080"/"true"/"10s" 可解析为 int/bool/time.Duration；无法解析时按 key 存在但类型不符 panic。
func TestEnv_OsEnvFallbackConversion(t *testing.T) {
	t.Run("int", func(t *testing.T) {
		resetForTest(t)
		t.Setenv("ZCCONFIG_OS_PORT", "8080")
		if got := Env("ZCCONFIG_OS_PORT", 0); got != 8080 {
			t.Errorf("OS 字符串 8080 转 int 期望 8080，实际 %d", got)
		}
	})
	t.Run("bool", func(t *testing.T) {
		resetForTest(t)
		t.Setenv("ZCCONFIG_OS_DEBUG", "true")
		if got := Env("ZCCONFIG_OS_DEBUG", false); got != true {
			t.Errorf("OS 字符串 true 转 bool 期望 true，实际 %v", got)
		}
	})
	t.Run("duration", func(t *testing.T) {
		resetForTest(t)
		t.Setenv("ZCCONFIG_OS_TIMEOUT", "10s")
		if got := Env("ZCCONFIG_OS_TIMEOUT", time.Second); got != 10*time.Second {
			t.Errorf("OS 字符串 10s 转 Duration 期望 10s，实际 %v", got)
		}
	})
	t.Run("mismatch_panics", func(t *testing.T) {
		resetForTest(t)
		t.Setenv("ZCCONFIG_OS_BAD_PORT", "abc")
		assertTypeMismatchPanic(t, "ZCCONFIG_OS_BAD_PORT", reflect.TypeFor[int](), reflect.TypeFor[string](), func() {
			Env("ZCCONFIG_OS_BAD_PORT", 0)
		})
	})
}

// TestEnvAll 验证零拷贝返回的是内部存储（map[string]string），
// 装载阶段不做类型推断，数字字面量保留为原文。
func TestEnvAll(t *testing.T) {
	resetForTest(t)
	content := "A=1\nB=hello\n"
	loadTestEnv(t, content)

	all := EnvAll()
	if len(all) != 2 {
		t.Errorf("期望 2 个 key，实际 %d", len(all))
	}
	if v, ok := all["A"]; !ok || v != "1" {
		t.Errorf("期望 A=\"1\"（原文），实际 %v", v)
	}
	if v, ok := all["B"]; !ok || v != "hello" {
		t.Errorf("期望 B=\"hello\"，实际 %v", v)
	}
}

// TestEnv_BoolFromLiteralOneZero 验证 .env 中 "1"/"0" 作为文本由 strconv.ParseBool 解析为 bool，
// 与 OS 通道同一条解析路径（装载阶段不再把 1/0 推断为 int）。
func TestEnv_BoolFromLiteralOneZero(t *testing.T) {
	resetForTest(t)
	content := "BOOL_ONE=1\nBOOL_ZERO=0\n"
	loadTestEnv(t, content)

	if v := Env("BOOL_ONE", false); v != true {
		t.Errorf(".env 中 BOOL_ONE=1 期望 true，实际 %v", v)
	}
	if v := Env("BOOL_ZERO", true); v != false {
		t.Errorf(".env 中 BOOL_ZERO=0 期望 false，实际 %v", v)
	}
}

// 并发读写压力测试：并发 LoadEnv + Env + EnvAll 不应产生数据竞争（需以 `go test -race` 运行验证）
func TestConcurrent_LoadEnvAndEnv(t *testing.T) {
	resetForTest(t)
	path := writeTestFile(t, "KEY=value\n")
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := LoadEnv(path); err != nil {
				t.Errorf("LoadEnv 失败: %v", err)
			}
			if v := Env("KEY", ""); v != "value" {
				t.Errorf("Env(KEY) 期望 'value'，实际 %q", v)
			}
			_ = EnvAll()
		}()
	}
	wg.Wait()
	// EnvAll 返回内部零拷贝引用，在 goroutine 内遍历其内容会与 LoadEnv 的写入
	// 构成数据竞争，因此内容断言统一放在 wg.Wait() 之后
	all := EnvAll()
	if v, ok := all["KEY"]; !ok || v != "value" {
		t.Errorf("EnvAll 期望 KEY=value，实际 %v（全部内容 %v）", v, all)
	}
}

// ZCC-03/04：加载不存在的文件应返回错误，且错误信息包含文件路径
func TestLoadEnv_FileNotFound(t *testing.T) {
	resetForTest(t)
	path := filepath.Join(t.TempDir(), "not_exist.env")
	err := LoadEnv(path)
	if err == nil {
		t.Fatalf("加载不存在的文件期望返回错误")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("错误信息期望包含文件路径 %q，实际 %q", path, err.Error())
	}
}

// TestLoadEnv_LineTooLong 验证单行超过 1MB 上限时返回 bufio.ErrTooLong，且错误信息包含文件路径
func TestLoadEnv_LineTooLong(t *testing.T) {
	resetForTest(t)
	longVal := strings.Repeat("a", 1024*1024+1)
	path := writeTestFile(t, "LONG_KEY="+longVal+"\n")
	err := LoadEnv(path)
	if err == nil {
		t.Fatalf("单行超过 1MB 期望返回错误")
	}
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Errorf("期望 errors.Is(err, bufio.ErrTooLong)，实际 %v", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("错误信息期望包含文件路径 %q，实际 %q", path, err.Error())
	}
}

// TestLoadEnv_LineLimitBoundary 锁死扫描上限的确切边界：
// 单行内容为 1MB-1 字节时可加载；达到 1MB 即返回 bufio.ErrTooLong，
// 且文件末尾是否有换行符不影响该边界（bufio.Scanner 的上限是 token 长度，不是“行+换行符”）。
func TestLoadEnv_LineLimitBoundary(t *testing.T) {
	const mb = 1024 * 1024

	t.Run("below_limit_with_newline", func(t *testing.T) {
		resetForTest(t)
		value := strings.Repeat("a", mb-3) // 行内容 = "K=" + value = 1MB-1 字节
		loadTestEnv(t, "K="+value+"\n")
		if got := len(Env("K", "")); got != len(value) {
			t.Errorf("期望读到 %d 字节，实际 %d", len(value), got)
		}
	})

	for _, trailingNewline := range []bool{true, false} {
		name := "at_limit_with_newline"
		if !trailingNewline {
			name = "at_limit_without_newline"
		}
		t.Run(name, func(t *testing.T) {
			resetForTest(t)
			content := "K=" + strings.Repeat("a", mb-2) // 行内容恰好 1MB
			if trailingNewline {
				content += "\n"
			}
			err := LoadEnv(writeTestFile(t, content))
			if err == nil {
				t.Fatalf("单行达到 1MB 期望返回错误")
			}
			if !errors.Is(err, bufio.ErrTooLong) {
				t.Errorf("期望 errors.Is(err, bufio.ErrTooLong)，实际 %v", err)
			}
		})
	}
}

// ZCC-04：加载失败不污染已有数据：先加载合法文件，再加载超限行文件失败后，
// 先前 key 仍可读，失败文件的局部解析结果不得泄漏
func TestLoadEnv_FailureDoesNotPollute(t *testing.T) {
	resetForTest(t)
	goodPath := writeTestFile(t, "GOOD_KEY=good_value\n")
	if err := LoadEnv(goodPath); err != nil {
		t.Fatalf("LoadEnv 合法文件失败: %v", err)
	}

	longVal := strings.Repeat("a", 1024*1024+1)
	badPath := writeTestFile(t, "BAD_KEY="+longVal+"\n")
	if err := LoadEnv(badPath); err == nil {
		t.Fatalf("LoadEnv 超限行文件期望返回错误")
	}

	// 先前加载的 key 完好
	if v := Env("GOOD_KEY", "fallback"); v != "good_value" {
		t.Errorf("加载失败后期望先前 key 仍可读 'good_value'，实际 %q", v)
	}
	// 失败文件的局部解析结果不得泄漏
	if v := Env("BAD_KEY", "fallback"); v != "fallback" {
		t.Errorf("加载失败后期望 BAD_KEY 回退默认值，实际 %q", v)
	}
}

// ZCC-06：无 "=" 的行与空 key 行被静默跳过，且不影响其他行解析
func TestLoadEnv_SkipsInvalidLines(t *testing.T) {
	resetForTest(t)
	content := `
PORT 8080
=value_without_key
   =also_skipped
VALID_KEY=value
`
	loadTestEnv(t, content)

	if v := Env("VALID_KEY", "fallback"); v != "value" {
		t.Errorf("期望 'value'，实际 %q", v)
	}
	// 仅剩 VALID_KEY 一个 key：无 = 行与空 key 行均未入库
	if all := EnvAll(); len(all) != 1 {
		t.Errorf("期望仅 1 个 key，实际 %d：%v", len(all), all)
	}
}

// ZCC-07：首行 UTF-8 BOM 被剥离，首行 key 可正常读取
func TestLoadEnv_Utf8Bom(t *testing.T) {
	resetForTest(t)
	loadTestEnv(t, "\ufeffAPP_NAME=bom_app\nOTHER_KEY=v\n")

	// BOM 剥离后首行 key 不带 \ufeff 前缀
	if v := Env("APP_NAME", "default"); v != "bom_app" {
		t.Errorf("期望 'bom_app'，实际 %q", v)
	}
	// 后续行不受影响
	if v := Env("OTHER_KEY", "default"); v != "v" {
		t.Errorf("期望 'v'，实际 %q", v)
	}
}

// TestLoadEnv_BOMIndentedFirstLine 验证 BOM 后缩进的 export、含等号注释、普通键与空行，并保留 BOM 前空白的兼容性。
func TestLoadEnv_BOMIndentedFirstLine(t *testing.T) {
	for _, leading := range []struct {
		name  string
		value string
	}{
		{"bom_first", ""},
		{"whitespace_before_bom", " \t"},
	} {
		for _, indent := range []struct {
			name  string
			value string
		}{
			{"spaces", "  "},
			{"tabs", "\t\t"},
		} {
			for _, tt := range []struct {
				name  string
				first string
				want  map[string]string
			}{
				{"export", "ExPoRt \tFIRST=ok", map[string]string{"FIRST": "ok", "SECOND": "kept"}},
				{"comment", "# COMMENT=must_not_load", map[string]string{"SECOND": "kept"}},
				{"key", "FIRST=ok", map[string]string{"FIRST": "ok", "SECOND": "kept"}},
				{"blank", "", map[string]string{"SECOND": "kept"}},
			} {
				t.Run(leading.name+"/"+indent.name+"/"+tt.name, func(t *testing.T) {
					resetForTest(t)
					loadTestEnv(t, leading.value+"\ufeff"+indent.value+tt.first+"\n \texport SECOND=kept\n")
					if got := EnvAll(); !reflect.DeepEqual(got, tt.want) {
						t.Errorf("EnvAll 完整键值集合不一致：期望 %#v，实际 %#v", tt.want, got)
					}
				})
			}
		}
	}
}

// TestEnv_NilInterfaceDefKeyExistsPanics 验证键存在、目标为接口类型且默认值为 nil 时，
// 值未实现该接口属于类型不符：panic 的期望类型报接口本身；绕过 LoadEnv 直接写 envData
// 是为了构造 LoadEnv 无法产生的 any 类型值（string 写入后以 error 接口读取）。
func TestEnv_NilInterfaceDefKeyExistsPanics(t *testing.T) {
	resetForTest(t)
	envMu.Lock()
	envData["SOME_KEY"] = "some-value"
	envMu.Unlock()

	assertTypeMismatchPanic(t, "SOME_KEY", reflect.TypeFor[error](), reflect.TypeFor[string](), func() {
		Env[error]("SOME_KEY", nil)
	})
}

// TestEnv_NamedBoolConversion 验证 .env 文本与 OS 字符串均可转换为具名 bool。
func TestEnv_NamedBoolConversion(t *testing.T) {
	type Flag bool
	for _, source := range []string{"file", "os"} {
		for _, tt := range []struct {
			input string
			want  Flag
		}{
			{"0", false},
			{"1", true},
			{"false", false},
			{"true", true},
		} {
			t.Run(source+"/"+tt.input, func(t *testing.T) {
				resetForTest(t)
				const key = "ZCCONFIG_NAMED_BOOL"
				if source == "file" {
					loadTestEnv(t, key+"="+tt.input+"\n")
				} else {
					t.Setenv(key, tt.input)
				}
				if got := Env(key, !tt.want); got != tt.want {
					t.Errorf("Env(%q, %v) 期望 %T(%v)，实际 %T(%v)", key, !tt.want, tt.want, tt.want, got, got)
				}
			})
		}
	}
}

// TestEnv_NamedStringConversion 验证 .env 和 OS 来源的字符串可读取为具名 string。
func TestEnv_NamedStringConversion(t *testing.T) {
	type Label string
	for _, source := range []string{"file", "os"} {
		t.Run(source, func(t *testing.T) {
			resetForTest(t)
			const key = "ZCCONFIG_NAMED_STRING"
			if source == "file" {
				loadTestEnv(t, key+"=hello\n")
			} else {
				t.Setenv(key, "hello")
			}
			if got := Env(key, Label("fallback")); got != Label("hello") {
				t.Errorf("Env 期望 Label(hello)，实际 %T(%q)", got, got)
			}
		})
	}
}

// TestEnv_DurationUnits 验证 .env 文本必须带时间单位才能读取为 time.Duration：
// "30s" 得到 30 秒；裸数字 "30"（含带引号形式）缺少单位，属类型不符 panic，
// 不会像改动前那样被推断为 int 后静默转成 30ns。
func TestEnv_DurationUnits(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input string
	}{
		{"raw_number", "30"},
		{"double_quoted_number", `"30"`},
		{"single_quoted_number", "'30'"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resetForTest(t)
			const key = "ZCCONFIG_DURATION_UNITS"
			loadTestEnv(t, key+"="+tt.input+"\n")
			assertTypeMismatchPanic(t, key, reflect.TypeFor[time.Duration](), reflect.TypeFor[string](), func() {
				Env(key, 5*time.Second)
			})
		})
	}

	t.Run("explicit_seconds", func(t *testing.T) {
		resetForTest(t)
		const key = "ZCCONFIG_DURATION_UNITS"
		loadTestEnv(t, key+"=30s\n")
		if got := Env(key, 5*time.Second); got != 30*time.Second {
			t.Errorf("Env 值 %q 配默认 5s 期望 30s，实际 %v", "30s", got)
		}
	})
}

// --- 装载侧不做类型推断：原文保留、通道差分、有损字面量 fail-fast ---

// envReadResult 描述一次 Env[T] 读取的可比较结果：成功值，或 panic 的 *TypeMismatchError 诊断类型。
// 不比较错误消息，因为消息内含 key，而差分用例的两条通道使用不同 key。
type envReadResult struct {
	value    string
	panicked bool
	expected reflect.Type
	actual   reflect.Type
}

// readEnvOutcome 执行一次读取并归一为 envReadResult，供两条通道逐项比对。
func readEnvOutcome[T any](f func() T) (res envReadResult) {
	defer func() {
		if r := recover(); r != nil {
			res.panicked = true
			var mismatch *TypeMismatchError
			if err, ok := r.(error); ok && errors.As(err, &mismatch) {
				res.expected = mismatch.Expected
				res.actual = mismatch.Actual
			}
		}
	}()
	res.value = fmt.Sprint(f())
	return res
}

// assertEnvChannelParity 断言同一字面量经 .env 与 OS 环境变量两条通道读取为 T 时结果完全一致
// （同成功且值相等，或同 panic 且期望/实际类型相等）。
func assertEnvChannelParity[T any](t *testing.T, fileKey, osKey string, def T) {
	t.Helper()
	fileRes := readEnvOutcome(func() T { return Env(fileKey, def) })
	osRes := readEnvOutcome(func() T { return Env(osKey, def) })
	if fileRes != osRes {
		t.Errorf("key %q/%q 通道不一致：.env=%+v，OS=%+v", fileKey, osKey, fileRes, osRes)
	}
}

// TestEnv_ChannelParity 是本模块的权威差分用例：同一字面量分别经 .env 文件与 OS 环境变量作为存储来源，
// 对每个目标类型的读取结果（成功值或 panic 的诊断类型）必须完全一致。
// 权威依据：两条通道的存储值都是文本，Env[T] 只在读取侧解析；os.LookupEnv 天然提供纯文本源。
// .env 通道一次性加载全部字面量；OS 通道用 t.Setenv（含 t.Setenv 的用例不得 t.Parallel）。
func TestEnv_ChannelParity(t *testing.T) {
	resetForTest(t)
	literals := []string{
		"123", "-1", "200", "99.99", "1e3", "007", "+5", "true", "2", "1", "0",
		"10s", "1h30m", "abc", "hello", "1.0", "99999999999999999999",
	}

	var content strings.Builder
	for i, literal := range literals {
		fmt.Fprintf(&content, "ZCCONFIG_PARITY_FILE_%d=%s\n", i, literal)
	}
	loadTestEnv(t, content.String())
	for i, literal := range literals {
		t.Setenv(fmt.Sprintf("ZCCONFIG_PARITY_OS_%d", i), literal)
	}

	for i, literal := range literals {
		fileKey := fmt.Sprintf("ZCCONFIG_PARITY_FILE_%d", i)
		osKey := fmt.Sprintf("ZCCONFIG_PARITY_OS_%d", i)
		t.Run(fmt.Sprintf("literal_%d_%s", i, literal), func(t *testing.T) {
			assertEnvChannelParity(t, fileKey, osKey, "")
			assertEnvChannelParity(t, fileKey, osKey, 0)
			assertEnvChannelParity(t, fileKey, osKey, int8(0))
			assertEnvChannelParity(t, fileKey, osKey, int64(0))
			assertEnvChannelParity(t, fileKey, osKey, uint(0))
			assertEnvChannelParity(t, fileKey, osKey, float32(0))
			assertEnvChannelParity(t, fileKey, osKey, 0.0)
			assertEnvChannelParity(t, fileKey, osKey, false)
			assertEnvChannelParity(t, fileKey, osKey, time.Duration(0))
		})
	}
}

// TestEnv_RawTextPreserved 验证装载阶段不再推断类型：数字/布尔字面量以 string 读取时返回文件原文
// （含前导零、尾随零、指数、大写布尔、正负号），且 EnvAll 的 value 全部为 string。
func TestEnv_RawTextPreserved(t *testing.T) {
	resetForTest(t)
	loadTestEnv(t, "ZERO_PAD=007\nTRAIL_ZERO=1.50\nEXPONENT=1e3\nUPPER_TRUE=TRUE\nPLUS=+5\nNEG_ZERO=-0\nQUOTED=\"123\"\n")

	want := map[string]string{
		"ZERO_PAD":   "007",
		"TRAIL_ZERO": "1.50",
		"EXPONENT":   "1e3",
		"UPPER_TRUE": "TRUE",
		"PLUS":       "+5",
		"NEG_ZERO":   "-0",
		"QUOTED":     "123",
	}
	for key, wantVal := range want {
		// 默认值带不可能出现的空格，防止“返回默认值”被误判为读取成功
		if got := Env(key, " fallback "); got != wantVal {
			t.Errorf("Env(%q, string) 期望原文 %q，实际 %q", key, wantVal, got)
		}
	}

	all := EnvAll()
	if len(all) != len(want) {
		t.Fatalf("EnvAll 期望 %d 个 key，实际 %d：%v", len(want), len(all), all)
	}
	for key, wantVal := range want {
		got, ok := all[key]
		if !ok {
			t.Errorf("EnvAll 缺少 key %q", key)
			continue
		}
		if got != wantVal {
			t.Errorf("EnvAll[%q] 期望原文 %q，实际 %q", key, wantVal, got)
		}
	}
}

// TestEnv_LossyLiteralFailFast 验证改动前因装载侧类型推断而被静默接受的字面量，
// 现在一律 panic(*TypeMismatchError)：窄化溢出、无符号负数、float→int 截断、
// 裸数字当作 time.Duration、非法 bool 字面量。改动前这些行分别得到 -56 / 18446744073709551615 / 99 / 30ns / true。
func TestEnv_LossyLiteralFailFast(t *testing.T) {
	resetForTest(t)
	loadTestEnv(t, "OUT_OF_RANGE=200\nNEGATIVE=-1\nFRACTION=99.99\nNO_UNIT=30\nBOOL_TWO=2\n")

	assertTypeMismatchPanic(t, "OUT_OF_RANGE", reflect.TypeFor[int8](), reflect.TypeFor[string](), func() {
		Env("OUT_OF_RANGE", int8(0))
	})
	assertTypeMismatchPanic(t, "NEGATIVE", reflect.TypeFor[uint](), reflect.TypeFor[string](), func() {
		Env("NEGATIVE", uint(0))
	})
	assertTypeMismatchPanic(t, "FRACTION", reflect.TypeFor[int](), reflect.TypeFor[string](), func() {
		Env("FRACTION", 0)
	})
	assertTypeMismatchPanic(t, "NO_UNIT", reflect.TypeFor[time.Duration](), reflect.TypeFor[string](), func() {
		Env("NO_UNIT", time.Second)
	})
	assertTypeMismatchPanic(t, "BOOL_TWO", reflect.TypeFor[bool](), reflect.TypeFor[string](), func() {
		Env("BOOL_TWO", false)
	})
}

// TestEnv_LargeIntegerText 验证超出 int64 的整数不再被推断或截断：
// 文本读取完整保留（与 OS 通道一致），整数读取按 strconv 规则失败并 panic。
func TestEnv_LargeIntegerText(t *testing.T) {
	resetForTest(t)
	const literal = "99999999999999999999"
	loadTestEnv(t, "BIG="+literal+"\n")

	if got := Env("BIG", ""); got != literal {
		t.Errorf("Env(BIG, string) 期望原文 %q，实际 %q", literal, got)
	}
	assertTypeMismatchPanic(t, "BIG", reflect.TypeFor[int](), reflect.TypeFor[string](), func() {
		Env("BIG", 0)
	})
}
