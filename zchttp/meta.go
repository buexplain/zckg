package zchttp

import (
	"log/slog"
	"reflect"
	"strconv"
	"sync"
	"time"
)

// OpenAPIMeta 可嵌入到 handler 的 Req 结构体中，用于声明 OpenAPI 操作级元信息。
// 通过结构体标签设置（各自独立的 tag）：
//   - tags:        以 "/" 分隔的标签，如 "User Management/Account"
//   - summary:     操作摘要
//   - description: 操作详细描述
//
// 示例：
//
//	type CreateUserReq struct {
//	    zchttp.OpenAPIMeta `tags:"User Management/Account" summary:"创建用户"`
//	    Name string `json:"name" nonzero:"true"`
//	}
type OpenAPIMeta struct{}

// metaType 用于识别嵌入的 OpenAPIMeta 字段
var metaType = reflect.TypeOf(OpenAPIMeta{})

// validatorType 是 Validator 接口的 reflect.Type，用于快速判断结构体是否实现了校验接口
var validatorType = reflect.TypeOf((*Validator)(nil)).Elem()

// fieldMeta 缓存单个结构体字段的绑定元信息，包括字段名、类型特征、标签值等
type fieldMeta struct {
	name         string              // 字段绑定名（form/json 标签或字段名）
	indices      []int               // 从根结构体出发的字段索引路径，如 []int{0}；嵌入字段展开后可能为多级如 []int{0,1}
	field        reflect.StructField // 实际字段的反射信息（类型、标签等），供 OpenAPI 生成使用
	nonzero      bool                // 是否不允许零值（nonzero:"true"）
	hasDefault   bool                // 是否声明了 default 标签
	defaultVal   string              // default 标签的值
	isSlice      bool                // 是否为非文件切片（用于默认值逗号分隔展开）
	isFile       bool                // 是否为 *multipart.FileHeader
	isFileSlice  bool                // 是否为 []*multipart.FileHeader
	timeFormat   string              // time_format 标签值
	timeLocation *time.Location      // time_location 标签解析后的时区
}

// structMeta 缓存单个结构体类型的聚合元信息
type structMeta struct {
	fields              []fieldMeta // 所有可绑定字段的元信息列表
	implementsValidator bool        // 结构体是否实现 Validator 接口
}

// operationMeta 从 Req 结构体的 OpenAPIMeta 嵌入字段解析出的操作级元信息
type operationMeta struct {
	tags        []string
	summary     string
	description string
}

// maxPtrDerefDepth 指针/元素解引用的最大层数，
// 防止自引用命名类型（如 type P *P、type S []S）导致死循环或无限递归
const maxPtrDerefDepth = 32

// derefType 解引用指针类型直到得到非指针类型；
// 带深度上限，防自引用指针类型（如 type P *P）死循环，超限时原样返回
func derefType(t reflect.Type) reflect.Type {
	for i := 0; t.Kind() == reflect.Ptr && i < maxPtrDerefDepth; i++ {
		t = t.Elem()
	}
	return t
}

// structMetaCache 全局缓存已构建的 structMeta（reflect.Type -> structMeta）。
// 请求阶段的嵌套遍历（nonzero 校验 / 请求阶段默认值填充）会遇到注册期未预计算的
// 嵌套类型，若每次请求都重新执行 buildStructMeta 会产生大量重复反射与标签解析；
// structMeta 构建后只读、不可变，可安全跨 goroutine 共享。
var structMetaCache sync.Map

// cachedStructMeta 返回类型 t 的 structMeta，优先命中缓存，未命中时构建并写入缓存。
// 请求阶段的嵌套类型元信息获取必须走本函数，避免每请求重复构建。
// 写入用 LoadOrStore：并发首次构建同一类型时所有 goroutine 返回同一份缓存实例
// （structMeta 为值类型、构建后只读，可安全共享；构建结果确定性相同，
// 竞态窗口内的重复构建仅浪费少量反射开销，无正确性问题）。
func cachedStructMeta(t reflect.Type) structMeta {
	if v, ok := structMetaCache.Load(t); ok {
		return v.(structMeta)
	}
	m := buildStructMeta(t)
	if actual, loaded := structMetaCache.LoadOrStore(t, m); loaded {
		// 并发 goroutine 已抢先写入，统一返回缓存中的实例
		m = actual.(structMeta)
	}
	return m
}

// buildStructMeta 通过反射遍历结构体字段，预计算 structMeta 与 fieldMeta 列表。
// 注册阶段一次性完成，请求阶段直接复用 meta 避免重复反射。
// 预计算内容包括：字段名解析、文件字段判定、nonzero/hasDefault 判定、时间格式预解析。
// 自引用嵌入（如 type E1 struct{ *E1 }）经 visiting 防环：首次命中环时展开为空 meta，
// 结果与入口路径无关（确定性），可安全缓存（修复 REC-01）。
func buildStructMeta(t reflect.Type) structMeta {
	return buildStructMetaWithVisiting(t, map[reflect.Type]bool{})
}

// buildStructMetaWithVisiting 是 buildStructMeta 的内部实现，携带 visiting（嵌入展开路径上的类型集）
// 检测自引用嵌入环。环上的嵌入字段被跳过并告警（不阻断注册），
// 确保合法但病态的自引用嵌入类型不会导致栈溢出。
func buildStructMetaWithVisiting(t reflect.Type, visiting map[reflect.Type]bool) structMeta {
	meta := structMeta{}
	if t.Kind() != reflect.Struct {
		return meta
	}
	if visiting[t] {
		// 自引用嵌入环：该类型的展开已由路径上更早的帧负责，此处返回空 meta 断开环
		return meta
	}
	visiting[t] = true
	defer delete(visiting, t)

	// 判断 *T 是否实现 Validator（值接收者和指针接收者均可）
	meta.implementsValidator = reflect.PointerTo(t).Implements(validatorType)

	// 预分配切片容量，避免大结构体反复扩容
	meta.fields = make([]fieldMeta, 0, t.NumField())

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		// 跳过未导出字段（但匿名嵌入的 struct 类型除外，因为 encoding/json 会展开其导出字段）。
		// 例外中的例外：未导出 + 匿名 + 指向 struct 的指针（如 type Req struct{ *base }）——
		// 展开后 fieldByIndex 对 nil 嵌入指针执行 v.Set(reflect.New(...)) 时，该字段由未导出字段
		// 获得，reflect 拒绝写入（encoding/json 对 nil 未导出嵌入指针同样报错而非展开），会 panic。
		// 故跳过展开并告警，其内部字段不进入可绑定集合。未导出「值」嵌入（type Req struct{ base }）
		// 不受影响：值字段已在内存中、fieldByIndex 不触发 Set，内部导出字段可正常绑定/填默认值/校验。
		if f.PkgPath != "" {
			if !(f.Anonymous && isStructLike(f.Type)) {
				continue
			}
			if f.Type.Kind() == reflect.Ptr {
				slog.Warn("unexported embedded pointer struct is not supported for binding, its fields are skipped; use an exported type name or value embedding",
					"struct", t.Name(),
					"field", f.Name,
					"embedded", f.Type.String(),
				)
				continue
			}
		}
		// 跳过内嵌的 OpenAPIMeta
		if f.Anonymous && f.Type == metaType {
			continue
		}

		// 匿名嵌入的 struct/指针指向 struct：对齐 encoding/json 扁平语义，递归展开其导出字段
		if f.Anonymous && isStructLike(f.Type) {
			// 复用带上限的 derefType，防自引用指针类型（修复 REC-07）
			embeddedType := derefType(f.Type)
			if embeddedType.Kind() == reflect.Struct {
				if visiting[embeddedType] {
					slog.Warn("recursive embedded struct detected, embedding skipped",
						"struct", t.Name(),
						"field", f.Name,
						"embedded", embeddedType.String(),
					)
					continue
				}
				subMeta := buildStructMetaWithVisiting(embeddedType, visiting)
				for _, subFm := range subMeta.fields {
					if subFm.name == "" || subFm.name == "-" {
						continue
					}
					// 多级索引：父字段索引 + 子字段索引
					flattened := make([]int, 0, 1+len(subFm.indices))
					flattened = append(flattened, i)
					flattened = append(flattened, subFm.indices...)
					subFm.indices = flattened
					meta.fields = append(meta.fields, subFm)
				}
				continue
			}
		}

		fm := fieldMeta{indices: []int{i}, field: f}
		fm.name = resolveFieldName(f)
		if fm.name == "" || fm.name == "-" {
			// 仍然记录字段，bindValues 中根据 name 跳过
			meta.fields = append(meta.fields, fm)
			continue
		}

		// 文件字段判定
		switch {
		case f.Type == fileHeaderPtrType:
			fm.isFile = true
		case f.Type.Kind() == reflect.Slice && f.Type.Elem() == fileHeaderPtrType:
			fm.isFileSlice = true
		case f.Type.Kind() == reflect.Slice:
			fm.isSlice = true
		}

		// default 与 nonzero 标签独立解析，两者不互斥。
		// hasDefault 同时驱动运行时默认值填充（applyDefaults）与 OpenAPI required 判定；
		// nonzero 校验不受 default 影响。
		if _, ok := f.Tag.Lookup("default"); ok && isDefaultSupported(f.Type) {
			fm.hasDefault = true
			fm.defaultVal = f.Tag.Get("default")
		}
		if v, ok := f.Tag.Lookup("nonzero"); ok {
			if b, err := strconv.ParseBool(v); err == nil && b {
				fm.nonzero = true
			}
		}

		// 时间相关标签（仅对 time.Time 类型解析）
		if f.Type == timeType {
			fm.timeFormat = f.Tag.Get("time_format")
			if locTag := f.Tag.Get("time_location"); locTag != "" {
				if loc, err := time.LoadLocation(locTag); err == nil {
					fm.timeLocation = loc
				} else {
					slog.Warn("invalid time_location tag, falling back to time.Local",
						"tag_value", locTag,
						"struct", t.Name(),
						"field", f.Name,
						"error", err,
					)
				}
			}
		}

		meta.fields = append(meta.fields, fm)
	}
	return meta
}

// fieldByIndex 沿索引路径从根结构体出发定位到目标字段，中间若遇 nil 指针则自动初始化。
// 供绑定/校验/默认值路径使用，保证嵌套结构体中的字段可安全访问。
// 支持多级索引路径（如 []int{0,1}），用于匿名嵌入 struct 展开后的字段定位。
// Ptr 分支处理嵌入指针字段（如 *Base）的自动初始化，确保多级路径可安全遍历。
func fieldByIndex(v reflect.Value, indices []int) reflect.Value {
	for _, i := range indices {
		if v.Kind() == reflect.Ptr {
			if v.IsNil() {
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
		v = v.Field(i)
	}
	return v
}

// isStructPtr 判断类型是否为指向结构体的指针
func isStructPtr(t reflect.Type) bool {
	return t.Kind() == reflect.Ptr && t.Elem().Kind() == reflect.Struct
}

// isStructLike 判断类型是否为 struct 或指向 struct 的指针（含多级指针）。
// 用于判定匿名嵌入字段是否应被展开（对齐 encoding/json 语义）。
// 复用带上限的 derefType，防自引用指针类型死循环（修复 REC-07；该触发路径实际
// 被编译器封死——嵌入 *T 要求 T 非指针，此处为预防性加固）。
func isStructLike(t reflect.Type) bool {
	return derefType(t).Kind() == reflect.Struct
}

// isFlattenableEmbed 判断匿名嵌入字段是否会被 buildStructMeta 展开为顶层字段：
// 导出 struct / 指向 struct 的指针，或未导出的值 struct（未导出指针嵌入已被跳过展开）。
// 供 hasNonzeroInTree / hasRequestPhaseDefaults / checkUnsupportedDefaults 三个
// 原始类型树扫描器使用，使其穿透未导出嵌入字段的规则与 buildStructMeta 的展开逻辑保持一致。
func isFlattenableEmbed(f reflect.StructField) bool {
	if !f.Anonymous || !isStructLike(f.Type) {
		return false
	}
	// 未导出指针嵌入（type Req struct{ *base }）已被 buildStructMeta 跳过展开
	if f.PkgPath != "" && f.Type.Kind() == reflect.Ptr {
		return false
	}
	return true
}
