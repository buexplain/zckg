package zchttp

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"sync"
)

// validateRequest 对已绑定的请求执行参数校验：nonzero 字段 + 自定义 Validator。
// meta 为注册阶段预计算的 structMeta，避免请求阶段重复反射解析。
// needsNonzero 为注册阶段预计算的传递性标记（见 hasNonzeroInTree）：
// Req 类型树任意位置存在 nonzero 字段才执行 validateNonzero，否则整体跳过避免无意义遍历。
func validateRequest(reqPtr reflect.Value, meta structMeta, needsNonzero bool) error {
	if needsNonzero {
		if err := validateNonzero(reqPtr, meta); err != nil {
			return err
		}
	}
	return validateCustom(reqPtr, meta)
}

// Validator 由 handler 的 Req 结构体可选实现，用于声明式 nonzero 之外的
// 业务校验与跨字段校验（如「两者至少填一个」「结束时间需晚于开始时间」等）。
// 绑定与 nonzero 校验通过后，若 Req 实现了该接口则调用其 Validate 方法。
type Validator interface {
	Validate() error
}

// validateCustom 调用 Req 的 Validate 方法（若实现 Validator），并将其错误归一化为
// *ValidationError，以便 HttpEngine 统一路由到 OnValidationError 回调（默认 400）。
// 仅校验顶层 Req，不递归进入嵌套结构体。
// meta 为注册阶段预计算的 structMeta，直接使用其 implementsValidator 判断。
func validateCustom(reqPtr reflect.Value, meta structMeta) error {
	if !meta.implementsValidator {
		return nil
	}
	v, ok := reqPtr.Interface().(Validator)
	if !ok {
		return nil
	}
	err := v.Validate()
	if err == nil {
		return nil
	}
	// 用户已返回结构化校验错误则透传，否则包装以保留原始错误链
	var ve *ValidationError
	if errors.As(err, &ve) {
		return err
	}
	return &ValidationError{Message: err.Error(), Err: err}
}

// validateNonzero 在参数绑定完成后，校验 nonzero 字段不得为零值。
// 递归进入嵌套结构体字段（含 *struct 指针穿透），使用 visited 防止循环引用。
//
// 校验规则：
//   - 只要字段标注 nonzero:"true"，就校验零值，所见即所得；
//   - 未标注 nonzero 的字段一律不做零值校验。
//
// 零值判定使用 reflect.Value.IsZero：nil 指针/切片、空字符串、数字 0、bool false 等均视为零值。
// meta 为注册阶段预计算的 structMeta，直接遍历其 fields 避免请求阶段反射。
func validateNonzero(reqPtr reflect.Value, meta structMeta) error {
	elem := reqPtr.Elem()
	if elem.Kind() != reflect.Struct {
		return nil
	}
	visited := acquireVisitMap()
	err := validateNonzeroWalk(elem, meta, visited, "", false)
	releaseVisitMap(visited)
	return err
}

// hasNonzeroInTree 扫描 Req 类型树，判定任意深度是否存在标记 nonzero:"true" 的字段
// （传递性标记）。注册阶段预计算并存入 routeEntry.needsNonzeroValidation。
//
// 该标记必须是传递性的：顶层无 nonzero 但嵌套层有时仍需校验，若仅统计顶层字段
// 做跳过决策会导致嵌套层 nonzero 漏校验。
// 穿透范围与 validateNonzeroWalk 一致（指针解引用、单层容器元素/值）；对多层容器
// 同样下探属于保守方向（最坏只是不跳过，不会漏检）。
// visiting 用于检测自引用循环，防止无限递归。
func hasNonzeroInTree(t reflect.Type, visiting map[reflect.Type]bool) bool {
	t = derefType(t)
	// 防环检查必须先于 Slice/Map 穿透：自引用容器类型（type S []S / type M map[string]M）
	// 的穿透 t.Elem() 恒返回自身，若不在此登记将永远无法到达 struct 分支的防环
	// （修复 REC-03，与 hasRequestPhaseDefaults 的 REC-02 修复同构同步）
	if visiting == nil {
		visiting = make(map[reflect.Type]bool)
	}
	if visiting[t] {
		return false // 自引用循环，停止递归
	}
	visiting[t] = true
	defer delete(visiting, t)

	switch t.Kind() {
	case reflect.Slice, reflect.Array:
		return hasNonzeroInTree(t.Elem(), visiting)
	case reflect.Map:
		return hasNonzeroInTree(t.Elem(), visiting)
	default:
	}
	if t.Kind() != reflect.Struct {
		return false
	}

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		if f.Anonymous && f.Type == metaType {
			continue
		}
		// 解析规则与 buildStructMeta 对齐：ParseBool 为 true 才视为标记
		if v, ok := f.Tag.Lookup("nonzero"); ok {
			if b, err := strconv.ParseBool(v); err == nil && b {
				return true
			}
		}
		if hasNonzeroInTree(f.Type, visiting) {
			return true
		}
	}
	return false
}

// visitKey 用于 visited map 的复合键，同时记录地址和类型，
// 避免值类型首字段与父结构体共享地址时被误判为循环引用。
type visitKey struct {
	ptr uintptr
	typ reflect.Type
}

// visitMapPool 复用 nonzero 校验与默认值填充的防环 visited map，减少每请求分配；
// 归还前用 clear 清空，超大 map 不入池避免池内存膨胀
var visitMapPool = sync.Pool{
	New: func() any { return make(map[visitKey]bool) },
}

// maxPooledVisitMapSize 是允许归还池的 visited map 大小上限
const maxPooledVisitMapSize = 1024

// acquireVisitMap 从池中获取防环标记 map
func acquireVisitMap() map[visitKey]bool {
	return visitMapPool.Get().(map[visitKey]bool)
}

// releaseVisitMap 清空并归还防环标记 map；超出大小上限的不归还，交由 GC 回收
func releaseVisitMap(m map[visitKey]bool) {
	if len(m) > maxPooledVisitMapSize {
		return
	}
	clear(m)
	visitMapPool.Put(m)
}

// validateNonzeroWalk 递归遍历结构体树，校验每个结构体的 nonzero 字段。
// 规则：
//   - 若字段 nonzero 且零值 → 报错
//   - 若字段 nonzero 且非零 → 校验通过，若为嵌套结构体/指针/结构体切片/结构体数组/map 则递归进入
//   - 若字段非 nonzero 但为嵌套结构体/指针/结构体切片/结构体数组/map 且非零值 → 不报本级，但递归进入子字段校验
//
// prefix 为嵌套路径前缀（如 "company."），顶层调用时传空字符串。
// 报错时 Field 为 prefix + 字段绑定名，如 "company.name"，与 API 命名一致，便于客户端定位。
// isTempCopy 表示 v 是临时副本（如 map 值的可寻址拷贝）：副本地址不代表原始数据，
// 且临时对象被 GC 回收后地址可能被后续分配复用，若计入 visited 会误判"已访问"
// 导致漏校验，因此不将副本自身地址注册进 visited（环检测由 map 桶指针键承担）。
func validateNonzeroWalk(v reflect.Value, meta structMeta, visited map[visitKey]bool, prefix string, isTempCopy bool) error {
	if v.Kind() != reflect.Struct {
		return nil
	}
	if visited == nil {
		visited = make(map[visitKey]bool)
	}
	if !isTempCopy {
		key := visitKey{ptr: v.Addr().Pointer(), typ: v.Type()}
		if visited[key] {
			return nil // 已访问，防止循环递归
		}
		visited[key] = true
		// 有意不回溯删除 visited[key]：这是防环设计的一部分。
		// 同一指针经不同路径第二次出现时直接跳过，理论上存在漏校验，
		// 但 JSON 反序列化不会产生共享指针（每个嵌套对象都是独立实例），故实际不可达。
	}

	for i := range meta.fields {
		fm := &meta.fields[i]
		fv := fieldByIndex(v, fm.indices)

		if fm.nonzero {
			// nonzero 字段：零值则报错
			if fv.IsZero() {
				return &ValidationError{Field: prefix + fm.name, Message: "is required"}
			}
		}

		// 若为非零值的嵌套结构体/指针字段，递归进入子字段校验
		// （nonzero 字段已校验通过；非 nonzero 字段只要非零值就递归）
		if fv.IsZero() {
			continue
		}
		subV := fv
		wasPtr := subV.Kind() == reflect.Ptr
		if wasPtr {
			subV = subV.Elem()
		}
		if subV.Kind() == reflect.Struct {
			subMeta := cachedStructMeta(subV.Type())
			// 指针解引用后的目标是真实堆对象（地址稳定）；
			// 值类型字段与所属 struct 同属一块内存，继承临时副本标记
			if err := validateNonzeroWalk(subV, subMeta, visited, prefix+fm.name+".", isTempCopy && !wasPtr); err != nil {
				return err
			}
		} else if subV.Kind() == reflect.Slice {
			// 结构体切片/结构体指针切片：递归校验每个元素的 nonzero 字段
			elemType := subV.Type().Elem()
			isPtrElem := elemType.Kind() == reflect.Ptr
			if isPtrElem {
				elemType = elemType.Elem()
			}
			if elemType.Kind() == reflect.Struct {
				subMeta := cachedStructMeta(elemType)
				for i := 0; i < subV.Len(); i++ {
					elem := subV.Index(i)
					if isPtrElem {
						if elem.IsNil() {
							continue
						}
						elem = elem.Elem()
					}
					// 切片元素位于共享的底层数组，地址稳定，非临时副本
					if err := validateNonzeroWalk(elem, subMeta, visited, prefix+fm.name+".", false); err != nil {
						return err
					}
				}
			}
		} else if subV.Kind() == reflect.Array {
			// 结构体数组/结构体指针数组：与切片相同处理（P2-02 四函数对齐联动，
			// 数组元素的 default 填充已支持，nonzero 校验必须同步对齐）
			elemType := subV.Type().Elem()
			isPtrElem := elemType.Kind() == reflect.Ptr
			if isPtrElem {
				elemType = elemType.Elem()
			}
			if elemType.Kind() == reflect.Struct {
				subMeta := cachedStructMeta(elemType)
				for i := 0; i < subV.Len(); i++ {
					elem := subV.Index(i)
					if isPtrElem {
						if elem.IsNil() {
							continue
						}
						elem = elem.Elem()
					}
					// 数组元素内嵌于字段值（或其指针目标），地址稳定，非临时副本
					if err := validateNonzeroWalk(elem, subMeta, visited, prefix+fm.name+".", false); err != nil {
						return err
					}
				}
			}
		} else if subV.Kind() == reflect.Map {
			// map[string]Struct / map[string]*Struct：递归校验每个 value 的 nonzero 字段
			valType := subV.Type().Elem()
			isPtrVal := valType.Kind() == reflect.Ptr
			if isPtrVal {
				valType = valType.Elem()
			}
			if valType.Kind() == reflect.Struct {
				// 对 map 自身（底层桶指针）做循环检测：
				// 非指针值类型的 struct 副本仍共享 map 底层桶，可形成环，
				// 仅靠副本地址或指针值地址无法覆盖此场景
				mapKey := visitKey{ptr: subV.Pointer(), typ: subV.Type()}
				if visited[mapKey] {
					continue // 该 map 已在递归路径中处理过，跳过防止循环递归
				}
				visited[mapKey] = true
				subMeta := cachedStructMeta(valType)
				for _, key := range subV.MapKeys() {
					val := subV.MapIndex(key)
					if isPtrVal {
						if val.IsNil() {
							continue
						}
						// 对指针类型的 map 值，使用原始指针地址做循环检测
						ptrKey := visitKey{ptr: val.Pointer(), typ: valType}
						if visited[ptrKey] {
							continue // 已访问，跳过防止循环递归
						}
						visited[ptrKey] = true
						val = val.Elem()
					}
					// MapIndex 返回的值不可寻址，需复制一份再传入校验；
					// 副本为临时对象，其地址不可靠，标记 isTempCopy=true
					valCopy := reflect.New(valType).Elem()
					valCopy.Set(val)
					// 错误路径拼接 map key 的字符串化，便于定位具体哪个 value 校验失败
					// （如 children.a.name），ValidationError.Field 语义不变
					childPrefix := prefix + fm.name + "." + fmt.Sprintf("%v", key) + "."
					if err := validateNonzeroWalk(valCopy, subMeta, visited, childPrefix, true); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}
