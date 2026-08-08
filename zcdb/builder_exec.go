package zcdb

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
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
	rows, err := b.query(ctx, sqlStr, args...)
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

	rows, err := b.query(ctx, sqlStr, args...)
	if err != nil {
		return err
	}

	return ScanStructClose(rows, dest)
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
// map 值为结构体/结构体指针时进入键列模式（keyBy）：唯一列参数作为键列，整行数据按 db tag 扫描进结构体：
//
//	var m map[int64]User // id => User 整行
//	err := db.Builder().Table("users").Pluck(ctx, &m, "id")
//
// NULL 值扫描为零值（与 Find 一致）；map 模式重复键后者覆盖前者，键列为 NULL 时使用零值键。
// Pluck 会覆盖 Builder 当前的 SELECT 列（与 Laravel pluck 语义一致）。
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

	info := parseStruct(elemType)
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

	fieldInfo := getScanFieldInfo(elemType)
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

	rows, err := b.query(ctx, dataSQL, dataArgs...)
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
func (b *Builder) Max(ctx context.Context, column string) (float64, error) {
	return b.aggregate(ctx, "MAX", column)
}

// Min 查询指定列的最小值。空表语义同 Max。
func (b *Builder) Min(ctx context.Context, column string) (float64, error) {
	return b.aggregate(ctx, "MIN", column)
}

// Sum 查询指定列的总和。空表/无匹配行时返回 0（SQL SUM 对空集聚合返回 NULL，此处归一为 0）。
func (b *Builder) Sum(ctx context.Context, column string) (float64, error) {
	return b.aggregate(ctx, "SUM", column)
}

// Avg 查询指定列的平均值。空表语义同 Sum。
func (b *Builder) Avg(ctx context.Context, column string) (float64, error) {
	return b.aggregate(ctx, "AVG", column)
}

// Average 是 Avg 的别名。
func (b *Builder) Average(ctx context.Context, column string) (float64, error) {
	return b.Avg(ctx, column)
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

// InsertUsing 将 SELECT 子查询的结果插入目标表，返回受影响行数。
// columns 为目标表的列名列表，callback 用于构建 SELECT 子查询。
//
//	affected, err := db.Builder().Table("users_archive").
//	    InsertUsing(ctx, []string{"name", "age"}, func(sub *Builder) {
//	        sub.Table("users").Select("name", "age").Where("status", "=", "active")
//	    })
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
// 参数与语义同 InsertUsing，仅冲突处理不同。
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

// Increment 原子自增指定列，返回受影响行数。
// extra 可交替传入更多列与增量（IncrementEach 语义）：
//
//	Increment(ctx, "wallet", 100, "level", 1)
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
//
//	err := db.Builder().Table("users").Truncate(ctx)
func (b *Builder) Truncate(ctx context.Context) error {
	// SQLite 方言：DELETE FROM 不会重置 AUTOINCREMENT 序列，
	// 需额外清空 sqlite_sequence 使自增主键从头开始（对齐 Laravel 行为）；
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
