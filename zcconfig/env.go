package zcconfig

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
)

var (
	envMu   sync.RWMutex
	envData = make(map[string]string)
)

// LoadEnv 读取 .env 文件，将键值对以文本形式存入 env 存储。
// 装载阶段不推断类型：值的类型由读取方（Env[T]）决定，Env 在读取时把文本解析为 T。
// 支持的行格式：
//   - KEY=value
//   - export KEY=value
//   - # 注释行（以 # 开头）
//   - 空行（跳过）
//
// 值两端的空白会被去除，成对的单引号或双引号会被去除；引号只影响文本内容，
// 不代表“按字符串存储”：KEY="123" 读取为 string 时得到 "123"，读取为 int 时得到 123。
// 空值（KEY=）会存为空字符串：键仍然存在，Env[string] 返回 ""，读取非 string 类型时按解析失败处理。
// 无 "=" 的行与空 key 行（=value）会被静默跳过，不影响其他行的解析。
// 首行的 UTF-8 BOM（\uFEFF）会被自动剥离，兼容 Windows 记事本等带 BOM 保存的文件。
// 多次调用 LoadEnv 会合并数据，后加载的同名 key 会覆盖先前的值。
//
// 先解析到局部 map，整个文件解析成功后才加锁合并到全局存储，
// 因此加载失败（文件不存在、单行 token 达到 1MB 上限等）不会修改已有数据。
// 打开/读取失败的错误信息均包含文件路径，便于多文件加载场景定位问题。
func LoadEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("打开 .env 文件失败: %s: %w", path, err)
	}
	defer func(f *os.File) {
		_ = f.Close()
	}(f)

	parsed := make(map[string]string)
	scanner := bufio.NewScanner(f)
	// 调大缓冲区（默认单行上限 64KB）：内联证书、长密钥等超长配置值很常见，
	// 超限会触发 bufio.ErrTooLong 导致整个文件加载失败。上限 1MB 是 token 长度上限，
	// 实际可加载的最大单行为 1MB-1 字节（与文件是否以换行符结尾无关）。
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	firstLine := true
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// 剥离首行的 UTF-8 BOM：Windows 记事本默认带 BOM 保存，
		// 剥离后再次清理空白，确保 BOM 后缩进不影响 export 和注释识别。
		if firstLine {
			line = strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
			firstLine = false
		}
		// 跳过空行和注释行
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// 去除可选的 "export " 前缀：不区分大小写，兼容多个空格/Tab。
		// 仅当 export 后跟空白字符才视为前缀，"exportKEY=v" 这类普通 key 不受影响。
		if len(line) > 6 && strings.EqualFold(line[:6], "export") && (line[6] == ' ' || line[6] == '\t') {
			line = strings.TrimLeft(line[6:], " \t")
		}

		idx := strings.Index(line, "=")
		if idx < 0 {
			// 无 "=" 的行（如拼写错误的 "PORT 8080"）静默跳过，宽松解析
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		if key == "" {
			// 空 key 行（=value）同样静默跳过
			continue
		}
		parsed[key] = unquote(value)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("读取 .env 文件失败: %s: %w", path, err)
	}

	envMu.Lock()
	for k, v := range parsed {
		envData[k] = v
	}
	envMu.Unlock()

	return nil
}

// unquote 去除值两端的单引号或双引号。
// 若引号不配对则原样返回。
func unquote(value string) string {
	if len(value) >= 2 {
		first := value[0]
		last := value[len(value)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

// Env 从 env 存储中按 key 查找值，转换为 T 类型返回。
// 查找顺序：先查 LoadEnv 加载的数据，再回退到操作系统环境变量。
// 两条通道的存储值都是文本，因此同一 key、同一字面量在两条通道下的转换结果与失败边界完全一致。
//
// 行为约定：
//   - key 不存在（.env 与 OS 环境变量都没有该 key）时返回默认值 def；
//   - key 存在但文本无法转换为 T 时 panic(*TypeMismatchError)，消息携带 key、期望类型与实际类型（Actual 恒为 string）；
//   - 目标为 string / 具名 string 时恒成功，返回 .env 原文（如 "007"、"1e3"、"1.50"）。
func Env[T any](key string, def T) T {
	envMu.RLock()
	v, ok := envData[key]
	envMu.RUnlock()
	if ok {
		return castOrPanic(key, v, def)
	}

	// 回退到操作系统环境变量
	if s, ok := os.LookupEnv(key); ok {
		return castOrPanic(key, s, def)
	}

	return def
}

// EnvAll 返回内部 env 存储的直接引用（零拷贝，不做任何拷贝）。
// 存储值为 .env 文件中的原文（已去引号），不含操作系统环境变量，也不含任何类型推断结果。
//
// 警示：
//  1. 返回值与内部存储共享同一份数据，任何写操作都会污染全局配置；
//  2. 写操作与并发读取（Env/LoadEnv）会产生数据竞争，可能导致程序崩溃；
//  3. 仅限只读场景（调试输出、配置导出等）。
func EnvAll() map[string]string {
	envMu.RLock()
	defer envMu.RUnlock()
	return envData
}
