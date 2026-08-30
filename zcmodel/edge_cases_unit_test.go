package zcmodel

import (
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
	filePath := filepath.Join(t.TempDir(), "no_such_dir", "a.go")
	err := writeFileAtomic(filePath, []byte("package x"))
	if err == nil || !strings.Contains(err.Error(), "创建临时文件失败") {
		t.Fatalf("期望创建临时文件失败错误，实际: %v", err)
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
}

// TestWriteOrReplaceStruct_PkgNameFallback 覆盖包名推导回退分支：
// 相对路径（无目录部分）的 Dir 为 "."，包名回退为 "main"。
func TestWriteOrReplaceStruct_PkgNameFallback(t *testing.T) {
	const filePath = "zcmodel_fallback_probe_output.go"
	t.Cleanup(func() { _ = os.Remove(filePath) })

	entityCode := "type FallBackEntity struct {\n\tID int\n}"
	doCode := "type FallBackDO struct {\n\tID any\n}"
	if err := writeOrReplaceStruct(filePath, "FallBackEntity", entityCode, "FallBackDO", doCode, nil); err != nil {
		t.Fatalf("writeOrReplaceStruct() error = %v", err)
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取生成文件失败: %v", err)
	}
	if !strings.HasPrefix(string(content), "package main\n") {
		t.Fatalf("无目录部分的相对路径应回退为 package main，实际:\n%s", content)
	}
}

// TestWriteOrReplaceStruct_ReadFileFails 覆盖现有文件读取失败的分支：
// 目标路径已存在但为目录时，os.Stat 成功而 ReadFile 失败。
func TestWriteOrReplaceStruct_ReadFileFails(t *testing.T) {
	target := filepath.Join(t.TempDir(), "adir.go")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	err := writeOrReplaceStruct(target, "E", "type E struct{}", "D", "type D struct{}", nil)
	if err == nil {
		t.Fatal("目标路径为目录时 ReadFile 应失败")
	}
}

// TestWriteOrReplaceStruct_SingleImportDocAndComment 覆盖单行 import 声明
// （无括号）合并缺失 import 的路径：ImportSpec 的 Doc/尾注释经 specSpan 保留，
// import 声明自身的 Doc 经 mergeMissingImports 的 first.Doc 分支保留，并展开为块形式。
func TestWriteOrReplaceStruct_SingleImportDocAndComment(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "user.go")
	orig := `package model

// import block doc
import "fmt" // trailing comment

var _ = fmt.Sprintf
`
	if err := os.WriteFile(filePath, []byte(orig), 0644); err != nil {
		t.Fatalf("写入初始文件失败: %v", err)
	}

	entityCode := "type UserEntity struct {\n\tID int\n}"
	doCode := "type UserDO struct {\n\tID any\n}"
	if err := writeOrReplaceStruct(filePath, "UserEntity", entityCode, "UserDO", doCode, []string{"time"}); err != nil {
		t.Fatalf("writeOrReplaceStruct() error = %v", err)
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	s := string(content)
	for _, want := range []string{"// import block doc", "import (", `"fmt" // trailing comment`, `"time"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("结果缺少 %q:\n%s", want, s)
		}
	}
}

// TestWriteOrReplaceStruct_MixedBlockDocAndTrailing 覆盖混合 type 块剔除生成类型时
// GenDecl 自身 Doc 注释保留（removeGeneratedSpecs 的 d.Doc 分支），
// 以及生成类型 Spec 带尾注释时的区间剔除（specSpan 的 TypeSpec.Comment 分支）。
func TestWriteOrReplaceStruct_MixedBlockDocAndTrailing(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "user.go")
	orig := `package model

// block doc
type (
	// user type doc
	UserType struct {
		Name string
	}
	UserEntity struct {
		ID int
	} // generated trailing
)
`
	if err := os.WriteFile(filePath, []byte(orig), 0644); err != nil {
		t.Fatalf("写入初始文件失败: %v", err)
	}

	entityCode := "type UserEntity struct {\n\tID int64\n}"
	doCode := "type UserDO struct {\n\tID any\n}"
	if err := writeOrReplaceStruct(filePath, "UserEntity", entityCode, "UserDO", doCode, nil); err != nil {
		t.Fatalf("writeOrReplaceStruct() error = %v", err)
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	s := string(content)
	if !strings.Contains(s, "// block doc") {
		t.Fatalf("混合 type 块的块级 Doc 注释应保留:\n%s", s)
	}
	if !strings.Contains(s, "UserType") || !strings.Contains(s, "// user type doc") {
		t.Fatalf("用户类型及其注释应保留:\n%s", s)
	}
	if strings.Contains(s, "generated trailing") {
		t.Fatalf("旧生成类型的尾注释应随 Spec 一并剔除:\n%s", s)
	}
	if !strings.Contains(s, "int64") {
		t.Fatalf("新生成的 Entity 代码应写入:\n%s", s)
	}
}

// TestWriteOrReplaceStruct_NonIdentReceiverKept 覆盖 receiverTypeName 对
// 非 Ident/指针接收者（如泛型实例化 Box[int]）返回空串的分支：
// 此类方法既非生成方法也不归属 Entity/DO，原样保留为其他用户代码。
func TestWriteOrReplaceStruct_NonIdentReceiverKept(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "user.go")
	orig := `package model

type Box[T any] struct{ V T }

func (b Box[int]) Marker() int { return 0 }

type UserEntity struct {
	ID int
}
`
	if err := os.WriteFile(filePath, []byte(orig), 0644); err != nil {
		t.Fatalf("写入初始文件失败: %v", err)
	}

	entityCode := "type UserEntity struct {\n\tID int64\n}"
	doCode := "type UserDO struct {\n\tID any\n}"
	if err := writeOrReplaceStruct(filePath, "UserEntity", entityCode, "UserDO", doCode, nil); err != nil {
		t.Fatalf("writeOrReplaceStruct() error = %v", err)
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	if !strings.Contains(string(content), "func (b Box[int]) Marker()") {
		t.Fatalf("泛型实例化接收者的用户方法应保留:\n%s", content)
	}
}

// TestWriteOrReplaceStruct_NoImportDeclAddMissing 覆盖文件完全没有 import 声明、
// 但 neededImports 非空时新建 import 声明前置的分支。
func TestWriteOrReplaceStruct_NoImportDeclAddMissing(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "user.go")
	orig := `package model

type UserEntity struct {
	ID int
}
`
	if err := os.WriteFile(filePath, []byte(orig), 0644); err != nil {
		t.Fatalf("写入初始文件失败: %v", err)
	}

	entityCode := "type UserEntity struct {\n\tCreatedAt time.Time\n}"
	doCode := "type UserDO struct {\n\tCreatedAt any\n}"
	if err := writeOrReplaceStruct(filePath, "UserEntity", entityCode, "UserDO", doCode, []string{"time"}); err != nil {
		t.Fatalf("writeOrReplaceStruct() error = %v", err)
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	if !strings.Contains(string(content), `import "time"`) {
		t.Fatalf("无 import 声明时应新建 import 声明:\n%s", content)
	}
}

// TestIsPathWithinDir_RelError 覆盖 filepath.Rel 因相对/绝对路径混用
// 而报错时返回 false 的分支。
func TestIsPathWithinDir_RelError(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("获取工作目录失败: %v", err)
	}
	if isPathWithinDir("relative/dir", filepath.Join(wd, "abs.go")) {
		t.Fatal("相对目录与绝对路径混用时 Rel 报错，应返回 false")
	}
}

// TestWriteOrReplaceStruct_NamedImportWithDoc 覆盖 specSpan 的 ImportSpec.Doc 分支
// 与 mergeMissingImports 的命名导入（Name 非空）分支：块内带文档注释的别名导入
// 应完整保留，缺失的标准导入仍可追加。
func TestWriteOrReplaceStruct_NamedImportWithDoc(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "user.go")
	orig := `package model

import (
	// fmt alias doc
	f "fmt"
)

type UserEntity struct {
	ID int
}
`
	if err := os.WriteFile(filePath, []byte(orig), 0644); err != nil {
		t.Fatalf("写入初始文件失败: %v", err)
	}

	entityCode := "type UserEntity struct {\n\tID int64\n}"
	doCode := "type UserDO struct {\n\tID any\n}"
	if err := writeOrReplaceStruct(filePath, "UserEntity", entityCode, "UserDO", doCode, []string{"time"}); err != nil {
		t.Fatalf("writeOrReplaceStruct() error = %v", err)
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	s := string(content)
	for _, want := range []string{"// fmt alias doc", `f "fmt"`, `"time"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("结果缺少 %q:\n%s", want, s)
		}
	}
}
