package zcdb

import (
	"context"
	"database/sql"
	"fmt"
	"iter"
	"reflect"
	"strings"
)

// Cursor 返回流式迭代器，通过 *sql.Rows 逐行扫描（一次查询、服务端游标式读取）。
// 适用于中小表或需要精确排序的场景。
// 迭代过程中持有数据库连接，break 时自动释放。
//
// dest 参数要求：
//
//   - 必须是结构体指针（如 *User）
//
//   - 结构体字段通过列映射标签（默认 db，可由 NewDBDao 的 tagName 参数自定义）匹配列名，未匹配的列会被忽略
//
//   - 每次迭代都会覆盖写入同一个 dest，无需在循环内重新声明
//
//     var user User
//     for err := range db.Builder().Table("users").OrderBy("id", "ASC").Cursor(ctx, &user) {
//     if err != nil { log.Fatal(err) }
//     fmt.Println(user.Name)
//     }
//     // SQL: SELECT * FROM `users` ORDER BY `id` ASC（一次执行，逐行扫描）
func (b *Builder) Cursor(ctx context.Context, dest any) iter.Seq[error] {
	return func(yield func(error) bool) {
		sqlStr, args, err := b.ToSelect()
		if err != nil {
			yield(err)
			return
		}

		rows, err := b.query(ctx, sqlStr, args...)
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

		fieldInfo := getScanFieldInfo(destElem.Type(), b.tagName())
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
// 注意：CursorBy 会忽略已设置的 ORDER BY，强制按 cursorColumn ASC或DESC 排序。
//
// 可选参数 desc 为 true 时按游标列倒序分批：条件为 cursorColumn < lastValue，
// 强制按 cursorColumn DESC 排序，适用于从末尾向前的批量处理；
// 省略或为 false 时默认升序 ASC。
//
// dest 参数要求：
//
//   - 必须是结构体指针（如 *User）
//
//   - 结构体字段通过列映射标签（默认 db，可由 NewDBDao 的 tagName 参数自定义）匹配列名，未匹配的列会被忽略
//
//   - 结构体必须包含 cursorColumn 对应的字段（通过标签或字段名匹配）
//
//   - 每次迭代都会覆盖写入同一个 dest，无需在循环内重新声明
//
//     var user User
//     for err := range db.Builder().Table("users").CursorBy(ctx, &user, 100, "id") {
//     if err != nil { log.Fatal(err) }
//     fmt.Println(user.Name)
//     }
//     // 每批 SQL 形如: SELECT * FROM `users` WHERE `id` > ? ORDER BY `id` ASC LIMIT 100（首批无 WHERE 游标条件）
//
// chunkSize 为 0 时直接返回、不执行任何查询；小于 0 时使用默认值 100。
// 游标列值为 NULL 时报 ErrCursorColumnNull 终止（否则条件恒假会无限重复同一批）。
func (b *Builder) CursorBy(ctx context.Context, dest any, chunkSize int, cursorColumn string, desc ...bool) iter.Seq[error] {
	by := len(desc) > 0 && desc[0]
	return func(yield func(error) bool) {
		// chunkSize 为 0 时直接返回，不执行任何查询
		if chunkSize == 0 {
			return
		}
		if chunkSize < 0 {
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
		fieldInfo := getScanFieldInfo(destElem.Type(), b.tagName())
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
		// 限定列名（table.column）场景：取点号后的列名再匹配结构体字段
		if !hasCursorField {
			if idx := strings.LastIndex(cursorColumn, "."); idx >= 0 {
				short := cursorColumn[idx+1:]
				cursorIdx, hasCursorField = fieldInfo.columnIndex[toSnakeCase(short)]
			}
		}
		if !hasCursorField {
			yield(ErrCursorFieldNotFound)
			return
		}

		wrappedCol := b.grammar.WrapColumn(cursorColumn)
		// 倒序分块：条件为 <，排序为 DESC
		cmpOp := ">"
		orderDir := "ASC"
		if by {
			cmpOp = "<"
			orderDir = "DESC"
		}
		var lastCursorValue any

		for {
			// 克隆构造器，避免修改原始状态
			clone := b.Clone()
			clone.limit = chunkSize
			clone.orders = nil // 清空已有排序，确保游标列是唯一排序依据

			// 添加游标条件：cursorColumn > lastValue（倒序为 <）
			if lastCursorValue != nil {
				clone.WhereRaw(wrappedCol+" "+cmpOp+" ?", lastCursorValue)
			}

			// 按游标列排序
			clone.OrderBy(cursorColumn, orderDir)

			sqlStr, args, err := clone.ToSelect()
			if err != nil {
				yield(err)
				return
			}

			rows, err := b.query(ctx, sqlStr, args...)
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
			// 迭代中断（连接错误、ctx 取消等）时 rows.Err() 非 nil，
			// 必须向调用方报告，否则「出错截断」会被误认为「正常迭代完毕」而静默丢数据
			iterErr := rows.Err()
			_ = rows.Close()
			if iterErr != nil {
				yield(iterErr)
				return
			}

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
	default:
	}
	return false
}
