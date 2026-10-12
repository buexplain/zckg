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

func writeTestFile(t *testing.T, content string) string {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}
	return path
}

// resetForTest 前置清空 env 与业务配置存储，并注册 t.Cleanup 在测试结束后再次清空，
// 保证单个测试的注册数据不泄漏到包内后续测试。
func resetForTest(t *testing.T) {
	t.Helper()
	reset()
	t.Cleanup(reset)
}

// loadTestEnv 将 content 写入临时 .env 文件并加载，失败时 t.Fatalf 终止用例。
// 仅用于成功加载路径；需要断言 LoadEnv 错误的用例请直接使用 writeTestFile。
func loadTestEnv(t *testing.T, content string) string {
	t.Helper()
	path := writeTestFile(t, content)
	if err := LoadEnv(path); err != nil {
		t.Fatalf("LoadEnv 失败: %v", err)
	}
	return path
}

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

	// 无法转换时返回默认值
	if v := Env("STR_VAL", 0); v != 0 {
		t.Errorf("期望默认值 0，实际 %d", v)
	}
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

func TestEnvAll(t *testing.T) {
	resetForTest(t)
	content := "A=1\nB=hello\n"
	loadTestEnv(t, content)

	all := EnvAll()
	if len(all) != 2 {
		t.Errorf("期望 2 个 key，实际 %d", len(all))
	}
	if v, ok := all["A"]; !ok || v != 1 {
		t.Errorf("期望 A=1，实际 %v", v)
	}
	if v, ok := all["B"]; !ok || v != "hello" {
		t.Errorf("期望 B=hello，实际 %v", v)
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

// .env 中 "1"/"0" 被 parseValue 推断为 int，经 cast 数值→bool 特判转为 bool
func TestEnv_BoolFromIntString(t *testing.T) {
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

// ZCC-03/04：单行超过 1MB 上限应返回 bufio.ErrTooLong，且错误信息包含文件路径
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
				want  map[string]any
			}{
				{"export", "ExPoRt \tFIRST=ok", map[string]any{"FIRST": "ok", "SECOND": "kept"}},
				{"comment", "# COMMENT=must_not_load", map[string]any{"SECOND": "kept"}},
				{"key", "FIRST=ok", map[string]any{"FIRST": "ok", "SECOND": "kept"}},
				{"blank", "", map[string]any{"SECOND": "kept"}},
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

// TestEnv_NilInterfaceDefKeyExists 验证键存在、目标为接口类型且默认值为 nil 时，
// Env 返回 nil 而非 panic（cast 对 nil reflect.Type 的守卫，修复 P1）。
// 绕过 LoadEnv 直接写 envData 是因为需要构造 LoadEnv 无法产生的 any 类型值
// （string 类型值存入 envData 后以 error 接口读取），以验证 cast 对 nil reflect.Type 的守卫。
func TestEnv_NilInterfaceDefKeyExists(t *testing.T) {
	resetForTest(t)
	envMu.Lock()
	envData["SOME_KEY"] = "some-value"
	envMu.Unlock()

	if v := Env[error]("SOME_KEY", nil); v != nil {
		t.Errorf("Env[error] 期望 nil，实际 %v", v)
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

// TestEnv_NamedBoolConversion 验证 .env 推断的数值与布尔值、OS 字符串均可转换为具名 bool。
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

// TestEnv_DurationUnits 验证原始或带引号的 30 搭配 5s 默认值仍为 30ns，显式 30s 才是秒。
func TestEnv_DurationUnits(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input string
		want  time.Duration
	}{
		{"raw_number", "30", 30 * time.Nanosecond},
		{"double_quoted_number", `"30"`, 30 * time.Nanosecond},
		{"single_quoted_number", "'30'", 30 * time.Nanosecond},
		{"explicit_seconds", "30s", 30 * time.Second},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resetForTest(t)
			const key = "ZCCONFIG_DURATION_UNITS"
			loadTestEnv(t, key+"="+tt.input+"\n")
			if got := Env(key, 5*time.Second); got != tt.want {
				t.Errorf("Env 值 %q 配默认 5s 期望 %v，实际 %v", tt.input, tt.want, got)
			}
		})
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
