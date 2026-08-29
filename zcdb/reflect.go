package zcdb

import (
	"reflect"
	"strings"
	"sync"
	"unicode"
)

// defaultTagName 默认的列映射结构体标签名
const defaultTagName = "db"

// structField 表示一个解析后的结构体字段信息
type structField struct {
	Column string // 数据库列名
	Index  []int  // 字段索引路径（支持嵌入结构体，如 [0, 1] 表示第一个嵌入字段的第二个字段）
}

// structInfo 缓存解析后的结构体元信息
type structInfo struct {
	Fields []structField
}

// structCacheKey 结构体元信息缓存键：同一结构体在不同标签名下解析结果不同，
// 缓存须按（类型, 标签名）复合键区分。
type structCacheKey struct {
	typ reflect.Type
	tag string
}

// 结构体元信息缓存
var structCache sync.Map // map[structCacheKey]*structInfo

// parseStruct 解析结构体类型，提取字段列名和索引。
// tagName 为列映射标签名（如 "db"），空值回退为默认的 db 标签；
// 使用标签获取列名，无标签时自动将字段名转为 snake_case。
// 标签值为 "-" 的字段会被跳过。
// 支持嵌入结构体（匿名字段），其内部字段会被递归展开。
func parseStruct(t reflect.Type, tagName string) *structInfo {
	// 确保是结构体类型
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	if tagName == "" {
		tagName = defaultTagName
	}

	// 缓存命中
	key := structCacheKey{typ: t, tag: tagName}
	if cached, ok := structCache.Load(key); ok {
		return cached.(*structInfo)
	}

	info := &structInfo{Fields: collectStructFields(t, tagName)}

	structCache.Store(key, info)
	return info
}

// collectStructFields 按「外层优先」顺序展开结构体字段（含匿名嵌入结构体）。
// 采用广度优先遍历：浅层字段先于深层（嵌入）字段处理，同层内按声明顺序；
// 遇到已出现的列名直接跳过（浅层遮蔽深层，对齐 encoding/json 语义）。
// 返回去重后的字段序列，供扫描映射与 Insert/Update 数据提取共用。
func collectStructFields(t reflect.Type, tagName string) []structField {
	type pending struct {
		typ   reflect.Type
		index []int
	}

	var fields []structField
	seen := make(map[string]bool)
	// visited 记录已展开（或已入队待展开）的嵌入结构体类型。自引用嵌入（type E struct{ *E }）
	// 或互引用嵌入（type A struct{ *B } / type B struct{ *A }）会让 BFS 队列无限入队、
	// 无界增长内存；同一类型只需展开一次——其列名由类型自身决定、与嵌入路径无关，
	// 重复展开只会产生已被 seen 去重的同名列，跳过不改变「外层优先 + 遮蔽」语义。
	visited := make(map[reflect.Type]bool)
	queue := []pending{{typ: t}}
	visited[t] = true

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		for i := 0; i < cur.typ.NumField(); i++ {
			field := cur.typ.Field(i)

			// 跳过未导出字段
			if !field.IsExported() {
				continue
			}

			// 构建当前字段的完整索引路径
			index := make([]int, len(cur.index)+1)
			copy(index, cur.index)
			index[len(cur.index)] = i

			// 处理嵌入结构体（匿名字段，支持值类型与指针类型）：
			// 不立即递归，而是入队待下一层处理，保证外层字段优先
			if field.Anonymous {
				embeddedType := field.Type
				if embeddedType.Kind() == reflect.Ptr {
					embeddedType = embeddedType.Elem()
				}
				if embeddedType.Kind() == reflect.Struct {
					if visited[embeddedType] {
						continue
					}
					visited[embeddedType] = true
					queue = append(queue, pending{typ: embeddedType, index: index})
					continue
				}
			}

			// 解析列映射标签
			tag := field.Tag.Get(tagName)
			if tag == "-" {
				continue
			}

			column := tag
			if column == "" {
				// 无标签时将 GoFieldName 转为 snake_case
				column = toSnakeCase(field.Name)
			}

			// 浅层遮蔽深层：同名列仅保留最先（最浅）出现者
			if seen[column] {
				continue
			}
			seen[column] = true
			fields = append(fields, structField{
				Column: column,
				Index:  index,
			})
		}
	}
	return fields
}

// extractInsertData 从 INSERT 数据中提取列名和值。
//
// data 接受以下输入形式：
//   - 结构体值：User{Name: "alice", Age: 25}
//   - 结构体指针：&User{Name: "alice", Age: 25}
//   - 结构体切片：[]User{{Name: "alice"}, {Name: "bob"}}
//   - 结构体指针切片：[]*User{&u1, &u2}
//
// 非结构体/切片类型返回 ErrInvalidStruct；空切片返回 ErrEmptyData；所有字段均为 nil 返回 ErrNoFields。
//
// 字段值处理规则（单结构体 & 切片首行）：
//   - any(interface{}) 字段：nil → 该列被跳过；非 nil → fv.Elem().Interface()
//   - 指针（*T）字段：nil → 该列被跳过；非 nil → fv.Elem().Interface() 解引用
//   - 其它类型：直接 fv.Interface()（含 Expression 与具体值类型）
//
// 切片逐行填值规则（列由首行确定）：
//   - any/指针字段：nil → 传入 nil（SQL NULL）；非 nil → 解引用
//   - 其它类型：直接 fv.Interface()
//
// 返回 (列名列表, 值的二维切片, error)
func extractInsertData(data any, tagName string) (columns []string, rows [][]any, err error) {
	if data == nil {
		return nil, nil, ErrInvalidStruct
	}
	v := reflect.ValueOf(data)
	if !v.IsValid() {
		return nil, nil, ErrInvalidStruct
	}
	t := v.Type()

	// 处理切片类型
	if t.Kind() == reflect.Slice {
		if v.Len() == 0 {
			return nil, nil, ErrEmptyData
		}
		elemType := t.Elem()
		if elemType.Kind() == reflect.Ptr {
			elemType = elemType.Elem()
		}
		if elemType.Kind() != reflect.Struct {
			return nil, nil, ErrInvalidStruct
		}

		info := parseStruct(elemType, tagName)
		if info == nil || len(info.Fields) == 0 {
			return nil, nil, ErrNoFields
		}

		// 用第一个元素确定列（所有元素必须使用相同结构体）
		// 收集所有非 nil 字段的列名（基于第一行）
		first := v.Index(0)
		if first.Kind() == reflect.Ptr {
			if first.IsNil() {
				return nil, nil, ErrInvalidStruct
			}
			first = first.Elem()
		}

		var colIndices [][]int
		for _, f := range info.Fields {
			fv, ok := fieldByIndexSafe(first, f.Index)
			if !ok {
				// nil 嵌入指针：字段不可用，跳过
				continue
			}
			// interface/指针类型：nil 则跳过；其它类型永远纳入
			if (fv.Kind() == reflect.Interface || fv.Kind() == reflect.Ptr) && fv.IsNil() {
				continue
			}
			columns = append(columns, f.Column)
			colIndices = append(colIndices, f.Index)
		}
		if len(columns) == 0 {
			return nil, nil, ErrNoFields
		}

		// 提取每行数据
		for i := 0; i < v.Len(); i++ {
			elem := v.Index(i)
			if elem.Kind() == reflect.Ptr {
				if elem.IsNil() {
					return nil, nil, ErrInvalidStruct
				}
				elem = elem.Elem()
			}
			row := make([]any, 0, len(colIndices))
			for _, idx := range colIndices {
				fv, ok := fieldByIndexSafe(elem, idx)
				if !ok {
					// nil 嵌入指针：该列取不到值，按 NULL 处理
					row = append(row, nil)
					continue
				}
				switch fv.Kind() {
				case reflect.Interface, reflect.Ptr:
					if fv.IsNil() {
						row = append(row, nil)
					} else {
						row = append(row, fv.Elem().Interface())
					}
				default:
					row = append(row, fv.Interface())
				}
			}
			rows = append(rows, row)
		}
		return columns, rows, nil
	}

	// 处理单个结构体
	if t.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil, nil, ErrInvalidStruct
		}
		v = v.Elem()
		t = v.Type()
	}
	if t.Kind() != reflect.Struct {
		return nil, nil, ErrInvalidStruct
	}

	info := parseStruct(t, tagName)
	if info == nil || len(info.Fields) == 0 {
		return nil, nil, ErrNoFields
	}

	var values []any
	for _, f := range info.Fields {
		fv, ok := fieldByIndexSafe(v, f.Index)
		if !ok {
			// nil 嵌入指针：字段不可用，跳过
			continue
		}
		switch fv.Kind() {
		case reflect.Interface:
			// any 字段为 nil 则跳过，否则取实际值
			if fv.IsNil() {
				continue
			}
			columns = append(columns, f.Column)
			values = append(values, fv.Elem().Interface())
		case reflect.Ptr:
			// 指针字段为 nil 则跳过，否则解引用为具体值
			if fv.IsNil() {
				continue
			}
			columns = append(columns, f.Column)
			values = append(values, fv.Elem().Interface())
		default:
			columns = append(columns, f.Column)
			values = append(values, fv.Interface())
		}
	}
	if len(columns) == 0 {
		return nil, nil, ErrNoFields
	}

	rows = [][]any{values}
	return columns, rows, nil
}

// extractUpdateData 从 UPDATE 数据中提取列名和值。
//
// data 必须是结构体或指向结构体的指针，否则返回 ErrInvalidStruct。
// 所有字段均为 nil 返回 ErrNoFields。
//
// 字段值处理规则：
//   - any(interface{}) 字段：nil → 该列被跳过（不参与 SET）；非 nil → fv.Elem().Interface()
//   - 指针（*T）字段：nil → 该列被跳过；非 nil → fv.Elem().Interface() 解引用
//   - 其它类型（含 Expression）：直接 fv.Interface()
//
// 返回 (列名列表, 值列表, error)
func extractUpdateData(data any, tagName string) (columns []string, values []any, err error) {
	if data == nil {
		return nil, nil, ErrInvalidStruct
	}
	v := reflect.ValueOf(data)
	if !v.IsValid() {
		return nil, nil, ErrInvalidStruct
	}
	t := v.Type()

	if t.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil, nil, ErrInvalidStruct
		}
		v = v.Elem()
		t = v.Type()
	}
	if t.Kind() != reflect.Struct {
		return nil, nil, ErrInvalidStruct
	}

	info := parseStruct(t, tagName)
	if info == nil || len(info.Fields) == 0 {
		return nil, nil, ErrNoFields
	}

	for _, f := range info.Fields {
		fv, ok := fieldByIndexSafe(v, f.Index)
		if !ok {
			// nil 嵌入指针：字段不可用，跳过
			continue
		}
		switch fv.Kind() {
		case reflect.Interface:
			// any 字段为 nil 则跳过，否则取实际值
			if fv.IsNil() {
				continue
			}
			columns = append(columns, f.Column)
			values = append(values, fv.Elem().Interface())
		case reflect.Ptr:
			// 指针字段为 nil 则跳过，否则解引用为具体值
			if fv.IsNil() {
				continue
			}
			columns = append(columns, f.Column)
			values = append(values, fv.Elem().Interface())
		default:
			columns = append(columns, f.Column)
			values = append(values, fv.Interface())
		}
	}
	if len(columns) == 0 {
		return nil, nil, ErrNoFields
	}
	return columns, values, nil
}

// toSnakeCase 将 PascalCase/camelCase 转换为 snake_case。
// 使用 rune 索引处理，支持中文等多字节字符字段名。
func toSnakeCase(s string) string {
	runes := []rune(s)
	var buf strings.Builder
	buf.Grow(len(s) + 4)

	for i, r := range runes {
		if unicode.IsUpper(r) {
			if i > 0 {
				prev := runes[i-1]
				if unicode.IsLower(prev) || (i+1 < len(runes) && unicode.IsLower(runes[i+1])) {
					buf.WriteByte('_')
				}
			}
			buf.WriteRune(unicode.ToLower(r))
		} else {
			buf.WriteRune(r)
		}
	}
	return buf.String()
}

// fieldByIndexSafe 沿索引路径安全取值：遇到 nil 中间指针（如未初始化的嵌入指针结构体）
// 返回零值并标记不可用，避免 FieldByIndex 直接 panic。
func fieldByIndexSafe(v reflect.Value, index []int) (reflect.Value, bool) {
	for _, i := range index {
		if v.Kind() == reflect.Ptr {
			if v.IsNil() {
				return reflect.Value{}, false
			}
			v = v.Elem()
		}
		v = v.Field(i)
	}
	return v, true
}
