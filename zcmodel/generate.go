package zcmodel

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Generate 根据表信息和列定义生成 Entity 结构体、DO 结构体及两者的互转方法（ToDO/ToEntity），
// 并将生成代码写入 OutputDir 下的 Go 文件。
// 处理流程：
//  1. 校验数据库方言、表名（防路径穿越）和 JSON tag 命名风格；
//  2. 复制列信息，为副本中未显式指定的字段补充字段名（toPascalCase）、字段类型（formatStructFieldType）
//     和 JSON tag 值（formatJSONTag），不改动调用方传入的数据；
//  3. 检测转换后字段名重复/为空的列并报错，避免产出无法编译的代码；
//  4. 生成 Entity（值类型字段）和 DO（any 类型字段）结构体及互转方法；
//  5. 将生成代码写入 {OutputDir}/{TableName}.go，已存在的文件通过 AST 解析保留用户自定义代码，
//     落盘前对完整内容做 go/parser 自校验，非法产物直接报错不写文件。
func Generate(input Input) error {
	// 校验数据库方言
	switch input.Dialect {
	case DialectMysql, DialectPostgres, DialectSqlite:
	default:
		return fmt.Errorf("不支持的数据库方言: %s", input.Dialect)
	}
	// 校验表名：表名将直接用作文件名，必须拒绝路径穿越与非法文件名字符
	if err := validateTableName(input.TableName); err != nil {
		return err
	}

	// 复制列信息：后续补全只作用于副本，避免原地修改调用方传入的 Column
	columns := make([]Column, len(input.Columns))
	for i, c := range input.Columns {
		columns[i] = *c
	}

	// 处理json tag的值
	if input.JsonTagValueCase != "" {
		if input.JsonTagValueCase.IsValid() == false {
			return errors.New("invalid json tag value case")
		}
		for i := range columns {
			if columns[i].StructFieldInfo.JsonTagValue == "" {
				columns[i].StructFieldInfo.JsonTagValue = formatJSONTag(columns[i].Name, input.JsonTagValueCase)
			}
		}
	}
	// 处理结构体字段名字和类型
	for i := range columns {
		if columns[i].StructFieldInfo.Name == "" {
			columns[i].StructFieldInfo.Name = toPascalCase(columns[i].Name)
		}
		if columns[i].StructFieldInfo.Type == "" {
			columns[i].StructFieldInfo.Type = formatStructFieldType(input.Dialect, columns[i].Type)
			// 未映射到的类型兜底为 string，避免生成非法代码
			if columns[i].StructFieldInfo.Type == "" {
				columns[i].StructFieldInfo.Type = "string"
			}
		}
		// 类型为 time.Time 时自动引入 time 包（调用者未显式指定 Import 时）
		if columns[i].StructFieldInfo.Type == "time.Time" && columns[i].StructFieldInfo.Import == "" {
			columns[i].StructFieldInfo.Import = "time"
		}
	}

	// 字段名合法性检测：空字段名与转换后重名都会产出无法编译的代码，直接报错
	seenNames := make(map[string]string, len(columns))
	for _, c := range columns {
		if c.StructFieldInfo.Name == "" {
			return fmt.Errorf("列 %q 转换后的字段名为空", c.Name)
		}
		if prev, ok := seenNames[c.StructFieldInfo.Name]; ok {
			return fmt.Errorf("列 %q 与 %q 转换后的字段名重复: %s", prev, c.Name, c.StructFieldInfo.Name)
		}
		seenNames[c.StructFieldInfo.Name] = c.Name
	}

	// 生成 Entity 和 DO 结构体及互转方法
	baseName := toPascalCase(input.TableName)
	entityName := baseName + "Entity"
	doName := baseName + "DO"

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
	// 防御性二次校验：清理后仍必须位于输出目录内，双保险防止任何意外逃逸
	if !isPathWithinDir(input.OutputDir, filePath) {
		return fmt.Errorf("输出文件路径逃逸输出目录: %s", filePath)
	}

	colPtrs := make([]*Column, len(columns))
	for i := range columns {
		colPtrs[i] = &columns[i]
	}
	if err := writeOrReplaceStruct(filePath, entityName, entityCode, doName, doCode, neededImportsOf(colPtrs)); err != nil {
		return fmt.Errorf("生成结构体失败: %v", err)
	}
	return nil
}

// validateTableName 校验表名可直接安全用作文件名：
// 不得为空、不得为 "." / ".."、不得含路径分隔符（/、\）与 Windows 非法文件名字符（<>:"|?*）。
func validateTableName(name string) error {
	if name == "" {
		return errors.New("表名为空")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("表名非法: %s", name)
	}
	if strings.ContainsAny(name, `/\<>:"|?*`) {
		return fmt.Errorf("表名包含非法字符: %s", name)
	}
	return nil
}

// isPathWithinDir 判断 path 经 filepath.Clean 后是否仍位于 dir 内，用于输出路径逃逸的防御性校验。
func isPathWithinDir(dir, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
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
