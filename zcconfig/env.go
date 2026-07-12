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
// 多次调用 LoadEnv 会合并数据，后加载的同名 key 会覆盖先前的值。
func LoadEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("打开 .env 文件失败: %w", err)
	}
	defer func(f *os.File) {
		_ = f.Close()
	}(f)

	parsed := make(map[string]any)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// 跳过空行和注释行
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// 去除可选的 "export " 前缀
		line = strings.TrimPrefix(line, "export ")
		line = strings.TrimPrefix(line, "export\t")

		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		if key == "" {
			continue
		}
		value = unquote(value)
		parsed[key] = parseValue(value)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("读取 .env 文件失败: %w", err)
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
	// 尝试解析为 int
	if n, err := strconv.ParseInt(value, 10, 64); err == nil {
		return int(n)
	}
	// 尝试解析为 float64
	if f, err := strconv.ParseFloat(value, 64); err == nil {
		return f
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

// EnvAll 返回当前所有已加载的 env 数据的副本。
// 包含 LoadEnv 加载的数据，不含操作系统环境变量。
func EnvAll() map[string]any {
	envMu.RLock()
	defer envMu.RUnlock()
	result := make(map[string]any, len(envData))
	for k, v := range envData {
		result[k] = v
	}
	return result
}
