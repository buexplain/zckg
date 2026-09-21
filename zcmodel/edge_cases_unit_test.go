package zcmodel

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestWriteFileAtomic_StatErrorNonNotExist 覆盖 writeFileAtomic 中
// os.Stat 返回「非 NotExist」错误时直接上报的分支（如权限错误/非法文件名）。
func TestWriteFileAtomic_StatErrorNonNotExist(t *testing.T) {
	dir := t.TempDir()

	var filePath string
	if runtime.GOOS == "windows" {
		// Windows：非法文件名字符使 CreateFile 返回 ERROR_INVALID_NAME（非 NotExist）
		filePath = filepath.Join(dir, "bad|name.go")
	} else {
		// Unix：无权限目录使 Stat 返回 EACCES（非 NotExist）
		blocked := filepath.Join(dir, "noperm")
		if err := os.MkdirAll(blocked, 0755); err != nil {
			t.Fatalf("创建目录失败: %v", err)
		}
		if err := os.Chmod(blocked, 0); err != nil {
			t.Fatalf("修改目录权限失败: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(blocked, 0755) })
		filePath = filepath.Join(blocked, "inner.go")
	}

	err := writeFileAtomic(filePath, []byte("package x"))
	if err == nil {
		t.Fatal("期望 Stat 返回非 NotExist 错误并上报，实际成功")
	}
}

// TestWriteFileAtomic_CreateTempFails 覆盖目标目录不存在时
// os.Stat 返回 NotExist（使用默认 0644）后 CreateTemp 失败的分支。
func TestWriteFileAtomic_CreateTempFails(t *testing.T) {
	root := t.TempDir()
	missingDir := filepath.Join(root, "no_such_dir")
	filePath := filepath.Join(missingDir, "a.go")
	err := writeFileAtomic(filePath, []byte("package x"))
	if err == nil || !strings.Contains(err.Error(), "创建临时文件失败") {
		t.Fatalf("期望创建临时文件失败错误，实际: %v", err)
	}
	// 不存在的父目录不应被隐式创建（writeFileAtomic 不负责建目录）
	if _, statErr := os.Stat(missingDir); !os.IsNotExist(statErr) {
		t.Fatalf("父目录 no_such_dir 不应被隐式创建，os.Stat 返回: %v", statErr)
	}
}

// TestWriteFileAtomic_RenameFails 覆盖 rename 替换目标失败的分支：
// 目标路径已存在且为目录时，各平台的覆盖式 rename 均失败。
func TestWriteFileAtomic_RenameFails(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "adir")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	err := writeFileAtomic(target, []byte("package x"))
	if err == nil || !strings.Contains(err.Error(), "替换目标文件失败") {
		t.Fatalf("期望替换目标文件失败错误，实际: %v", err)
	}

	// rename 失败后 defer 应清理临时文件：父目录内只应剩下目标条目
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取父目录失败: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "adir" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("rename 失败后不应有临时文件残留，父目录实际条目: %v", names)
	}
	// 目标未被破坏：仍是原来的目录
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("目标路径应仍存在: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("rename 失败后目标路径应仍为目录，不应被替换为文件")
	}
}

// TestIsPathWithinDir_RelError 覆盖 filepath.Rel 报错时 isPathWithinDir 直接返回 false 的分支。
// 相对目录与绝对路径混用时 filepath.Rel 在各平台均报错：Rel 比较两侧「是否以分隔符开头」，
// 不一致即报错（Windows 上还叠加卷名不同）。
// 由于返回 false 也可能来自后续的 ".." 前缀检查分支，先断言 Rel 确实报错，确保覆盖目标分支。
func TestIsPathWithinDir_RelError(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("获取工作目录失败: %v", err)
	}
	const relDir = "relative/dir"
	absPath := filepath.Join(wd, "abs.go")

	// 前提校验：与 isPathWithinDir 内部一致地传入 Clean 后的路径
	if _, relErr := filepath.Rel(filepath.Clean(relDir), filepath.Clean(absPath)); relErr == nil {
		t.Fatalf("前提不成立：filepath.Rel(%q, %q) 未报错，本用例无法覆盖 Rel 错误分支", relDir, absPath)
	}
	if isPathWithinDir(relDir, absPath) {
		t.Fatal("相对目录与绝对路径混用时 Rel 报错，应返回 false")
	}
}

// TestBuildStruct_CompositePrimaryKey 验证联合主键：多列均生成 primary_key:"true"
// （主键在 DDL 中是表级约束而非列内联属性，故不并入 ddl 片段，仅由该 tag 与索引块呈现）。
func TestBuildStruct_CompositePrimaryKey(t *testing.T) {
	cols := []Column{
		{Name: "user_id", Type: "bigint", Nullable: boolPtr(false), PrimaryKey: true, StructFieldInfo: StructFieldInfo{Name: "UserID", Type: "int64"}},
		{Name: "role_id", Type: "bigint", Nullable: boolPtr(false), PrimaryKey: true, StructFieldInfo: StructFieldInfo{Name: "RoleID", Type: "int64"}},
		{Name: "granted_at", Type: "datetime", Nullable: boolPtr(false), StructFieldInfo: StructFieldInfo{Name: "GrantedAt", Type: "time.Time"}},
	}
	got := buildStruct("UserRoleEntity", cols, false, "", "db", nil)
	if n := strings.Count(got, `primary_key:"true"`); n != 2 {
		t.Errorf("联合主键应标记两列，实际标记 %d 处:\n%s", n, got)
	}
	if strings.Contains(got, "granted_at\" primary_key") {
		t.Errorf("非主键列不应带 primary_key tag:\n%s", got)
	}
	// 主键标记位于 ddl 之前（tag 顺序 json → db → primary_key → ddl → description）
	assertContains(t, got, "`db:\"user_id\" primary_key:\"true\" ddl:\"bigint NOT NULL\"`", "主键列的 tag 顺序")
	// ddl 片段不含 PRIMARY KEY 约束文本（表级信息由 primary_key tag 与索引块承载）
	assertNotContains(t, got, "ddl:\"bigint NOT NULL PRIMARY KEY", "ddl 片段不应内联主键约束")
}

// TestBuildStruct_EmptyColumnsWithIndexes 覆盖「空列 + 非空索引」组合：
// 结构体为空但索引块照常渲染（数据驱动，两者各自独立），且产物可被 go/parser 解析。
func TestBuildStruct_EmptyColumnsWithIndexes(t *testing.T) {
	indexes := []IndexInfo{{Name: "PRIMARY", Columns: []string{"id"}, Unique: true, Primary: true}}
	got := buildStruct("EmptyEntity", nil, false, "空表", "db", indexes)
	want := "// 空表\n//\n// 索引:\n//   - PRIMARY KEY (id)\ntype EmptyEntity struct {\n}"
	if got != want {
		t.Errorf("空列 + 非空索引的产物错误\ngot:  %q\nwant: %q", got, want)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "", "package model\n"+got, parser.AllErrors); err != nil {
		t.Errorf("产物应可解析: %v\n%s", err, got)
	}
}

// TestBuildIndexCommentBlock_SymbolOnlyName 覆盖纯符号索引名（如 MySQL 函数索引的内部名、用户自造的符号名）：
// 不校验列名/索引名合法性，原样呈现（索引块是提示性元数据，严格校验会误报表达式索引），
// 且符号文本不会破坏注释行结构（产物仍可解析）。
func TestBuildIndexCommentBlock_SymbolOnlyName(t *testing.T) {
	got := buildIndexCommentBlock([]IndexInfo{
		{Name: "-|-", Columns: []string{"a"}},
		{Name: "#expr_idx", Columns: []string{"#expr"}, Unique: true},
	})
	want := []string{
		"//   - UNIQUE KEY #expr_idx (#expr)",
		"//   - KEY -|- (a)",
	}
	if len(got) != len(want) {
		t.Fatalf("索引块行数错误\ngot:  %q\nwant: %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("索引块第 %d 行错误\ngot:  %q\nwant: %q", i+1, got[i], want[i])
		}
	}
	structCode := buildStruct("TEntity", []Column{{Name: "a", Type: "text", StructFieldInfo: StructFieldInfo{Name: "A", Type: "string"}}}, false, "T 表", "db", []IndexInfo{{Name: "-|-", Columns: []string{"a"}}})
	if _, err := parser.ParseFile(token.NewFileSet(), "", "package model\n"+structCode, parser.AllErrors); err != nil {
		t.Errorf("含符号索引名的产物应可解析: %v\n%s", err, structCode)
	}
}
