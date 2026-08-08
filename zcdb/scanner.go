package zcdb

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"time"
)

// scanFieldInfo 缓存的扫描字段信息，用于列名到结构体字段的映射。
type scanFieldInfo struct {
	// columnIndex 列名 → 结构体字段索引路径的映射（支持嵌入结构体）
	columnIndex map[string][]int
}

// scanCache 扫描结果的字段映射缓存，按 reflect.Type 缓存。
var scanCache sync.Map // map[reflect.Type]*scanFieldInfo

// getScanFieldInfo 获取或构建结构体类型的列名→字段索引映射。
// 支持嵌入结构体（匿名字段），其内部字段会被递归展开。
func getScanFieldInfo(t reflect.Type) *scanFieldInfo {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if cached, ok := scanCache.Load(t); ok {
		return cached.(*scanFieldInfo)
	}

	info := &scanFieldInfo{
		columnIndex: make(map[string][]int),
	}
	buildScanFields(t, info, nil)

	scanCache.Store(t, info)
	return info
}

// buildScanFields 递归构建扫描字段映射，支持嵌入结构体。
func buildScanFields(t reflect.Type, info *scanFieldInfo, indexPrefix []int) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}

		// 构建当前字段的完整索引路径
		index := make([]int, len(indexPrefix)+1)
		copy(index, indexPrefix)
		index[len(indexPrefix)] = i

		// 处理嵌入结构体（匿名字段，支持值类型与指针类型）
		if field.Anonymous {
			embeddedType := field.Type
			if embeddedType.Kind() == reflect.Ptr {
				embeddedType = embeddedType.Elem()
			}
			if embeddedType.Kind() == reflect.Struct {
				buildScanFields(embeddedType, info, index)
				continue
			}
		}

		tag := field.Tag.Get("db")
		if tag == "-" {
			continue
		}
		column := tag
		if column == "" {
			column = toSnakeCase(field.Name)
		}
		info.columnIndex[column] = index
	}
}

// ScanStruct 将 *sql.Rows 扫描到 dest 中，不关闭 rows。
// dest 可以是：
//   - *struct 指针：扫描第一行到结构体，未找到返回 sql.ErrNoRows
//   - *[]struct 指针：扫描所有行到结构体切片
//   - *[]*struct 指针：扫描所有行到结构体指针切片
//
// 根据 db 标签或 snake_case 自动匹配列名到字段。
// 调用方负责关闭 rows。
//
//	rows, err := db.Query(ctx, sqlStr, args...)
//	if err != nil { ... }
//	defer rows.Close()
//	err := zcdb.ScanStruct(rows, &users)
func ScanStruct(rows *sql.Rows, dest any) error {
	destValue := reflect.ValueOf(dest)
	if destValue.Kind() != reflect.Ptr {
		return fmt.Errorf("zcdb: ScanStruct dest must be a pointer, got %T", dest)
	}
	destValue = destValue.Elem()

	columns, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("zcdb: get columns failed: %w", err)
	}

	switch destValue.Kind() {
	case reflect.Struct:
		return scanOneRow(rows, columns, destValue)
	case reflect.Slice:
		return scanAllRows(rows, columns, destValue)
	default:
		return fmt.Errorf("zcdb: ScanStruct dest must be a pointer to struct or slice, got *%s", destValue.Kind())
	}
}

// ScanStructClose 将 *sql.Rows 扫描到 dest 中，完成后自动关闭 rows。
// 是 ScanStruct 的便捷封装，适用于不需要流式读取的场景。
func ScanStructClose(rows *sql.Rows, dest any) error {
	defer func(rows *sql.Rows) {
		_ = rows.Close()
	}(rows)
	return ScanStruct(rows, dest)
}

// scanOneRow 扫描第一行到结构体，未找到返回 sql.ErrNoRows。
func scanOneRow(rows *sql.Rows, columns []string, dest reflect.Value) error {
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}

	fieldInfo := getScanFieldInfo(dest.Type())
	values := makeScanValues(columns, fieldInfo, dest)
	if err := rows.Scan(values...); err != nil {
		return fmt.Errorf("zcdb: scan row failed: %w", err)
	}
	return rows.Err()
}

// scanAllRows 扫描所有行到结构体切片。
func scanAllRows(rows *sql.Rows, columns []string, dest reflect.Value) error {
	elemType := dest.Type().Elem()
	isPtr := elemType.Kind() == reflect.Ptr
	if isPtr {
		elemType = elemType.Elem()
	}
	if elemType.Kind() != reflect.Struct {
		return fmt.Errorf("zcdb: slice element must be struct or *struct, got %s", dest.Type().Elem())
	}

	fieldInfo := getScanFieldInfo(elemType)

	for rows.Next() {
		elem := reflect.New(elemType).Elem()
		values := makeScanValues(columns, fieldInfo, elem)
		if err := rows.Scan(values...); err != nil {
			return fmt.Errorf("zcdb: scan row failed: %w", err)
		}
		if isPtr {
			ptr := reflect.New(elemType)
			ptr.Elem().Set(elem)
			dest.Set(reflect.Append(dest, ptr))
		} else {
			dest.Set(reflect.Append(dest, elem))
		}
	}
	return rows.Err()
}

// makeScanValues 为每一行创建扫描目标值切片。
// 按列名匹配结构体字段，未匹配的列扫描到 discard（忽略）。
// 支持 NULL 值：非指针字段遇到 NULL 时保留零值，指针字段遇到 NULL 时设为 nil。
func makeScanValues(columns []string, info *scanFieldInfo, structValue reflect.Value) []any {
	values := make([]any, len(columns))
	for i, col := range columns {
		if idx, ok := info.columnIndex[col]; ok {
			field, ok := fieldByIndexSafe(structValue, idx)
			if !ok {
				// nil 嵌入指针：该列无法填充，忽略
				values[i] = &discard{}
			} else {
				values[i] = &nullSafeField{field: field}
			}
		} else {
			// 未匹配的列，扫描到 discard 忽略
			values[i] = &discard{}
		}
	}
	return values
}

// discard 实现 sql.Scanner，用于忽略不需要的列。
type discard struct{}

func (d *discard) Scan(_ any) error {
	return nil
}

// nullSafeField 包装结构体字段，实现 sql.Scanner 接口。
// 遇到 NULL 时保留零值（非指针）或设为 nil（指针），避免扫描报错。
type nullSafeField struct {
	field reflect.Value
}

func (n *nullSafeField) Scan(src any) error {
	// NULL 值处理：设为零值（指针类型为 nil，非指针类型为零值）
	if src == nil {
		n.field.Set(reflect.Zero(n.field.Type()))
		return nil
	}

	// 确定实际要设置的目标值（处理指针间接）
	isPtr := n.field.Kind() == reflect.Ptr
	targetType := n.field.Type()
	if isPtr {
		targetType = targetType.Elem()
	}
	targetKind := targetType.Kind()

	// 类型转换
	val := reflect.ValueOf(src)

	// 辅助函数：将转换后的值设置到字段（处理指针间接）
	setFinalValue := func(converted reflect.Value) {
		if isPtr {
			newVal := reflect.New(targetType).Elem()
			newVal.Set(converted)
			n.field.Set(newVal.Addr())
		} else {
			n.field.Set(converted)
		}
	}

	// 处理 []byte → 目标类型的转换
	if val.Kind() == reflect.Slice && val.Type().Elem().Kind() == reflect.Uint8 {
		switch targetKind {
		case reflect.String:
			setFinalValue(reflect.ValueOf(string(val.Bytes())))
			return nil
		case reflect.Slice:
			if targetType.Elem().Kind() == reflect.Uint8 {
				// []byte → []byte 或 []byte → json.RawMessage（底层都是 []byte）
				dst := reflect.MakeSlice(targetType, len(val.Bytes()), len(val.Bytes()))
				reflect.Copy(dst, val)
				setFinalValue(dst)
				return nil
			}
			// 其它切片类型（如 []int、[]string）：数据库通常以 JSON 文本存储，
			// 直接 reflect.Copy 会因元素类型不匹配而 panic，故走 JSON 反序列化
			dst := reflect.New(targetType)
			if err := json.Unmarshal(val.Bytes(), dst.Interface()); err != nil {
				return fmt.Errorf("zcdb: cannot unmarshal JSON to %v: %w", targetType, err)
			}
			setFinalValue(dst.Elem())
			return nil
		case reflect.Map:
			// []byte → map[string]any（JSON 反序列化）
			if targetType.Key().Kind() == reflect.String {
				m := reflect.New(targetType).Interface()
				if err := json.Unmarshal(val.Bytes(), m.(any)); err != nil {
					return fmt.Errorf("zcdb: cannot unmarshal JSON to map: %w", err)
				}
				setFinalValue(reflect.ValueOf(m).Elem())
				return nil
			}
		case reflect.Struct:
			// []byte → 自定义结构体（JSON 反序列化）
			dst := reflect.New(targetType)
			if err := json.Unmarshal(val.Bytes(), dst.Interface()); err != nil {
				return fmt.Errorf("zcdb: cannot unmarshal JSON to struct: %w", err)
			}
			setFinalValue(dst.Elem())
			return nil
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			i, err := strconv.ParseInt(string(val.Bytes()), 10, 64)
			if err != nil {
				return fmt.Errorf("zcdb: cannot convert %q to %s", string(val.Bytes()), targetKind)
			}
			setFinalValue(reflect.ValueOf(i).Convert(targetType))
			return nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			u, err := strconv.ParseUint(string(val.Bytes()), 10, 64)
			if err != nil {
				return fmt.Errorf("zcdb: cannot convert %q to %s", string(val.Bytes()), targetKind)
			}
			setFinalValue(reflect.ValueOf(u).Convert(targetType))
			return nil
		case reflect.Float32, reflect.Float64:
			f, err := strconv.ParseFloat(string(val.Bytes()), 64)
			if err != nil {
				return fmt.Errorf("zcdb: cannot convert %q to %s", string(val.Bytes()), targetKind)
			}
			setFinalValue(reflect.ValueOf(f).Convert(targetType))
			return nil
		case reflect.Bool:
			b, err := strconv.ParseBool(string(val.Bytes()))
			if err != nil {
				return fmt.Errorf("zcdb: cannot convert %q to bool", string(val.Bytes()))
			}
			setFinalValue(reflect.ValueOf(b))
			return nil
		default:
			//无需任何处理，接着往下执行
		}
	}

	// 处理 time.Time → string 的转换（MySQL 日期时间类型）
	if _, ok := src.(time.Time); ok {
		if targetKind == reflect.String {
			setFinalValue(reflect.ValueOf(src.(time.Time).Format(time.RFC3339)))
			return nil
		}
	}

	// 处理数值 → string 的转换（SQLite/PostgreSQL 驱动对数值列返回 int64/float64，
	// 目标是 string 字段时若走 ConvertibleTo 会把数值当成 rune 码转成字符，
	// 如 123 → "{"，必须显式格式化为数字字符串）
	if targetKind == reflect.String {
		switch val.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			setFinalValue(reflect.ValueOf(strconv.FormatInt(val.Int(), 10)))
			return nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			setFinalValue(reflect.ValueOf(strconv.FormatUint(val.Uint(), 10)))
			return nil
		case reflect.Float32, reflect.Float64:
			setFinalValue(reflect.ValueOf(strconv.FormatFloat(val.Float(), 'g', -1, 64)))
			return nil
		default:
			// 无需任何处理，接着往下执行
		}
	}

	// 处理 string → 目标类型的转换（PostgreSQL TEXT 驱动返回 string）
	if val.Kind() == reflect.String {
		switch targetKind {
		case reflect.Slice:
			if targetType.Elem().Kind() == reflect.Uint8 {
				// string → []byte（或 []byte 别名）：直接转换，拷贝字节
				setFinalValue(val.Convert(targetType))
				return nil
			}
			// string → 其它切片类型（如 []int、[]string）：数据库通常以 JSON 文本存储
			dst := reflect.New(targetType)
			if err := json.Unmarshal([]byte(val.String()), dst.Interface()); err != nil {
				return fmt.Errorf("zcdb: cannot unmarshal JSON to %v: %w", targetType, err)
			}
			setFinalValue(dst.Elem())
			return nil
		case reflect.Map:
			// string → map[string]any（JSON 反序列化，与 []byte→map 分支对称；
			// PG/SQLite 驱动的 TEXT 列返回 string）
			if targetType.Key().Kind() == reflect.String {
				m := reflect.New(targetType).Interface()
				if err := json.Unmarshal([]byte(val.String()), m.(any)); err != nil {
					return fmt.Errorf("zcdb: cannot unmarshal JSON to map: %w", err)
				}
				setFinalValue(reflect.ValueOf(m).Elem())
				return nil
			}
		case reflect.Struct:
			// string → 自定义结构体（JSON 反序列化，与 []byte→struct 分支对称）
			dst := reflect.New(targetType)
			if err := json.Unmarshal([]byte(val.String()), dst.Interface()); err != nil {
				return fmt.Errorf("zcdb: cannot unmarshal JSON to struct: %w", err)
			}
			setFinalValue(dst.Elem())
			return nil
		case reflect.Bool:
			b, err := strconv.ParseBool(val.String())
			if err != nil {
				return fmt.Errorf("zcdb: cannot convert %q to bool", val.String())
			}
			setFinalValue(reflect.ValueOf(b))
			return nil
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			i, err := strconv.ParseInt(val.String(), 10, 64)
			if err != nil {
				return fmt.Errorf("zcdb: cannot convert %q to %s", val.String(), targetKind)
			}
			setFinalValue(reflect.ValueOf(i).Convert(targetType))
			return nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			u, err := strconv.ParseUint(val.String(), 10, 64)
			if err != nil {
				return fmt.Errorf("zcdb: cannot convert %q to %s", val.String(), targetKind)
			}
			setFinalValue(reflect.ValueOf(u).Convert(targetType))
			return nil
		case reflect.Float32, reflect.Float64:
			f, err := strconv.ParseFloat(val.String(), 64)
			if err != nil {
				return fmt.Errorf("zcdb: cannot convert %q to %s", val.String(), targetKind)
			}
			setFinalValue(reflect.ValueOf(f).Convert(targetType))
			return nil
		default:
			//无需任何处理，接着往下执行
		}
	}

	// 处理数值 → bool 的转换（SQLite 用 0/1 存储布尔值，驱动返回 int64）
	if targetKind == reflect.Bool {
		switch val.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			setFinalValue(reflect.ValueOf(val.Int() != 0))
			return nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			setFinalValue(reflect.ValueOf(val.Uint() != 0))
			return nil
		case reflect.Float32, reflect.Float64:
			setFinalValue(reflect.ValueOf(val.Float() != 0))
			return nil
		default:
			//无需任何处理，接着往下执行
		}
	}

	// 直接类型匹配
	if val.Type().ConvertibleTo(targetType) {
		setFinalValue(val.Convert(targetType))
		return nil
	}

	return fmt.Errorf("zcdb: cannot convert %T to %s", src, targetType)
}
