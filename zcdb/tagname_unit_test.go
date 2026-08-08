// 本文件为自定义列映射标签名（NewDBDao 的 tagName 参数）的单元测试，
// 仅验证编译与内部逻辑，不依赖数据库连接。
package zcdb

import (
	"reflect"
	"testing"
)

// newTagDao 创建仅用于编译测试的 DAO（无需连接池，编译路径不触达连接）。
func newTagDao(tag string) *DBDao {
	return &DBDao{grammar: NewMySQLGrammar(), tagName: tag}
}

// 自定义标签测试结构体：zc 标签映射列名，db 标签此时不应生效
type userCustomTag struct {
	Name string `zc:"user_name" db:"name"`
	Age  int    `zc:"user_age"`
	// 无自定义标签：回退 snake_case 字段名
	EmailAddress string
	// 自定义标签值为 "-"：跳过
	Secret string `zc:"-"`
}

// TestNewDBDao_TagName_Insert 验证自定义标签下的 INSERT 编译：
// zc 标签生效、db 标签被忽略、无标签字段走 snake_case、"-" 字段跳过。
func TestNewDBDao_TagName_Insert(t *testing.T) {
	dao := newTagDao("zc")
	sql, args, err := NewBuilder(dao.grammar, dao).
		Table("users").
		ToInsert(userCustomTag{Name: "alice", Age: 25, EmailAddress: "a@t.com", Secret: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "INSERT INTO `users` (`user_name`, `user_age`, `email_address`) VALUES (?, ?, ?)"
	if sql != expected {
		t.Errorf("sql mismatch:\ngot:  %s\nwant: %s", sql, expected)
	}
	if len(args) != 3 || args[0] != "alice" || args[1] != 25 || args[2] != "a@t.com" {
		t.Errorf("args mismatch: %v", args)
	}
}

// TestNewDBDao_TagName_Update 验证自定义标签下的 UPDATE 编译。
func TestNewDBDao_TagName_Update(t *testing.T) {
	dao := newTagDao("zc")
	sql, args, err := NewBuilder(dao.grammar, dao).
		Table("users").
		Where("user_name", "=", "alice").
		ToUpdate(userCustomTag{Name: "alice", Age: 26})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "UPDATE `users` SET `user_name` = ?, `user_age` = ?, `email_address` = ? WHERE `user_name` = ?"
	if sql != expected {
		t.Errorf("sql mismatch:\ngot:  %s\nwant: %s", sql, expected)
	}
	if len(args) != 4 {
		t.Errorf("args mismatch: %v", args)
	}
}

// TestNewDBDao_TagName_EmptyDefaults 验证 tagName 传空字符串时使用默认 db 标签。
func TestNewDBDao_TagName_EmptyDefaults(t *testing.T) {
	dao := newTagDao("") // 模拟 NewDBDao 归一化前的空值场景

	type u struct {
		Name string `db:"name" zc:"zc_name"`
	}
	sql, _, err := NewBuilder(dao.grammar, dao).Table("users").ToInsert(u{Name: "alice"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "INSERT INTO `users` (`name`) VALUES (?)"
	if sql != expected {
		t.Errorf("sql mismatch:\ngot:  %s\nwant: %s", sql, expected)
	}
}

// TestBuilder_TagName_NilDao 验证 dao 为 nil 时回退默认 db 标签。
func TestBuilder_TagName_NilDao(t *testing.T) {
	b := NewBuilder(NewMySQLGrammar(), nil)
	if got := b.tagName(); got != defaultTagName {
		t.Errorf("tagName() = %q, want %q", got, defaultTagName)
	}
}

// TestParseStruct_CustomTag 验证 parseStruct 按自定义标签解析列名。
func TestParseStruct_CustomTag(t *testing.T) {
	info := parseStruct(reflect.TypeOf(userCustomTag{}), "zc")
	if info == nil {
		t.Fatal("parseStruct returned nil")
	}

	got := make([]string, 0, len(info.Fields))
	for _, f := range info.Fields {
		got = append(got, f.Column)
	}
	want := []string{"user_name", "user_age", "email_address"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("columns = %v, want %v", got, want)
	}
}

// TestGetScanFieldInfo_CustomTag 验证扫描映射缓存按（类型, 标签名）区分：
// 同一类型在 db / zc 两个标签名下得到不同的列映射，互不串扰。
func TestGetScanFieldInfo_CustomTag(t *testing.T) {
	dbInfo := getScanFieldInfo(reflect.TypeOf(userCustomTag{}), "db")
	zcInfo := getScanFieldInfo(reflect.TypeOf(userCustomTag{}), "zc")

	// db 标签映射：name/age/email_address（zc 标签不生效；Secret 无 db 标签走 snake_case）
	for _, col := range []string{"name", "age", "email_address", "secret"} {
		if _, ok := dbInfo.columnIndex[col]; !ok {
			t.Errorf("db tag: column %q not found", col)
		}
	}
	// zc 标签映射：user_name/user_age/email_address，secret 被 "-" 跳过
	for _, col := range []string{"user_name", "user_age", "email_address"} {
		if _, ok := zcInfo.columnIndex[col]; !ok {
			t.Errorf("zc tag: column %q not found", col)
		}
	}
	if _, ok := zcInfo.columnIndex["secret"]; ok {
		t.Error("zc tag: column 'secret' should be skipped (zc:\"-\")")
	}
	if _, ok := zcInfo.columnIndex["name"]; ok {
		t.Error("zc tag: column 'name' should not exist (db tag ignored)")
	}

	// 缓存命中：相同（类型, 标签名）返回同一指针
	if getScanFieldInfo(reflect.TypeOf(userCustomTag{}), "zc") != zcInfo {
		t.Error("expected cache hit for same (type, tag)")
	}
}

// TestPickTagName 验证标签名选取规则：未传/空值回退默认 db 标签。
func TestPickTagName(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{"no arg", nil, "db"},
		{"empty arg", []string{""}, "db"},
		{"custom", []string{"zc"}, "zc"},
		{"first wins", []string{"zc", "other"}, "zc"},
	}
	for _, tt := range tests {
		if got := pickTagName(tt.in); got != tt.want {
			t.Errorf("%s: pickTagName(%v) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}
