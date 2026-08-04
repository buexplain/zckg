package zcmodel

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// buildToDOMethod 生成 Entity 的 ToDO 转换方法。
// 支持传入已有 DO 实例进行复用；Entity 字段为值类型，直接赋给 DO 的 any 字段，无需 nil 判断。
func buildToDOMethod(entityName, doName string, columns []Column) string {
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
		buf.WriteString(fmt.Sprintf("\td.%s = e.%s\n", col.StructFieldInfo.Name, col.StructFieldInfo.Name))
	}
	buf.WriteString("\treturn d\n")
	buf.WriteString("}")
	return buf.String()
}

// buildToEntityMethod 生成 DO 的 ToEntity 转换方法。
// 支持传入已有 Entity 实例进行复用；DO 字段为 any 类型，通过类型断言还原为具体类型并赋值，断言失败则跳过该字段。
func buildToEntityMethod(entityName, doName string, columns []Column) string {
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
		buf.WriteString(fmt.Sprintf("\tif v, ok := d.%s.(%s); ok {\n", col.StructFieldInfo.Name, col.StructFieldInfo.Type))
		buf.WriteString(fmt.Sprintf("\t\te.%s = v\n", col.StructFieldInfo.Name))
		buf.WriteString("\t}\n")
	}
	buf.WriteString("\treturn e\n")
	buf.WriteString("}")
	return buf.String()
}

// sanitizeTagValue 净化写入结构体 tag 的值，保证生成代码语法合法且反射读取完整。
// 结构体 tag 由反引号包裹，reflect.StructTag.Lookup 对值的约束：
//   - 反引号会导致反引号字符串提前终止（语法错误），替换为单引号；
//   - 裸换行/回车等控制字符会使 strconv.Unquote 解析失败，Get 返回空值；
//   - 双引号会被误认为值分隔符，裸双引号会导致解析失败。
//
// 因此先用 strconv.Quote 将控制字符、双引号、反斜杠转义为标准转义序列（\n、\"、\\），
// 反射读取时会通过 strconv.Unquote 完整还原原值。
func sanitizeTagValue(v string) string {
	q := strconv.Quote(v)
	return strings.ReplaceAll(q[1:len(q)-1], "`", "'")
}

// buildStruct 生成结构体定义的字符串，tag 顺序为 json、tagName、description，
// 字段名和类型按 gofmt 风格对齐（字段名/类型宽度按最长者计算）。
// 当 col.Comment 非空时生成 `description:"Comment"` tag。
// comment 非空时在 type 声明前生成 // 注释行。
// useAny 为 true 时字段类型使用 any（用于 DO 结构体），为 false 时使用具体类型（用于 Entity 结构体）。
// columnTagName 指定结构体字段的 tag 名称（如 "column"、"db" 等）。
func buildStruct(name string, columns []Column, useAny bool, comment string, columnTagName string) string {
	// 计算字段名和类型的最大宽度，用于对齐
	maxNameLen, maxTypeLen := 0, 0
	for _, col := range columns {
		if l := len(col.StructFieldInfo.Name); l > maxNameLen {
			maxNameLen = l
		}
		typeLen := len(col.StructFieldInfo.Type)
		if useAny {
			typeLen = len("any")
		}
		if typeLen > maxTypeLen {
			maxTypeLen = typeLen
		}
	}
	var buf bytes.Buffer
	if comment != "" {
		buf.WriteString(fmt.Sprintf("// %s\n", comment))
	}
	buf.WriteString(fmt.Sprintf("type %s struct {\n", name))
	for _, col := range columns {
		var goType string
		if useAny {
			goType = "any"
		} else {
			goType = col.StructFieldInfo.Type
		}
		// 构建字段 tag：顺序为 json、columnTag、description
		var tagParts []string
		if col.StructFieldInfo.JsonTagValue != "" {
			tagParts = append(tagParts, fmt.Sprintf("json:\"%s\"", col.StructFieldInfo.JsonTagValue))
		}
		tagParts = append(tagParts, fmt.Sprintf("%s:\"%s\"", columnTagName, col.Name))
		if col.Comment != "" {
			tagParts = append(tagParts, fmt.Sprintf("description:\"%s\"", sanitizeTagValue(col.Comment)))
		}
		tag := "`" + strings.Join(tagParts, " ") + "`"
		buf.WriteString(fmt.Sprintf("\t%-*s %-*s %s\n", maxNameLen, col.StructFieldInfo.Name, maxTypeLen, goType, tag))
	}
	buf.WriteString("}")
	return buf.String()
}

// buildImportDecl 将 import 路径列表组装为合法的 import 声明：
// 单个路径输出 import "time"，多个路径输出 import (…) 块；空列表返回空字符串。
// 路径按字典序排序，保证输出稳定且符合 gofmt 风格。
func buildImportDecl(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	sort.Strings(paths)
	if len(paths) == 1 {
		return fmt.Sprintf("import %q", paths[0])
	}
	var buf bytes.Buffer
	buf.WriteString("import (\n")
	for _, p := range paths {
		buf.WriteString(fmt.Sprintf("\t%q\n", p))
	}
	buf.WriteString(")")
	return buf.String()
}

// buildFileContent 组装新建文件的完整内容：package + imports + Entity 生成代码 + DO 生成代码。
func buildFileContent(pkgName, entityCode, doCode string, neededImports []string) string {
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("package %s\n", pkgName))
	if imp := buildImportDecl(neededImports); imp != "" {
		buf.WriteString("\n")
		buf.WriteString(imp)
		buf.WriteString("\n")
	}
	buf.WriteString("\n")
	buf.WriteString(entityCode)
	buf.WriteString("\n\n")
	buf.WriteString(doCode)
	buf.WriteString("\n")
	return buf.String()
}

// writeOrReplaceStruct 将 Entity 和 DO 结构体（含关联方法）写入文件。
// 文件最终布局为：package + imports + Entity 生成代码 + Entity 自定义方法 + DO 生成代码 + DO 自定义方法 + 其他用户代码。
// 若文件已存在，通过 AST 解析识别并移除旧的生成代码（Entity/DO 结构体、ToDO/ToEntity 方法），
// 保留用户自定义代码（含 Entity/DO 上的自定义方法）并按上述布局重新组织。
// neededImports 为生成代码引用的包（如 time.Time 需要 time），写入时若文件缺少对应 import 会自动补上。
// filePath: 输出文件路径
// entityName: Entity 结构体名
// entityCode: Entity 结构体及 ToDO 方法的生成代码
// doName: DO 结构体名
// doCode: DO 结构体及 ToEntity 方法的生成代码
// neededImports: 生成代码需要导入的包路径列表
func writeOrReplaceStruct(filePath, entityName, entityCode, doName, doCode string, neededImports []string) error {
	// 从输出目录路径推导 Go 包名，无法推导时回退为 "main"
	pkgName := filepath.Base(filepath.Dir(filePath))
	if pkgName == "" || pkgName == "." || pkgName == "/" {
		pkgName = "main"
	}

	// 文件不存在时直接创建，自动引入生成代码需要的 import
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return os.WriteFile(filePath, []byte(buildFileContent(pkgName, entityCode, doCode, neededImports)), 0644)
	}

	// 读取并解析现有文件
	origContent, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	// 文件为空时按新建处理
	if strings.TrimSpace(string(origContent)) == "" {
		content := buildFileContent(pkgName, entityCode, doCode, neededImports)
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
	existingImports := make(map[string]bool)

	for _, decl := range file.Decls {
		if isGenerated(decl) {
			continue
		}
		if genDecl, ok := decl.(*ast.GenDecl); ok && genDecl.Tok == token.IMPORT {
			importDecls = append(importDecls, getDeclSource(decl))
			// 记录现有文件已导入的包路径，供 neededImports 校验缺失时补充
			for _, spec := range genDecl.Specs {
				impSpec, ok := spec.(*ast.ImportSpec)
				if !ok {
					continue
				}
				if path, err := strconv.Unquote(impSpec.Path.Value); err == nil {
					existingImports[path] = true
				}
			}
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
	// 生成代码需要的 import 缺失时自动补充（放在原有 import 之前，保持同一位置）
	var missingImports []string
	for _, p := range neededImports {
		if !existingImports[p] {
			missingImports = append(missingImports, p)
		}
	}
	if len(missingImports) > 0 {
		importDecls = append([]string{buildImportDecl(missingImports)}, importDecls...)
	}

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
