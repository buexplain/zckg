package zcdb

import (
	"context"
	"database/sql"
	"fmt"
	"iter"
	"reflect"
)

// Cursor 返回流式迭代器，通过 *sql.Rows 逐行扫描。
// 适用于中小表或需要精确排序的场景。
// 迭代过程中持有数据库连接，break 时自动释放。
//
// dest 参数要求：
//
//   - 必须是结构体指针（如 *User）
//
//   - 结构体字段通过 db 标签匹配列名，未匹配的列会被忽略
//
//   - 每次迭代都会覆盖写入同一个 dest，无需在循环内重新声明
//
//     var user User
//     for err := range db.Builder().Table("users").OrderBy("id", "ASC").Cursor(ctx, &user) {
//     if err != nil { log.Fatal(err) }
//     fmt.Println(user.Name)
//     }
func (b *Builder) Cursor(ctx context.Context, dest any) iter.Seq[error] {
	return func(yield func(error) bool) {
		sqlStr, args, err := b.ToSelect()
		if err != nil {
			yield(err)
			return
		}

		rows, err := b.dao.Query(ctx, sqlStr, args...)
		if err != nil {
			yield(err)
			return
		}
		defer func(rows *sql.Rows) {
			_ = rows.Close()
		}(rows)

		columns, err := rows.Columns()
		if err != nil {
			yield(err)
			return
		}

		destVal := reflect.ValueOf(dest)
		if destVal.Kind() != reflect.Ptr {
			yield(ErrNotPointer)
			return
		}
		destElem := destVal.Elem()
		if destElem.Kind() != reflect.Struct {
			yield(ErrNotStruct)
			return
		}

		fieldInfo := getScanFieldInfo(destElem.Type())
		for rows.Next() {
			values := makeScanValues(columns, fieldInfo, destElem)
			if err := rows.Scan(values...); err != nil {
				yield(fmt.Errorf("zcdb: cursor scan failed: %w", err))
				return
			}
			if !yield(nil) {
				return
			}
		}
		if err := rows.Err(); err != nil {
			yield(err)
		}
	}
}

// CursorBy 返回基于游标分页的迭代器，通过 WHERE cursorColumn > lastValue 分批获取数据。
// 适用于大表批量处理，每批独立查询，不长时间占用连接。
// cursorColumn 必须是有序且唯一的列（通常为主键）。
// 注意：CursorBy 会忽略已设置的 ORDER BY，强制按 cursorColumn ASC 排序。
//
// dest 参数要求：
//
//   - 必须是结构体指针（如 *User）
//
//   - 结构体字段通过 db 标签匹配列名，未匹配的列会被忽略
//
//   - 结构体必须包含 cursorColumn 对应的字段（通过 db 标签或字段名匹配）
//
//   - 每次迭代都会覆盖写入同一个 dest，无需在循环内重新声明
//
//     var user User
//     for err := range db.Builder().Table("users").CursorBy(ctx, &user, 100, "id") {
//     if err != nil { log.Fatal(err) }
//     fmt.Println(user.Name)
//     }
func (b *Builder) CursorBy(ctx context.Context, dest any, chunkSize int, cursorColumn string) iter.Seq[error] {
	return func(yield func(error) bool) {
		if chunkSize <= 0 {
			chunkSize = 100
		}

		destVal := reflect.ValueOf(dest)
		if destVal.Kind() != reflect.Ptr {
			yield(ErrNotPointer)
			return
		}
		destElem := destVal.Elem()
		if destElem.Kind() != reflect.Struct {
			yield(ErrNotStruct)
			return
		}

		// 查找游标列在结构体中的字段索引
		fieldInfo := getScanFieldInfo(destElem.Type())
		cursorIdx, hasCursorField := fieldInfo.columnIndex[toSnakeCase(cursorColumn)]
		// 也尝试直接按字段名匹配
		if !hasCursorField {
			for i := 0; i < destElem.NumField(); i++ {
				if destElem.Type().Field(i).Name == cursorColumn {
					cursorIdx = []int{i}
					hasCursorField = true
					break
				}
			}
		}
		if !hasCursorField {
			yield(ErrCursorFieldNotFound)
			return
		}

		wrappedCol := b.grammar.WrapColumn(cursorColumn)
		var lastCursorValue any

		for {
			// 克隆构造器，避免修改原始状态
			clone := b.Clone()
			clone.limit = chunkSize
			clone.orders = nil // 清空已有排序，确保游标列是唯一排序依据

			// 添加游标条件：cursorColumn > lastValue
			if lastCursorValue != nil {
				clone.WhereRaw(wrappedCol+" > ?", lastCursorValue)
			}

			// 按游标列排序
			clone.OrderBy(cursorColumn, "ASC")

			sqlStr, args, err := clone.ToSelect()
			if err != nil {
				yield(err)
				return
			}

			rows, err := b.dao.Query(ctx, sqlStr, args...)
			if err != nil {
				yield(err)
				return
			}

			columns, err := rows.Columns()
			if err != nil {
				_ = rows.Close()
				yield(err)
				return
			}

			count := 0
			for rows.Next() {
				values := makeScanValues(columns, fieldInfo, destElem)
				if err := rows.Scan(values...); err != nil {
					_ = rows.Close()
					yield(fmt.Errorf("zcdb: cursor scan failed: %w", err))
					return
				}

				// 提取游标列值：使用安全取值，避免 nil 嵌入指针 panic
				cursorField, ok := fieldByIndexSafe(destElem, cursorIdx)
				if !ok {
					_ = rows.Close()
					yield(ErrCursorFieldUnavailable)
					return
				}
				cursorValue := cursorField.Interface()
				if isNilValue(cursorValue) {
					// 游标列值为 NULL：无法继续分页（条件 cursorColumn > NULL 恒为假），
					// 且若不终止会无限重复返回同一批数据，故直接报错终止
					_ = rows.Close()
					yield(ErrCursorColumnNull)
					return
				}
				count++
				lastCursorValue = cursorValue

				if !yield(nil) {
					_ = rows.Close()
					return
				}
			}
			_ = rows.Close()

			// 不足一批，说明已迭代完毕
			if count < chunkSize {
				return
			}
		}
	}
}

// isNilValue 判断 interface 包裹的值是否为 nil（指针、接口、map、slice、func、chan）。
// 用于识别游标列值为 NULL 的情况：如 *int 字段扫描出 nil 指针后，
// Interface() 返回的类型化 nil 接口，直接与 nil 比较会失真。
func isNilValue(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return rv.IsNil()
	}
	return false
}
