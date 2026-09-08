package zcmodel

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeAndVerify 执行 writeOrReplaceStruct 的标准流程并返回产物内容：
// 在独立临时目录下建立包目录 model（目录名即新建文件的包名推导来源），
// initialContent 非空时先落盘为存量文件（模拟再生成场景），空串表示新建文件场景；
// 随后以固定的 UserEntity/UserDO 结构体名调用 writeOrReplaceStruct，失败即 Fatal。
//
// 仅适用于「标准目录 + 标准结构体名 + 预期成功」的场景；
// 只读目录、权限保持、包名回退等特殊场景应保留独立的测试实现。
func writeAndVerify(t *testing.T, initialContent, entityCode, doCode string, neededImports []string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "model")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	filePath := filepath.Join(dir, "user.go")
	if initialContent != "" {
		if err := os.WriteFile(filePath, []byte(initialContent), 0644); err != nil {
			t.Fatalf("写入初始文件失败: %v", err)
		}
	}
	if err := writeOrReplaceStruct(filePath, "UserEntity", entityCode, "UserDO", doCode, neededImports); err != nil {
		t.Fatalf("writeOrReplaceStruct() error = %v", err)
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	return string(content)
}

// truncateForLog 截断过长的实际内容，避免单条失败信息淹没测试输出。
func truncateForLog(s string) string {
	const max = 2000
	if len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("\n...(内容已截断，共 %d 字节)", len(s))
}

// assertContains 断言 got 包含 want，失败时输出缺失片段与截断后的实际内容。
// desc 说明被断言的语义，用于区分同一测试内的多处断言。
// 断言失败按 Errorf 处理（不中断），需要中断的前置条件请直接使用 t.Fatalf。
func assertContains(t *testing.T, got, want, desc string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("%s：缺少 %q\n实际内容:\n%s", desc, want, truncateForLog(got))
	}
}

// assertNotContains 断言 got 不包含 notWant，失败时输出多余片段与截断后的实际内容。
func assertNotContains(t *testing.T, got, notWant, desc string) {
	t.Helper()
	if strings.Contains(got, notWant) {
		t.Errorf("%s：不应包含 %q\n实际内容:\n%s", desc, notWant, truncateForLog(got))
	}
}

// assertContainsAll 断言 got 包含 wants 中的每个片段，逐项报告缺失项而非首个即停。
func assertContainsAll(t *testing.T, got string, wants []string, desc string) {
	t.Helper()
	for _, want := range wants {
		assertContains(t, got, want, desc)
	}
}
