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
