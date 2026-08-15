package zcdb

// Expression 表示一个原始 SQL 表达式，不会被参数化或标识符引用。
// 它是查询构造器的"逃逸口"，允许用户注入任意 SQL 片段。
//
// 安全警示：Expression 的内容原样拼接进 SQL，防注入责任归调用方——
// 绝不要将用户输入拼入 Expression，用户输入应走绑定参数（普通值）。
//
// 占位符边界：PostgreSQL 方言下 Expression 内容中的 `?` 不会被转换为 `$N`
// （占位符重编号只作用于构造器生成的 `?` 与 Raw 子句的绑定），
// 请勿在 Expression 内容中使用 `?` 占位符。
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
