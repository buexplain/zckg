package zcdb

// 本文件包含 Builder 的写入执行方法（终端方法）：
// Insert/Upsert/Update/Increment/Decrement/Delete/DeleteJoin/Truncate 系列，
// 以及无 WHERE 条件的破坏性操作保护（hasEffectiveWhere/hasEffectiveJoin）。
// SELECT 查询执行方法见 builder_query.go。

import (
	"context"
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
//
//	id, err := db.Builder().Table("users").InsertGetId(ctx, &user)
//	// SQL: INSERT INTO `users` (`name`, `age`) VALUES (?, ?)
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
// uniqueBy 为唯一索引列名，updateColumns 为冲突时要更新的列名（为空时更新全部插入列）。
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
// extra 长度为奇数（不成对）时返回 ErrIncrementColumns。
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
// MySQL/PostgreSQL 编译为 TRUNCATE TABLE，SQLite 转为 DELETE FROM 并额外清空
// sqlite_sequence（使自增主键从头开始，表从未使用 AUTOINCREMENT 时忽略该错误）。
//
//	err := db.Builder().Table("users").Truncate(ctx)
//	// MySQL/PG SQL: TRUNCATE TABLE `users`
//	// SQLite SQL:   DELETE FROM "users"
func (b *Builder) Truncate(ctx context.Context) error {
	// SQLite 方言：DELETE FROM 不会重置 AUTOINCREMENT 序列，
	// 需额外清空 sqlite_sequence 使自增主键从头开始；
	// 表从未使用 AUTOINCREMENT 时 sqlite_sequence 表不存在，该错误忽略
	if _, ok := b.grammar.(*SQLiteGrammar); ok {
		_, err := b.dao.Exec(ctx, "DELETE FROM sqlite_sequence WHERE name = ?", b.table)
		if err != nil && !strings.Contains(err.Error(), "no such table") {
			return err
		}
	}

	sqlStr, err := b.ToTruncate()
	if err != nil {
		return err
	}

	_, err = b.dao.Exec(ctx, sqlStr)
	return err
}
