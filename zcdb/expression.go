package zcdb

// Expression 表示一个原始 SQL 表达式，不会被参数化或标识符引用。
// 它是查询构造器的"逃逸口"，允许用户注入任意 SQL 片段。
//
// 安全警示：Expression 的内容原样拼接进 SQL，防注入责任归调用方——
// 绝不要将用户输入拼入 Expression，用户输入应走绑定参数（普通值）。
//
// 占位符边界：Expression 内容中的 `?` 在任何编译路径上都没有配对的绑定参数：
// INSERT VALUES 与 Raw 子句绑定中 Expression 直接内嵌为 SQL 文本，`?` 原样保留
// （在 PostgreSQL 方言下成为非法占位符）；UPDATE SET 值中 PostgreSQL 还会把 `?`
// 转为 $N 并占用占位符编号（内部自增/自减编译依赖该机制并另行配对绑定），
// 用户自带的 Expression 均无配对绑定。请勿在 Expression 内容中使用 `?` 占位符。
type Expression struct {
	value string
}

// NewExpression 创建一个原始 SQL 表达式。
func NewExpression(value string) Expression {
	return Expression{value: value}
}

// Value 返回表达式的原始 SQL 字符串。
func (e Expression) Value() string {
	return e.value
}
