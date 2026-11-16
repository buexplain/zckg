package zcconfig_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
)

// docMainExampleRE 匹配文档中以 ```go 开头、内容为完整 package main 的代码块。
var docMainExampleRE = regexp.MustCompile("(?ms)^```go\\r?\\n(package main\\r?\\n.*?)^```\\r?$")

// TestDocExamples_Compile 提取 docs/ 下所有 Markdown 中的完整 main 示例，
// 逐个在独立临时目录用 Go 编译器验证（只构建、不执行）。
// 片段示例（非 package main）由文档维护流程做函数体级语法校验，不在本测试范围内。
func TestDocExamples_Compile(t *testing.T) {
	docs, err := filepath.Glob("docs/*.md")
	if err != nil {
		t.Fatalf("枚举文档失败: %v", err)
	}
	if len(docs) == 0 {
		t.Fatalf("docs 目录下没有 Markdown 文档")
	}

	type example struct {
		source string
		origin string
	}
	var examples []example
	for _, docPath := range docs {
		doc, err := os.ReadFile(docPath)
		if err != nil {
			t.Fatalf("读取文档 %s 失败: %v", docPath, err)
		}
		for _, match := range docMainExampleRE.FindAllSubmatch(doc, -1) {
			examples = append(examples, example{source: string(match[1]), origin: docPath})
		}
	}
	// 至少两个完整示例：Env 通道（README）与 Config 通道（config.md）各一。
	if len(examples) < 2 {
		t.Fatalf("期望文档中至少包含 2 个完整 main 示例，实际 %d 个", len(examples))
	}

	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("解析模块根目录失败: %v", err)
	}
	for i, ex := range examples {
		t.Run(fmt.Sprintf("example_%d", i+1), func(t *testing.T) {
			dir := t.TempDir()
			source := filepath.Join(dir, "main.go")
			if err := os.WriteFile(source, []byte(ex.source), 0600); err != nil {
				t.Fatalf("写入示例失败: %v", err)
			}
			cmd := exec.Command("go", "build", "-o", filepath.Join(dir, "example.exe"), source)
			cmd.Dir = root
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("文档示例编译失败（%s）: %v\n%s", ex.origin, err, output)
			}
		})
	}
}
