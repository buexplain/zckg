// 本文件为 MySQL 集成测试——SQL 编译（ToXxx 系列）。
// 测试需真实数据库连接，连接与建表 helper 见 builder_mysql_integration_test.go。
//
// 当前 MySQL 的 ToXxx 编译验证分散在各类别用例中（如 builder_where_mysql_integration_test.go
// 的 NewApi_NullSafe 以 ToSelect 断言静态 SQL），暂无独立用例；本文件占位以保持
// "方言 × 类别"目录结构完整。
package zcdb
