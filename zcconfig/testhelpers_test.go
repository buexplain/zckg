package zcconfig

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTestFile 将 content 写入临时目录下的 .env 文件并返回其路径。
func writeTestFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}
	return path
}

// resetForTest 前置清空 env 与业务配置存储，并注册 t.Cleanup 在测试结束后再次清空，
// 保证单个测试的注册数据不泄漏到包内后续测试。
func resetForTest(t *testing.T) {
	t.Helper()
	reset()
	t.Cleanup(reset)
}

// loadTestEnv 将 content 写入临时 .env 文件并加载，失败时 t.Fatalf 终止用例。
// 仅用于成功加载路径；需要断言 LoadEnv 错误的用例请直接使用 writeTestFile。
func loadTestEnv(t *testing.T, content string) string {
	t.Helper()
	path := writeTestFile(t, content)
	if err := LoadEnv(path); err != nil {
		t.Fatalf("LoadEnv 失败: %v", err)
	}
	return path
}
