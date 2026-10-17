package zcmodel

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// boolPtr / strPtr 构造指针字面量：Column.Nullable 与 Column.Default 用指针区分
// 「未知 / 无默认值」与零值，测试需显式区分这两种状态。
func boolPtr(v bool) *bool { return &v }

func strPtr(v string) *string { return &v }

// sampleFieldExpectation 是 §3.4 样板表某字段的期望产物：字段名、Entity 侧的 Go 类型与完整 tag。
// DO 侧字段类型恒为 any，tag 与 Entity 完全一致（由用例复用同一份期望）。
type sampleFieldExpectation struct {
	Field string // 结构体字段名
	Type  string // Entity 侧 Go 类型（DO 侧为 any，不在此列）
	Tag   string // 完整 tag（反引号内含 json → 列 tag → primary_key → ddl → description）
}

// sampleFieldExpectations 返回样板表的字段期望，供单元 golden 用例与真库端到端用例**共用同一份
// 事实源**，避免两处各写一份 tag 期望而分叉（样板表的 MySQL DDL 见 integration_test.go 的
// sampleUserOrderDDL，两处期望必须同时改）。
func sampleFieldExpectations() []sampleFieldExpectation {
	return []sampleFieldExpectation{
		{"ID", "int64", "`json:\"id\" db:\"id\" primary_key:\"true\" ddl:\"bigint unsigned NOT NULL AUTO_INCREMENT\" description:\"主键\"`"},
		{"OrderNo", "string", "`json:\"orderNo\" db:\"order_no\" ddl:\"varchar(32) NOT NULL\" description:\"订单号\"`"},
		{"UserID", "int64", "`json:\"userId\" db:\"user_id\" ddl:\"bigint NOT NULL\" description:\"用户ID\"`"},
		{"Email", "string", "`json:\"email\" db:\"email\" ddl:\"varchar(64) NOT NULL\" description:\"用户邮箱\"`"},
		{"Amount", "float64", "`json:\"amount\" db:\"amount\" ddl:\"decimal(10,2) NOT NULL DEFAULT 0.00\" description:\"订单金额（保留两位小数）\"`"},
		{"Status", "string", "`json:\"status\" db:\"status\" ddl:\"enum('pending','paid','refunded') NOT NULL DEFAULT pending\" description:\"订单状态\"`"},
		{"Remark", "string", "`json:\"remark\" db:\"remark\" ddl:\"varchar(255) NULL\" description:\"备注（可为空）\"`"},
		{"CreatedAt", "time.Time", "`json:\"createdAt\" db:\"created_at\" ddl:\"datetime NOT NULL DEFAULT CURRENT_TIMESTAMP\" description:\"创建时间\"`"},
	}
}

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

// typeCheckGoSource 把 src 作为一个独立的单文件 package 做编译级类型检查（go/types，规则与编译器前端一致），
// 能发现 imported and not used、字段与方法同名、未定义标识符等 go/parser 层面发现不了的错误。
func typeCheckGoSource(t *testing.T, src string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "gen.go", src, 0)
	if err != nil {
		t.Fatalf("产物解析失败: %v\n%s", err, truncateForLog(src))
	}
	conf := types.Config{Importer: importer.Default()}
	if _, err := conf.Check(f.Name.Name, fset, []*ast.File{f}, nil); err != nil {
		t.Fatalf("产物未通过编译级类型检查: %v\n%s", err, truncateForLog(src))
	}
}

// importPathsOf 用 AST 提取 src 中全部 import 路径（按出现顺序），解析失败即 Fatal。
func importPathsOf(t *testing.T, src string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "gen.go", src, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("产物解析失败: %v\n%s", err, truncateForLog(src))
	}
	var paths []string
	for _, spec := range f.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("import 路径非法: %v", err)
		}
		paths = append(paths, path)
	}
	return paths
}

// truncateForLog 截断过长的实际内容，避免单条失败信息淹没测试输出。
func truncateForLog(s string) string {
	const max = 2000
	if len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("\n...(内容已截断，共 %d 字节)", len(s))
}

// decodeFuzzColumns 把 fuzz 的列规格解码为 []*Column：'|' 分隔列，';' 分隔
// 列名/类型/注释，最多 16 列；列名、类型、注释各自截断到 1KB，控制单输入规模。
// "nil" 块表示 nil 列，用于覆盖 Generate 的 nil 列错误分支。
func decodeFuzzColumns(spec string) []*Column {
	if spec == "" {
		return nil
	}
	chunks := strings.Split(spec, "|")
	if len(chunks) > 16 {
		chunks = chunks[:16]
	}
	columns := make([]*Column, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk == "nil" {
			columns = append(columns, nil)
			continue
		}
		parts := strings.SplitN(chunk, ";", 3)
		column := &Column{}
		if len(parts) > 0 {
			column.Name = truncateFuzzItem(parts[0])
		}
		if len(parts) > 1 {
			column.Type = truncateFuzzItem(parts[1])
		}
		if len(parts) > 2 {
			column.Comment = truncateFuzzItem(parts[2])
		}
		columns = append(columns, column)
	}
	return columns
}

// truncateFuzzItem 将单个列字段限制在 1KB 内，避免随机输入放大文件 I/O 与类型检查耗时。
func truncateFuzzItem(s string) string {
	const max = 1 << 10
	if len(s) > max {
		return s[:max]
	}
	return s
}

// assertFuzzNoTempFiles 断言目录内没有原子写入遗留的临时文件；目录不存在视为通过。
func assertFuzzNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("read output dir: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp") {
			t.Fatalf("temporary file left behind: %s", entry.Name())
		}
	}
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

// assertDeclOrder 断言 subs 各片段在 got 中按给定顺序出现（以 strings.Index 首个命中位置比较）。
// 任一缺失即 Fatal（后续顺序判断失去意义）；发现逆序即报告该片段位置错误。
// 用于锁定「骨架保持式合并」的顶层声明顺序。
func assertDeclOrder(t *testing.T, got string, subs []string, desc string) {
	t.Helper()
	last := -1
	for _, s := range subs {
		idx := strings.Index(got, s)
		if idx < 0 {
			t.Fatalf("%s：输出缺少 %q\n%s", desc, s, truncateForLog(got))
		}
		if idx < last {
			t.Errorf("%s：顺序错误，%q 位置不正确\n实际:\n%s", desc, s, truncateForLog(got))
		}
		last = idx
	}
}
