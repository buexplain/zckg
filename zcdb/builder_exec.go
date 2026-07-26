package zcdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// First 查询第一条记录，扫描到 dest（*struct 指针）。返回是否有数据。
func (b *Builder) First(ctx context.Context, dest any) (bool, error) {
	// 克隆并设置 LIMIT 1，避免修改原 Builder
	clone := b.Clone()
	clone.limit = 1

	sqlStr, args, err := clone.ToSelect()
	if err != nil {
		return false, err
	}

	rows, err := b.dao.Query(ctx, sqlStr, args...)
	if err != nil {
		return false, err
	}

	err = Scan(rows, dest)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Find 查询多条记录，扫描到 dest（*[]struct 或 *[]*struct 指针）。
func (b *Builder) Find(ctx context.Context, dest any) error {
	sqlStr, args, err := b.ToSelect()
	if err != nil {
		return err
	}

	rows, err := b.dao.Query(ctx, sqlStr, args...)
	if err != nil {
		return err
	}

	return Scan(rows, dest)
}

// Paginate 分页查询，自动计算总数。
// dest 为 *[]struct 或 *[]*struct 指针
func (b *Builder) Paginate(ctx context.Context, dest any) (totalCount int, err error) {
	// 先查总数：克隆 Builder，移除排序和分页，编译 COUNT
	countBuilder := b.Clone()
	countBuilder.orders = nil
	countBuilder.limit = 0
	countBuilder.offset = 0
	countBuilder.columns = nil

	countSQL, countArgs, err := countBuilder.ToCount()
	if err != nil {
		return 0, err
	}

	// 执行 COUNT 查询
	countRows, err := b.dao.Query(ctx, countSQL, countArgs...)
	if err != nil {
		return 0, err
	}
	var total int
	if err = scanScalar(countRows, &total); err != nil {
		return 0, err
	}
	// 如果总数为 0，直接返回
	if total == 0 {
		return 0, nil
	}

	if b.limit < 1 || b.offset < 1 {
		b.ForPage(1, 20)
	}

	dataSQL, dataArgs, err := b.ToSelect()
	if err != nil {
		return 0, err
	}

	rows, err := b.dao.Query(ctx, dataSQL, dataArgs...)
	if err != nil {
		return 0, err
	}

	return 0, Scan(rows, dest)
}

// Count 查询记录总数。
func (b *Builder) Count(ctx context.Context) (int, error) {
	sqlStr, args, err := b.ToCount()
	if err != nil {
		return 0, err
	}

	rows, err := b.dao.Query(ctx, sqlStr, args...)
	if err != nil {
		return 0, err
	}

	var count int
	if err := scanScalar(rows, &count); err != nil {
		return 0, fmt.Errorf("zcdb: count query failed: %w", err)
	}
	return count, nil
}

// Exists 判断是否有记录。
func (b *Builder) Exists(ctx context.Context) (bool, error) {
	clone := b.Clone()
	clone.limit = 1
	clone.columns = nil

	sqlStr, args, err := clone.ToSelect()
	if err != nil {
		return false, err
	}

	rows, err := b.dao.Query(ctx, sqlStr, args...)
	if err != nil {
		return false, err
	}
	defer func() {
		_ = rows.Close()
	}()

	return rows.Next(), rows.Err()
}

// Value 查询单个标量值，扫描到 dest 指针中。
func (b *Builder) Value(ctx context.Context, dest any) error {
	clone := b.Clone()
	clone.limit = 1

	sqlStr, args, err := clone.ToSelect()
	if err != nil {
		return err
	}

	rows, err := b.dao.Query(ctx, sqlStr, args...)
	if err != nil {
		return err
	}

	return scanScalar(rows, dest)
}

// Insert 插入数据，返回受影响行数。
// data 可以是结构体、结构体指针、结构体切片或结构体指针切片。
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
// data 必须是单个结构体或结构体指针。
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
// uniqueBy 为唯一索引列名，updateColumns 为冲突时要更新的列名。
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
func (b *Builder) Update(ctx context.Context, data any) (int64, error) {
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
func (b *Builder) Delete(ctx context.Context) (int64, error) {
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

// Truncate 清空表。
func (b *Builder) Truncate(ctx context.Context) error {
	sqlStr, err := b.ToTruncate()
	if err != nil {
		return err
	}

	_, err = b.dao.Exec(ctx, sqlStr)
	return err
}

// ==================== 内部辅助方法 ====================

// ToCount 编译 COUNT 查询（内部使用）。
// 通过复用 CompileSelect 生成 SELECT COUNT(*) FROM ... 语句。
func (b *Builder) ToCount() (string, []any, error) {
	if b.table == "" && b.fromSub == nil {
		return "", nil, ErrEmptyTable
	}

	// 保存原始列并设置为 COUNT(*)，清除 SELECT 子查询
	origColumns := b.columns
	origSelectSubs := b.selectSubs
	b.columns = []string{"COUNT(*)"}
	b.selectSubs = nil

	sqlStr := b.grammar.CompileSelect(b, b.columns)
	// collectSelectBindings 会收集 FROM_SUB → JOIN → WHERE → HAVING → UNION 的绑定参数
	// 由于 selectSubs 已清空，不会包含 SELECT 子查询的参数
	args := b.collectSelectBindings()

	b.columns = origColumns
	b.selectSubs = origSelectSubs

	return sqlStr, args, nil
}

// scanScalar 从 *sql.Rows 中扫描单个标量值。
func scanScalar(rows *sql.Rows, dest any) error {
	defer func() {
		_ = rows.Close()
	}()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}

	if err := rows.Scan(dest); err != nil {
		return err
	}

	return rows.Err()
}
