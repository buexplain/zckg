package zcmodel

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Generate 根据表信息和列定义生成 Entity 结构体、DO 结构体及两者的互转方法（ToDO/ToEntity），
// 并将生成代码写入 OutputDir 下的 Go 文件。
// 处理流程：
//  1. 校验数据库方言和 JSON tag 命名风格；
//  2. 为未显式指定的字段补充字段名（toPascalCase）、字段类型（formatStructFieldType）和 JSON tag 值（formatJSONTag）；
//  3. 生成 Entity（值类型字段）和 DO（any 类型字段）结构体及互转方法；
//  4. 将生成代码写入 {OutputDir}/{TableName}.go，已存在的文件通过 AST 解析保留用户自定义代码。
func Generate(input Input) error {
	// 校验数据库方言
	switch input.Dialect {
	case DialectMysql, DialectPostgres, DialectSqlite:
	default:
		return fmt.Errorf("不支持的数据库方言: %s", input.Dialect)
	}
	// 处理json tag的值
	if input.JsonTagValueCase != "" {
		if input.JsonTagValueCase.IsValid() == false {
			return errors.New("invalid json tag value case")
		}
		for _, c := range input.Columns {
			if c.StructFieldInfo.JsonTagValue == "" {
				c.StructFieldInfo.JsonTagValue = formatJSONTag(c.Name, input.JsonTagValueCase)
			}
		}
	}
	// 处理结构体字段名字和类型
	for _, c := range input.Columns {
		if c.StructFieldInfo.Name == "" {
			c.StructFieldInfo.Name = toPascalCase(c.Name)
		}
		if c.StructFieldInfo.Type == "" {
			c.StructFieldInfo.Type = formatStructFieldType(input.Dialect, c.Type)
			// 未映射到的类型兜底为 string，避免生成非法代码
			if c.StructFieldInfo.Type == "" {
				c.StructFieldInfo.Type = "string"
			}
		}
		// 类型为 time.Time 时自动引入 time 包（调用者未显式指定 Import 时）
		if c.StructFieldInfo.Type == "time.Time" && c.StructFieldInfo.Import == "" {
			c.StructFieldInfo.Import = "time"
		}
	}

	// 生成 Entity 和 DO 结构体及互转方法
	baseName := toPascalCase(input.TableName)
	entityName := baseName + "Entity"
	doName := baseName + "DO"

	columns := make([]Column, 0, len(input.Columns))
	for _, c := range input.Columns {
		columns = append(columns, *c)
	}

	tableComment := "表，"
	if input.TableComment != "" {
		tableComment = input.TableComment + "，"
	}

	entityComment := fmt.Sprintf("%s %s.%s %sentity结构体，常用于数据库读取操作。", entityName, input.Database, input.TableName, tableComment)
	doComment := fmt.Sprintf("%s %s.%s %sdo结构体，常用于数据库写入操作。", doName, input.Database, input.TableName, tableComment)

	entityStruct := buildStruct(entityName, columns, false, entityComment, input.ColumnTagName)
	doStruct := buildStruct(doName, columns, true, doComment, input.ColumnTagName)
	toDOMethod := buildToDOMethod(entityName, doName, columns)
	toEntityMethod := buildToEntityMethod(entityName, doName, columns)

	entityCode := entityStruct + "\n\n" + toDOMethod
	doCode := doStruct + "\n\n" + toEntityMethod

	if err := os.MkdirAll(input.OutputDir, 0755); err != nil {
		return fmt.Errorf("创建输出目录失败: %v", err)
	}

	filePath := filepath.Join(input.OutputDir, input.TableName+".go")
	if err := writeOrReplaceStruct(filePath, entityName, entityCode, doName, doCode, neededImportsOf(input.Columns)); err != nil {
		return fmt.Errorf("生成结构体失败: %v", err)
	}
	return nil
}

// neededImportsOf 收集生成代码需要的 import 路径：
// 遍历列的 StructFieldInfo.Import（调用者显式指定或 Generate 自动填充），去重并按字典序排序。
// Import 为空（内置类型或无需引入包的类型）时不收集。
func neededImportsOf(columns []*Column) []string {
	seen := make(map[string]bool)
	var imports []string
	for _, c := range columns {
		path := c.StructFieldInfo.Import
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		imports = append(imports, path)
	}
	sort.Strings(imports)
	return imports
}
