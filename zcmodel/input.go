package zcmodel

type NameCase string

const (
	// NameCaseLowerCamel lowerCamel 风格，如 getUserById
	NameCaseLowerCamel NameCase = "lowerCamel"
	// NameCaseUpperCamel upperCamel 风格，如 GetUserById
	NameCaseUpperCamel NameCase = "upperCamel"
	// NameCaseLowerSnake lowerSnake 风格，如 user_id
	NameCaseLowerSnake NameCase = "lowerSnake"
	// NameCaseUpperSnake upperSnake 风格，如 USER_ID
	NameCaseUpperSnake NameCase = "upperSnake"
	// NameCaseLowerKebab lowerKebab 风格，如 user-id
	NameCaseLowerKebab NameCase = "lowerKebab"
	// NameCaseUpperKebab upperKebab 风格，如 USER-ID
	NameCaseUpperKebab NameCase = "upperKebab"
)

// IsValid 判断 NameCase 是否为已定义的枚举值
func (c NameCase) IsValid() bool {
	switch c {
	case NameCaseLowerCamel, NameCaseUpperCamel, NameCaseLowerSnake,
		NameCaseUpperSnake, NameCaseLowerKebab, NameCaseUpperKebab:
		return true
	}
	return false
}

// Dialect 数据库方言
type Dialect string

const (
	DialectMysql    Dialect = "mysql"
	DialectPostgres Dialect = "postgres"
	DialectSqlite   Dialect = "sqlite"
)

type Input struct {
	OutputDir        string      // 输出目录
	Database         string      // 数据库名
	Dialect          Dialect     // 数据库方言（"mysql"、"postgres"、"sqlite"）
	TableName        string      // 表名（支持任意命名风格，用于推导文件名和结构体名）
	TableComment     string      // 表注释
	ColumnTagName    string      // 表字段的结构体字段的 tag 名称（如 "column"、"db" 等）
	JsonTagValueCase NameCase    // JSON tag 的命名风格，空值不生成 json tag
	Columns          []*Column   //表所有的字段
	Indexes          []IndexInfo // 表索引；空则不生成结构体注释中的索引块
}

// StructFieldInfo 生成结构体字段时候的信息
type StructFieldInfo struct {
	Name         string // 表字段转成结构体字段的名字
	Type         string // 表字段类型转成结构体字段的类型
	Import       string // Type 对应的 import 路径（如 time.Time 需要 "time"），空值不引入任何包
	JsonTagValue string //表字段转成结构体字段的json tag的值
}

// IndexInfo 表索引信息，渲染为 Entity/DO 结构体注释中的索引块。
type IndexInfo struct {
	Name    string   // 索引名（仅唯一/普通索引渲染该名字；主键行渲染为 PRIMARY KEY (cols)，不显示方言内部物理索引名）
	Columns []string // 索引列，按定义顺序；表达式列可能为表达式文本或占位符
	Unique  bool     // 是否唯一索引
	Primary bool     // 是否主键索引
}

type Column struct {
	// 列名
	Name string
	// 列类型（如 "VARCHAR(255)"、"bigint(20)"、"text"），ddl 片段的锚点
	Type string
	// 列注释，会生成到结构体字段的tag中，示例：description:"Comment"
	Comment string
	// 是否可空；nil 表示未知，ddl 片段省略 NULL 标记；true 生成 NULL，false 生成 NOT NULL
	Nullable *bool
	// 默认值（nil 表示无默认值，含显式 DEFAULT NULL），方言原生格式：
	// MySQL 为裸值（active）、PostgreSQL 为表达式（'active'::character varying）、SQLite 为字面量；
	// 非 nil 时在 ddl 片段中追加 DEFAULT 段（判定只看指针不看值，空串渲染为 DEFAULT ''）
	Default *string
	// 是否主键列（联合主键多列均为 true），生成 primary_key:"true"
	PrimaryKey bool
	// MySQL information_schema 的 EXTRA 列原样（如 "auto_increment"、"DEFAULT_GENERATED"、
	// "on update CURRENT_TIMESTAMP"、"VIRTUAL GENERATED"）；ddl 片段按白名单过滤渲染，PG/SQLite 恒空
	Extra string
	// 生成结构体字段时候的信息
	StructFieldInfo StructFieldInfo
}
