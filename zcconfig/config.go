package zcconfig

import (
	"strings"
	"sync"
)

var (
	configMu   sync.RWMutex
	configData = make(map[string]any)
)

// Register 注册业务配置数据。
// key 为配置路径，支持用 "." 分隔的层级路径，如 "app.database"。
// fn 在注册时立即执行，返回的数据会按 key 路径深度合并到配置树中。
// key 为空字符串 "" 时，返回值直接合并到配置树根节点。
// 多次调用 Register 会合并数据，后注册的同名 key 会覆盖先前的值。
//
// 约定与行为：
//   - fn 返回的 map 及其嵌套结构在 Register 之后不得再被修改；
//     zcconfig 不拷贝 fn 的返回值（直接存储引用），外部修改会污染全局配置。
//   - 若 key 路径的中间层级已存在标量值（如 "app.name" 已注册为 string），
//     后续在该路径下注册子节点（如 "app.name.sub"）会覆盖原标量值为 map。
//   - Register 仅允许在启动阶段调用，运行期配置只读。
func Register(key string, fn func() map[string]any) {
	data := fn()
	if data == nil {
		return
	}

	configMu.Lock()
	defer configMu.Unlock()

	if key == "" {
		mergeMap(configData, data)
		return
	}

	parts := strings.Split(key, ".")
	target := ensureMapAt(configData, parts)
	mergeMap(target, data)
}

// ensureMapAt 在 dst 中沿 parts 路径导航，缺少的中间层级自动创建为 map[string]any。
// 返回路径终点的 map[string]any 引用，用于后续合并数据。
func ensureMapAt(dst map[string]any, parts []string) map[string]any {
	current := dst
	for _, part := range parts {
		next, ok := current[part].(map[string]any)
		if !ok {
			next = make(map[string]any)
			current[part] = next
		}
		current = next
	}
	return current
}

// mergeMap 将 src 深度合并到 dst 中。
// 当 src 和 dst 的同名 key 都为 map[string]any 时递归合并，否则 src 的值覆盖 dst。
func mergeMap(dst, src map[string]any) {
	for k, v := range src {
		if srcMap, ok := v.(map[string]any); ok {
			if dstMap, ok := dst[k].(map[string]any); ok {
				mergeMap(dstMap, srcMap)
				continue
			}
		}
		dst[k] = v
	}
}

// Config 从业务配置存储中按 key 查找值，转换为 T 类型返回。
// key 支持用 "." 做分隔符，按层级递归查找嵌套的 map[string]any{}。
// 例如 key 为 "app.database.host" 时，依次查找 configData["app"]["database"]["host"]。
// 若任意层级的 key 不存在或路径中途值非 map 类型，返回默认值 def。
func Config[T any](key string, def T) T {
	configMu.RLock()
	defer configMu.RUnlock()

	current := configData
	parts := strings.Split(key, ".")
	for i, part := range parts {
		v, ok := current[part]
		if !ok {
			return def
		}
		// 最后一个 key，直接取值并做类型转换
		if i == len(parts)-1 {
			return cast(v, def)
		}
		// 中间层级必须是 map[string]any，否则路径无效
		next, ok := v.(map[string]any)
		if !ok {
			return def
		}
		current = next
	}
	return def
}

// ConfigAll 返回内部配置存储的直接引用（零拷贝，不做任何拷贝）。
//
// 警示：
//  1. 返回值与内部配置共享同一份数据，任何写操作都会污染全局配置；
//  2. 写操作与并发读取（Config/Register）会产生数据竞争，可能导致程序崩溃；
//  3. 仅限只读场景（调试输出、配置导出等）。
//
// 架构约定：Register 仅允许在启动阶段调用，运行期配置只读。
func ConfigAll() map[string]any {
	configMu.RLock()
	defer configMu.RUnlock()
	return configData
}

// reset 清空所有已加载的 env 数据和已注册的业务配置数据。
// 主要用于测试场景下的状态重置。
func reset() {
	envMu.Lock()
	envData = make(map[string]any)
	envMu.Unlock()

	configMu.Lock()
	configData = make(map[string]any)
	configMu.Unlock()
}
