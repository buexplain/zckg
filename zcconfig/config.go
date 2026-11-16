package zcconfig

import (
	"reflect"
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
//   - nil 目标 map 视为待初始化节点，沿路径注册时分配，合并时由源 map 覆盖；
//     nil 源 map 不清空已有的非 nil 目标 map，fn 返回 nil 时不做任何操作。
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

// ensureMapAt 在 dst 中沿 parts 路径导航，缺失、非 map 或 nil map 节点视为待初始化节点，分配 map[string]any。
// 返回路径终点的 map[string]any 引用，用于后续合并数据。
func ensureMapAt(dst map[string]any, parts []string) map[string]any {
	current := dst
	for _, part := range parts {
		next, ok := current[part].(map[string]any)
		if !ok || next == nil {
			next = make(map[string]any)
			current[part] = next
		}
		current = next
	}
	return current
}

// mergeMap 将 src 深度合并到非 nil 的 dst 中。
// 当同名 key 均为 map[string]any 且目标 map 非 nil 时递归合并，nil 源 map 不清空已有数据。
// nil 目标 map 视为待初始化节点，由源值覆盖；其他不满足递归条件的值也由源值覆盖。
func mergeMap(dst, src map[string]any) {
	for k, v := range src {
		if srcMap, ok := v.(map[string]any); ok {
			if dstMap, ok := dst[k].(map[string]any); ok && dstMap != nil {
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
// "." 一律作为分隔符、无法转义：注册在 map 里、字面含 "." 的 key 无法按路径读到；
// 空路径段（如 "app."）按空字符串键查找。
//
// 行为约定：
//   - key 不存在（任一层级缺少节点）时返回默认值 def；
//   - key 存在但值无法转换为 T 时 panic(*TypeMismatchError)，消息携带 key、期望类型与实际类型；
//   - 路径中间层的值存在但不是 map[string]any 时同样 panic(*TypeMismatchError)，
//     期望类型记为 map[string]any，key 为调用方传入的完整 key；
//   - 存储值为 nil 视为未配置，返回默认值 def，不 panic。
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
		// 最后一个 key，直接取值并做类型转换；类型不符时 panic
		if i == len(parts)-1 {
			return castOrPanic(key, v, def)
		}
		// 中间层级必须是 map[string]any，否则属于配置结构错误；nil 视为未配置
		next, ok := v.(map[string]any)
		if !ok {
			if v == nil {
				return def
			}
			panic(&TypeMismatchError{
				Key:      key,
				Expected: reflect.TypeFor[map[string]any](),
				Actual:   reflect.TypeOf(v),
			})
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
	envData = make(map[string]string)
	envMu.Unlock()

	configMu.Lock()
	configData = make(map[string]any)
	configMu.Unlock()
}
