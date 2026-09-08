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
