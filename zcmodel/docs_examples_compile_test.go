package zcmodel_test

// docs/zcmodel.md 示例代码的编译级校验（对应「文档与代码一致」中的示例可编译性）。
// 手工收录文档示例，不自动读取 Markdown；文档示例变更须同步本文件。
// 使用外部测试包（zcmodel_test）以调用方视角校验 API 写法，不伪造任何测试存根。
// 除 TestDocExamples_ManualInput 实际执行一次生成流程外，其余函数仅用于编译期核对。
// 通过 go test ./zcmodel -run '^$' 使用真实 Go 编译器检查外部包调用。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/buexplain/zckg/zcdb"
	"github.com/buexplain/zckg/zcmodel"
	_ "github.com/go-sql-driver/mysql"
)

// docManualInput 对应 docs/zcmodel.md「手工构造 Input 生成」示例的 Input 构造部分。
// OutputDir 提为参数便于测试写入临时目录（文档中为 "./model"），其余字段与文档逐项一致。
func docManualInput(outputDir string) zcmodel.Input {
	// Nullable 与 Default 为指针：nil 表示「未知 / 无默认值」，需辅助函数取字面量地址
	nullable := func(v bool) *bool { return &v }
	strPtr := func(v string) *string { return &v }

	return zcmodel.Input{
		OutputDir:        outputDir, // 目录名 "model" 即生成文件的包名
		Database:         "test_db",
		Dialect:          zcmodel.DialectMysql,
		TableName:        "user_order",
		TableComment:     "订单表",
		ColumnTagName:    "db", // 与 zcdb 的列映射标签保持一致
		JsonTagValueCase: zcmodel.NameCaseLowerCamel,
		Columns: []*zcmodel.Column{
			{Name: "id", Type: "bigint unsigned", Nullable: nullable(false), PrimaryKey: true, Extra: "auto_increment", Comment: "主键"},
			{Name: "order_no", Type: "varchar(64)", Nullable: nullable(false), Comment: "订单号"},
			{Name: "amount", Type: "decimal(10,2)", Nullable: nullable(false), Default: strPtr("0.00")},
			{Name: "remark", Type: "varchar(255)", Nullable: nullable(true), Comment: "备注"},
			{Name: "created_at", Type: "datetime", Nullable: nullable(false), Default: strPtr("CURRENT_TIMESTAMP"), Comment: "创建时间"},
		},
		// 索引块（可选）：留空则结构体 doc 注释中不生成索引清单
		Indexes: []zcmodel.IndexInfo{
			{Name: "PRIMARY", Columns: []string{"id"}, Unique: true, Primary: true},
			{Name: "uk_order_no", Columns: []string{"order_no"}, Unique: true},
		},
	}
}

// docManualExample 逐字镜像文档示例的主流程与错误处理（OutputDir 见 docManualInput 说明）。
func docManualExample() {
	err := zcmodel.Generate(docManualInput("./model"))
	if err != nil {
		panic(err)
	}
	// 生成 ./model/user_order.go：UserOrderEntity + UserOrderDO + ToDO/ToEntity
}

// docExplicitFieldInfo 对应 docs/zcmodel.md「显式指定字段信息（覆盖自动推导）」示例。
func docExplicitFieldInfo() {
	columns := []*zcmodel.Column{
		{
			Name: "extra", Type: "json", Comment: "扩展信息",
			StructFieldInfo: zcmodel.StructFieldInfo{
				Name:   "Extra",
				Type:   "map[string]any", // 手工指定类型，跳过映射表
				Import: "",               // 内置类型无需 import
			},
		},
	}
	_ = columns
}

// docSchemaBridge 对应 docs/zcmodel.md「配合 zcdb Schema 从真实数据库生成」示例：
// NewDBDao 第四参数为列映射标签名（空串使用 db），第五参数为默认 SQL 短业务标识（空串保持无注释基线）。
// 列元数据经取址桥接为 zcmodel 的 *bool（可空性）并透传 Default/PrimaryKey/Extra；
// 索引元数据经 inspector.Indexes 桥接为 zcmodel.IndexInfo，生成结构体 doc 注释中的索引块。
func docSchemaBridge() {
	pool, err := zcdb.NewPool(zcdb.PoolConfig{
		DriverName: "mysql",
		DSN:        "user:pass@tcp(127.0.0.1:3306)/test?parseTime=true",
	})
	if err != nil {
		panic(err)
	}
	dao, err := zcdb.NewDBDao(pool, "mysql", nil, "", "")
	if err != nil {
		panic(err)
	}
	defer func() { _ = dao.Close() }()

	// 1. 读取表结构（列名、列类型、列注释、可空性、主键标记与 MySQL EXTRA）
	inspector, err := dao.Schema()
	if err != nil {
		panic(err)
	}
	cols, err := inspector.Columns(context.Background(), "user_order")
	if err != nil {
		panic(err)
	}
	columns := make([]*zcmodel.Column, 0, len(cols))
	for _, c := range cols {
		nullable := c.Nullable // 取副本地址：Column.Nullable 为 *bool，而 SchemaInspector 给出的可空性恒已知
		columns = append(columns, &zcmodel.Column{
			Name: c.Name, Type: c.Type, Comment: c.Comment,
			Nullable: &nullable, Default: c.Default, PrimaryKey: c.PrimaryKey, Extra: c.Extra,
		})
	}

	// 2. 读取索引（主键、唯一、普通索引）——生成结构体 doc 注释中的索引块
	indexes, err := inspector.Indexes(context.Background(), "user_order")
	if err != nil {
		panic(err)
	}
	idxInfos := make([]zcmodel.IndexInfo, 0, len(indexes))
	for _, i := range indexes {
		idxInfos = append(idxInfos, zcmodel.IndexInfo{
			Name: i.Name, Columns: i.Columns, Unique: i.Unique, Primary: i.Primary,
		})
	}

	// 3. 生成模型代码
	err = zcmodel.Generate(zcmodel.Input{
		OutputDir:        "./model",
		Database:         "test",
		Dialect:          zcmodel.DialectMysql,
		TableName:        "user_order",
		ColumnTagName:    "db",
		JsonTagValueCase: zcmodel.NameCaseLowerCamel,
		Columns:          columns,
		Indexes:          idxInfos,
	})
	if err != nil {
		panic(err)
	}
}

// TestDocExamples_ManualInput 执行文档「手工构造 Input 生成」示例的 Input，核对文档描述的产物：
// 文件带说明头、Entity/DO 各含索引块（PRIMARY 行不显示物理索引名）、主键自增与可空列的 ddl 片段形态符合文档示例。
func TestDocExamples_ManualInput(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "model")
	if err := zcmodel.Generate(docManualInput(dir)); err != nil {
		t.Fatalf("文档示例的 Input 应生成成功，error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "user_order.go"))
	if err != nil {
		t.Fatalf("读取生成文件失败: %v", err)
	}
	got := string(content)
	for _, want := range []string{
		"// 本文件由 zcmodel 部分生成：Entity/DO 结构体及 ToDO/ToEntity 方法在再次调用",
		"//   - PRIMARY KEY (id)",
		"//   - UNIQUE KEY uk_order_no (order_no)",
		"`json:\"id\" db:\"id\" primary_key:\"true\" ddl:\"bigint unsigned NOT NULL AUTO_INCREMENT\" description:\"主键\"`",
		"`json:\"remark\" db:\"remark\" ddl:\"varchar(255) NULL\" description:\"备注\"`",
		"`json:\"createdAt\" db:\"created_at\" ddl:\"datetime NOT NULL DEFAULT CURRENT_TIMESTAMP\" description:\"创建时间\"`",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("文档示例产物缺少 %q\n实际内容:\n%s", want, got)
		}
	}
	// 索引块在 Entity 与 DO 的 doc 注释中各出现一次
	if n := strings.Count(got, "//   - PRIMARY KEY (id)"); n != 2 {
		t.Errorf("索引块应在 Entity 与 DO 各出现一次，实际 %d 次:\n%s", n, got)
	}
}
