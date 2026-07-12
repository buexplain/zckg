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

// ConfigAll 返回当前所有已注册的业务配置数据的副本。
func ConfigAll() map[string]any {
	configMu.RLock()
	defer configMu.RUnlock()
	return deepCopy(configData)
}

// deepCopy 深拷贝 map[string]any，避免外部修改影响内部数据。
func deepCopy(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		if m, ok := v.(map[string]any); ok {
			dst[k] = deepCopy(m)
		} else {
			dst[k] = v
		}
	}
	return dst
}

// Reset 清空所有已加载的 env 数据和已注册的业务配置数据。
// 主要用于测试场景下的状态重置。
func Reset() {
	envMu.Lock()
	envData = make(map[string]any)
	envMu.Unlock()

	configMu.Lock()
	configData = make(map[string]any)
	configMu.Unlock()
}
