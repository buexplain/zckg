package zcconfig_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
)

// TestDocExamples_Compile 原样提取文档中的两个完整 main，在独立临时目录用 Go 编译器验证，不执行示例。
func TestDocExamples_Compile(t *testing.T) {
	doc, err := os.ReadFile("docs/zcconfig.md")
	if err != nil {
		t.Fatalf("读取文档失败: %v", err)
	}
	examples := regexp.MustCompile("(?ms)^```go\\r?\\n(package main\\r?\\n.*?)^```\\r?$").FindAllSubmatch(doc, -1)
	if len(examples) != 2 {
		t.Fatalf("期望文档包含两个完整 main 示例，实际 %d 个", len(examples))
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("解析模块根目录失败: %v", err)
	}
	for i, example := range examples {
		t.Run(fmt.Sprintf("example_%d", i+1), func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "main.go")
			if err := os.WriteFile(source, example[1], 0600); err != nil {
				t.Fatalf("写入示例失败: %v", err)
			}
			cmd := exec.Command("go", "build", "-o", filepath.Join(dir, "example.exe"), source)
			cmd.Dir = root
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("文档示例编译失败: %v\n%s", err, output)
			}
		})
	}
}
