package zcdb

import (
	"context"
	"database/sql"
)

// First 查询第一条记录，扫描到 dest。
// dest 必须是结构体指针（*struct），未找到记录时返回 sql.ErrNoRows。
//
//	err := db.Builder().Table("users").Where("id", "=", 1).First(ctx, &user)
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
	rows, err := b.dao.Query(ctx, sqlStr, args...)
	if err != nil {
		return err
	}
	return ScanStructClose(rows, dest)
}

// Find 查询多条记录，扫描到 dest。
// dest 必须是结构体切片指针（*[]struct）或结构体指针切片指针（*[]*struct）。
//
//	err := db.Builder().Table("users").Where("status", "=", "active").Find(ctx, &users)
func (b *Builder) Find(ctx context.Context, dest any) error {
	sqlStr, args, err := b.ToSelect()
	if err != nil {
		return err
	}

	rows, err := b.dao.Query(ctx, sqlStr, args...)
	if err != nil {
		return err
	}

	return ScanStructClose(rows, dest)
}

// Paginate 分页查询，自动计算总数。
// dest 必须是结构体切片指针（*[]struct）或结构体指针切片指针（*[]*struct）。
//
//	total, err := db.Builder().Table("users").ForPage(1, 20).Paginate(ctx, &users)
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

	rows, err := b.dao.Query(ctx, dataSQL, dataArgs...)
	if err != nil {
		return 0, err
	}

	return total, ScanStructClose(rows, dest)
}

// Count 查询记录总数。
//
//	count, err := db.Builder().Table("users").Where("status", "=", "active").Count(ctx)
func (b *Builder) Count(ctx context.Context) (int, error) {
	sqlStr, args, err := b.ToCount()
	if err != nil {
		return 0, err
	}

	var count int
	rows, err := b.dao.Query(ctx, sqlStr, args...)
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
func (b *Builder) Exists(ctx context.Context) (bool, error) {
	sqlStr, args, err := b.ToExists()
	if err != nil {
		return false, err
	}

	rows, err := b.dao.Query(ctx, sqlStr, args...)
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

// Value 查询单个标量值，扫描到 dest。
// dest 必须是基本类型指针（如 *string、*int、*int64）或 nil 指针（如 **string 用于区分 NULL）。
// 未找到记录时返回 sql.ErrNoRows。
//
//	err := db.Builder().Table("users").Where("id", "=", 1).Value(ctx, &name)
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

	rows, err := b.dao.Query(ctx, sqlStr, args...)
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

// Insert 插入数据，返回受影响行数。
// data 支持以下类型：
//
//   - 单个结构体：struct{}
//
//   - 结构体指针：*struct{}
//
//   - 结构体切片：[]struct{}
//
//   - 结构体指针切片：[]*struct{}
//
//     affected, err := db.Builder().Table("users").Insert(ctx, &user)
func (b *Builder) Insert(ctx context.Context, data any) (int64, error) {
	sqlStr, args, err := b.ToInsert(data)
	if err != nil {
		return 0, err
	}

	result, err := b.dao.Exec(ctx, sqlStr, args...)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// InsertGetId 插入数据并返回自增 ID。
// data 必须是单个结构体（struct{}）或结构体指针（*struct{}），不支持切片。
//
//	id, err := db.Builder().Table("users").InsertGetId(ctx, &user)
func (b *Builder) InsertGetId(ctx context.Context, data any) (int64, error) {
	sqlStr, args, err := b.ToInsert(data)
	if err != nil {
		return 0, err
	}

	result, err := b.dao.Exec(ctx, sqlStr, args...)
	if err != nil {
		return 0, err
	}

	return result.LastInsertId()
}

// InsertOrIgnore 插入数据（忽略冲突），返回受影响行数。
// data 支持类型同 Insert：struct{}、*struct{}、[]struct{}、[]*struct{}。
//
//	affected, err := db.Builder().Table("users").InsertOrIgnore(ctx, &user)
func (b *Builder) InsertOrIgnore(ctx context.Context, data any) (int64, error) {
	sqlStr, args, err := b.ToInsertOrIgnore(data)
	if err != nil {
		return 0, err
	}

	result, err := b.dao.Exec(ctx, sqlStr, args...)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// Upsert 插入或更新数据。
// data 支持类型同 Insert：struct{}、*struct{}、[]struct{}、[]*struct{}。
// uniqueBy 为唯一索引列名，updateColumns 为冲突时要更新的列名。
//
//	affected, err := db.Builder().Table("users").Upsert(ctx, &user, []string{"email"}, []string{"name", "age"})
func (b *Builder) Upsert(ctx context.Context, data any, uniqueBy []string, updateColumns []string) (int64, error) {
	sqlStr, args, err := b.ToUpsert(data, uniqueBy, updateColumns)
	if err != nil {
		return 0, err
	}

	result, err := b.dao.Exec(ctx, sqlStr, args...)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// Update 更新数据，返回受影响行数。
// data 必须是单个结构体（struct{}）或结构体指针（*struct{}），不支持切片。
// 无 WHERE 条件时默认拒绝执行（防误操作全表更新），
// 确需全表更新请显式调用 Force()。
//
//	affected, err := db.Builder().Table("users").Where("id", "=", 1).Update(ctx, &user)
func (b *Builder) Update(ctx context.Context, data any) (int64, error) {
	// 破坏性操作保护：无有效 WHERE/JOIN 限定条件时拒绝执行，需显式 Force() 或 Where("1=1")
	if !b.force && !b.hasEffectiveWhere() && !b.hasEffectiveJoin() {
		return 0, ErrUpdateWithoutWhere
	}

	sqlStr, args, err := b.ToUpdate(data)
	if err != nil {
		return 0, err
	}

	result, err := b.dao.Exec(ctx, sqlStr, args...)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// Delete 删除数据，返回受影响行数。
// 无 WHERE 条件时默认拒绝执行（防误操作全表删除），
// 确需全表删除请显式调用 Force()。
//
//	affected, err := db.Builder().Table("users").Where("id", "=", 1).Delete(ctx)
func (b *Builder) Delete(ctx context.Context) (int64, error) {
	// 破坏性操作保护：无有效 WHERE/JOIN 限定条件时拒绝执行，需显式 Force() 或 Where("1=1")
	if !b.force && !b.hasEffectiveWhere() && !b.hasEffectiveJoin() {
		return 0, ErrDeleteWithoutWhere
	}

	sqlStr, args, err := b.ToDelete()
	if err != nil {
		return 0, err
	}

	result, err := b.dao.Exec(ctx, sqlStr, args...)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// hasEffectiveWhere 判断是否存在实际生效的 WHERE 条件。
// 排除空嵌套（如 WhereNested 传入空回调）等编译后为空的伪条件，
// 防止空嵌套绕过无 WHERE 保护导致全表删除/更新。
func (b *Builder) hasEffectiveWhere() bool {
	for _, w := range b.wheres {
		switch w.Type {
		case WhereTypeNested, WhereTypeExists, WhereTypeNotExists:
			if w.Nested != nil && w.Nested.hasEffectiveWhere() {
				return true
			}
		default:
			return true
		}
	}
	return false
}

// hasEffectiveJoin 判断是否存在带条件的 JOIN。
// UPDATE/DELETE 中 JOIN 的 ON/Where 条件同样限定操作范围，
// 视为有效限定条件；无条件 JOIN 会产生笛卡尔积，不视为限定。
func (b *Builder) hasEffectiveJoin() bool {
	for _, j := range b.joins {
		if len(j.Conditions) > 0 {
			return true
		}
	}
	return false
}

// Truncate 清空表。
//
//	err := db.Builder().Table("users").Truncate(ctx)
func (b *Builder) Truncate(ctx context.Context) error {
	sqlStr, err := b.ToTruncate()
	if err != nil {
		return err
	}

	_, err = b.dao.Exec(ctx, sqlStr)
	return err
}
