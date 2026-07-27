package zcdb

import (
	"database/sql"
	"fmt"
	"reflect"
	"sync"
)

// scanFieldInfo 缓存的扫描字段信息，用于列名到结构体字段的映射。
type scanFieldInfo struct {
	// columnIndex 列名 → 结构体字段索引的映射
	columnIndex map[string]int
}

// scanCache 扫描结果的字段映射缓存，按 reflect.Type 缓存。
var scanCache sync.Map // map[reflect.Type]*scanFieldInfo

// getScanFieldInfo 获取或构建结构体类型的列名→字段索引映射。
func getScanFieldInfo(t reflect.Type) *scanFieldInfo {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if cached, ok := scanCache.Load(t); ok {
		return cached.(*scanFieldInfo)
	}

	info := &scanFieldInfo{
		columnIndex: make(map[string]int),
	}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		tag := field.Tag.Get("db")
		if tag == "-" {
			continue
		}
		column := tag
		if column == "" {
			column = toSnakeCase(field.Name)
		}
		info.columnIndex[column] = i
	}

	scanCache.Store(t, info)
	return info
}

// ScanStruct 将 *sql.Rows 扫描到 dest 中。
// dest 可以是：
//   - *struct 指针：扫描第一行到结构体，未找到返回 sql.ErrNoRows
//   - *[]struct 指针：扫描所有行到结构体切片
//   - *[]*struct 指针：扫描所有行到结构体指针切片
//
// 根据 db 标签或 snake_case 自动匹配列名到字段。
//
//	err := zcdb.ScanStruct(rows, &user)
//	err := zcdb.ScanStruct(rows, &users)
func ScanStruct(rows *sql.Rows, dest any) error {
	defer func() {
		_ = rows.Close()
	}()

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
func makeScanValues(columns []string, info *scanFieldInfo, structValue reflect.Value) []any {
	values := make([]any, len(columns))
	for i, col := range columns {
		if idx, ok := info.columnIndex[col]; ok {
			field := structValue.Field(idx)
			values[i] = field.Addr().Interface()
		} else {
			// 未匹配的列，扫描到 discard 忽略
			values[i] = &discard{}
		}
	}
	return values
}

// discard 实现 sql.Scanner，用于忽略不需要的列。
type discard struct{}

func (d *discard) Scan(src any) error {
	return nil
}
