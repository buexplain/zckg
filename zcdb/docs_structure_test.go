// 本文件是 zcdb 文档的结构门禁，保证分层重构后的文档不会静默腐化：
//  1. docs/ 下每份用户文档的内部链接（相对路径）都能解析到真实文件；
//  2. zcdb 的每个导出 API（Builder/JoinBuilder/DBDao/Pool 方法与包级导出函数）至少在文档中
//     被提到一次，新增公开 API 却完全没写文档时失败；
//  3. 重构前的扁平主题文档（query-builder.md 等）不得回流，避免与分层文档并存造成事实分叉。
//
// 说明：只校验链接目标存在，不校验 #锚点；审查报告（consistency-review*）不参与。
package zcdb_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var docsLinkRE = regexp.MustCompile(`\]\(([^)\s]+)\)`)

// walkUserDocs 遍历 docs/ 下除审查报告外的全部 Markdown。
func walkUserDocs(t *testing.T, fn func(path string, content []byte)) {
	t.Helper()
	err := filepath.WalkDir("docs", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") || strings.HasPrefix(d.Name(), "consistency-review") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fn(path, content)
		return nil
	})
	if err != nil {
		t.Fatalf("遍历 docs 失败: %v", err)
	}
}

// TestDocsLinksResolve 校验每份用户文档中的相对链接都指向存在的文件。
func TestDocsLinksResolve(t *testing.T) {
	count := 0
	walkUserDocs(t, func(path string, content []byte) {
		dir := filepath.Dir(path)
		for _, m := range docsLinkRE.FindAllStringSubmatch(string(content), -1) {
			target := m[1]
			if target == "" || strings.HasPrefix(target, "#") ||
				strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") ||
				strings.HasPrefix(target, "mailto:") {
				continue
			}
			if idx := strings.IndexByte(target, '#'); idx >= 0 {
				target = target[:idx]
			}
			if target == "" || filepath.IsAbs(target) {
				continue
			}
			resolved := filepath.Join(dir, filepath.FromSlash(target))
			if _, err := os.Stat(resolved); err != nil {
				t.Errorf("%s: 链接 %q 解析失败: %v", path, m[1], err)
			}
			count++
		}
	})
	if count == 0 {
		t.Fatal("未在 docs 中发现任何相对链接，提取规则可能失效")
	}
}

// TestDocsCoverExportedAPI 校验每个导出 API 至少在文档中出现一次（按标识符词边界匹配）。
// 新增公开方法/函数时，若完全未写入任何文档，本测试失败并给出遗漏名单。
func TestDocsCoverExportedAPI(t *testing.T) {
	apiNames, err := exportedAPINames(".")
	if err != nil {
		t.Fatalf("解析 zcdb 导出 API 失败: %v", err)
	}
	if len(apiNames) == 0 {
		t.Fatal("未解析到任何导出 API，解析规则可能失效")
	}

	var docs strings.Builder
	walkUserDocs(t, func(_ string, content []byte) {
		docs.Write(content)
		docs.WriteByte('\n')
	})
	text := docs.String()

	for _, name := range apiNames {
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
		if !re.MatchString(text) {
			t.Errorf("导出 API %s 未出现在任何用户文档中", name)
		}
	}
}

// exportedAPINames 收集 zcdb 包中面向使用者的导出标识符：
// Builder/JoinBuilder/DBDao/Pool 的导出方法，以及全部包级导出函数（构造器、ScanStruct 等）。
func exportedAPINames(dir string) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	fset := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			return nil, err
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !fn.Name.IsExported() {
				continue
			}
			if fn.Recv == nil {
				seen[fn.Name.Name] = true
				continue
			}
			recv := receiverTypeName(fn.Recv)
			switch recv {
			case "Builder", "JoinBuilder", "DBDao", "Pool":
				seen[fn.Name.Name] = true
			}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	return names, nil
}

// receiverTypeName 返回方法接收者的基础类型名（去掉指针与泛型参数）。
func receiverTypeName(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) == 0 {
		return ""
	}
	switch expr := recv.List[0].Type.(type) {
	case *ast.StarExpr:
		if ident, ok := expr.X.(*ast.Ident); ok {
			return ident.Name
		}
	case *ast.Ident:
		return expr.Name
	}
	return ""
}

// TestDocsOldFlatPagesRemoved 防止重构前的扁平主题文档回流。
func TestDocsOldFlatPagesRemoved(t *testing.T) {
	oldPages := []string{
		"docs/compile.md",
		"docs/connection.md",
		"docs/mutate.md",
		"docs/query-builder.md",
		"docs/query-exec.md",
		"docs/schema.md",
	}
	for _, page := range oldPages {
		if _, err := os.Stat(page); err == nil {
			t.Errorf("旧扁平文档 %s 仍存在：内容应已迁入 concepts/reference/how-to", page)
		} else if !os.IsNotExist(err) {
			t.Errorf("检查 %s 失败: %v", page, err)
		}
	}
}
