package zcconfig

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func writeTestFile(t *testing.T, content string) string {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}
	return path
}

func TestLoadEnv_BasicTypes(t *testing.T) {
	reset()
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
	path := writeTestFile(t, content)
	if err := LoadEnv(path); err != nil {
		t.Fatalf("LoadEnv 失败: %v", err)
	}

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
	reset()
	longVal := strings.Repeat("a", 70*1024)
	path := writeTestFile(t, "LONG_KEY="+longVal+"\n")
	if err := LoadEnv(path); err != nil {
		t.Fatalf("70KB 单行 LoadEnv 失败: %v", err)
	}

	if v := Env("LONG_KEY", "fallback"); v != longVal {
		t.Errorf("期望长值完整读回（长度 %d），实际长度 %d", len(longVal), len(v))
	}
}

// export 前缀识别：大小写不敏感，兼容多个空格/Tab；无空白分隔的 "exportKEY=v" 不受影响。
func TestLoadEnv_ExportPrefixVariants(t *testing.T) {
	reset()
	content := "EXPORT KEY=v1\nexport  KEY2=v2\nexport\tKEY3=v3\nExPoRt   KEY4=v4\nexportKEY=v5\n"
	path := writeTestFile(t, content)
	if err := LoadEnv(path); err != nil {
		t.Fatalf("LoadEnv 失败: %v", err)
	}

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
	reset()

	path1 := writeTestFile(t, "KEY1=value1\nSHARED=from_file1")
	path2 := writeTestFile(t, "KEY2=value2\nSHARED=from_file2")

	if err := LoadEnv(path1); err != nil {
		t.Fatalf("LoadEnv path1 失败: %v", err)
	}
	if err := LoadEnv(path2); err != nil {
		t.Fatalf("LoadEnv path2 失败: %v", err)
	}

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
	reset()
	content := `
INT_VAL=42
FLOAT_VAL=3.14
BOOL_VAL=true
STR_VAL=hello
`
	path := writeTestFile(t, content)
	if err := LoadEnv(path); err != nil {
		t.Fatalf("LoadEnv 失败: %v", err)
	}

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
	reset()
	_ = os.Setenv("MY_TEST_OS_ENV", "os_value")
	defer func() {
		_ = os.Unsetenv("MY_TEST_OS_ENV")
	}()

	// .env 中没有此 key，回退到 OS 环境变量
	if v := Env("MY_TEST_OS_ENV", "default"); v != "os_value" {
		t.Errorf("期望 'os_value'，实际 %s", v)
	}
}

func TestConfig_BasicLookup(t *testing.T) {
	reset()
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
	reset()
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
	reset()
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
	reset()
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
	reset()
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
	reset()
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
	reset()
	content := "A=1\nB=hello\n"
	path := writeTestFile(t, content)
	if err := LoadEnv(path); err != nil {
		t.Fatalf("LoadEnv 失败: %v", err)
	}

	all := EnvAll()
	if len(all) != 2 {
		t.Errorf("期望 2 个 key，实际 %d", len(all))
	}
	if v, ok := all["A"]; !ok || v != 1 {
		t.Errorf("期望 A=1，实际 %v", v)
	}
}

func TestConfigAll(t *testing.T) {
	reset()
	Register("app", func() map[string]any {
		return map[string]any{
			"name": "test",
		}
	})

	all := ConfigAll()
	if v, ok := all["app"].(map[string]any); !ok {
		t.Errorf("期望 app 为 map[string]any")
	} else if v["name"] != "test" {
		t.Errorf("期望 name=test，实际 %v", v["name"])
	}
}

// .env 中 "1"/"0" 被 parseValue 推断为 int，经 cast 数值→bool 特判转为 bool
func TestEnv_BoolFromIntString(t *testing.T) {
	reset()
	content := "BOOL_ONE=1\nBOOL_ZERO=0\n"
	path := writeTestFile(t, content)
	if err := LoadEnv(path); err != nil {
		t.Fatalf("LoadEnv 失败: %v", err)
	}

	if v := Env("BOOL_ONE", false); v != true {
		t.Errorf(".env 中 BOOL_ONE=1 期望 true，实际 %v", v)
	}
	if v := Env("BOOL_ZERO", true); v != false {
		t.Errorf(".env 中 BOOL_ZERO=0 期望 false，实际 %v", v)
	}
}

// 并发读写压力测试：并发 Register + Config 不应产生数据竞争
func TestConcurrent_RegisterAndConfig(t *testing.T) {
	reset()
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
}

// 并发读写压力测试：并发 LoadEnv + Env + EnvAll 不应产生数据竞争
func TestConcurrent_LoadEnvAndEnv(t *testing.T) {
	reset()
	path := writeTestFile(t, "KEY=value\n")
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = LoadEnv(path)
			_ = Env("KEY", "")
			_ = EnvAll()
		}()
	}
	wg.Wait()
}

// ConfigAll 返回内部存储的直接引用（零拷贝）：写入返回值会穿透到内部
// 注意：此写操作仅在测试单 goroutine 下安全；生产代码中写返回值会产生
// 数据竞争，文档已警示仅限只读场景。
func TestConfigAll_ZeroCopy(t *testing.T) {
	reset()
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
	reset()
	Register("app", func() map[string]any {
		return map[string]any{"name": "test"}
	})
	path := writeTestFile(t, "RESET_ENV_KEY=reset_val\n")
	if err := LoadEnv(path); err != nil {
		t.Fatalf("LoadEnv 失败: %v", err)
	}

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
	reset()
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
	reset()
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
	reset()
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
	reset()
	content := `
PORT 8080
=value_without_key
   =also_skipped
VALID_KEY=value
`
	path := writeTestFile(t, content)
	if err := LoadEnv(path); err != nil {
		t.Fatalf("LoadEnv 失败: %v", err)
	}

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
	reset()
	path := writeTestFile(t, "\ufeffAPP_NAME=bom_app\nOTHER_KEY=v\n")
	if err := LoadEnv(path); err != nil {
		t.Fatalf("LoadEnv 失败: %v", err)
	}

	// BOM 剥离后首行 key 不带 \ufeff 前缀
	if v := Env("APP_NAME", "default"); v != "bom_app" {
		t.Errorf("期望 'bom_app'，实际 %q", v)
	}
	// 后续行不受影响
	if v := Env("OTHER_KEY", "default"); v != "v" {
		t.Errorf("期望 'v'，实际 %q", v)
	}
}
