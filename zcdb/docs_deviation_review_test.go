package zcdb

// 文档-代码偏离审查（docs/docs-code-deviation-review-report.md）的核对与回归锁死测试：
// 将 7 份功能文档中的 SQL/args 注释、方言差异总览表、默认值与行为语义声明，
// 用三个 Grammar 直接编译比对（不依赖外部数据库；游标/聚合空集等执行语义
// 使用内存 SQLite 实测）。发现偏离时先修订文档/代码，再更新本文件断言。

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

var (
	docMySQL    = &MySQLGrammar{}
	docPostgres = &PostgresGrammar{}
	docSQLite   = &SQLiteGrammar{}
)

// docBuilder 创建无 DAO 的纯编译用 Builder。
func docBuilder(g Grammar) *Builder { return NewBuilder(g, nil) }

func docAssertSQL(t *testing.T, label string, gotSQL string, gotArgs []any, wantSQL string, wantArgs []any) {
	t.Helper()
	if gotSQL != wantSQL {
		t.Errorf("%s SQL 不符:\n got:  %s\n want: %s", label, gotSQL, wantSQL)
	}
	if len(gotArgs) != len(wantArgs) {
		t.Errorf("%s args 长度不符: got %v want %v", label, gotArgs, wantArgs)
		return
	}
	for i := range wantArgs {
		if fmt.Sprint(gotArgs[i]) != fmt.Sprint(wantArgs[i]) {
			t.Errorf("%s args[%d] 不符: got %v want %v", label, i, gotArgs, wantArgs)
			break
		}
	}
}

// ---------- README.md：快速上手与方言差异总览 ----------

// 快速上手示例（README.md）：Find 与 InsertGetId 的 SQL/args 注释。
func TestDocReview_ReadmeQuickStart(t *testing.T) {
	sqlStr, args, err := docBuilder(docMySQL).Table("users").
		Where("age", ">", 18).OrderBy("id", "ASC").Limit(10).ToSelect()
	if err != nil {
		t.Fatal(err)
	}
	docAssertSQL(t, "README 快速上手 Find", sqlStr, args,
		"SELECT * FROM `users` WHERE `age` > ? ORDER BY `id` ASC LIMIT 10", []any{18})

	type User struct {
		Id   int64  `db:"id"`
		Name string `db:"name"`
		Age  int    `db:"age"`
	}
	sqlStr, args, err = docBuilder(docMySQL).Table("users").
		ToInsert(User{Name: "alice", Age: 25})
	if err != nil {
		t.Fatal(err)
	}
	// README 声明：零值列也会参与插入（Id 零值仍在列集合中）
	docAssertSQL(t, "README 快速上手 InsertGetId", sqlStr, args,
		"INSERT INTO `users` (`id`, `name`, `age`) VALUES (?, ?, ?)", []any{int64(0), "alice", 25})
}

// 方言差异总览表（README.md）11 行逐项核对。
func TestDocReview_ReadmeDialectTable(t *testing.T) {
	// 共享锁三方言形态
	sqlStr, args, err := docBuilder(docMySQL).Table("users").Where("id", "=", 1).SharedLock().ToSelect()
	if err != nil {
		t.Fatal(err)
	}
	docAssertSQL(t, "SharedLock/MySQL", sqlStr, args,
		"SELECT * FROM `users` WHERE `id` = ? LOCK IN SHARE MODE", []any{1})
	sqlStr, args, err = docBuilder(docPostgres).Table("users").Where("id", "=", 1).SharedLock().ToSelect()
	if err != nil {
		t.Fatal(err)
	}
	docAssertSQL(t, "SharedLock/PG", sqlStr, args,
		`SELECT * FROM "users" WHERE "id" = $1 FOR SHARE`, []any{1})
	if _, _, err = docBuilder(docSQLite).Table("users").SharedLock().ToSelect(); !errors.Is(err, ErrSQLiteLockNotSupported) {
		t.Errorf("SharedLock/SQLite 应报 ErrSQLiteLockNotSupported: got %v", err)
	}

	// UNION + 锁：PG 编译报错，MySQL 支持（锁置于 UNION 之后）
	if _, _, err = docBuilder(docPostgres).Table("users").Union(docBuilder(docPostgres).Table("admins")).LockForUpdate().ToSelect(); !errors.Is(err, ErrPgUnionLockNotSupported) {
		t.Errorf("UNION+锁/PG 应报 ErrPgUnionLockNotSupported: got %v", err)
	}
	sqlStr, _, err = docBuilder(docMySQL).Table("users").Union(docBuilder(docMySQL).Table("admins")).LockForUpdate().ToSelect()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(sqlStr, " FOR UPDATE") {
		t.Errorf("UNION+锁/MySQL 锁应置于 UNION 之后: %s", sqlStr)
	}

	// CROSS JOIN ON：PG 转 INNER JOIN，MySQL/SQLite 直译
	sqlStr, _, _ = docBuilder(docPostgres).Table("users").CrossJoinOn("colors", "colors.id", "=", "users.id").ToSelect()
	if sqlStr != `SELECT * FROM "users" INNER JOIN "colors" ON "colors"."id" = "users"."id"` {
		t.Errorf("CrossJoinOn/PG 应转 INNER JOIN: %s", sqlStr)
	}
	sqlStr, _, _ = docBuilder(docMySQL).Table("users").CrossJoinOn("colors", "colors.id", "=", "users.id").ToSelect()
	if sqlStr != "SELECT * FROM `users` CROSS JOIN `colors` ON `colors`.`id` = `users`.`id`" {
		t.Errorf("CrossJoinOn/MySQL 应直译: %s", sqlStr)
	}

	// UNION 子查询括号：MySQL/PG 加括号，SQLite 不加
	sqlStr, _, _ = docBuilder(docMySQL).Table("users").Select("name").Union(docBuilder(docMySQL).Table("admins").Select("name")).ToSelect()
	if sqlStr != "(SELECT `name` FROM `users`) UNION (SELECT `name` FROM `admins`)" {
		t.Errorf("UNION 括号/MySQL: %s", sqlStr)
	}
	sqlStr, _, _ = docBuilder(docSQLite).Table("users").Select("name").Union(docBuilder(docSQLite).Table("admins").Select("name")).ToSelect()
	if sqlStr != `SELECT "name" FROM "users" UNION SELECT "name" FROM "admins"` {
		t.Errorf("UNION 括号/SQLite 应不加括号: %s", sqlStr)
	}

	// Truncate 三方言（README 表：PG 带 RESTART IDENTITY、SQLite DELETE FROM）
	if s, err := docBuilder(docMySQL).Table("users").ToTruncate(); err != nil || s != "TRUNCATE TABLE `users`" {
		t.Errorf("Truncate/MySQL: %s, %v", s, err)
	}
	if s, err := docBuilder(docPostgres).Table("users").ToTruncate(); err != nil || s != `TRUNCATE TABLE "users" RESTART IDENTITY` {
		t.Errorf("Truncate/PG 应为 TRUNCATE TABLE \"users\" RESTART IDENTITY: %s, %v", s, err)
	}
	if s, err := docBuilder(docSQLite).Table("users").ToTruncate(); err != nil || s != `DELETE FROM "users"` {
		t.Errorf("Truncate/SQLite: %s, %v", s, err)
	}

	// 随机排序
	if s := docMySQL.CompileRandom(); s != "RAND()" {
		t.Errorf("随机排序/MySQL: %s", s)
	}
	if s := docPostgres.CompileRandom(); s != "RANDOM()" {
		t.Errorf("随机排序/PG: %s", s)
	}
	if s := docSQLite.CompileRandom(); s != "RANDOM()" {
		t.Errorf("随机排序/SQLite: %s", s)
	}

	// 标识符包裹与占位符
	sqlStr, args, _ = docBuilder(docPostgres).Table("users").Where("age", ">", 25).ToSelect()
	docAssertSQL(t, "PG 双引号与 $N 占位符", sqlStr, args,
		`SELECT * FROM "users" WHERE "age" > $1`, []any{25})
}

// dialect 取值别名（README.md / connection.md）：pgsql、sqlite3 别名生效。
func TestDocReview_DialectAliases(t *testing.T) {
	for _, c := range []struct {
		dialect string
		want    string
	}{
		{"mysql", "*zcdb.MySQLGrammar"},
		{"postgresql", "*zcdb.PostgresGrammar"},
		{"postgres", "*zcdb.PostgresGrammar"},
		{"pgsql", "*zcdb.PostgresGrammar"},
		{"sqlite", "*zcdb.SQLiteGrammar"},
		{"sqlite3", "*zcdb.SQLiteGrammar"},
	} {
		g, err := dialectGrammar(c.dialect)
		if err != nil || fmt.Sprintf("%T", g) != c.want {
			t.Errorf("dialect %q: got %T, %v; want %s", c.dialect, g, err, c.want)
		}
	}
	if _, err := dialectGrammar(""); !errors.Is(err, ErrDialectRequired) {
		t.Errorf("空 dialect 应报 ErrDialectRequired: %v", err)
	}
	if _, err := dialectGrammar("oracle"); !errors.Is(err, ErrUnknownDialect) {
		t.Errorf("未知 dialect 应报 ErrUnknownDialect: %v", err)
	}
}

// ---------- compile.md ----------

func TestDocReview_CompileMd(t *testing.T) {
	// ToSelect 三个错误条件
	if _, _, err := docBuilder(docMySQL).ToSelect(); !errors.Is(err, ErrEmptyTable) {
		t.Errorf("ToSelect 无表应报 ErrEmptyTable: %v", err)
	}

	// ToCount 包裹规则（GROUP BY 分支列替换为 SELECT 1、DISTINCT 包裹、别名 t）
	sqlStr, _, err := docBuilder(docMySQL).Table("orders").GroupBy("user_id").ToCount()
	if err != nil {
		t.Fatal(err)
	}
	if sqlStr != "SELECT COUNT(*) FROM (SELECT 1 FROM `orders` GROUP BY `user_id`) AS `t`" {
		t.Errorf("ToCount GROUP BY 包裹: %s", sqlStr)
	}
	sqlStr, _, _ = docBuilder(docMySQL).Table("users").Select("city").Distinct().ToCount()
	if sqlStr != "SELECT COUNT(*) FROM (SELECT DISTINCT `city` FROM `users`) AS `t`" {
		t.Errorf("ToCount DISTINCT 包裹: %s", sqlStr)
	}

	// ToExists
	sqlStr, args, _ := docBuilder(docMySQL).Table("users").Where("id", "=", 1).ToExists()
	docAssertSQL(t, "ToExists", sqlStr, args, "SELECT 1 FROM `users` WHERE `id` = ? LIMIT 1", []any{1})

	// ToAggregate 白名单与别名
	if _, _, err := docBuilder(docMySQL).Table("users").ToAggregate("COUNT", "id"); !errors.Is(err, ErrInvalidAggregate) {
		t.Errorf("ToAggregate 非法函数应报 ErrInvalidAggregate: %v", err)
	}
	sqlStr, args, _ = docBuilder(docMySQL).Table("orders").Where("status", "=", "paid").ToAggregate("MAX", "amount")
	docAssertSQL(t, "ToAggregate", sqlStr, args,
		"SELECT MAX(`amount`) AS `aggregate` FROM `orders` WHERE `status` = ?", []any{"paid"})

	// ToInsert 批量
	type User struct {
		Name string `db:"name"`
		Age  int    `db:"age"`
	}
	sqlStr, args, _ = docBuilder(docMySQL).Table("users").
		ToInsert([]User{{Name: "alice", Age: 25}, {Name: "bob", Age: 30}})
	docAssertSQL(t, "ToInsert 批量", sqlStr, args,
		"INSERT INTO `users` (`name`, `age`) VALUES (?, ?), (?, ?)", []any{"alice", 25, "bob", 30})

	// ToInsertOrIgnore 三方言
	u3 := struct {
		Name  string `db:"name"`
		Age   int    `db:"age"`
		Email string `db:"email"`
	}{"alice", 25, "a@t.com"}
	sqlStr, args, _ = docBuilder(docMySQL).Table("users").ToInsertOrIgnore(u3)
	docAssertSQL(t, "InsertOrIgnore/MySQL", sqlStr, args,
		"INSERT IGNORE INTO `users` (`name`, `age`, `email`) VALUES (?, ?, ?)", []any{"alice", 25, "a@t.com"})
	sqlStr, _, _ = docBuilder(docPostgres).Table("users").ToInsertOrIgnore(u3)
	if sqlStr != `INSERT INTO "users" ("name", "age", "email") VALUES ($1, $2, $3) ON CONFLICT DO NOTHING` {
		t.Errorf("InsertOrIgnore/PG: %s", sqlStr)
	}
	sqlStr, _, _ = docBuilder(docSQLite).Table("users").ToInsertOrIgnore(u3)
	if sqlStr != `INSERT OR IGNORE INTO "users" ("name", "age", "email") VALUES (?, ?, ?)` {
		t.Errorf("InsertOrIgnore/SQLite: %s", sqlStr)
	}

	// ToUpsert MySQL / PG
	sqlStr, _, _ = docBuilder(docMySQL).Table("users").ToUpsert(u3, []string{"email"}, []string{"name", "age"})
	if sqlStr != "INSERT INTO `users` (`name`, `age`, `email`) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE `name` = VALUES(`name`), `age` = VALUES(`age`)" {
		t.Errorf("Upsert/MySQL: %s", sqlStr)
	}
	sqlStr, _, _ = docBuilder(docPostgres).Table("users").ToUpsert(u3, []string{"email"}, []string{"name", "age"})
	if sqlStr != `INSERT INTO "users" ("name", "age", "email") VALUES ($1, $2, $3) ON CONFLICT ("email") DO UPDATE SET "name" = EXCLUDED."name", "age" = EXCLUDED."age"` {
		t.Errorf("Upsert/PG: %s", sqlStr)
	}

	// Upsert 退化形态：全部插入列均为 uniqueBy → PG DO NOTHING、MySQL 自赋值 no-op
	onlyEmail := struct {
		Email string `db:"email"`
	}{"a@t.com"}
	sqlStr, _, _ = docBuilder(docPostgres).Table("users").ToUpsert(onlyEmail, []string{"email"}, nil)
	if !strings.HasSuffix(sqlStr, " DO NOTHING") {
		t.Errorf("Upsert 退化/PG 应为 DO NOTHING: %s", sqlStr)
	}
	sqlStr, _, _ = docBuilder(docMySQL).Table("users").ToUpsert(onlyEmail, []string{"email"}, nil)
	if !strings.HasSuffix(sqlStr, "ON DUPLICATE KEY UPDATE `email` = VALUES(`email`)") {
		t.Errorf("Upsert 退化/MySQL 应为自赋值 no-op: %s", sqlStr)
	}
	// PG/SQLite 缺 uniqueBy 报错；MySQL 可省略
	if _, _, err := docBuilder(docPostgres).Table("users").ToUpsert(u3, nil, nil); !errors.Is(err, ErrUpsertUniqueByRequired) {
		t.Errorf("Upsert/PG 缺 uniqueBy 应报错: %v", err)
	}
	if _, _, err := docBuilder(docSQLite).Table("users").ToUpsert(u3, nil, nil); !errors.Is(err, ErrUpsertUniqueByRequired) {
		t.Errorf("Upsert/SQLite 缺 uniqueBy 应报错: %v", err)
	}
	if _, _, err := docBuilder(docMySQL).Table("users").ToUpsert(u3, nil, []string{"name"}); err != nil {
		t.Errorf("Upsert/MySQL 可省略 uniqueBy: %v", err)
	}

	// ToInsertUsing 与列数校验
	sqlStr, args, _ = docBuilder(docMySQL).Table("users_archive").
		ToInsertUsing([]string{"name", "age"}, func(sub *Builder) {
			sub.Table("users").Select("name", "age").Where("status", "=", "active")
		})
	docAssertSQL(t, "ToInsertUsing", sqlStr, args,
		"INSERT INTO `users_archive` (`name`, `age`) SELECT `name`, `age` FROM `users` WHERE `status` = ?", []any{"active"})
	if _, _, err := docBuilder(docMySQL).Table("users_archive").
		ToInsertUsing([]string{"name"}, func(sub *Builder) {
			sub.Table("users").Select("name", "age")
		}); !errors.Is(err, ErrInsertUsingColumnMismatch) {
		t.Errorf("列数不一致应报 ErrInsertUsingColumnMismatch: %v", err)
	}

	// ToUpdate / ToDelete
	type UserUpdate struct {
		Name *string `db:"name"`
		Age  *int    `db:"age"`
	}
	name, age := "alice_new", 26
	sqlStr, args, _ = docBuilder(docMySQL).Table("users").Where("id", "=", 1).ToUpdate(UserUpdate{Name: &name, Age: &age})
	docAssertSQL(t, "ToUpdate", sqlStr, args,
		"UPDATE `users` SET `name` = ?, `age` = ? WHERE `id` = ?", []any{"alice_new", 26, 1})
	sqlStr, args, _ = docBuilder(docMySQL).Table("users").Where("id", "=", 1).ToDelete()
	docAssertSQL(t, "ToDelete", sqlStr, args, "DELETE FROM `users` WHERE `id` = ?", []any{1})

	// ToTruncate：PG 实际输出带 RESTART IDENTITY（compile.md/mutate.md 示例注释漏写，已修订文档）
	sqlTrunc, err := docBuilder(docPostgres).Table("users").ToTruncate()
	if err != nil || sqlTrunc != `TRUNCATE TABLE "users" RESTART IDENTITY` {
		t.Errorf("ToTruncate/PG: %s, %v", sqlTrunc, err)
	}

	// ToIncrement 等长切片
	sqlStr, args, _ = docBuilder(docMySQL).Table("users").Where("id", "=", 1).
		ToIncrement([]string{"wallet", "level"}, []any{100, 1})
	docAssertSQL(t, "ToIncrement", sqlStr, args,
		"UPDATE `users` SET `wallet` = `wallet` + ?, `level` = `level` + ? WHERE `id` = ?", []any{100, 1, 1})
	// columns/amounts 不等长或为空也报 ErrIncrementColumns（errors.go 注释语义）
	if _, _, err := docBuilder(docMySQL).Table("users").ToIncrement(nil, nil); !errors.Is(err, ErrIncrementColumns) {
		t.Errorf("ToIncrement 空切片应报 ErrIncrementColumns: %v", err)
	}
	if _, _, err := docBuilder(docMySQL).Table("users").ToIncrement([]string{"a"}, []any{1, 2}); !errors.Is(err, ErrIncrementColumns) {
		t.Errorf("ToIncrement 不等长应报 ErrIncrementColumns: %v", err)
	}

	// Expression：不占绑定位置、直接内嵌
	sqlStr, args, _ = docBuilder(docMySQL).Table("users").
		Where("id", "=", NewExpression("parent_id")).ToSelect()
	docAssertSQL(t, "Expression Where 值", sqlStr, args,
		"SELECT * FROM `users` WHERE `id` = parent_id", nil)
	sqlStr, args, _ = docBuilder(docMySQL).Table("users").Where("id", "=", 1).
		ToUpdate(struct {
			UpdatedAt any `db:"updated_at"`
		}{UpdatedAt: NewExpression("NOW()")})
	docAssertSQL(t, "Expression Update 值", sqlStr, args,
		"UPDATE `users` SET `updated_at` = NOW() WHERE `id` = ?", []any{1})
}

// ---------- query-builder.md ----------

func TestDocReview_QueryBuilderMd(t *testing.T) {
	// TableSub：子查询绑定排在外层 WHERE 之前
	sub := docBuilder(docMySQL).Table("orders").Select("user_id").Where("amount", ">", 100)
	sqlStr, args, _ := docBuilder(docMySQL).TableSub(sub, "o").Where("o.user_id", ">", 1).ToSelect()
	docAssertSQL(t, "TableSub", sqlStr, args,
		"SELECT * FROM (SELECT `user_id` FROM `orders` WHERE `amount` > ?) AS `o` WHERE `o`.`user_id` > ?", []any{100, 1})

	// Select 替换 / AddSelect 追加去重 / SelectRaw / SelectSub
	sqlStr, _, _ = docBuilder(docMySQL).Table("users").Select("id").AddSelect("name", "id").ToSelect()
	if sqlStr != "SELECT `id`, `name` FROM `users`" {
		t.Errorf("AddSelect 去重: %s", sqlStr)
	}
	cnt := docBuilder(docMySQL).Table("orders").SelectRaw("COUNT(*)").WhereColumn("orders.user_id", "=", "users.id")
	sqlStr, _, _ = docBuilder(docMySQL).Table("users").Select("id").SelectSub(cnt, "order_count").ToSelect()
	if sqlStr != "SELECT `id`, (SELECT COUNT(*) FROM `orders` WHERE `orders`.`user_id` = `users`.`id`) AS `order_count` FROM `users`" {
		t.Errorf("SelectSub: %s", sqlStr)
	}

	// 两参简写与 nil 特判
	sqlStr, args, _ = docBuilder(docMySQL).Table("users").Where("status", "active").ToSelect()
	docAssertSQL(t, "Where 两参简写", sqlStr, args, "SELECT * FROM `users` WHERE `status` = ?", []any{"active"})
	sqlStr, args, _ = docBuilder(docMySQL).Table("users").Where("deleted_at", "=", nil).ToSelect()
	docAssertSQL(t, "Where nil 特判", sqlStr, args, "SELECT * FROM `users` WHERE `deleted_at` IS NULL", nil)
	sqlStr, args, _ = docBuilder(docMySQL).Table("users").Where("deleted_at", "!=", nil).ToSelect()
	docAssertSQL(t, "Where nil 特判 !=", sqlStr, args, "SELECT * FROM `users` WHERE `deleted_at` IS NOT NULL", nil)
	// 非法运算符编译期报错
	if _, _, err := docBuilder(docMySQL).Table("users").Where("a", "EVIL", 1).ToSelect(); !errors.Is(err, ErrInvalidOperator) {
		t.Errorf("非法运算符应报 ErrInvalidOperator: %v", err)
	}

	// WhereIn/WhereNotIn 空切片
	sqlStr, _, _ = docBuilder(docMySQL).Table("users").WhereIn("id", []any{}).ToSelect()
	if sqlStr != "SELECT * FROM `users` WHERE 0 = 1" {
		t.Errorf("WhereIn 空切片应恒假: %s", sqlStr)
	}
	sqlStr, _, _ = docBuilder(docMySQL).Table("users").WhereNotIn("id", []any{}).ToSelect()
	if sqlStr != "SELECT * FROM `users` WHERE 1 = 1" {
		t.Errorf("WhereNotIn 空切片应恒真: %s", sqlStr)
	}

	// WhereNull 多列 AND 展开
	sqlStr, _, _ = docBuilder(docMySQL).Table("users").WhereNull("deleted_at", "remark").ToSelect()
	if sqlStr != "SELECT * FROM `users` WHERE `deleted_at` IS NULL AND `remark` IS NULL" {
		t.Errorf("WhereNull 多列: %s", sqlStr)
	}

	// Between 四形态与 WhereValueBetween
	sqlStr, args, _ = docBuilder(docMySQL).Table("users").WhereBetween("age", 18, 30).ToSelect()
	docAssertSQL(t, "WhereBetween", sqlStr, args, "SELECT * FROM `users` WHERE `age` BETWEEN ? AND ?", []any{18, 30})
	sqlStr, args, _ = docBuilder(docMySQL).Table("users").WhereValueBetween(25, "min_age", "max_age").ToSelect()
	docAssertSQL(t, "WhereValueBetween", sqlStr, args,
		"SELECT * FROM `users` WHERE ? BETWEEN `min_age` AND `max_age`", []any{25})
	sqlStr, _, _ = docBuilder(docMySQL).Table("products").WhereBetweenColumns("price", "min_price", "max_price").ToSelect()
	if sqlStr != "SELECT * FROM `products` WHERE `price` BETWEEN `min_price` AND `max_price`" {
		t.Errorf("WhereBetweenColumns: %s", sqlStr)
	}

	// 嵌套组五形态
	sqlStr, args, _ = docBuilder(docMySQL).Table("users").Where("status", "active").
		WhereNested(func(q *Builder) { q.Where("age", ">", 18).Where("vip", "=", 1) }).ToSelect()
	docAssertSQL(t, "WhereNested", sqlStr, args,
		"SELECT * FROM `users` WHERE `status` = ? AND (`age` > ? AND `vip` = ?)", []any{"active", 18, 1})
	sqlStr, args, _ = docBuilder(docMySQL).Table("users").WhereNot(func(q *Builder) {
		q.Where("status", "banned").Where("age", "<", 18)
	}).ToSelect()
	docAssertSQL(t, "WhereNot", sqlStr, args,
		"SELECT * FROM `users` WHERE NOT (`status` = ? AND `age` < ?)", []any{"banned", 18})
	sqlStr, args, _ = docBuilder(docMySQL).Table("users").WhereAny(func(q *Builder) {
		q.Where("age", ">", 60).Where("vip", "=", 1)
	}).ToSelect()
	docAssertSQL(t, "WhereAny", sqlStr, args,
		"SELECT * FROM `users` WHERE (`age` > ? OR `vip` = ?)", []any{60, 1})
	sqlStr, args, _ = docBuilder(docMySQL).Table("users").WhereNone(func(q *Builder) {
		q.Where("status", "banned").Where("age", "<", 18)
	}).ToSelect()
	docAssertSQL(t, "WhereNone", sqlStr, args,
		"SELECT * FROM `users` WHERE NOT (`status` = ? OR `age` < ?)", []any{"banned", 18})
	// 空回调组被忽略
	sqlStr, _, _ = docBuilder(docMySQL).Table("users").Where("id", "=", 1).WhereNested(func(q *Builder) {}).ToSelect()
	if sqlStr != "SELECT * FROM `users` WHERE `id` = ?" {
		t.Errorf("空嵌套组应忽略: %s", sqlStr)
	}

	// WhereDate 三方言
	sqlStr, args, _ = docBuilder(docMySQL).Table("users").WhereDate("created_at", "2026-08-08").ToSelect()
	docAssertSQL(t, "WhereDate/MySQL", sqlStr, args,
		"SELECT * FROM `users` WHERE date(`created_at`) = ?", []any{"2026-08-08"})
	sqlStr, args, _ = docBuilder(docPostgres).Table("users").WhereDate("created_at", "2026-08-08").ToSelect()
	docAssertSQL(t, "WhereDate/PG", sqlStr, args,
		`SELECT * FROM "users" WHERE "created_at"::date = $1`, []any{"2026-08-08"})
	sqlStr, args, _ = docBuilder(docSQLite).Table("users").WhereDate("created_at", "2026-08-08").ToSelect()
	docAssertSQL(t, "WhereDate/SQLite", sqlStr, args,
		`SELECT * FROM "users" WHERE strftime('%Y-%m-%d', "created_at") = ?`, []any{"2026-08-08"})

	// WhereLike 三方言（默认与 caseSensitive）
	sqlStr, _, _ = docBuilder(docPostgres).Table("users").WhereLike("name", "%alice%").ToSelect()
	if sqlStr != `SELECT * FROM "users" WHERE "name" ILIKE $1` {
		t.Errorf("WhereLike/PG 默认应 ILIKE: %s", sqlStr)
	}
	sqlStr, _, _ = docBuilder(docMySQL).Table("users").WhereLike("name", "a%", true).ToSelect()
	if sqlStr != "SELECT * FROM `users` WHERE BINARY `name` LIKE ?" {
		t.Errorf("WhereLike/MySQL 区分大小写: %s", sqlStr)
	}
	sqlStr, _, _ = docBuilder(docSQLite).Table("users").WhereLike("name", "a%", true).ToSelect()
	if sqlStr != `SELECT * FROM "users" WHERE "name" GLOB ?` {
		t.Errorf("WhereLike/SQLite 区分大小写应 GLOB: %s", sqlStr)
	}
	sqlStr, _, _ = docBuilder(docPostgres).Table("users").WhereNotLike("name", "%test%").ToSelect()
	if sqlStr != `SELECT * FROM "users" WHERE "name" NOT ILIKE $1` {
		t.Errorf("WhereNotLike/PG: %s", sqlStr)
	}

	// WhereNullSafe 三方言
	sqlStr, args, _ = docBuilder(docMySQL).Table("users").WhereNullSafeEquals("remark", nil).ToSelect()
	docAssertSQL(t, "NullSafe/MySQL", sqlStr, args, "SELECT * FROM `users` WHERE `remark` <=> ?", []any{nil})
	sqlStr, _, _ = docBuilder(docPostgres).Table("users").WhereNullSafeEquals("remark", nil).ToSelect()
	if sqlStr != `SELECT * FROM "users" WHERE "remark" IS NOT DISTINCT FROM $1` {
		t.Errorf("NullSafe/PG: %s", sqlStr)
	}
	sqlStr, _, _ = docBuilder(docSQLite).Table("users").WhereNullSafeEquals("remark", nil).ToSelect()
	if sqlStr != `SELECT * FROM "users" WHERE "remark" IS ?` {
		t.Errorf("NullSafe/SQLite: %s", sqlStr)
	}
	sqlStr, _, _ = docBuilder(docMySQL).Table("users").WhereNullSafeNotEquals("remark", "x").ToSelect()
	if sqlStr != "SELECT * FROM `users` WHERE NOT `remark` <=> ?" {
		t.Errorf("NullSafeNot/MySQL: %s", sqlStr)
	}

	// 子查询条件三形态
	sqlStr, _, _ = docBuilder(docMySQL).Table("users").WhereExists(func(q *Builder) {
		q.Table("orders").SelectRaw("1").WhereColumn("orders.user_id", "=", "users.id")
	}).ToSelect()
	if sqlStr != "SELECT * FROM `users` WHERE EXISTS (SELECT 1 FROM `orders` WHERE `orders`.`user_id` = `users`.`id`)" {
		t.Errorf("WhereExists: %s", sqlStr)
	}
	// 非法子查询类型
	if _, _, err := docBuilder(docMySQL).Table("users").WhereExists(123).ToSelect(); !errors.Is(err, ErrInvalidSubQuery) {
		t.Errorf("WhereExists 非法类型应报 ErrInvalidSubQuery: %v", err)
	}
	sqlStr, _, _ = docBuilder(docMySQL).Table("users").WhereSub("age", ">", func(q *Builder) {
		q.Table("stats").SelectRaw("AVG(age)")
	}).ToSelect()
	if sqlStr != "SELECT * FROM `users` WHERE `age` > (SELECT AVG(age) FROM `stats`)" {
		t.Errorf("WhereSub: %s", sqlStr)
	}
	sqlStr, args, _ = docBuilder(docMySQL).Table("users").WhereInSub("dept_id", func(q *Builder) {
		q.Table("depts").Select("id").Where("level", ">", 3)
	}).ToSelect()
	docAssertSQL(t, "WhereInSub", sqlStr, args,
		"SELECT * FROM `users` WHERE `dept_id` IN (SELECT `id` FROM `depts` WHERE `level` > ?)", []any{3})

	// JOIN 简写与嵌套 join 组
	sqlStr, _, _ = docBuilder(docMySQL).Table("users").Select("users.name", "orders.amount").
		Join("orders", "users.id", "=", "orders.user_id").ToSelect()
	if sqlStr != "SELECT `users`.`name`, `orders`.`amount` FROM `users` INNER JOIN `orders` ON `users`.`id` = `orders`.`user_id`" {
		t.Errorf("Join 简写: %s", sqlStr)
	}
	sqlStr, _, _ = docBuilder(docMySQL).Table("users").JoinOn("orders", func(j *JoinBuilder) {
		j.On("orders.user_id", "=", "users.id").JoinOn("order_items", func(q *JoinBuilder) {
			q.On("order_items.order_id", "=", "orders.id")
		})
	}).ToSelect()
	if sqlStr != "SELECT * FROM `users` INNER JOIN (`orders` INNER JOIN `order_items` ON `order_items`.`order_id` = `orders`.`id`) ON `orders`.`user_id` = `users`.`id`" {
		t.Errorf("嵌套 join 组: %s", sqlStr)
	}

	// JoinBuilder 条件组合示例（文档 args: [paid shipped 100 2026]）
	sqlStr, args, _ = docBuilder(docMySQL).Table("users").JoinOn("orders", func(j *JoinBuilder) {
		j.On("orders.user_id", "=", "users.id").
			WhereIn("orders.status", []any{"paid", "shipped"}).
			WhereNested(func(q *JoinBuilder) { q.Where("orders.amount", ">", 100) }).
			Raw("YEAR(orders.created_at) = ?", 2026)
	}).ToSelect()
	docAssertSQL(t, "JoinBuilder 组合", sqlStr, args,
		"SELECT * FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id` AND `orders`.`status` IN (?, ?) AND (`orders`.`amount` > ?) AND YEAR(orders.created_at) = ?",
		[]any{"paid", "shipped", 100, 2026})

	// JoinSub：派生表绑定先于 ON 条件
	latest := docBuilder(docMySQL).Table("logs").Select("user_id").
		SelectRaw("MAX(created_at) AS last_at").GroupBy("user_id")
	sqlStr, args, _ = docBuilder(docMySQL).Table("users").Select("users.name", "l.last_at").
		JoinSub(latest, "l", func(j *JoinBuilder) { j.On("l.user_id", "=", "users.id") }).ToSelect()
	docAssertSQL(t, "JoinSub", sqlStr, args,
		"SELECT `users`.`name`, `l`.`last_at` FROM `users` INNER JOIN (SELECT `user_id`, MAX(created_at) AS last_at FROM `logs` GROUP BY `user_id`) AS `l` ON `l`.`user_id` = `users`.`id`", nil)

	// GroupByRaw / Having / HavingRaw
	sqlStr, _, _ = docBuilder(docMySQL).Table("orders").
		SelectRaw("DATE(created_at) AS d, COUNT(*) AS cnt").GroupByRaw("DATE(created_at)").ToSelect()
	if sqlStr != "SELECT DATE(created_at) AS d, COUNT(*) AS cnt FROM `orders` GROUP BY DATE(created_at)" {
		t.Errorf("GroupByRaw: %s", sqlStr)
	}
	sqlStr, args, _ = docBuilder(docMySQL).Table("orders").Select("user_id").
		SelectRaw("SUM(amount) AS total").GroupBy("user_id").Having("total", ">", 100).ToSelect()
	docAssertSQL(t, "Having", sqlStr, args,
		"SELECT `user_id`, SUM(amount) AS total FROM `orders` GROUP BY `user_id` HAVING `total` > ?", []any{100})

	// OrderBy 方向规则与 ForPage
	sqlStr, _, _ = docBuilder(docMySQL).Table("users").OrderBy("age", "DESC").OrderBy("name").ToSelect()
	if sqlStr != "SELECT * FROM `users` ORDER BY `age` DESC, `name` ASC" {
		t.Errorf("OrderBy: %s", sqlStr)
	}
	sqlStr, _, _ = docBuilder(docMySQL).Table("users").ForPage(2, 20).ToSelect()
	if sqlStr != "SELECT * FROM `users` LIMIT 20 OFFSET 20" {
		t.Errorf("ForPage: %s", sqlStr)
	}
	sqlStr, _, _ = docBuilder(docMySQL).Table("users").ForPage(0, 20).ToSelect()
	if sqlStr != "SELECT * FROM `users` LIMIT 20" {
		t.Errorf("ForPage page<1 应修正为 1: %s", sqlStr)
	}

	// Union/UnionAll（MySQL 括号形态；SQLite 例外见方言表测试）
	admins := docBuilder(docMySQL).Table("admins").Select("name")
	sqlStr, _, _ = docBuilder(docMySQL).Table("users").Select("name").UnionAll(admins).ToSelect()
	if sqlStr != "(SELECT `name` FROM `users`) UNION ALL (SELECT `name` FROM `admins`)" {
		t.Errorf("UnionAll: %s", sqlStr)
	}

	// 行锁：LockForUpdate
	sqlStr, args, _ = docBuilder(docMySQL).Table("users").Where("id", "=", 1).LockForUpdate().ToSelect()
	docAssertSQL(t, "LockForUpdate", sqlStr, args, "SELECT * FROM `users` WHERE `id` = ? FOR UPDATE", []any{1})

	// Clone 深拷贝（usePrimary 标记经 Clone 保留）
	base := docBuilder(docMySQL).Table("users").Where("status", "active").Primary()
	cl := base.Clone()
	if !cl.usePrimary {
		t.Errorf("Clone 应拷贝 usePrimary 标记")
	}
	cl.Where("role", "admin")
	sqlBase, _, _ := base.ToSelect()
	if sqlBase != "SELECT * FROM `users` WHERE `status` = ?" {
		t.Errorf("Clone 隔离性: %s", sqlBase)
	}
}

// ---------- mutate.md ----------

func TestDocReview_MutateMd(t *testing.T) {
	// DeleteJoin 三方言形态与绑定顺序
	bMySQL := docBuilder(docMySQL).Table("users").
		Join("orders", "orders.user_id", "=", "users.id").
		Where("orders.status", "=", "cancelled")
	sqlStr, args, err := bMySQL.ToDeleteJoin()
	if err != nil {
		t.Fatal(err)
	}
	docAssertSQL(t, "DeleteJoin/MySQL", sqlStr, args,
		"DELETE `users` FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id` WHERE `orders`.`status` = ?", []any{"cancelled"})
	bPG := docBuilder(docPostgres).Table("users").
		Join("orders", "orders.user_id", "=", "users.id").
		Where("orders.status", "=", "cancelled")
	sqlStr, args, _ = bPG.ToDeleteJoin()
	docAssertSQL(t, "DeleteJoin/PG", sqlStr, args,
		`DELETE FROM "users" USING "orders" WHERE "orders"."user_id" = "users"."id" AND "orders"."status" = $1`, []any{"cancelled"})
	bSQLite := docBuilder(docSQLite).Table("users").
		Join("orders", "orders.user_id", "=", "users.id").
		Where("orders.status", "=", "cancelled")
	sqlStr, args, _ = bSQLite.ToDeleteJoin()
	docAssertSQL(t, "DeleteJoin/SQLite", sqlStr, args,
		`DELETE FROM "users" WHERE "id" IN (SELECT "users"."id" FROM "users" INNER JOIN "orders" ON "orders"."user_id" = "users"."id" WHERE "orders"."status" = ?)`, []any{"cancelled"})

	// 无 JOIN 报 ErrDeleteJoinNoJoin
	if _, _, err := docBuilder(docMySQL).Table("users").ToDeleteJoin(); !errors.Is(err, ErrDeleteJoinNoJoin) {
		t.Errorf("DeleteJoin 无 JOIN 应报错: %v", err)
	}

	// Increment extra 变参
	sqlStr, args, _ = docBuilder(docMySQL).Table("users").Where("id", "=", 1).ToIncrement(
		[]string{"wallet", "level"}, []any{100, 1})
	docAssertSQL(t, "Increment 多列", sqlStr, args,
		"UPDATE `users` SET `wallet` = `wallet` + ?, `level` = `level` + ? WHERE `id` = ?", []any{100, 1, 1})

	// extra 奇数 / 列名非 string 报 ErrIncrementColumns
	if _, err := docBuilder(docMySQL).Table("users").Increment(context.Background(), "wallet", 100, "level"); err == nil || !errors.Is(err, ErrIncrementColumns) {
		t.Errorf("Increment extra 奇数应报 ErrIncrementColumns: %v", err)
	}
	if _, err := docBuilder(docMySQL).Table("users").Increment(context.Background(), "wallet", 100, 1, 1); err == nil || !errors.Is(err, ErrIncrementColumns) {
		t.Errorf("Increment extra 列名非 string 应报 ErrIncrementColumns: %v", err)
	}

	// 破坏性操作保护：拒绝路径在编译前返回，不需要 DAO
	if _, err := docBuilder(docMySQL).Table("users").Update(context.Background(), struct{}{}); !errors.Is(err, ErrUpdateWithoutWhere) {
		t.Errorf("无 WHERE Update 应被拒绝: %v", err)
	}
	if _, err := docBuilder(docMySQL).Table("users").Delete(context.Background()); !errors.Is(err, ErrDeleteWithoutWhere) {
		t.Errorf("无 WHERE Delete 应被拒绝: %v", err)
	}
	// 空嵌套回调不算有效限定
	b := docBuilder(docMySQL).Table("users")
	b.WhereNested(func(q *Builder) {})
	if b.hasEffectiveWhere() {
		t.Errorf("空嵌套回调不应计为有效限定")
	}
	// 带 ON 条件的 JOIN 算有效限定，无条件 JOIN 不算
	b2 := docBuilder(docMySQL).Table("users")
	b2.Join("orders", "orders.user_id", "=", "users.id")
	if !b2.hasEffectiveJoin() {
		t.Errorf("带条件 JOIN 应计为有效限定")
	}
	b3 := docBuilder(docMySQL).Table("users")
	b3.CrossJoin("orders")
	if b3.hasEffectiveJoin() {
		t.Errorf("无条件 JOIN 不应计为有效限定")
	}

	// InsertGetId PG 方言执行前直接报错
	if _, err := docBuilder(docPostgres).Table("users").InsertGetId(context.Background(),
		struct {
			Name string `db:"name"`
		}{"alice"}); err == nil || !strings.Contains(err.Error(), "not supported on postgres") {
		t.Errorf("InsertGetId/PG 应在执行前报错: %v", err)
	}
}

// ---------- connection.md / query-exec.md（内存 SQLite 实测执行语义） ----------

func TestDocReview_ConnectionMd_Lifecycle(t *testing.T) {
	// 缺失参数错误文案（文档 connection.md 写为无前缀，源码实际带 zcdb: 前缀）
	if _, err := NewPool(PoolConfig{DSN: ":memory:"}); err == nil || err.Error() != "zcdb: DriverName is required" {
		t.Errorf("NewPool 缺 DriverName 错误文案: %v", err)
	}
	if _, err := NewPool(PoolConfig{DriverName: "sqlite"}); err == nil || err.Error() != "zcdb: DSN is required" {
		t.Errorf("NewPool 缺 DSN 错误文案: %v", err)
	}

	pool, err := NewPool(PoolConfig{DriverName: "sqlite", DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	// Close 幂等
	if err := pool.Close(); err != nil {
		t.Errorf("首次 Close 应成功: %v", err)
	}
	if err := pool.Close(); err != nil {
		t.Errorf("重复 Close 应幂等返回 nil: %v", err)
	}
	// Close 后 AddSlave 拒绝
	if err := pool.AddSlave(":memory:"); err == nil || err.Error() != "zcdb: pool is closed" {
		t.Errorf("Close 后 AddSlave 应报 zcdb: pool is closed: %v", err)
	}

	// NewDBDao 参数校验
	if _, err := NewDBDao(nil, "mysql", nil, ""); !errors.Is(err, ErrPoolRequired) {
		t.Errorf("pool 为 nil 应报 ErrPoolRequired: %v", err)
	}
}

// docReviewSQLiteDAO 创建内存 SQLite DAO，onSQL 记录执行的 SQL。
func docReviewSQLiteDAO(t *testing.T, onSQL SlowSQLCallback) *DBDao {
	t.Helper()
	pool, err := NewPool(PoolConfig{DriverName: "sqlite", DSN: ":memory:", MaxOpenConns: 1})
	if err != nil {
		t.Fatalf("open sqlite pool: %v", err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	dao, err := NewDBDao(pool, "sqlite", onSQL, "")
	if err != nil {
		t.Fatalf("new dao: %v", err)
	}
	return dao
}

// query-exec.md：CursorBy 每批 LIMIT 实际为 chunkSize+1（多取一条探测下一页），
// 文档示例注释写 LIMIT 100，与实际 LIMIT 101 不符（已修订文档）。
func TestDocReview_QueryExecMd_CursorByLimit(t *testing.T) {
	var sqls []string
	dao := docReviewSQLiteDAO(t, func(ctx context.Context, _ time.Duration, sqlStr string, args []any) {
		sqls = append(sqls, sqlStr)
	})
	ctx := context.Background()
	if _, err := dao.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		if _, err := dao.Exec(ctx, `INSERT INTO users (id, name) VALUES (?, ?)`, i, fmt.Sprint("u", i)); err != nil {
			t.Fatal(err)
		}
	}

	type User struct {
		Id   int64  `db:"id"`
		Name string `db:"name"`
	}
	var u User
	n := 0
	for err := range dao.Builder().Table("users").CursorBy(ctx, &u, 100, "id") {
		if err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n != 5 {
		t.Errorf("CursorBy 应迭代出全部 5 行: got %d", n)
	}
	if len(sqls) == 0 {
		t.Fatal("未捕获到 SQL")
	}
	// 取首条 SELECT（sqls 中混有建表/插入语句）
	var first string
	for _, s := range sqls {
		if strings.HasPrefix(s, "SELECT") {
			first = s
			break
		}
	}
	// 实际 SQL 应为 LIMIT 101（chunkSize+1 探测行），文档原注释 LIMIT 100 为偏离
	if !strings.HasSuffix(first, `ORDER BY "id" ASC LIMIT 101`) {
		t.Errorf("CursorBy 首批 SQL 应为 LIMIT 101: %s", first)
	}
}

// query-exec.md：聚合空集语义表（Max/Min 返回 (0, sql.ErrNoRows)，Sum/Avg 返回 (0, nil)）。
func TestDocReview_QueryExecMd_AggregateEmptySet(t *testing.T) {
	dao := docReviewSQLiteDAO(t, nil)
	ctx := context.Background()
	if _, err := dao.Exec(ctx, `CREATE TABLE nums (v INTEGER)`); err != nil {
		t.Fatal(err)
	}
	b := dao.Builder().Table("nums")
	if v, err := b.Max(ctx, "v"); v != 0 || !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Max 空集应返回 (0, sql.ErrNoRows): %v, %v", v, err)
	}
	if v, err := b.Min(ctx, "v"); v != 0 || !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Min 空集应返回 (0, sql.ErrNoRows): %v, %v", v, err)
	}
	if v, err := b.Sum(ctx, "v"); v != 0 || err != nil {
		t.Errorf("Sum 空集应返回 (0, nil): %v, %v", v, err)
	}
	if v, err := b.Avg(ctx, "v"); v != 0 || err != nil {
		t.Errorf("Avg 空集应返回 (0, nil): %v, %v", v, err)
	}
}

// query-exec.md：Find/First/Value 无结果行为与 First LIMIT 1；Pluck 三模式；Paginate COUNT。
func TestDocReview_QueryExecMd_TerminalSemantics(t *testing.T) {
	var sqls []string
	dao := docReviewSQLiteDAO(t, func(ctx context.Context, _ time.Duration, sqlStr string, args []any) {
		sqls = append(sqls, sqlStr)
	})
	ctx := context.Background()
	if _, err := dao.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, vip INTEGER)`); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 30; i++ {
		if _, err := dao.Exec(ctx, `INSERT INTO users (id, name, vip) VALUES (?, ?, ?)`, i, fmt.Sprint("u", i), i%2); err != nil {
			t.Fatal(err)
		}
	}

	type User struct {
		Id   int64  `db:"id"`
		Name string `db:"name"`
		Vip  int    `db:"vip"`
	}

	// Find 无结果：空切片、err nil
	var users []User
	if err := dao.Builder().Table("users").Where("id", "=", 999).Find(ctx, &users); err != nil || users != nil && len(users) != 0 {
		t.Errorf("Find 无结果应空切片且 err nil: len=%d, %v", len(users), err)
	}
	// First 无结果：sql.ErrNoRows
	var one User
	if err := dao.Builder().Table("users").Where("id", "=", 999).First(ctx, &one); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("First 无结果应报 sql.ErrNoRows: %v", err)
	}
	// First 强制 LIMIT 1
	sqls = nil
	if err := dao.Builder().Table("users").Where("id", "=", 1).First(ctx, &one); err != nil {
		t.Fatal(err)
	}
	if len(sqls) != 1 || !strings.HasSuffix(sqls[0], `LIMIT 1`) {
		t.Errorf("First SQL 应以 LIMIT 1 结尾: %v", sqls)
	}
	// Value 无结果与二级指针区分 NULL
	var name string
	if err := dao.Builder().Table("users").Select("name").Where("id", "=", 999).Value(ctx, &name); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Value 无结果应报 sql.ErrNoRows: %v", err)
	}

	// Pluck 模式一：切片单列
	var names []string
	if err := dao.Builder().Table("users").Where("vip", "=", 1).Pluck(ctx, &names, "name"); err != nil || len(names) != 15 {
		t.Errorf("Pluck 切片模式: len=%d, %v", len(names), err)
	}
	// Pluck 模式二：map 键值对（第一列值、第二列键）
	var m map[int64]string
	if err := dao.Builder().Table("users").Pluck(ctx, &m, "name", "id"); err != nil || len(m) != 30 || m[1] != "u1" {
		t.Errorf("Pluck map 模式: len=%d, m[1]=%q, %v", len(m), m[1], err)
	}
	// Pluck 模式三：keyBy 结构体
	var mu map[int64]User
	if err := dao.Builder().Table("users").Pluck(ctx, &mu, "id"); err != nil || len(mu) != 30 || mu[2].Name != "u2" {
		t.Errorf("Pluck keyBy 模式: len=%d, %v", len(mu), err)
	}

	// Paginate：先 COUNT 后数据查询
	sqls = nil
	var page []User
	total, err := dao.Builder().Table("users").OrderBy("id", "ASC").ForPage(2, 20).Paginate(ctx, &page)
	if err != nil || total != 30 || len(page) != 10 {
		t.Errorf("Paginate: total=%d, len=%d, %v", total, len(page), err)
	}
	if len(sqls) != 2 || !strings.HasPrefix(sqls[0], "SELECT COUNT(*) FROM") {
		t.Errorf("Paginate 应先执行 COUNT: %v", sqls)
	}
}
