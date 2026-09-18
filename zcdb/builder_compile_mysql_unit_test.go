// 本文件为 MySQL 方言单元测试——SQL 编译（ToXxx 系列）。
// 仅验证 Grammar 编译结果，不依赖数据库连接。
package zcdb

import (
	"slices"
	"testing"
)

// TestBug_ToUpdateWithJoin_MySQL 验证 MySQL UPDATE + JOIN 的绑定参数数量。
// CompileUpdate 在 JOIN ON value 条件中生成 ? 占位符，
// 但 ToUpdate 只收集 WHERE 绑定，不收集 JOIN 绑定，导致参数数量不匹配。
func TestBug_ToUpdateWithJoin_MySQL(t *testing.T) {
	g := NewMySQLGrammar()
	type updateData struct {
		Name string `db:"name"`
	}
	b := newTestBuilder(g, nil).
		Table("users").
		JoinOn("profiles", func(jb *JoinBuilder) {
			jb.On("users.id", "=", "profiles.user_id")
			jb.Where("profiles.active", "=", 99)
		}).
		Where("users.id", "=", 1)

	sql, args, err := b.ToUpdate(updateData{Name: "x"})
	assertNoError(t, err)

	// SQL 中应有 3 个 ?：SET 1个 + JOIN ON value 1个 + WHERE 1个
	expectedSQL := "UPDATE `users` INNER JOIN `profiles` ON `users`.`id` = `profiles`.`user_id` AND `profiles`.`active` = ? SET `name` = ? WHERE `users`.`id` = ?"
	assertSQL(t, expectedSQL, sql)
	// 绑定应为 3 个：[99, "x", 1]
	assertArgs(t, []any{99, "x", 1}, args)
}

// TestMySQLCompile_InsertUsingColumnMismatch 验证 MySQL 方言 InsertUsing 列数校验
// （详见 assertInsertUsingColumnMismatch：不一致报 ErrInsertUsingColumnMismatch，
// 一致/通配符/默认 SELECT * 时通过编译，后两者由数据库运行时校验）。
func TestMySQLCompile_InsertUsingColumnMismatch(t *testing.T) {
	assertInsertUsingColumnMismatch(t, NewMySQLGrammar())
}

// TestMySQLCompile_UpsertEmptyUpdateColumnsElse 覆盖 MySQL CompileUpsert 在
// updateColumns 为空且 uniqueBy 为空时退化为 columns[:1] 的 else 分支。
// 该分支经公共 ToUpsert 不可达（空 updateColumns 会被展开为全部非 uniqueBy 列），
// 故以 grammar 直调方式锁死。
func TestMySQLCompile_UpsertEmptyUpdateColumnsElse(t *testing.T) {
	g := NewMySQLGrammar()
	b := newTestBuilder(g, nil).Table("t")
	sql := g.CompileUpsert(b, []string{"id"}, [][]any{{int64(1)}}, nil, nil, nil)
	if sql == "" || sql != "INSERT INTO `t` (`id`) VALUES (?) ON DUPLICATE KEY UPDATE `id` = VALUES(`id`)" {
		t.Fatalf("空 updateColumns + 空 uniqueBy 应退化为首列自赋值，实际: %s", sql)
	}
}

// TestMySQLCompile_InsertOrIgnoreMultiRowAndExpression 覆盖 MySQL 方言
// CompileInsertOrIgnore 的多行分隔符与 Expression 内联分支。
func TestMySQLCompile_InsertOrIgnoreMultiRowAndExpression(t *testing.T) {
	rows := [][]any{
		{int64(1), NewExpression("UPPER('a')")},
		{int64(2), "b"},
	}
	my := NewMySQLGrammar()
	if sql := my.CompileInsertOrIgnore(newTestBuilder(my, nil).Table("t"), []string{"id", "name"}, rows); sql == "" {
		t.Fatal("MySQL CompileInsertOrIgnore 应产出 SQL")
	}
}

// TestMySQLCompile_SQLComment 验证全部公开编译入口及包装分支的精确 SQL/参数、重复编译和直接 Grammar 边界。
func TestMySQLCompile_SQLComment(t *testing.T) {
	assertCommentCompileCases(t, NewMySQLGrammar(), mysqlCommentCompileExpected())
}

// mysqlCommentCompileExpected 仅保存人工核对的完整 SQL 字面量，不由被测 Grammar 推导期望值。
func mysqlCommentCompileExpected() map[string]commentCompileExpected {
	return map[string]commentCompileExpected{
		"select_group":        {"SELECT `name` FROM `users` WHERE `id` = ? GROUP BY `name` HAVING COUNT(*) > ? ORDER BY `name` ASC LIMIT 5 OFFSET 2", []any{7, 2}},
		"select_distinct":     {"SELECT DISTINCT `name` FROM `users` WHERE `id` = ? ORDER BY `name` ASC LIMIT 5 OFFSET 2", []any{7}},
		"select":              {"SELECT `name` FROM `users` WHERE `id` = ? ORDER BY `name` ASC LIMIT 5 OFFSET 2", []any{7}},
		"select_lock":         {"SELECT `name` FROM `users` WHERE `id` = ? ORDER BY `name` ASC LIMIT 5 OFFSET 2 FOR UPDATE", []any{7}},
		"select_shared_lock":  {"SELECT `name` FROM `users` WHERE `id` = ? ORDER BY `name` ASC LIMIT 5 OFFSET 2 LOCK IN SHARE MODE", []any{7}},
		"select_union":        {"(SELECT `name` FROM `users` WHERE `id` = ?) UNION (SELECT `name` FROM `admins` WHERE `rank` > ?) ORDER BY `name` ASC LIMIT 5 OFFSET 2", []any{7, 9}},
		"insert":              {"INSERT INTO `users` (`name`, `age`) VALUES (?, ?)", []any{"alice", 23}},
		"insert_ignore":       {"INSERT IGNORE INTO `users` (`name`, `age`) VALUES (?, ?)", []any{"alice", 23}},
		"upsert":              {"INSERT INTO `users` (`name`, `age`) VALUES (?, ?) ON DUPLICATE KEY UPDATE `age` = VALUES(`age`)", []any{"alice", 23}},
		"upsert_no_update":    {"INSERT INTO `users` (`name`) VALUES (?) ON DUPLICATE KEY UPDATE `name` = VALUES(`name`)", []any{"alice"}},
		"insert_using":        {"INSERT INTO `users` (`name`) SELECT `name` FROM `source` WHERE `score` > ?", []any{11}},
		"insert_ignore_using": {"INSERT IGNORE INTO `users` (`name`) SELECT `name` FROM `source` WHERE `score` > ?", []any{11}},
		"update":              {"UPDATE `users` INNER JOIN `profiles` ON `users`.`id` = `profiles`.`user_id` AND `profiles`.`active` = ? SET `name` = ?, `age` = ? WHERE `id` = ?", []any{99, "alice", 23, 7}},
		"delete":              {"DELETE FROM `users` WHERE `id` = ? ORDER BY `name` ASC LIMIT 5", []any{7}},
		"delete_join":         {"DELETE `users` FROM `users` INNER JOIN `profiles` ON `users`.`id` = `profiles`.`user_id` AND `profiles`.`active` = ? WHERE `id` = ?", []any{99, 7}},
		"truncate":            {"TRUNCATE TABLE `users`", nil},
		"count":               {"SELECT COUNT(*) FROM `users` WHERE `id` = ?", []any{7}},
		"count_union":         {"SELECT COUNT(*) FROM ((SELECT `name` FROM `users` WHERE `id` = ?) UNION (SELECT `name` FROM `admins` WHERE `rank` > ?)) AS `t`", []any{7, 9}},
		"count_group":         {"SELECT COUNT(*) FROM (SELECT 1 FROM `users` WHERE `id` = ? GROUP BY `name` HAVING COUNT(*) > ?) AS `t`", []any{7, 2}},
		"count_distinct":      {"SELECT COUNT(*) FROM (SELECT DISTINCT `name` FROM `users` WHERE `id` = ?) AS `t`", []any{7}},
		"exists":              {"SELECT 1 FROM `users` WHERE `id` = ? LIMIT 1", []any{7}},
		"exists_union":        {"SELECT 1 FROM ((SELECT 1 FROM `users` WHERE `id` = ?) UNION (SELECT `name` FROM `admins` WHERE `rank` > ?)) AS `t` LIMIT 1", []any{7, 9}},
		"aggregate":           {"SELECT SUM(`age`) AS `aggregate` FROM `users` WHERE `id` = ?", []any{7}},
		"aggregate_union":     {"SELECT MAX(`name`) AS `aggregate` FROM ((SELECT `name` FROM `users` WHERE `id` = ?) UNION (SELECT `name` FROM `admins` WHERE `rank` > ?)) AS `t`", []any{7, 9}},
		"increment":           {"UPDATE `users` INNER JOIN `profiles` ON `users`.`id` = `profiles`.`user_id` AND `profiles`.`active` = ? SET `wallet` = `wallet` + ?, `level` = `level` + ? WHERE `id` = ?", []any{99, 100, 2, 7}},
		"decrement":           {"UPDATE `users` INNER JOIN `profiles` ON `users`.`id` = `profiles`.`user_id` AND `profiles`.`active` = ? SET `wallet` = `wallet` - ?, `level` = `level` - ? WHERE `id` = ?", []any{99, 50, 1, 7}},
	}
}

// TestMySQLCompile_SQLCommentSubqueries 验证结构化子查询注释不内嵌、外层清空及子查询独立编译保留状态。
func TestMySQLCompile_SQLCommentSubqueries(t *testing.T) {
	assertCommentSubqueries(t, NewMySQLGrammar(), "mysql", map[string]commentCompileExpected{
		"child":                {"SELECT `name` FROM `source` WHERE `score` > ?", []any{11}},
		"select":               {"SELECT `name`, (SELECT `name` FROM `source` WHERE `score` > ?) AS `picked` FROM `users` WHERE `id` = ?", []any{11, 7}},
		"from":                 {"SELECT `name` FROM (SELECT `name` FROM `source` WHERE `score` > ?) AS `s` WHERE `id` = ?", []any{11, 7}},
		"where_scalar":         {"SELECT `name` FROM `users` WHERE `id` = ? AND `name` = (SELECT `name` FROM `source` WHERE `score` > ?)", []any{7, 11}},
		"where_in":             {"SELECT `name` FROM `users` WHERE `id` = ? AND `name` IN (SELECT `name` FROM `source` WHERE `score` > ?)", []any{7, 11}},
		"where_exists":         {"SELECT `name` FROM `users` WHERE `id` = ? AND EXISTS (SELECT `name` FROM `source` WHERE `score` > ?)", []any{7, 11}},
		"where_exists_builder": {"SELECT `name` FROM `users` WHERE `id` = ? AND EXISTS (SELECT `name` FROM `source` WHERE `score` > ?)", []any{7, 11}},
		"join_table":           {"SELECT `name` FROM `users` INNER JOIN (SELECT `name` FROM `source` WHERE `score` > ?) AS `s` ON `users`.`name` = `s`.`name` AND `s`.`name` <> ? WHERE `id` = ?", []any{11, "blocked", 7}},
		"join_scalar":          {"SELECT `name` FROM `users` INNER JOIN `profiles` ON `profiles`.`name` = (SELECT `name` FROM `source` WHERE `score` > ?) WHERE `id` = ?", []any{11, 7}},
		"join_in":              {"SELECT `name` FROM `users` INNER JOIN `profiles` ON `profiles`.`name` IN (SELECT `name` FROM `source` WHERE `score` > ?) WHERE `id` = ?", []any{11, 7}},
		"join_exists":          {"SELECT `name` FROM `users` INNER JOIN `profiles` ON EXISTS (SELECT `name` FROM `source` WHERE `score` > ?) WHERE `id` = ?", []any{11, 7}},
		"union":                {"(SELECT `name` FROM `users` WHERE `id` = ?) UNION (SELECT `name` FROM `source` WHERE `score` > ?)", []any{7, 11}},
		"insert_using":         {"INSERT INTO `users` (`name`) SELECT `name` FROM `source` WHERE `score` > ?", []any{11}},
		"insert_ignore_using":  {"INSERT IGNORE INTO `users` (`name`) SELECT `name` FROM `source` WHERE `score` > ?", []any{11}},
		"select_from_where":    {"SELECT `name`, (SELECT `name` FROM `source` WHERE `score` > ?) AS `picked` FROM (SELECT `name` FROM `source` WHERE `score` > ?) AS `s` WHERE `id` = ? AND `name` IN (SELECT `name` FROM `source` WHERE `score` > ?)", []any{11, 11, 7, 11}},
	})
}

// TestMySQLCompile_SQLCommentErrors 验证带注释的累积错误、空表、非法输入及环引用仍返回空 SQL/nil args 和原哨兵错误。
func TestMySQLCompile_SQLCommentErrors(t *testing.T) {
	assertCommentCompileErrors(t, NewMySQLGrammar())
}

// TestMySQLCompile_SQLCommentTransientState 在八个临时状态分支中验证正常返回、Grammar panic 和绑定收集 panic 后恢复；SELECT 子查询列也必须恢复。
func TestMySQLCompile_SQLCommentTransientState(t *testing.T) {
	for _, branch := range []string{"count", "count_union", "count_group", "count_distinct", "exists", "exists_union", "aggregate", "aggregate_union"} {
		t.Run(branch, func(t *testing.T) {
			for _, phase := range []string{"success", "grammar_panic", "bindings_panic"} {
				t.Run(phase, func(t *testing.T) {
					g := NewMySQLGrammar()
					var tc commentCompileFixture
					for _, fixture := range commentCompileFixtures(g) {
						if fixture.name == branch {
							tc = fixture
							break
						}
					}
					if tc.b == nil {
						t.Fatalf("缺少分支 %s", branch)
					}
					b := tc.b.Comment("state:query").Primary().Force()
					if len(b.unions) == 0 {
						b.SelectSub(newTestBuilder(g, nil).Table("source").Select("name").Where("score", ">", 11).Comment("inner:state"), "picked")
					}
					selects := map[string]commentCompileExpected{
						"plain":    {"SELECT `name`, (SELECT `name` FROM `source` WHERE `score` > ?) AS `picked` FROM `users` WHERE `id` = ? ORDER BY `name` ASC LIMIT 5 OFFSET 2 FOR UPDATE /* state:query */", []any{11, 7}},
						"group":    {"SELECT `name`, (SELECT `name` FROM `source` WHERE `score` > ?) AS `picked` FROM `users` WHERE `id` = ? GROUP BY `name` HAVING COUNT(*) > ? ORDER BY `name` ASC LIMIT 5 OFFSET 2 FOR UPDATE /* state:query */", []any{11, 7, 2}},
						"distinct": {"SELECT DISTINCT `name`, (SELECT `name` FROM `source` WHERE `score` > ?) AS `picked` FROM `users` WHERE `id` = ? ORDER BY `name` ASC LIMIT 5 OFFSET 2 FOR UPDATE /* state:query */", []any{11, 7}},
						"union":    {"(SELECT `name` FROM `users` WHERE `id` = ?) UNION (SELECT `name` FROM `admins` WHERE `rank` > ?) ORDER BY `name` ASC LIMIT 5 OFFSET 2 FOR UPDATE /* state:query */", []any{7, 9}},
					}
					selectKind := "plain"
					if len(b.unions) > 0 {
						selectKind = "union"
					}
					if branch == "count_group" {
						selectKind = "group"
					}
					if branch == "count_distinct" {
						selectKind = "distinct"
					}
					sql, args, err := b.ToSelect()
					assertCommentCompileResult(t, selects[selectKind], sql, args, err)
					originalWheres := b.wheres
					if phase == "success" {
						before := snapshotCommentCompileState(b)
						want := mysqlCommentCompileExpected()[branch]
						if branch == "count_distinct" {
							want = commentCompileExpected{"SELECT COUNT(*) FROM (SELECT DISTINCT `name`, (SELECT `name` FROM `source` WHERE `score` > ?) AS `picked` FROM `users` WHERE `id` = ?) AS `t`", []any{11, 7}}
						}
						sql, args, err = tc.compile(b)
						assertCommentCompileResult(t, commentCompileExpected{want.sql + " /* state:query */", want.args}, sql, args, err)
						assertCommentCompileState(t, b, before)
					} else {
						panicker := &commentPanicGrammar{Grammar: g, phase: phase, t: t}
						b.grammar = panicker
						wantPanic := "comment:compile-panic"
						if phase == "bindings_panic" {
							// Grammar 返回后才进入 collectWhereBindings 的未知类型 default panic。
							b.wheres = append(slices.Clone(b.wheres), WhereClause{Type: WhereType(-1)})
							wantPanic = "unhandled default case"
						}
						before := snapshotCommentCompileState(b)
						var recovered any
						func() {
							defer func() { recovered = recover() }()
							sql, args, err := tc.compile(b)
							t.Fatalf("期望 panic，实际正常返回 (%q, %#v, %v)", sql, args, err)
						}()
						if recovered != wantPanic || !panicker.called {
							t.Fatalf("panic 阶段不符：got %#v，want %q，Grammar called=%v", recovered, wantPanic, panicker.called)
						}
						assertCommentCompileState(t, b, before)
						// 仅移除本用例注入的故障；上面已经验证未替换/清理前的完整状态。
						b.grammar, b.wheres = g, originalWheres
					}
					for range 2 {
						sql, args, err = b.ToSelect()
						assertCommentCompileResult(t, selects[selectKind], sql, args, err)
					}
				})
			}
		})
	}
}

// commentPanicGrammar 仅用于 MySQL 编译单测的临时状态恢复故障注入。
type commentPanicGrammar struct {
	Grammar
	phase  string
	t      *testing.T
	called bool
}

func (g *commentPanicGrammar) CompileSelect(b *Builder, _ []SelectColumn) string {
	g.t.Helper()
	g.called = true
	if b.limit != 0 || b.offset != 0 || len(b.orders) != 0 || b.lockClause != "" {
		g.t.Fatalf("故障注入前应已进入临时清除状态：%#v", b)
	}
	if g.phase == "grammar_panic" {
		panic("comment:compile-panic")
	}
	// 不使用真实 Grammar 编译未知 WHERE 类型，确保 panic 来自绑定收集阶段。
	return "SELECT 1"
}
