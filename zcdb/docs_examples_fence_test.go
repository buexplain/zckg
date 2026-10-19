// 本文件是文档 Go 代码块的语法门禁，等价于
// `doc_go_examples.py --docs-dir zcdb/docs --strict`：
// 从 Markdown 重新提取每个 ```go 块，按「package p + 块内容」解析，要求全部为语法正确的
// Go（顶层声明，或包在 func example() 中的语句序列），且不含 `...` / `{ ... }` 占位符。
// 修改文档示例导致块不再可解析时本测试失败，提示同步为可解析形态。
package zcdb_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// minDocsGoBlocks 是当前文档的 Go 块数量下限，用于发现「提取规则失效导致零提取」的假通过。
const minDocsGoBlocks = 90

var (
	goFenceStartRE = regexp.MustCompile("^```go\\b")
	fenceEndRE     = regexp.MustCompile("^```\\s*$")
	placeholderRE  = regexp.MustCompile(`^\s*\.\.\.\s*$|\{\s*\.\.\.\s*\}`)
)

// TestDocsGoBlockSyntax 逐个校验 docs/*.md 的 Go 代码块可被 Go 语法解析（不要求编译）。
func TestDocsGoBlockSyntax(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("docs", "*.md"))
	if err != nil {
		t.Fatalf("枚举 docs/*.md 失败: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("未找到 docs/*.md")
	}
	total := 0
	for _, file := range files {
		// 审查报告不是用户文档，不参与示例门禁。
		if strings.HasPrefix(filepath.Base(file), "consistency-review") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", file, err)
		}
		lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
		for i := 0; i < len(lines); i++ {
			if !goFenceStartRE.MatchString(strings.TrimSpace(lines[i])) {
				continue
			}
			start := i + 1 // 1-based 围栏行号
			var body []string
			i++
			for ; i < len(lines) && !fenceEndRE.MatchString(strings.TrimSpace(lines[i])); i++ {
				body = append(body, lines[i])
			}
			if i >= len(lines) {
				t.Fatalf("%s:%d Go 代码块未闭合", file, start)
			}
			total++
			src := "package p\n\n" + strings.Join(body, "\n")
			if _, err := parser.ParseFile(token.NewFileSet(), file, src, parser.AllErrors); err != nil {
				t.Errorf("%s:%d Go 代码块语法错误（--strict 等价检查）: %v", file, start, err)
			}
			for _, line := range body {
				code := line
				if idx := strings.Index(code, "//"); idx >= 0 {
					code = code[:idx]
				}
				if placeholderRE.MatchString(code) {
					t.Errorf("%s:%d Go 代码块含占位符（裸 ... 或 { ... }）", file, start)
				}
			}
		}
	}
	if total < minDocsGoBlocks {
		t.Fatalf("docs 中只提取到 %d 个 go 代码块，期望至少 %d（提取规则可能失效）", total, minDocsGoBlocks)
	}
}
