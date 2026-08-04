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
	OutputDir        string    // 输出目录
	Database         string    // 数据库名
	Dialect          Dialect   // 数据库方言（"mysql"、"postgres"、"sqlite"）
	TableName        string    // 表名（支持任意命名风格，用于推导文件名和结构体名）
	TableComment     string    // 表注释
	ColumnTagName    string    // 表字段的结构体字段的 tag 名称（如 "column"、"db" 等）
	JsonTagValueCase NameCase  // JSON tag 的命名风格，空值不生成 json tag
	Columns          []*Column //表所有的字段
}

// StructFieldInfo 生成结构体字段时候的信息
type StructFieldInfo struct {
	Name         string // 表字段转成结构体字段的名字
	Type         string // 表字段类型转成结构体字段的类型
	Import       string // Type 对应的 import 路径（如 time.Time 需要 "time"），空值不引入任何包
	JsonTagValue string //表字段转成结构体字段的json tag的值
}

type Column struct {
	// 列名
	Name string
	// 列类型（如 "VARCHAR(255)"、"bigint(20)"、"text"）
	Type string
	// 列注释，会生成到结构体字段的tag中，示例：description:"Comment"
	Comment string
	// 生成结构体字段时候的信息
	StructFieldInfo StructFieldInfo
}
