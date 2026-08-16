package zcmodel

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
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
	// 首字符按 rune 小写（lowerFirst），避免多字节字符被按字节截断产生非法参数名
	lowerDO := lowerFirst(doName)
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
	lowerEnt := lowerFirst(entityName)
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

// buildStruct 生成结构体定义的字符串，tag 顺序为 json、columnTagName、description，
// 字段名和类型按最长者计算宽度对齐（最终产物在 writeGeneratedFile 中会经 go/format 统一格式化）。
// 当 col.Comment 非空时生成 `description:"Comment"` tag。
// comment 非空时在 type 声明前生成 // 注释行。
// useAny 为 true 时字段类型使用 any（用于 DO 结构体），为 false 时使用具体类型（用于 Entity 结构体）。
// columnTagName 指定结构体字段的 tag 名称（如 "column"、"db" 等），为空时不生成列名 tag，避免产生空 tag 名 `:"colname"`。
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
		// 构建字段 tag：顺序为 json、columnTagName、description；columnTagName 为空时跳过列名 tag
		var tagParts []string
		if col.StructFieldInfo.JsonTagValue != "" {
			tagParts = append(tagParts, fmt.Sprintf("json:\"%s\"", col.StructFieldInfo.JsonTagValue))
		}
		if columnTagName != "" {
			tagParts = append(tagParts, fmt.Sprintf("%s:\"%s\"", columnTagName, col.Name))
		}
		if col.Comment != "" {
			tagParts = append(tagParts, fmt.Sprintf("description:\"%s\"", sanitizeTagValue(col.Comment)))
		}
		var tag string
		if len(tagParts) > 0 {
			tag = "`" + strings.Join(tagParts, " ") + "`"
		}
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

// writeFileAtomic 原子写入：同目录创建临时文件，写入并应用权限位后 rename 覆盖目标，
// 避免进程中断/磁盘满时留下截断的半文件；目标文件已存在时保留其原权限位，不存在时使用 0644。
func writeFileAtomic(filePath string, content []byte) error {
	dir := filepath.Dir(filePath)
	mode := os.FileMode(0644)
	if info, err := os.Stat(filePath); err == nil {
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(filePath)+".tmp*")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %v", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()
	if _, err := tmp.Write(content); err != nil {
		return fmt.Errorf("写入临时文件失败: %v", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		return fmt.Errorf("设置文件权限失败: %v", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭临时文件失败: %v", err)
	}
	// Go 在 Windows 上使用 MOVEFILE_REPLACE_EXISTING，rename 可直接覆盖已存在的目标文件
	if err := os.Rename(tmpName, filePath); err != nil {
		return fmt.Errorf("替换目标文件失败: %v", err)
	}
	return nil
}

// writeGeneratedFile 落盘前的最终防线：对完整文件内容执行 go/parser 自校验（go/format.Source
// 内部解析失败即返回错误），任何语法非法的产物都不写出；通过后以 gofmt 标准格式原子写入，
// 顺带解决多字节列名的对齐、import 分组、空行等格式问题。
func writeGeneratedFile(filePath string, content []byte) error {
	formatted, err := format.Source(content)
	if err != nil {
		return fmt.Errorf("生成代码存在语法错误: %v", err)
	}
	return writeFileAtomic(filePath, formatted)
}

// writeOrReplaceStruct 将 Entity 和 DO 结构体（含关联方法）写入文件。
// 文件最终布局为：原文件头（build tags、文件级注释、原 package 行）+ imports + Entity 生成代码 +
// Entity 自定义方法 + DO 生成代码 + DO 自定义方法 + 其他用户代码。
// 若文件已存在，通过 AST 解析识别并移除旧的生成代码（Entity/DO 结构体、ToDO/ToEntity 方法，
// 含值接收者版本），保留用户自定义代码（含 Entity/DO 上的自定义方法）并按上述布局重新组织；
// go/parser 不会把 package 声明之前的 build tags/文件级注释放进 file.Decls，重建时按源码偏移
// 显式前置保留，已有文件的包名尊重原文件，仅新建文件使用目录推导包名。
// type 声明块按 Spec 粒度过滤：块中混有用户类型时仅剔除生成的类型，用户类型（含注释）保留。
// 最终产物统一经 go/format 格式化并原子写入，保留原文件权限位。
// neededImports 为生成代码引用的包（如 time.Time 需要 time），写入时若文件缺少对应 import 会自动补上；
// 存量文件以别名（含 _、. 导入）引入所需包时不视为已存在，仍会补充默认导入，保证生成代码可编译。
// filePath: 输出文件路径
// entityName: Entity 结构体名
// entityCode: Entity 结构体及 ToDO 方法的生成代码
// doName: DO 结构体名
// doCode: DO 结构体及 ToEntity 方法的生成代码
// neededImports: 生成代码需要导入的包路径列表
func writeOrReplaceStruct(filePath, entityName, entityCode, doName, doCode string, neededImports []string) error {
	// 从输出目录路径推导 Go 包名，无法推导时回退为 "main"（仅用于新建文件）
	pkgName := filepath.Base(filepath.Dir(filePath))
	if pkgName == "" || pkgName == "." || pkgName == "/" {
		pkgName = "main"
	}

	// 文件不存在时直接创建，自动引入生成代码需要的 import
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return writeGeneratedFile(filePath, []byte(buildFileContent(pkgName, entityCode, doCode, neededImports)))
	}

	// 读取并解析现有文件
	origContent, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	// 文件为空时按新建处理
	if strings.TrimSpace(string(origContent)) == "" {
		return writeGeneratedFile(filePath, []byte(buildFileContent(pkgName, entityCode, doCode, neededImports)))
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filePath, origContent, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("解析文件失败: %v", err)
	}
	tf := fset.File(file.Pos())

	// 保留 package 声明之前的全部原文（build tags、文件级注释）与原 package 行：
	// go/parser 不把它们放进 file.Decls，重建时必须显式前置，否则再生成会静默丢失。
	// 已有文件的包名尊重原文件，不强制改为输出目录推导的包名。
	pkgOffset := tf.Offset(file.Package)
	fileHeader := origContent[:pkgOffset]
	pkgClause := origContent[pkgOffset:tf.Offset(file.Name.End())]

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

	// isGeneratedTypeSpec 判断 TypeSpec 是否为生成的结构体声明（Entity/DO）
	isGeneratedTypeSpec := func(spec ast.Spec) bool {
		typeSpec, ok := spec.(*ast.TypeSpec)
		return ok && (typeSpec.Name.Name == entityName || typeSpec.Name.Name == doName)
	}

	// isGeneratedMethod 判断方法是否为生成代码（需被移除并重新生成）。
	// 同时识别指针接收者 (*EntityName) 与值接收者 (EntityName)：用户手写的值接收者
	// ToDO/ToEntity 视为对生成方法的覆盖，一并移除，避免同名方法共存导致编译失败。
	isGeneratedMethod := func(decl *ast.FuncDecl) bool {
		if decl.Recv == nil || len(decl.Recv.List) == 0 {
			return false
		}
		var recvName string
		switch t := decl.Recv.List[0].Type.(type) {
		case *ast.StarExpr:
			if ident, ok := t.X.(*ast.Ident); ok {
				recvName = ident.Name
			}
		case *ast.Ident:
			recvName = t.Name
		}
		// 仅 ToDO（Entity 上）和 ToEntity（DO 上）为生成方法
		if recvName == entityName && decl.Name.Name == "ToDO" {
			return true
		}
		if recvName == doName && decl.Name.Name == "ToEntity" {
			return true
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
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok == token.IMPORT {
				importDecls = append(importDecls, getDeclSource(decl))
				// 记录现有文件已导入的包路径，供 neededImports 校验缺失时补充。
				// 仅统计无别名的导入：别名导入（含 _ 与 . 导入）绑定的是非默认包名，
				// 生成代码引用的默认包名（如 time.Time 的 time）仍不可用，
				// 不记入已存在，允许补充标准导入（两种导入可合法共存）。
				for _, spec := range d.Specs {
					impSpec, ok := spec.(*ast.ImportSpec)
					if !ok {
						continue
					}
					if impSpec.Name != nil {
						continue
					}
					if path, err := strconv.Unquote(impSpec.Path.Value); err == nil {
						existingImports[path] = true
					}
				}
				continue
			}
			if d.Tok == token.TYPE {
				// type 声明按 Spec 粒度判定：仅当块内混有用户类型时才需要部分过滤
				generated, kept := 0, 0
				for _, spec := range d.Specs {
					if isGeneratedTypeSpec(spec) {
						generated++
					} else {
						kept++
					}
				}
				if generated == 0 {
					// 无生成类型，整体保留
					userDecls = append(userDecls, getDeclSource(decl))
					continue
				}
				if kept == 0 {
					// 全部为生成类型，整体移除
					continue
				}
				// 混合 type 块：剔除生成类型，其余 Spec（含各自注释）用 go/printer 重写保留
				filtered := *d
				filtered.Specs = make([]ast.Spec, 0, kept)
				for _, spec := range d.Specs {
					if !isGeneratedTypeSpec(spec) {
						filtered.Specs = append(filtered.Specs, spec)
					}
				}
				var sb bytes.Buffer
				if err := printer.Fprint(&sb, fset, &filtered); err != nil {
					return fmt.Errorf("保留用户类型声明失败: %v", err)
				}
				userDecls = append(userDecls, sb.String())
				continue
			}
			// 其他 GenDecl（var/const）：整体保留
			userDecls = append(userDecls, getDeclSource(decl))
		case *ast.FuncDecl:
			if isGeneratedMethod(d) {
				continue
			}
			// 判断是否为 Entity 或 DO 上的自定义方法（接收者为 *EntityName 或 *DOName）
			if d.Recv != nil && len(d.Recv.List) > 0 {
				if starExpr, ok := d.Recv.List[0].Type.(*ast.StarExpr); ok {
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
		default:
			// 未知声明类型（防御性）：原样保留，避免任何用户代码丢失
			userDecls = append(userDecls, getDeclSource(decl))
		}
	}

	// 重建文件：文件头 + 原 package 行 + imports + Entity生成代码 + Entity自定义方法 + DO生成代码 + DO自定义方法 + 其他用户代码
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
	buf.Write(fileHeader)
	buf.Write(pkgClause)
	buf.WriteString("\n")

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

	return writeGeneratedFile(filePath, buf.Bytes())
}
