package internal

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// Column 表示数据库表的一列
type Column struct {
	// 列名
	Name string
	// 列类型
	Type string
	// 列注释，会生成到结构体字段的tag中，示例：description:"Comment"
	Comment string
}

// Generate 根据表名和列信息生成 Entity 结构体、DO 结构体及两者的互转方法（ToDO/ToEntity），
// 并将生成代码写入指定输出目录下的 Go 文件。生成代码包含结构体注释，格式为：
// "// {StructName} {dbName}.{tableName} 表 {entity|do} 结构体，常用于数据库{读取|写入}操作。"
// 每个字段附带 `tagName:"列名"` tag，tagName 由参数指定。
// 未冲突字段使用 toPascalCase 和 formatJSONTag 生成字段名和 JSON tag；
// 冲突字段使用 sanitizeColumnName 生成字段名和 JSON tag，保证两者一致且唯一。
// outputDir: 输出目录
// dbName: 数据库名（用于生成结构体注释）
// tableName: 表名（支持任意命名风格，用于推导文件名和结构体名）
// tagName: 结构体字段的 tag 名称（如 "column"、"db" 等）
// jsonTagValueCase: JSON tag 的命名风格（lowerCamel/upperCamel/lowerSnake/upperSnake/lowerKebab/upperKebab），空值不生成 json tag
// columns: 表的列定义
func Generate(outputDir string, dbName string, tableName string, tagName string, jsonTagValueCase string, columns []Column) {
	baseName := toPascalCase(tableName)
	entityName := baseName + "Entity"
	doName := baseName + "DO"

	fieldNameMap := buildFieldNameMap(columns)

	entityComment := fmt.Sprintf("%s %s.%s 表 entity 结构体，常用于数据库读取操作。", entityName, dbName, tableName)
	doComment := fmt.Sprintf("%s %s.%s 表 do 结构体，常用于数据库写入操作。", doName, dbName, tableName)

	entityStruct := buildStruct(entityName, columns, false, entityComment, tagName, jsonTagValueCase, fieldNameMap)
	doStruct := buildStruct(doName, columns, true, doComment, tagName, jsonTagValueCase, fieldNameMap)
	toDOMethod := buildToDOMethod(entityName, doName, columns, fieldNameMap)
	toEntityMethod := buildToEntityMethod(entityName, doName, columns, fieldNameMap)

	entityCode := entityStruct + "\n\n" + toDOMethod
	doCode := doStruct + "\n\n" + toEntityMethod

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		panic(fmt.Sprintf("创建输出目录失败: %v", err))
	}

	filePath := filepath.Join(outputDir, tableName+".go")
	if err := writeOrReplaceStruct(filePath, entityName, entityCode, doName, doCode); err != nil {
		panic(fmt.Sprintf("生成结构体失败: %v", err))
	}
	fmt.Printf("已生成: %s\n", filePath)
}

// toPascalCase 将任意风格的名称（表名/列名）转换为 Go 规范的 PascalCase 风格，
// 单词 "id"（不区分大小写）统一转换为 "ID"。
// 支持的输入风格包括：snake_case、camelCase、PascalCase、kebab-case、UPPER_SNAKE 等混合风格。
func toPascalCase(s string) string {
	words := splitWords(s)
	var buf bytes.Buffer
	for _, w := range words {
		if w == "" {
			continue
		}
		lower := strings.ToLower(w)
		if lower == "id" {
			buf.WriteString("ID")
		} else {
			runes := []rune(lower)
			runes[0] = unicode.ToUpper(runes[0])
			buf.WriteString(string(runes))
		}
	}
	return buf.String()
}

// splitWords 将任意风格的字符串拆分为单词列表。
// 支持的输入风格包括：snake_case、camelCase、PascalCase、kebab-case、UPPER_SNAKE 等混合风格。
// 拆分规则：
//   - 下划线 (_)、连字符 (-) 和空格作为分隔符
//   - 小写→大写转换处拆分（如 userName → user | Name）
//   - 连续大写后接小写时，在最后一个大写前拆分（如 HTTPServer → HTTP | Server）
func splitWords(s string) []string {
	if s == "" {
		return nil
	}
	var words []string
	var current strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '_' || r == '-' || r == ' ' {
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
			continue
		}
		if current.Len() > 0 {
			prev := []rune(current.String())
			last := prev[len(prev)-1]
			if unicode.IsLower(last) && unicode.IsUpper(r) {
				words = append(words, current.String())
				current.Reset()
			} else if unicode.IsUpper(last) && unicode.IsUpper(r) && i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
				words = append(words, current.String())
				current.Reset()
			}
		}
		current.WriteRune(r)
	}
	if current.Len() > 0 {
		words = append(words, current.String())
	}
	return words
}

// toLowerCamel 将单词列表转换为 lowerCamel 风格，如 getUserById
func toLowerCamel(words []string) string {
	if len(words) == 0 {
		return ""
	}
	var buf bytes.Buffer
	for i, w := range words {
		if w == "" {
			continue
		}
		lower := strings.ToLower(w)
		if i == 0 {
			buf.WriteString(lower)
		} else {
			runes := []rune(lower)
			runes[0] = unicode.ToUpper(runes[0])
			buf.WriteString(string(runes))
		}
	}
	return buf.String()
}

// toUpperCamel 将单词列表转换为 UpperCamel（PascalCase）风格，如 GetUserById
func toUpperCamel(words []string) string {
	var buf bytes.Buffer
	for _, w := range words {
		if w == "" {
			continue
		}
		lower := strings.ToLower(w)
		runes := []rune(lower)
		runes[0] = unicode.ToUpper(runes[0])
		buf.WriteString(string(runes))
	}
	return buf.String()
}

// toLowerSnake 将单词列表转换为 lower_snake 风格，如 user_id
func toLowerSnake(words []string) string {
	parts := make([]string, 0, len(words))
	for _, w := range words {
		if w == "" {
			continue
		}
		parts = append(parts, strings.ToLower(w))
	}
	return strings.Join(parts, "_")
}

// toUpperSnake 将单词列表转换为 UPPER_SNAKE 风格，如 USER_ID
func toUpperSnake(words []string) string {
	parts := make([]string, 0, len(words))
	for _, w := range words {
		if w == "" {
			continue
		}
		parts = append(parts, strings.ToUpper(w))
	}
	return strings.Join(parts, "_")
}

// toLowerKebab 将单词列表转换为 lower-kebab 风格，如 user-id
func toLowerKebab(words []string) string {
	parts := make([]string, 0, len(words))
	for _, w := range words {
		if w == "" {
			continue
		}
		parts = append(parts, strings.ToLower(w))
	}
	return strings.Join(parts, "-")
}

// toUpperKebab 将单词列表转换为 UPPER-KEBAB 风格，如 USER-ID
func toUpperKebab(words []string) string {
	parts := make([]string, 0, len(words))
	for _, w := range words {
		if w == "" {
			continue
		}
		parts = append(parts, strings.ToUpper(w))
	}
	return strings.Join(parts, "-")
}

// formatJSONTag 根据列名和命名风格生成 JSON tag 的值
// 支持的风格：lowerCamel、upperCamel、lowerSnake、upperSnake、lowerKebab、upperKebab
// jsonTagValueCase 为空时返回空字符串，表示不生成 json tag
func formatJSONTag(colName string, jsonTagValueCase string) string {
	if jsonTagValueCase == "" {
		return ""
	}
	words := splitWords(colName)
	switch jsonTagValueCase {
	case "lowerCamel":
		return toLowerCamel(words)
	case "upperCamel":
		return toUpperCamel(words)
	case "lowerSnake":
		return toLowerSnake(words)
	case "upperSnake":
		return toUpperSnake(words)
	case "lowerKebab":
		return toLowerKebab(words)
	case "upperKebab":
		return toUpperKebab(words)
	default:
		return ""
	}
}

// sanitizeColumnName 将列名转换为合法的 Go 标识符，用于冲突字段的字段名和 JSON tag。
// 参数 replace 指定连字符/下划线的替换方式：
//   - replace="_"：将 - 替换为 _，保留原始命名结构（如 user-Account → User_Account）
//   - replace=""：移除 - 和 _ 并将分隔符左侧单词尾字母和右侧单词首字母大写（如 user-Account → UseRAccount，User_Account → UseRAccount）
//
// 最后统一保证首字母大写。
func sanitizeColumnName(name string, replace string) string {
	if name == "" {
		return ""
	}
	var sanitized string
	if replace == "" {
		// 替换分隔符后还有冲突，将分隔符左侧的单词尾字母改为大写，
		// 分隔符右侧的单词首字母改为大写，并移除分隔符
		runes := []rune(name)
		n := len(runes)
		var result []rune
		for i := 0; i < n; i++ {
			if runes[i] == '-' || runes[i] == '_' {
				// 左侧单词尾字母大写
				if len(result) > 0 && unicode.IsLetter(result[len(result)-1]) {
					result[len(result)-1] = unicode.ToUpper(result[len(result)-1])
				}
				// 跳过分隔符（右侧首字母在下一轮迭代中处理）
				continue
			}
			// 分隔符右侧首字母大写：前一个字符是分隔符时大写当前字母
			if i > 0 && (runes[i-1] == '-' || runes[i-1] == '_') && unicode.IsLetter(runes[i]) {
				result = append(result, unicode.ToUpper(runes[i]))
			} else {
				result = append(result, runes[i])
			}
		}
		if len(result) == 0 {
			return ""
		}
		sanitized = string(result)
	} else {
		// 替换连字符
		sanitized = strings.ReplaceAll(name, "-", replace)
	}
	// 保证首字母大写
	runes := []rune(sanitized)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// buildFieldNameMap 检测列名经 toPascalCase 转换后是否存在碰撞，
// 若多个列产生相同字段名，则这些列改用 sanitizeColumnName 生成唯一字段名。
// 返回列名 → 字段名的映射表。
func buildFieldNameMap(columns []Column) map[string]string {
	defaultNames := make(map[string]string, len(columns))
	count := make(map[string]int)
	for _, col := range columns {
		name := toPascalCase(col.Name)
		defaultNames[col.Name] = name
		count[name]++
	}
	result := make(map[string]string, len(columns))
	usedNames := make(map[string]bool) // 记录已分配的字段名，用于二级碰撞检测
	for _, col := range columns {
		if count[defaultNames[col.Name]] > 1 {
			tmp := sanitizeColumnName(col.Name, "_")
			if usedNames[tmp] {
				// 一级 sanitize（替换为下划线）后仍有冲突，使用二级 sanitize（移除分隔符）
				tmp = sanitizeColumnName(col.Name, "")
			}
			result[col.Name] = tmp
			usedNames[tmp] = true
		} else {
			result[col.Name] = defaultNames[col.Name]
			usedNames[defaultNames[col.Name]] = true
		}
	}
	return result
}

// mapSQLTypeToGoType 将 SQL 类型映射为 Go 类型，未匹配的类型默认返回 "string"
func mapSQLTypeToGoType(sqlType string) string {
	switch strings.ToLower(sqlType) {
	case "bigint":
		return "int64"
	case "int", "integer", "smallint", "tinyint":
		return "int"
	case "float", "double", "decimal":
		return "float64"
	case "bool", "boolean":
		return "bool"
	case "string", "varchar", "char", "text", "longtext":
		return "string"
	case "time", "datetime", "timestamp", "date":
		return "time.Time"
	default:
		return "string"
	}
}

// buildStruct 生成结构体定义的字符串，tag 顺序为 json、tagName、description。
// 未冲突字段使用 toPascalCase 生成字段名、formatJSONTag 生成 JSON tag；
// 冲突字段使用 sanitizeColumnName 生成字段名和 JSON tag，保证两者一致。
// 当 col.Comment 非空时生成 `description:"Comment"` tag。
// comment 非空时在 type 声明前生成 // 注释行。
// isPointer 为 true 时字段类型使用指针（用于 DO 结构体），为 false 时使用值类型（用于 Entity 结构体）。
// tagName 指定结构体字段的 tag 名称（如 "column"、"db" 等）。
// jsonTagValueCase 指定 JSON tag 的命名风格，空值不生成 json tag。
// fieldNameMap 为列名到字段名的映射（由 buildFieldNameMap 生成，已处理碰撞）。
func buildStruct(name string, columns []Column, isPointer bool, comment string, tagName string, jsonTagValueCase string, fieldNameMap map[string]string) string {
	// 检测碰撞：统计 toPascalCase 结果中出现超过一次的字段名
	collisionSet := buildCollisionSet(columns)

	var buf bytes.Buffer
	if comment != "" {
		buf.WriteString(fmt.Sprintf("// %s\n", comment))
	}
	buf.WriteString(fmt.Sprintf("type %s struct {\n", name))
	for _, col := range columns {
		fieldName := fieldNameMap[col.Name]
		goType := mapSQLTypeToGoType(col.Type)
		if isPointer {
			goType = "*" + goType
		}
		// 构建字段 tag：顺序为 json、tagName、description
		var tagParts []string
		var jsonTag string
		if collisionSet[col.Name] {
			// 冲突字段：JSON tag 与字段名一致
			jsonTag = fieldName
		} else {
			// 非冲突字段：按风格转换
			jsonTag = formatJSONTag(col.Name, jsonTagValueCase)
		}
		if jsonTag != "" {
			tagParts = append(tagParts, fmt.Sprintf("json:\"%s\"", jsonTag))
		}
		tagParts = append(tagParts, fmt.Sprintf("%s:\"%s\"", tagName, col.Name))
		if col.Comment != "" {
			tagParts = append(tagParts, fmt.Sprintf("description:\"%s\"", col.Comment))
		}
		tag := "`" + strings.Join(tagParts, " ") + "`"
		buf.WriteString(fmt.Sprintf("\t%s %s %s\n", fieldName, goType, tag))
	}
	buf.WriteString("}")
	return buf.String()
}

// buildCollisionSet 检测列名经 toPascalCase 转换后是否存在碰撞，
// 返回碰撞列名集合。
func buildCollisionSet(columns []Column) map[string]bool {
	count := make(map[string]int)
	for _, col := range columns {
		count[toPascalCase(col.Name)]++
	}
	collisionSet := make(map[string]bool)
	if len(count) < len(columns) {
		for _, col := range columns {
			if count[toPascalCase(col.Name)] > 1 {
				collisionSet[col.Name] = true
			}
		}
	}
	return collisionSet
}

// buildToDOMethod 生成 Entity 的 ToDO 转换方法。
// 支持传入已有 DO 实例进行复用；Entity 字段为值类型，直接取地址赋值给 DO 的指针字段，无需 nil 判断。
func buildToDOMethod(entityName, doName string, columns []Column, fieldNameMap map[string]string) string {
	var buf bytes.Buffer
	lowerDO := strings.ToLower(doName[:1]) + doName[1:]
	buf.WriteString(fmt.Sprintf("func (e *%s) ToDO(%s ...*%s) *%s {\n", entityName, lowerDO, doName, doName))
	buf.WriteString("\tvar d *" + doName + "\n")
	buf.WriteString(fmt.Sprintf("\tif len(%s) > 0 && %s[0] != nil {\n", lowerDO, lowerDO))
	buf.WriteString("\t\td = " + lowerDO + "[0]\n")
	buf.WriteString("\t} else {\n")
	buf.WriteString("\t\td = &" + doName + "{}\n")
	buf.WriteString("\t}\n")
	for _, col := range columns {
		fieldName := fieldNameMap[col.Name]
		buf.WriteString(fmt.Sprintf("\td.%s = &e.%s\n", fieldName, fieldName))
	}
	buf.WriteString("\treturn d\n")
	buf.WriteString("}")
	return buf.String()
}

// buildToEntityMethod 生成 DO 的 ToEntity 转换方法。
// 支持传入已有 Entity 实例进行复用；DO 字段为指针类型，解引用前需逐字段做 nil 判断。
func buildToEntityMethod(entityName, doName string, columns []Column, fieldNameMap map[string]string) string {
	var buf bytes.Buffer
	lowerEnt := strings.ToLower(entityName[:1]) + entityName[1:]
	buf.WriteString(fmt.Sprintf("func (d *%s) ToEntity(%s ...*%s) *%s {\n", doName, lowerEnt, entityName, entityName))
	buf.WriteString("\tvar e *" + entityName + "\n")
	buf.WriteString(fmt.Sprintf("\tif len(%s) > 0 && %s[0] != nil {\n", lowerEnt, lowerEnt))
	buf.WriteString("\t\te = " + lowerEnt + "[0]\n")
	buf.WriteString("\t} else {\n")
	buf.WriteString("\t\te = &" + entityName + "{}\n")
	buf.WriteString("\t}\n")
	for _, col := range columns {
		fieldName := fieldNameMap[col.Name]
		buf.WriteString(fmt.Sprintf("\tif d.%s != nil {\n", fieldName))
		buf.WriteString(fmt.Sprintf("\t\te.%s = *d.%s\n", fieldName, fieldName))
		buf.WriteString("\t}\n")
	}
	buf.WriteString("\treturn e\n")
	buf.WriteString("}")
	return buf.String()
}

// writeOrReplaceStruct 将 Entity 和 DO 结构体（含关联方法）写入文件。
// 文件最终布局为：package + imports + Entity 生成代码 + Entity 自定义方法 + DO 生成代码 + DO 自定义方法 + 其他用户代码。
// 若文件已存在，通过 AST 解析识别并移除旧的生成代码（Entity/DO 结构体、ToDO/ToEntity 方法），
// 保留用户自定义代码（含 Entity/DO 上的自定义方法）并按上述布局重新组织。
// filePath: 输出文件路径
// entityName: Entity 结构体名
// entityCode: Entity 结构体及 ToDO 方法的生成代码
// doName: DO 结构体名
// doCode: DO 结构体及 ToEntity 方法的生成代码
func writeOrReplaceStruct(filePath, entityName, entityCode, doName, doCode string) error {
	pkgName := packageName(filepath.Dir(filePath))

	// 文件不存在时直接创建
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		content := fmt.Sprintf("package %s\n\n%s\n\n%s\n", pkgName, entityCode, doCode)
		return os.WriteFile(filePath, []byte(content), 0644)
	}

	// 读取并解析现有文件
	origContent, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	// 文件为空时按新建处理
	if strings.TrimSpace(string(origContent)) == "" {
		content := fmt.Sprintf("package %s\n\n%s\n\n%s\n", pkgName, entityCode, doCode)
		return os.WriteFile(filePath, []byte(content), 0644)
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filePath, origContent, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("解析文件失败: %v", err)
	}

	tf := fset.File(file.Pos())

	// getDeclSource 从原始文件内容中截取声明的源码文本，包含前置 Doc 注释
	getDeclSource := func(decl ast.Decl) string {
		start := tf.Offset(decl.Pos())
		end := tf.Offset(decl.End())
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Doc != nil {
				start = tf.Offset(d.Doc.Pos())
			}
		case *ast.FuncDecl:
			if d.Doc != nil {
				start = tf.Offset(d.Doc.Pos())
			}
		}
		return string(origContent[start:end])
	}

	// isGenerated 判断声明是否为生成代码（需被移除并重新生成）。
	// 生成代码包括：Entity/DO 结构体声明，以及 Entity 的 ToDO 方法和 DO 的 ToEntity 方法。
	isGenerated := func(decl ast.Decl) bool {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				return false
			}
			for _, spec := range d.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if typeSpec.Name.Name == entityName || typeSpec.Name.Name == doName {
					return true
				}
			}
			return false
		case *ast.FuncDecl:
			if d.Recv == nil || len(d.Recv.List) == 0 {
				return false
			}
			starExpr, ok := d.Recv.List[0].Type.(*ast.StarExpr)
			if !ok {
				return false
			}
			ident, ok := starExpr.X.(*ast.Ident)
			if !ok {
				return false
			}
			// 仅 ToDO（Entity 上）和 ToEntity（DO 上）为生成方法
			if ident.Name == entityName && d.Name.Name == "ToDO" {
				return true
			}
			if ident.Name == doName && d.Name.Name == "ToEntity" {
				return true
			}
			return false
		}
		return false
	}

	// 分类收集用户代码和 import 声明
	// 用户代码分为三类：
	//   1. Entity 上的自定义方法（放在 Entity 生成代码后面）
	//   2. DO 上的自定义方法（放在 DO 生成代码后面）
	//   3. 其他用户代码（放在最后）
	var importDecls []string
	var entityMethodDecls []string
	var doMethodDecls []string
	var userDecls []string

	for _, decl := range file.Decls {
		if isGenerated(decl) {
			continue
		}
		if genDecl, ok := decl.(*ast.GenDecl); ok && genDecl.Tok == token.IMPORT {
			importDecls = append(importDecls, getDeclSource(decl))
			continue
		}
		// 判断是否为 Entity 或 DO 上的自定义方法（接收者为 *EntityName 或 *DOName）
		if funcDecl, ok := decl.(*ast.FuncDecl); ok && funcDecl.Recv != nil && len(funcDecl.Recv.List) > 0 {
			starExpr, ok := funcDecl.Recv.List[0].Type.(*ast.StarExpr)
			if ok {
				if ident, ok := starExpr.X.(*ast.Ident); ok && ident.Name == entityName {
					entityMethodDecls = append(entityMethodDecls, getDeclSource(decl))
					continue
				}
				if ident, ok := starExpr.X.(*ast.Ident); ok && ident.Name == doName {
					doMethodDecls = append(doMethodDecls, getDeclSource(decl))
					continue
				}
			}
		}
		userDecls = append(userDecls, getDeclSource(decl))
	}

	// 重建文件：package + imports + Entity生成代码 + Entity自定义方法 + DO生成代码 + DO自定义方法 + 其他用户代码
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("package %s\n", pkgName))

	for _, imp := range importDecls {
		buf.WriteString("\n")
		buf.WriteString(imp)
		buf.WriteString("\n")
	}

	buf.WriteString("\n")
	buf.WriteString(entityCode)

	for _, m := range entityMethodDecls {
		buf.WriteString("\n\n")
		buf.WriteString(m)
	}

	buf.WriteString("\n\n")
	buf.WriteString(doCode)

	for _, m := range doMethodDecls {
		buf.WriteString("\n\n")
		buf.WriteString(m)
	}

	for _, u := range userDecls {
		buf.WriteString("\n\n")
		buf.WriteString(u)
	}
	buf.WriteString("\n")

	return os.WriteFile(filePath, buf.Bytes(), 0644)
}

// packageName 从输出目录路径推导 Go 包名，无法推导时回退为 "main"
func packageName(outputDir string) string {
	name := filepath.Base(outputDir)
	if name != "" && name != "." && name != "/" {
		return name
	}
	return "main"
}
