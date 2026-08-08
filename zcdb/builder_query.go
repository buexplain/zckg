package zcdb

// 本文件包含 Builder 的 SELECT 查询执行方法（终端方法）：
// First/Find/Pluck/Paginate/Count/Exists/Value 与聚合 Max/Min/Sum/Avg，
// 以及内部统一查询入口 query（带锁查询强制走写连接）。

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
)

// query 执行 SELECT 查询的内部统一入口：
// 带锁查询（lockClause 非空）强制走写（主库）连接，
// 读写分离下锁查询打到从库会报错或锁不生效；无锁查询正常路由读库。
func (b *Builder) query(ctx context.Context, sqlStr string, args ...any) (*sql.Rows, error) {
	if b.lockClause != "" {
		return b.dao.QueryPrimary(ctx, sqlStr, args...)
	}
	return b.dao.Query(ctx, sqlStr, args...)
}

// First 查询第一条记录，扫描到 dest。
// dest 必须是结构体指针（*struct），字段按列映射标签（默认 db，可由 NewDBDao 的 tagName 参数自定义）匹配列名；未找到记录时返回 sql.ErrNoRows。
// 内部克隆并强制 LIMIT 1，不修改原 Builder。
//
//	var user User
//	err := db.Builder().Table("users").Where("id", "=", 1).First(ctx, &user)
//	// SQL: SELECT * FROM `users` WHERE `id` = ? LIMIT 1
func (b *Builder) First(ctx context.Context, dest any) error {
	// 克隆并设置 LIMIT 1，避免修改原 Builder
	var clone *Builder
	if b.limit == 1 {
		clone = b
	} else {
		clone = b.Clone()
		clone.limit = 1
	}

	sqlStr, args, err := clone.ToSelect()
	if err != nil {
		return err
	}
	rows, err := b.query(ctx, sqlStr, args...)
	if err != nil {
		return err
	}
	return ScanStructClose(rows, dest, b.tagName())
}

// Find 查询多条记录，扫描到 dest。
// dest 必须是结构体切片指针（*[]struct）或结构体指针切片指针（*[]*struct）；
// 无结果时 dest 为空切片、err 为 nil（与 First 的 ErrNoRows 语义不同）。
//
//	var users []User
//	err := db.Builder().Table("users").Where("status", "=", "active").Find(ctx, &users)
//	// SQL: SELECT * FROM `users` WHERE `status` = ?
func (b *Builder) Find(ctx context.Context, dest any) error {
	sqlStr, args, err := b.ToSelect()
	if err != nil {
		return err
	}

	rows, err := b.query(ctx, sqlStr, args...)
	if err != nil {
		return err
	}

	return ScanStructClose(rows, dest, b.tagName())
}

// Pluck 提取查询结果中的列值到目标容器。
// dest 为切片指针时提取单列（第一列）：
//
//	var names []string
//	err := db.Builder().Table("users").OrderBy("id", "ASC").Pluck(ctx, &names, "name")
//
// dest 为 map 指针时提取键值对（第一列为值、第二列为键）：
//
//	var m map[int64]string
//	err := db.Builder().Table("users").Pluck(ctx, &m, "name", "id") // id => name
//
// map 值为结构体/结构体指针时进入键列模式（keyBy）：唯一列参数作为键列，整行数据按列映射标签扫描进结构体：
//
//	var m map[int64]User // id => User 整行
//	err := db.Builder().Table("users").Pluck(ctx, &m, "id")
//
// NULL 值扫描为零值（与 Find 一致）；map 模式重复键后者覆盖前者，键列为 NULL 时使用零值键。
// Pluck 会覆盖 Builder 当前的 SELECT 列。
func (b *Builder) Pluck(ctx context.Context, dest any, columns ...string) error {
	destValue := reflect.ValueOf(dest)
	if destValue.Kind() != reflect.Ptr || destValue.IsNil() {
		return ErrPluckDest
	}
	destValue = destValue.Elem()

	switch destValue.Kind() {
	case reflect.Slice:
		if len(columns) != 1 {
			return ErrPluckColumns
		}
	case reflect.Map:
		// nil map 初始化，避免 SetMapIndex panic
		if destValue.IsNil() {
			destValue.Set(reflect.MakeMap(destValue.Type()))
		}
		// map 值类型为结构体/结构体指针时进入键列模式（keyBy）：
		// 唯一列参数作为键列，整行数据按 db tag 扫描进结构体
		valType := destValue.Type().Elem()
		if valType.Kind() == reflect.Ptr {
			valType = valType.Elem()
		}
		if valType.Kind() == reflect.Struct {
			if len(columns) != 1 {
				return ErrPluckColumns
			}
			return b.pluckKeyBy(ctx, destValue, columns[0])
		}
		if len(columns) != 2 {
			return ErrPluckColumns
		}
	default:
		return ErrPluckDest
	}

	// 只查询需要的列（覆盖既有 SELECT）
	b.Select(columns...)
	sqlStr, args, err := b.ToSelect()
	if err != nil {
		return err
	}

	rows, err := b.query(ctx, sqlStr, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		if destValue.Kind() == reflect.Slice {
			elem := reflect.New(destValue.Type().Elem()).Elem()
			if err := rows.Scan(&nullSafeField{field: elem}); err != nil {
				return fmt.Errorf("zcdb: pluck scan row failed: %w", err)
			}
			destValue.Set(reflect.Append(destValue, elem))
			continue
		}
		// map 模式：第一列为值、第二列为键
		val := reflect.New(destValue.Type().Elem()).Elem()
		key := reflect.New(destValue.Type().Key()).Elem()
		if err := rows.Scan(&nullSafeField{field: val}, &nullSafeField{field: key}); err != nil {
			return fmt.Errorf("zcdb: pluck scan row failed: %w", err)
		}
		destValue.SetMapIndex(key, val)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return nil
}

// pluckKeyBy 键列模式（keyBy）：map 值为结构体/结构体指针时，columns 唯一元素作为键列，
// 整行数据按 db tag 扫描进结构体后以键列值作为 map 键。
// 键列若已在结构体字段中则不重复 SELECT，直接复用字段扫描值；否则追加为 SELECT 最后一列。
func (b *Builder) pluckKeyBy(ctx context.Context, destMap reflect.Value, keyColumn string) error {
	valType := destMap.Type().Elem()
	isPtr := valType.Kind() == reflect.Ptr
	elemType := valType
	if isPtr {
		elemType = elemType.Elem()
	}

	info := parseStruct(elemType, b.tagName())
	if info == nil || len(info.Fields) == 0 {
		return ErrNoFields
	}

	// SELECT 列 = 结构体字段列（按字段顺序）+ 键列（若不在字段列中，追加到末尾）
	columns := make([]string, 0, len(info.Fields)+1)
	keyIndex := -1 // 键列在 SELECT 列中的位置
	for i, f := range info.Fields {
		columns = append(columns, f.Column)
		if f.Column == keyColumn {
			keyIndex = i
		}
	}
	if keyIndex < 0 {
		keyIndex = len(columns)
		columns = append(columns, keyColumn)
	}

	b.Select(columns...)
	sqlStr, args, err := b.ToSelect()
	if err != nil {
		return err
	}

	rows, err := b.query(ctx, sqlStr, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	fieldInfo := getScanFieldInfo(elemType, b.tagName())
	for rows.Next() {
		elem := reflect.New(elemType).Elem()
		values := makeScanValues(columns, fieldInfo, elem)
		// 键列不在结构体字段中时，将其扫描目标替换为 key（在字段中时扫描后从字段取值）
		key := reflect.New(destMap.Type().Key()).Elem()
		keyInFields := keyIndex < len(info.Fields)
		if !keyInFields {
			values[keyIndex] = &nullSafeField{field: key}
		}
		if err := rows.Scan(values...); err != nil {
			return fmt.Errorf("zcdb: pluck scan row failed: %w", err)
		}
		// 键列在结构体字段中：扫描完成后从字段取值（类型做可转换检查）
		if keyInFields {
			kf, ok := fieldByIndexSafe(elem, info.Fields[keyIndex].Index)
			if ok && kf.Type().ConvertibleTo(key.Type()) {
				key.Set(kf.Convert(key.Type()))
			}
		}
		if isPtr {
			ptr := reflect.New(elemType)
			ptr.Elem().Set(elem)
			destMap.SetMapIndex(key, ptr)
		} else {
			destMap.SetMapIndex(key, elem)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return nil
}

// Paginate 分页查询，自动计算总数。
// dest 必须是结构体切片指针（*[]struct）或结构体指针切片指针（*[]*struct）。
// 内部先克隆并清除排序/分页/列后执行 COUNT，总数为 0 时不再执行数据查询；
// 分页范围请用 ForPage/Limit+Offset 预设。
//
//	var users []User
//	total, err := db.Builder().Table("users").ForPage(1, 20).Paginate(ctx, &users)
//	// COUNT SQL: SELECT COUNT(*) FROM `users`
//	// 数据 SQL:  SELECT * FROM `users` LIMIT 20
func (b *Builder) Paginate(ctx context.Context, dest any) (totalCount int, err error) {
	// 执行 COUNT 查询
	c := b.Clone()
	c.orders = nil
	c.limit = 0
	c.offset = 0
	c.columns = nil
	total, err := c.Count(ctx)
	if err != nil {
		return 0, err
	}

	// 如果总数为 0，直接返回
	if total == 0 {
		return 0, nil
	}

	dataSQL, dataArgs, err := b.ToSelect()
	if err != nil {
		return 0, err
	}

	rows, err := b.query(ctx, dataSQL, dataArgs...)
	if err != nil {
		return 0, err
	}

	return total, ScanStructClose(rows, dest, b.tagName())
}

// Count 查询记录总数。
// 包含 UNION/GROUP BY/DISTINCT 时自动包裹子查询计数（见 ToCount）。
//
//	count, err := db.Builder().Table("users").Where("status", "=", "active").Count(ctx)
//	// SQL: SELECT COUNT(*) FROM `users` WHERE `status` = ?
func (b *Builder) Count(ctx context.Context) (int, error) {
	sqlStr, args, err := b.ToCount()
	if err != nil {
		return 0, err
	}

	var count int
	rows, err := b.query(ctx, sqlStr, args...)
	if err != nil {
		return 0, err
	}
	defer func(rows *sql.Rows) {
		_ = rows.Close()
	}(rows)
	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			return 0, err
		}
	}
	return count, rows.Err()
}

// Exists 判断是否有记录。
// 走 SELECT 1 ... LIMIT 1，找到第一条记录即返回，避免 COUNT(*) 全表计数。
//
//	exists, err := db.Builder().Table("users").Where("id", "=", 1).Exists(ctx)
//	// SQL: SELECT 1 FROM `users` WHERE `id` = ? LIMIT 1
func (b *Builder) Exists(ctx context.Context) (bool, error) {
	sqlStr, args, err := b.ToExists()
	if err != nil {
		return false, err
	}

	rows, err := b.query(ctx, sqlStr, args...)
	if err != nil {
		return false, err
	}
	defer func(rows *sql.Rows) {
		_ = rows.Close()
	}(rows)

	// 有行即存在
	exists := rows.Next()
	return exists, rows.Err()
}

// Max 查询指定列的最大值。
// 空表/无匹配行时返回 (0, sql.ErrNoRows)（对齐 First 未命中语义）。
//
//	maxAge, err := db.Builder().Table("users").Max(ctx, "age")
//	// SQL: SELECT MAX(`age`) AS `aggregate` FROM `users`
func (b *Builder) Max(ctx context.Context, column string) (float64, error) {
	return b.aggregate(ctx, "MAX", column)
}

// Min 查询指定列的最小值。空表语义同 Max。
//
//	minAge, err := db.Builder().Table("users").Min(ctx, "age")
//	// SQL: SELECT MIN(`age`) AS `aggregate` FROM `users`
func (b *Builder) Min(ctx context.Context, column string) (float64, error) {
	return b.aggregate(ctx, "MIN", column)
}

// Sum 查询指定列的总和。空表/无匹配行时返回 0（SQL SUM 对空集聚合返回 NULL，此处归一为 0）。
//
//	total, err := db.Builder().Table("orders").Where("status", "=", "paid").Sum(ctx, "amount")
//	// SQL: SELECT SUM(`amount`) AS `aggregate` FROM `orders` WHERE `status` = ?
func (b *Builder) Sum(ctx context.Context, column string) (float64, error) {
	return b.aggregate(ctx, "SUM", column)
}

// Avg 查询指定列的平均值。空表语义同 Sum。
//
//	avgAge, err := db.Builder().Table("users").Avg(ctx, "age")
//	// SQL: SELECT AVG(`age`) AS `aggregate` FROM `users`
func (b *Builder) Avg(ctx context.Context, column string) (float64, error) {
	return b.aggregate(ctx, "AVG", column)
}

// aggregate Max/Min/Sum/Avg 共用实现：ToAggregate + 单行 Scan。
// MAX/MIN 结果为 NULL 时返回 sql.ErrNoRows；SUM/AVG 结果为 NULL 时返回 0。
func (b *Builder) aggregate(ctx context.Context, fn string, column string) (float64, error) {
	sqlStr, args, err := b.ToAggregate(fn, column)
	if err != nil {
		return 0, err
	}

	rows, err := b.query(ctx, sqlStr, args...)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, err
		}
		return 0, sql.ErrNoRows
	}
	var result sql.NullFloat64
	if err := rows.Scan(&result); err != nil {
		return 0, err
	}
	if !result.Valid {
		// SUM/AVG 空集聚合返回 NULL → 0；MAX/MIN 无值 → ErrNoRows
		if fn == "MAX" || fn == "MIN" {
			return 0, sql.ErrNoRows
		}
		return 0, nil
	}
	return result.Float64, rows.Err()
}

// Value 查询单个标量值，扫描到 dest。
// dest 必须是基本类型指针（如 *string、*int、*int64）或 nil 指针（如 **string 用于区分 NULL）。
// 未找到记录时返回 sql.ErrNoRows；内部克隆并强制 LIMIT 1，不修改原 Builder。
//
//	var name string
//	err := db.Builder().Table("users").Select("name").Where("id", "=", 1).Value(ctx, &name)
//	// SQL: SELECT `name` FROM `users` WHERE `id` = ? LIMIT 1
func (b *Builder) Value(ctx context.Context, dest any) error {
	var clone *Builder
	if b.limit == 1 {
		clone = b
	} else {
		clone = b.Clone()
		clone.limit = 1
	}
	sqlStr, args, err := clone.ToSelect()
	if err != nil {
		return err
	}

	rows, err := b.query(ctx, sqlStr, args...)
	if err != nil {
		return err
	}
	defer func(rows *sql.Rows) {
		_ = rows.Close()
	}(rows)
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	return rows.Scan(dest)
}
