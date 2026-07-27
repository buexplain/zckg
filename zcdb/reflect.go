package zcdb

import (
	"reflect"
	"strings"
	"sync"
	"unicode"
)

// structField 表示一个解析后的结构体字段信息
type structField struct {
	Column string // 数据库列名
	Index  int    // 在结构体中的字段索引
}

// structInfo 缓存解析后的结构体元信息
type structInfo struct {
	Fields []structField
}

// 结构体元信息缓存
var structCache sync.Map // map[reflect.Type]*structInfo

// parseStruct 解析结构体类型，提取字段列名和索引。
// 使用 `db` 标签获取列名，无标签时自动将字段名转为 snake_case。
// `db:"-"` 的字段会被跳过。
func parseStruct(t reflect.Type) *structInfo {
	// 确保是结构体类型
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}

	// 缓存命中
	if cached, ok := structCache.Load(t); ok {
		return cached.(*structInfo)
	}

	info := &structInfo{}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// 跳过未导出字段
		if !field.IsExported() {
			continue
		}

		// 解析 db 标签
		tag := field.Tag.Get("db")
		if tag == "-" {
			continue
		}

		column := tag
		if column == "" {
			// 无标签时将 GoFieldName 转为 snake_case
			column = toSnakeCase(field.Name)
		}

		info.Fields = append(info.Fields, structField{
			Column: column,
			Index:  i,
		})
	}

	structCache.Store(t, info)
	return info
}

// extractInsertData 从 INSERT 数据中提取列名和值。
//
// data 接受以下输入形式：
//   - 结构体值：User{Name: "alice", Age: 25}
//   - 结构体指针：&User{Name: "alice", Age: 25}
//   - 结构体切片：[]User{{Name: "alice"}, {Name: "bob"}}
//   - 结构体指针切片：[]*User{&u1, &u2}
//
// 非结构体/切片类型返回 ErrInvalidStruct；所有字段均为 nil 返回 ErrNoFields。
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
func extractInsertData(data any) (columns []string, rows [][]any, err error) {
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

		info := parseStruct(elemType)
		if info == nil || len(info.Fields) == 0 {
			return nil, nil, ErrNoFields
		}

		// 用第一个元素确定列（所有元素必须使用相同结构体）
		// 收集所有非 nil 字段的列名（基于第一行）
		first := v.Index(0)
		if first.Kind() == reflect.Ptr {
			first = first.Elem()
		}

		var colIndices []int
		for _, f := range info.Fields {
			fv := first.Field(f.Index)
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
				elem = elem.Elem()
			}
			row := make([]any, 0, len(colIndices))
			for _, idx := range colIndices {
				fv := elem.Field(idx)
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
		v = v.Elem()
		t = v.Type()
	}
	if t.Kind() != reflect.Struct {
		return nil, nil, ErrInvalidStruct
	}

	info := parseStruct(t)
	if info == nil || len(info.Fields) == 0 {
		return nil, nil, ErrNoFields
	}

	var values []any
	for _, f := range info.Fields {
		fv := v.Field(f.Index)
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
func extractUpdateData(data any) (columns []string, values []any, err error) {
	if data == nil {
		return nil, nil, ErrInvalidStruct
	}
	v := reflect.ValueOf(data)
	if !v.IsValid() {
		return nil, nil, ErrInvalidStruct
	}
	t := v.Type()

	if t.Kind() == reflect.Ptr {
		v = v.Elem()
		t = v.Type()
	}
	if t.Kind() != reflect.Struct {
		return nil, nil, ErrInvalidStruct
	}

	info := parseStruct(t)
	if info == nil || len(info.Fields) == 0 {
		return nil, nil, ErrNoFields
	}

	for _, f := range info.Fields {
		fv := v.Field(f.Index)
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
func toSnakeCase(s string) string {
	var buf strings.Builder
	buf.Grow(len(s) + 4)

	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				prev := rune(s[i-1])
				if unicode.IsLower(prev) || (i+1 < len(s) && unicode.IsLower(rune(s[i+1]))) {
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
