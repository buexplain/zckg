package zcconfig

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

var (
	envMu   sync.RWMutex
	envData = make(map[string]any)
)

// LoadEnv 读取 .env 文件，将键值对解析后存入 env 存储。
// 值会自动推断类型：整数解析为 int，浮点数解析为 float64，布尔值解析为 bool，其余为 string。
// 支持的行格式：
//   - KEY=value
//   - export KEY=value
//   - # 注释行（以 # 开头）
//   - 空行（跳过）
//
// 值两端的引号（单引号或双引号）会被去除。
// 无 "=" 的行与空 key 行（=value）会被静默跳过，不影响其他行的解析。
// 首行的 UTF-8 BOM（\uFEFF）会被自动剥离，兼容 Windows 记事本等带 BOM 保存的文件。
// 多次调用 LoadEnv 会合并数据，后加载的同名 key 会覆盖先前的值。
//
// 先解析到局部 map，整个文件解析成功后才加锁合并到全局存储，
// 因此加载失败（文件不存在、单行超过 1MB 等）不会修改已有数据。
// 打开/读取失败的错误信息均包含文件路径，便于多文件加载场景定位问题。
func LoadEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("打开 .env 文件失败: %s: %w", path, err)
	}
	defer func(f *os.File) {
		_ = f.Close()
	}(f)

	parsed := make(map[string]any)
	scanner := bufio.NewScanner(f)
	// 调大缓冲区（默认单行上限 64KB）：内联证书、长密钥等超长配置值很常见，
	// 超限会触发 bufio.ErrTooLong 导致整个文件加载失败。上限 1MB。
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	firstLine := true
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// 剥离首行的 UTF-8 BOM：Windows 记事本默认带 BOM 保存，
		// 不剥离会导致首行 key 带上 \ufeff 前缀而静默查不到
		if firstLine {
			line = strings.TrimPrefix(line, "\ufeff")
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
		value = unquote(value)
		parsed[key] = parseValue(value)
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

// parseValue 尝试将字符串值推断为具体类型。
// 依次尝试 int、float64、bool，均失败时返回原始字符串。
func parseValue(value string) any {
	// 尝试解析为 int：按平台 int 位宽（strconv.IntSize）解析，
	// 32 位平台上超出 int 范围的值解析失败、保持 string，避免静默截断。
	if n, err := strconv.ParseInt(value, 10, strconv.IntSize); err == nil {
		return int(n)
	}
	// 仅当值包含小数点或指数标记时才尝试 float64，
	// 避免超大整数静默降级为 float64 丢失精度
	if strings.ContainsAny(value, ".eE") {
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return f
		}
	}
	// 尝试解析为 bool
	if b, err := strconv.ParseBool(value); err == nil {
		return b
	}
	return value
}

// Env 从 env 存储中按 key 查找值，转换为 T 类型返回。
// 若 key 不存在或类型转换失败，返回默认值 def。
// 查找顺序：先查 LoadEnv 加载的数据，再回退到操作系统环境变量。
func Env[T any](key string, def T) T {
	envMu.RLock()
	v, ok := envData[key]
	envMu.RUnlock()
	if ok {
		return cast(v, def)
	}

	// 回退到操作系统环境变量
	if s, ok := os.LookupEnv(key); ok {
		return cast(s, def)
	}

	return def
}

// EnvAll 返回内部 env 存储的直接引用（零拷贝，不做任何拷贝）。
// 包含 LoadEnv 加载的数据，不含操作系统环境变量。
//
// 警示：
//  1. 返回值与内部存储共享同一份数据，任何写操作都会污染全局配置；
//  2. 写操作与并发读取（Env/LoadEnv）会产生数据竞争，可能导致程序崩溃；
//  3. 仅限只读场景（调试输出、配置导出等）。
func EnvAll() map[string]any {
	envMu.RLock()
	defer envMu.RUnlock()
	return envData
}
