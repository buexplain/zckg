package zcdb

// 本文件包含 Builder 的写入执行方法（终端方法）：
// Insert/Upsert/Update/Increment/Decrement/Delete/DeleteJoin/Truncate 系列，
// 以及无 WHERE 条件的破坏性操作保护（hasEffectiveWhere/hasEffectiveJoin）。
// SELECT 查询执行方法见 builder_query.go。

import (
	"context"
	"errors"
	"strings"
)

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
// 字段映射与值处理规则见 ToInsert（db 标签/nil 跳过/指针解引用）。
//
//	affected, err := db.Builder().Table("users").Insert(ctx, &user)
//	// SQL: INSERT INTO `users` (`name`, `age`) VALUES (?, ?)
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
// 不支持 PostgreSQL 方言：lib/pq 不支持 LastInsertId（PG 需 RETURNING 子句），
// 为避免「插入成功但返回错误」的半成功状态，PG 下在执行前直接返回错误。
//
//	id, err := db.Builder().Table("users").InsertGetId(ctx, &user)
//	// SQL: INSERT INTO `users` (`name`, `age`) VALUES (?, ?)
func (b *Builder) InsertGetId(ctx context.Context, data any) (int64, error) {
	// 先做数据校验与编译（参数错误优先于方言限制返回）
	sqlStr, args, err := b.ToInsert(data)
	if err != nil {
		return 0, err
	}

	// lib/pq 不支持 LastInsertId：在执行前返回错误，避免「插入成功但返回错误」的半成功状态
	if _, ok := b.grammar.(*PostgresGrammar); ok {
		return 0, errors.New("zcdb: InsertGetId is not supported on postgres dialect (lib/pq does not support LastInsertId); use Insert or raw SQL with RETURNING instead")
	}

	result, err := b.dao.Exec(ctx, sqlStr, args...)
	if err != nil {
		return 0, err
	}

	return result.LastInsertId()
}

// InsertUsing 将 SELECT 子查询的结果插入目标表，返回受影响行数。
// columns 为目标表的列名列表，callback 用于构建 SELECT 子查询。
//
//	affected, err := db.Builder().Table("users_archive").
//	    InsertUsing(ctx, []string{"name", "age"}, func(sub *Builder) {
//	        sub.Table("users").Select("name", "age").Where("status", "=", "active")
//	    })
//	// SQL: INSERT INTO `users_archive` (`name`, `age`) SELECT `name`, `age` FROM `users` WHERE `status` = ?
func (b *Builder) InsertUsing(ctx context.Context, columns []string, callback func(*Builder)) (int64, error) {
	sqlStr, args, err := b.ToInsertUsing(columns, callback)
	if err != nil {
		return 0, err
	}

	result, err := b.dao.Exec(ctx, sqlStr, args...)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// InsertOrIgnoreUsing 将 SELECT 子查询的结果插入目标表（冲突时静默跳过），返回受影响行数。
// 参数与语义同 InsertUsing，仅冲突处理不同（方言差异见 ToInsertOrIgnoreUsing）。
//
//	affected, err := db.Builder().Table("users_archive").
//	    InsertOrIgnoreUsing(ctx, []string{"name", "age"}, func(sub *Builder) {
//	        sub.Table("users").Select("name", "age")
//	    })
//	// MySQL SQL: INSERT IGNORE INTO `users_archive` (`name`, `age`) SELECT `name`, `age` FROM `users`
func (b *Builder) InsertOrIgnoreUsing(ctx context.Context, columns []string, callback func(*Builder)) (int64, error) {
	sqlStr, args, err := b.ToInsertOrIgnoreUsing(columns, callback)
	if err != nil {
		return 0, err
	}

	result, err := b.dao.Exec(ctx, sqlStr, args...)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// InsertOrIgnore 插入数据（忽略冲突），返回受影响行数。
// data 支持类型同 Insert：struct{}、*struct{}、[]struct{}、[]*struct{}。
// 方言差异见 ToInsertOrIgnore（MySQL INSERT IGNORE/PG ON CONFLICT DO NOTHING/SQLite INSERT OR IGNORE）。
//
//	affected, err := db.Builder().Table("users").InsertOrIgnore(ctx, &user)
//	// MySQL SQL: INSERT IGNORE INTO `users` (`name`, `age`) VALUES (?, ?)
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
// uniqueBy 为唯一索引列名，updateColumns 为冲突时要更新的列名（为空时更新全部插入列，排除 uniqueBy 列）。
// 方言差异见 ToUpsert；PostgreSQL/SQLite 下 uniqueBy 为空报错。
//
//	affected, err := db.Builder().Table("users").Upsert(ctx, &user, []string{"email"}, []string{"name", "age"})
//	// MySQL SQL: INSERT INTO `users` (`name`, `age`, `email`) VALUES (?, ?, ?)
//	//            ON DUPLICATE KEY UPDATE `name` = VALUES(`name`), `age` = VALUES(`age`)
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
//	// SQL: UPDATE `users` SET `name` = ?, `age` = ? WHERE `id` = ?
func (b *Builder) Update(ctx context.Context, data any) (int64, error) {
	// 破坏性操作保护：无有效 WHERE/JOIN 限定条件时拒绝执行，需显式 Force() 或 WhereRaw("1=1")
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

// Increment 原子自增指定列，返回受影响行数。
// extra 可交替传入更多列与增量（IncrementEach 语义）：
//
//	Increment(ctx, "wallet", 100, "level", 1)
//	// SQL: UPDATE `users` SET `wallet` = `wallet` + ?, `level` = `level` + ? WHERE ...
//
// 无 WHERE 条件时默认拒绝执行（同 Update），确需全表自增请显式 Force()。
func (b *Builder) Increment(ctx context.Context, column string, amount any, extra ...any) (int64, error) {
	columns, amounts, err := parseIncDecArgs(column, amount, extra)
	if err != nil {
		return 0, err
	}
	// 破坏性操作保护：复用 Update 的无 WHERE 拒绝机制
	if !b.force && !b.hasEffectiveWhere() && !b.hasEffectiveJoin() {
		return 0, ErrUpdateWithoutWhere
	}

	sqlStr, args, err := b.ToIncrement(columns, amounts)
	if err != nil {
		return 0, err
	}

	result, err := b.dao.Exec(ctx, sqlStr, args...)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// Decrement 原子自减指定列，返回受影响行数。参数规则与保护机制同 Increment。
//
//	affected, err := db.Builder().Table("users").Where("id", "=", 1).Decrement(ctx, "wallet", 50)
//	// SQL: UPDATE `users` SET `wallet` = `wallet` - ? WHERE `id` = ?
func (b *Builder) Decrement(ctx context.Context, column string, amount any, extra ...any) (int64, error) {
	columns, amounts, err := parseIncDecArgs(column, amount, extra)
	if err != nil {
		return 0, err
	}
	// 破坏性操作保护：复用 Update 的无 WHERE 拒绝机制
	if !b.force && !b.hasEffectiveWhere() && !b.hasEffectiveJoin() {
		return 0, ErrUpdateWithoutWhere
	}

	sqlStr, args, err := b.ToDecrement(columns, amounts)
	if err != nil {
		return 0, err
	}

	result, err := b.dao.Exec(ctx, sqlStr, args...)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// parseIncDecArgs 解析 Increment/Decrement 参数：
// 首个 (column, amount) 加上 extra 交替传入的 (column, amount) 对，
// extra 长度为奇数（不成对）或 extra 中列名位置元素非 string 类型时返回 ErrIncrementColumns。
func parseIncDecArgs(column string, amount any, extra []any) ([]string, []any, error) {
	if len(extra)%2 != 0 {
		return nil, nil, ErrIncrementColumns
	}
	columns := make([]string, 0, 1+len(extra)/2)
	amounts := make([]any, 0, 1+len(extra)/2)
	columns = append(columns, column)
	amounts = append(amounts, amount)
	for i := 0; i < len(extra); i += 2 {
		col, ok := extra[i].(string)
		if !ok {
			return nil, nil, ErrIncrementColumns
		}
		columns = append(columns, col)
		amounts = append(amounts, extra[i+1])
	}
	return columns, amounts, nil
}

// Delete 删除数据，返回受影响行数。
// 无 WHERE 条件时默认拒绝执行（防误操作全表删除），
// 确需全表删除请显式调用 Force()。
//
//	affected, err := db.Builder().Table("users").Where("id", "=", 1).Delete(ctx)
//	// SQL: DELETE FROM `users` WHERE `id` = ?
func (b *Builder) Delete(ctx context.Context) (int64, error) {
	// 破坏性操作保护：无有效 WHERE/JOIN 限定条件时拒绝执行，需显式 Force() 或 WhereRaw("1=1")
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

// DeleteJoin 按关联条件删除主表行，返回受影响行数。
// 通过 JoinOn/Join 等链式调用指定关联，可配合 Where 追加过滤条件：
//
//	affected, err := db.Builder().Table("users").
//	    JoinOn("orders", func(j *zcdb.JoinBuilder) { j.On("orders.user_id", "=", "users.id") }).
//	    Where("orders.status", "=", "cancelled").
//	    DeleteJoin(ctx)
//	// MySQL SQL:  DELETE `users` FROM `users` INNER JOIN `orders` ON ... WHERE `orders`.`status` = ?
//	// PG SQL:     DELETE FROM "users" USING "orders" WHERE ...
//	// SQLite SQL: DELETE FROM "users" WHERE "id" IN (SELECT ...)
//
// 无 WHERE 条件时默认拒绝执行（同 Delete）；带条件的 JOIN 本身视为有效限定。
func (b *Builder) DeleteJoin(ctx context.Context) (int64, error) {
	// 破坏性操作保护：复用 Delete 的无 WHERE 拒绝机制（hasEffectiveJoin 已覆盖 join 限定场景）
	if !b.force && !b.hasEffectiveWhere() && !b.hasEffectiveJoin() {
		return 0, ErrDeleteWithoutWhere
	}

	sqlStr, args, err := b.ToDeleteJoin()
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
// 排除空嵌套（如 WhereNested 传入空回调）、空 WhereRaw（编译后为空串）等伪条件，
// 防止空条件绕过无 WHERE 保护导致全表删除/更新。
func (b *Builder) hasEffectiveWhere() bool {
	for _, w := range b.wheres {
		switch w.Type {
		case WhereTypeNested, WhereTypeExists, WhereTypeNotExists:
			if w.Nested != nil && w.Nested.hasEffectiveWhere() {
				return true
			}
		case WhereTypeRaw:
			// 空串/纯空白 Raw 编译后会被跳过，不产生任何条件，不得计为有效限定
			if strings.TrimSpace(w.SQL) != "" {
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
// MySQL/PostgreSQL 编译为 TRUNCATE TABLE，SQLite 转为 DELETE FROM 并额外清空
// sqlite_sequence（使自增主键从头开始；表从未使用 AUTOINCREMENT 时该表不存在，
// 经 sqlite_master 预查询确认后跳过清理）。
//
//	err := db.Builder().Table("users").Truncate(ctx)
//	// MySQL SQL: TRUNCATE TABLE `users`
//	// PG SQL:    TRUNCATE TABLE "users" RESTART IDENTITY
//	// SQLite SQL: DELETE FROM "users"
func (b *Builder) Truncate(ctx context.Context) error {
	sqlStr, err := b.ToTruncate()
	if err != nil {
		return err
	}

	// SQLite 方言：DELETE FROM 不会重置 AUTOINCREMENT 序列，需额外清空 sqlite_sequence。
	// 两步（含预查询共三步）包进同一事务，避免“数据已删但序列未重置”的中间状态；
	// 调用方 ctx 已携带事务时经 Transaction 嵌套传播自动并入外层事务。
	// 顺序不可颠倒：先清序列则主语句失败时序列已丢、状态不一致；
	// 清理前先查 sqlite_master 确认 sqlite_sequence 存在：表从未使用 AUTOINCREMENT 时
	// 该表不存在，直接跳过清理——不依赖驱动错误文案（错误文案随驱动版本变化，
	// 且其它真实错误可能恰好含相同子串而被误吞）；清理失败是真实错误，必须如实上报。
	if _, ok := b.grammar.(*SQLiteGrammar); ok {
		return b.dao.Transaction(ctx, func(txCtx context.Context) error {
			if _, err := b.dao.Exec(txCtx, sqlStr); err != nil {
				return err
			}
			exists, err := b.sqliteSequenceExists(txCtx)
			if err != nil {
				return err
			}
			if exists {
				if _, err := b.dao.Exec(txCtx, "DELETE FROM sqlite_sequence WHERE name = ?", b.table); err != nil {
					return err
				}
			}
			return nil
		})
	}

	_, err = b.dao.Exec(ctx, sqlStr)
	return err
}

// sqliteSequenceExists 查询 sqlite_master 判断 sqlite_sequence 表是否存在。
// SQLite 仅在含 AUTOINCREMENT 列的表被写入后才创建 sqlite_sequence 表。
func (b *Builder) sqliteSequenceExists(ctx context.Context) (bool, error) {
	rows, err := b.dao.Query(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='sqlite_sequence'")
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()

	var count int
	if rows.Next() {
		if err := rows.Scan(&count); err != nil {
			return false, err
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return count > 0, nil
}
