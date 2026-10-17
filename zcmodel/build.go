package zcmodel

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// genKind 标识骨架中的生成声明种类（Entity/DO 结构体、ToDO/ToEntity 方法），
// 其枚举顺序即生成声明的标准排列顺序。
type genKind int

const (
	genEntType  genKind = iota // Entity 结构体
	genToDO                    // Entity 上的 ToDO 方法
	genDoType                  // DO 结构体
	genToEntity                // DO 上的 ToEntity 方法
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

// sanitizeCommentLine 将文本中的换行/回车替换为空格，避免注释被撑断成裸行导致生成代码语法错误。
// TableComment、索引名与索引列文本进入 doc 注释前统一经此净化。
// gofmt 会把 doc 注释中连续两个单引号/反引号规范化为弯引号，因此在相邻两个引号之间插入
// 零宽空格 U+200B 阻止改写，使注释读起来与原文一致。
func sanitizeCommentLine(s string) string {
	s = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(s)
	for _, pair := range []string{"''", "``"} {
		for strings.Contains(s, pair) {
			s = strings.ReplaceAll(s, pair, pair[:1]+"\u200b"+pair[1:])
		}
	}
	return s
}

// ddlExtraWhiteList 是 ddl 片段允许渲染的 MySQL EXTRA 标记：marker 为大小写不敏感的匹配串，
// output 为 DDL 惯例的大写输出形态，切片顺序即片段内的输出顺序（保证产物稳定）。
// precision 为 true 的项会追加 EXTRA 中紧随 marker 的 (n) 精度（如 CURRENT_TIMESTAMP(3)）。
// DEFAULT_GENERATED 不在白名单内——默认值已由 DEFAULT 段呈现，属元数据噪音。
var ddlExtraWhiteList = []struct {
	marker    string
	output    string
	precision bool
}{
	{"auto_increment", "AUTO_INCREMENT", false},
	{"on update current_timestamp", "ON UPDATE CURRENT_TIMESTAMP", true},
	{"virtual generated", "VIRTUAL GENERATED", false},
	{"stored generated", "STORED GENERATED", false},
}

// assembleDDLFragment 组装字段的类 DDL 列定义片段：{Type} [{NULL|NOT NULL}] [DEFAULT 值] [EXTRA 白名单项]。
// Type 与 Default 取方言元数据原样，NULL 标记为显式书写（Nullable 为 nil 表示未知，整段省略，
// 避免手动构造 Input 时把可空列误标为 NOT NULL），EXTRA 仅保留白名单项。
// 返回未净化的原始文本，写入 tag 前由调用方经 sanitizeTagValue 转义。
func assembleDDLFragment(col Column) string {
	parts := []string{col.Type}
	if col.Nullable != nil {
		if *col.Nullable {
			parts = append(parts, "NULL")
		} else {
			parts = append(parts, "NOT NULL")
		}
	}
	if col.Default != nil {
		parts = append(parts, "DEFAULT "+formatDDLDefault(*col.Default))
	}
	parts = append(parts, ddLExtraParts(col.Extra)...)
	return strings.Join(parts, " ")
}

// formatDDLDefault 渲染 DEFAULT 段的值：空串补一对单引号还原为字符串字面量（MySQL 元数据对
// DEFAULT 空串给出裸空串，直接拼接会产出悬空的 DEFAULT 关键字，而空串唯一无歧义地只能是
// 字符串字面量），其余值方言原样。
// 注：本注释刻意用「一对单引号」文字描述而非直接写出——gofmt 对声明级 doc 注释做智能引号
// 规范化，连续两个单引号会被改写成右双引号、连续两个反引号会被改写成左双引号，直接写出
// 会导致 gofmt 校验不通过（返回值本身不受影响，仍是普通字符串字面量）。
func formatDDLDefault(v string) string {
	if v == "" {
		return "''"
	}
	return v
}

// ddLExtraParts 按白名单顺序取出 EXTRA 中允许渲染的标记（大小写不敏感匹配），输出 DDL 惯例的大写形态，
// ON UPDATE CURRENT_TIMESTAMP 保留其后的 (n) 精度。
// 生成列（VIRTUAL/STORED GENERATED）必须保留：该列不可写，片段是 DO 侧唯一能提示这一点的地方。
func ddLExtraParts(extra string) []string {
	if extra == "" {
		return nil
	}
	lower := strings.ToLower(extra)
	var parts []string
	for _, item := range ddlExtraWhiteList {
		i := strings.Index(lower, item.marker)
		if i < 0 {
			continue
		}
		out := item.output
		if item.precision {
			out += precisionSuffix(lower[i+len(item.marker):])
		}
		parts = append(parts, out)
	}
	return parts
}

// precisionSuffix 返回 s 开头形如 (n) 的精度后缀（n 为一位或多位数字），不存在时返回空串。
func precisionSuffix(s string) string {
	if !strings.HasPrefix(s, "(") {
		return ""
	}
	end := strings.IndexByte(s, ')')
	if end < 2 {
		return ""
	}
	for _, r := range s[1:end] {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return s[:end+1]
}

// buildStructDocComment 组装结构体 doc 注释：首行为 comment，其后追加索引注释块（无索引时不追加）。
// comment 与索引文本经换行净化，避免注释行断裂；两者均为空时返回空串（不生成注释）。
func buildStructDocComment(comment string, indexes []IndexInfo) string {
	lines := make([]string, 0, len(indexes)+4)
	if comment != "" {
		lines = append(lines, "// "+sanitizeCommentLine(comment))
	}
	if block := buildIndexCommentBlock(indexes); len(block) > 0 {
		lines = append(lines, "//", "// 索引:")
		lines = append(lines, block...)
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// buildIndexCommentBlock 渲染索引清单，排序固定为 PRIMARY → UNIQUE（索引名字典序）→ 普通（索引名字典序），
// 主键行渲染为 PRIMARY KEY (cols)，不显示方言内部物理索引名。返回的行均为完整注释行，
// bullet 前缀采用 gofmt 对 doc 注释清单的规范形态（// + 三个空格 + "- "），使产物与 gofmt 输出一致。
func buildIndexCommentBlock(indexes []IndexInfo) []string {
	if len(indexes) == 0 {
		return nil
	}
	// 副本排序：渲染不得改动调用方传入的切片（generate.go 直接透传 Input.Indexes）
	ordered := make([]IndexInfo, len(indexes))
	copy(ordered, indexes)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Primary != ordered[j].Primary {
			return ordered[i].Primary
		}
		if ordered[i].Unique != ordered[j].Unique {
			return ordered[i].Unique
		}
		return ordered[i].Name < ordered[j].Name
	})
	lines := make([]string, 0, len(ordered))
	for _, idx := range ordered {
		cols := sanitizeCommentLine(strings.Join(idx.Columns, ", "))
		switch {
		case idx.Primary:
			lines = append(lines, "//   - PRIMARY KEY ("+cols+")")
		case idx.Unique:
			lines = append(lines, "//   - UNIQUE KEY "+sanitizeCommentLine(idx.Name)+" ("+cols+")")
		default:
			lines = append(lines, "//   - KEY "+sanitizeCommentLine(idx.Name)+" ("+cols+")")
		}
	}
	return lines
}

// buildStruct 生成结构体定义的字符串，tag 顺序为 json、columnTagName、primary_key、ddl、description，
// 字段名和类型按最长者计算宽度对齐（最终产物在 writeGeneratedFile 中会经 go/format 统一格式化）。
// 当 col.Comment 非空时生成 `description:"Comment"` tag；col.Type 非空时生成 `ddl` tag；
// col.PrimaryKey 为 true 时生成 `primary_key:"true"` tag。
// comment 非空或 indexes 非空时在 type 声明前生成 doc 注释（含索引块）。
// useAny 为 true 时字段类型使用 any（用于 DO 结构体），为 false 时使用具体类型（用于 Entity 结构体）。
// columnTagName 指定结构体字段的 tag 名称（如 "column"、"db" 等），为空时不生成列名 tag，避免产生空 tag 名 `:"colname"`。
// json、列映射、ddl、description 的 tag 值均经 sanitizeTagValue 转义，含引号、反斜杠、换行的列名也能经反射完整还原。
func buildStruct(name string, columns []Column, useAny bool, comment string, columnTagName string, indexes []IndexInfo) string {
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
	if doc := buildStructDocComment(comment, indexes); doc != "" {
		buf.WriteString(doc)
	}
	buf.WriteString(fmt.Sprintf("type %s struct {\n", name))
	for _, col := range columns {
		var goType string
		if useAny {
			goType = "any"
		} else {
			goType = col.StructFieldInfo.Type
		}
		// 构建字段 tag：顺序为 json、columnTagName、primary_key、ddl、description；各自数据为空时跳过
		var tagParts []string
		if col.StructFieldInfo.JsonTagValue != "" {
			tagParts = append(tagParts, fmt.Sprintf("json:\"%s\"", sanitizeTagValue(col.StructFieldInfo.JsonTagValue)))
		}
		if columnTagName != "" {
			tagParts = append(tagParts, fmt.Sprintf("%s:\"%s\"", columnTagName, sanitizeTagValue(col.Name)))
		}
		if col.PrimaryKey {
			tagParts = append(tagParts, `primary_key:"true"`)
		}
		if col.Type != "" {
			tagParts = append(tagParts, fmt.Sprintf("ddl:\"%s\"", sanitizeTagValue(assembleDDLFragment(col))))
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

// packageNameOf 推导新建文件的 Go 包名：取输出文件所在目录的绝对路径末级目录名，
// 使相对路径 "." 也能解析为当前目录的实际名称；无法推导（Abs 失败、位于根目录）时回退为 "main"。
func packageNameOf(filePath string) string {
	dir, err := filepath.Abs(filepath.Dir(filePath))
	if err != nil {
		return "main"
	}
	name := filepath.Base(dir)
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "main"
	}
	return name
}

// importLocalName 返回 import spec 在文件内绑定的包名，无法确定时返回空串：
// 具名别名取别名；_ 与 . 导入、"C" 不绑定可移除的包名；无别名时按 goimports 惯例取路径末段，
// 末段不是合法标识符（如 yaml.v3）或为主版本后缀（如 v2）时实际包名不可知，返回空串。
func importLocalName(spec *ast.ImportSpec) string {
	if spec.Name != nil {
		if spec.Name.Name == "_" || spec.Name.Name == "." {
			return ""
		}
		return spec.Name.Name
	}
	path, err := strconv.Unquote(spec.Path.Value)
	if err != nil || path == "C" {
		return ""
	}
	name := path[strings.LastIndex(path, "/")+1:]
	if !token.IsIdentifier(name) {
		return ""
	}
	if len(name) > 1 && name[0] == 'v' && strings.Trim(name[1:], "0123456789") == "" {
		return ""
	}
	return name
}

// collectSelectorRefs 收集节点内选择器表达式 X.Sel 中作为 X 的标识符名（包名引用的候选集合）。
func collectSelectorRefs(node ast.Node, into map[string]bool) {
	ast.Inspect(node, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok {
				into[id.Name] = true
			}
		}
		return true
	})
}

// splitGeneratedCode 把生成代码块拆成结构体声明段与方法声明段，后者可能为空：
// 结构体段为名为 structName 的 type 声明（含其 Doc 注释），方法段为名为 methodName 的函数声明（含其 Doc 注释）。
// 之所以要拆分，是因为骨架保持式合并按「逐声明独立槽位」替换——Entity 结构体与 ToDO 方法、
// DO 结构体与 ToEntity 方法在原文件中各自占一个槽位，必须能分别原位替换。
// 生成代码本身非法时返回错误（落盘前还有 go/format 全量自校验兜底）。
func splitGeneratedCode(code, structName, methodName string) (structText, methodText string, err error) {
	src := "package p\n\n" + code + "\n"
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return "", "", err
	}
	tf := fset.File(f.Pos())
	off := func(p token.Pos) int { return tf.Offset(p) }
	for _, decl := range f.Decls {
		start := off(decl.Pos())
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Doc != nil {
				start = off(d.Doc.Pos())
			}
			for _, spec := range d.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok && ts.Name.Name == structName {
					structText = src[start:off(decl.End())]
				}
			}
		case *ast.FuncDecl:
			if d.Doc != nil {
				start = off(d.Doc.Pos())
			}
			if d.Name.Name == methodName {
				methodText = src[start:off(decl.End())]
			}
		}
	}
	return structText, methodText, nil
}

// writeOrReplaceStruct 将 Entity 和 DO 结构体（含关联方法）写入文件，采用「骨架保持式合并」。
//
// 骨架 = 原文件顶层的元素序列（原文件头、package 行、import、各顶层声明、文件尾注释）。
// 合并时骨架的排列结构原封不动——有哪些声明、按什么顺序、各自在什么位置都不变：
// 属于生成区的声明（Entity/DO 结构体、ToDO/ToEntity 方法）在各自原位替换为新内容，
// 用户声明（含其前置游离注释、同行尾注释、声明间游离注释与文件尾注释）的内容与顺序保留在原位置。
// 用户代码不产生位置漂移的噪音；但整个产物最终经 go/format 统一格式化（缩进、对齐、import 排序，
// 槽位间统一以一个空行分隔），因此首次再生成可能同时出现用户区的格式类变更。
//
// 生成声明按「逐声明独立槽位」识别与替换：Entity 结构体、ToDO、DO 结构体、ToEntity 各占一个槽位，
// 即使它们之间夹着用户代码也不搬动。生成声明在原文件中缺失时（首次引入 DO、用户手删过 ToDO 等）
// 按配对落点补入：Entity 结构体↔ToDO、DO 结构体↔ToEntity 互为配对，结构体缺失补在其方法前、
// 方法缺失补在其结构体后；两个都缺或都无配对时，追加到最后一个生成声明之后
// （原文件没有任何生成声明时置于首个用户声明之前），并按标准顺序
// Entity 结构体 → ToDO → DO 结构体 → ToEntity 排列。
// 同类生成声明出现多份时（如用户手写的值接收者 ToDO 与生成的指针接收者 ToDO 共存），
// 首个槽位原位替换、其余重复槽位移除，避免同名方法共存导致编译失败。
//
// type 声明块按 Spec 粒度处理：块内生成 Spec 就地替换为新结构体、不再生成的生成 Spec 剔除，
// 重复生成的 Spec（含跨槽位重复）只保留首个、其余剔除，用户类型（含各自注释）与块内
// 不附着于任何 Spec 的游离注释原样保留；仅含一个生成 Spec 的声明以独立声明形态输出
// （不保留括号块），与首次生成的形态保持一致。
//
// go/parser 不会把 package 声明之前的 build tags/文件级注释放进 file.Decls，重建时按源码偏移
// 显式前置保留；已有文件的包名尊重原文件，仅新建文件使用目录推导包名（见 packageNameOf）。
// 最终产物统一经 go/format 格式化并原子写入，保留原文件权限位。
//
// neededImports 为生成代码引用的包（如 time.Time 需要 time），写入时若文件缺少对应 import 会自动补上
// （合并进第一个保留的 import 块，避免形成两个 import 块）；
// 存量文件以别名（含 _、. 导入）引入所需包时不视为已存在，仍会补充默认导入，保证生成代码可编译。
// 反向地，旧生成代码引用过、而新生成代码与保留的用户代码都不再引用的 import spec 会被移除
// （如删去最后一个时间列后的 time），块内 spec 全部移除时整个 import 声明一并移除；
// _、. 导入、"C" 及包名无法由路径确定的 import 不做移除。
// filePath: 输出文件路径
// entityName: Entity 结构体名
// entityCode: Entity 结构体及 ToDO 方法的生成代码
// doName: DO 结构体名
// doCode: DO 结构体及 ToEntity 方法的生成代码
// neededImports: 生成代码需要导入的包路径列表
func writeOrReplaceStruct(filePath, entityName, entityCode, doName, doCode string, neededImports []string) error {
	pkgName := packageNameOf(filePath)

	// 文件不存在时直接创建，自动引入生成代码需要的 import
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return writeGeneratedFile(filePath, []byte(buildFileContent(pkgName, entityCode, doCode, neededImports)))
	}

	// 读取现有文件
	origContent, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	// 文件为空时按新建处理
	if strings.TrimSpace(string(origContent)) == "" {
		return writeGeneratedFile(filePath, []byte(buildFileContent(pkgName, entityCode, doCode, neededImports)))
	}

	// 把生成代码拆成四个声明（Entity 结构体、ToDO、DO 结构体、ToEntity），供逐声明原位替换。
	// 某段为空表示生成代码不含该声明（如调用方只传结构体）；此时该生成声明不写入，原文件中的旧槽位一并移除。
	entStructText, toDOMethodText, err := splitGeneratedCode(entityCode, entityName, "ToDO")
	if err != nil {
		return fmt.Errorf("生成代码存在语法错误: %v", err)
	}
	doStructText, toEntityMethodText, err := splitGeneratedCode(doCode, doName, "ToEntity")
	if err != nil {
		return fmt.Errorf("生成代码存在语法错误: %v", err)
	}
	genText := map[genKind]string{
		genEntType:  entStructText,
		genToDO:     toDOMethodText,
		genDoType:   doStructText,
		genToEntity: toEntityMethodText,
	}
	// genSpecText 为结构体的「Spec 形态」文本：去掉 type 关键字、保留 Doc 注释，
	// 用于把新结构体就地替换进 type (…) 块（块内 Spec 不带 type 关键字）。
	specTextFor := func(declText, name string) string {
		marker := "type "
		if i := strings.Index(declText, marker+name); i >= 0 {
			return declText[:i] + declText[i+len(marker):]
		}
		return declText
	}
	genSpecText := map[genKind]string{
		genEntType: specTextFor(entStructText, entityName),
		genDoType:  specTextFor(doStructText, doName),
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filePath, origContent, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("解析文件失败: %v", err)
	}
	tf := fset.File(file.Pos())
	off := func(p token.Pos) int { return tf.Offset(p) }

	// declStarts[i] 为第 i 个声明的起始偏移（含前置 Doc 注释）
	declStarts := make([]int, len(file.Decls))
	for i, decl := range file.Decls {
		declStarts[i] = off(decl.Pos())
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Doc != nil {
				declStarts[i] = off(d.Doc.Pos())
			}
		case *ast.FuncDecl:
			if d.Doc != nil {
				declStarts[i] = off(d.Doc.Pos())
			}
		}
	}
	// nextStart 返回第 i 个声明之后的下一个声明起点（无则为文件末尾），作为同行尾注释的扩展上限
	nextStart := func(i int) int {
		if i+1 < len(declStarts) {
			return declStarts[i+1]
		}
		return len(origContent)
	}
	// lineCommentEnd 若 end 之后同一行（仅隔空格/制表符）紧跟注释组、且注释组起点早于 limit，
	// 返回该注释组的结束偏移，否则原样返回 end。用于把 `var X = 1 // c`、`func F() {} // c`
	// 这类位于声明 End() 之后的行尾注释并入声明区间。
	lineCommentEnd := func(end, limit int) int {
		for _, cg := range file.Comments {
			from := off(cg.Pos())
			if from < end {
				continue
			}
			if from >= limit || strings.TrimLeft(string(origContent[end:from]), " \t") != "" {
				break
			}
			return off(cg.End())
		}
		return end
	}

	// 保留 package 声明之前的全部原文（build tags、文件级注释）与原 package 行（含同行尾注释）：
	// go/parser 不把它们放进 file.Decls，重建时必须显式前置，否则再生成会静默丢失。
	// 已有文件的包名尊重原文件，不强制改为输出目录推导的包名。
	pkgOffset := off(file.Package)
	pkgEnd := lineCommentEnd(off(file.Name.End()), nextStart(-1))
	fileHeader := origContent[:pkgOffset]
	pkgClause := origContent[pkgOffset:pkgEnd]

	// generatedTypeKind 判断 TypeSpec 是否为生成的结构体声明，并返回其种类：
	// Entity 结构体（genEntType）或 DO 结构体（genDoType）；非生成类型返回 false。
	generatedTypeKind := func(spec ast.Spec) (genKind, bool) {
		typeSpec, ok := spec.(*ast.TypeSpec)
		if !ok {
			return 0, false
		}
		switch typeSpec.Name.Name {
		case entityName:
			return genEntType, true
		case doName:
			return genDoType, true
		}
		return 0, false
	}

	// isGeneratedTypeSpec 判断 TypeSpec 是否为生成的结构体声明（Entity/DO）
	isGeneratedTypeSpec := func(spec ast.Spec) bool {
		_, ok := generatedTypeKind(spec)
		return ok
	}

	// specSpan 返回单个 Spec 的源码区间 [from, to)，包含其前置 Doc 注释与尾注释；
	// 无注释时退化为 Spec 自身的源码区间。
	specSpan := func(spec ast.Spec) (from, to int) {
		from = off(spec.Pos())
		to = off(spec.End())
		switch s := spec.(type) {
		case *ast.ImportSpec:
			if s.Doc != nil {
				from = off(s.Doc.Pos())
			}
			if s.Comment != nil {
				to = off(s.Comment.End())
			}
		case *ast.TypeSpec:
			if s.Doc != nil {
				from = off(s.Doc.Pos())
			}
			if s.Comment != nil {
				to = off(s.Comment.End())
			}
		}
		return from, to
	}

	// lineSpan 把 [from, to) 扩展为整行区间（含行首缩进与行尾换行），仅当该区间独占若干整行时生效；
	// 与其他 spec 同行时连同其后的分号一并剔除（残留的 ; 会构成缺少路径的空 spec），否则原样返回；
	// 用于移除 import spec 时不留下空行或语法错误。
	lineSpan := func(from, to int) (int, int) {
		ls := from
		for ls > 0 && (origContent[ls-1] == ' ' || origContent[ls-1] == '\t') {
			ls--
		}
		le := to
		for le < len(origContent) && (origContent[le] == ' ' || origContent[le] == '\t' || origContent[le] == '\r') {
			le++
		}
		if (ls == 0 || origContent[ls-1] == '\n') && le < len(origContent) && origContent[le] == '\n' {
			return ls, le + 1
		}
		if le < len(origContent) && origContent[le] == ';' {
			le++
			for le < len(origContent) && (origContent[le] == ' ' || origContent[le] == '\t') {
				le++
			}
			return from, le
		}
		return from, to
	}

	// renderTypeBlock 在 type 声明块（区间 [start, end)）中就地替换生成的类型 Spec：
	// 每个生成 Spec 的「Doc 注释 + 声明 + 尾注释」区间整体换为新结构体文本
	// （新文本为空表示该生成声明不再生成，即剔除），用户类型 Spec、块级 Doc 注释、
	// 块内游离注释与其余原文原样保留。相比用 go/printer 重写过滤后的 AST，
	// 此法不重建声明节点，块内不附着于任何 Spec 的游离注释不会被丢弃。
	//
	// 生成 Spec 按 genKind 去重：alreadySeen 为进入本块前已出现过的种类（前序独立声明或
	// 前序块），本块内同种类也只输出首个；重复项就地剔除，避免同名类型共存。
	// 返回值 emitted 为本次实际写入的生成种类，供调用方更新 seenGen/carrierIdx。
	renderTypeBlock := func(d *ast.GenDecl, start, end int, alreadySeen map[genKind]bool) (string, map[genKind]bool) {
		var sb strings.Builder
		last := start
		emitted := make(map[genKind]bool)
		for _, spec := range d.Specs {
			k, ok := generatedTypeKind(spec)
			if !ok {
				continue
			}
			from, to := specSpan(spec)
			sb.WriteString(string(origContent[last:from]))
			if genText[k] != "" && !alreadySeen[k] && !emitted[k] {
				sb.WriteString(genSpecText[k])
				emitted[k] = true
			}
			last = to
		}
		sb.WriteString(string(origContent[last:end]))
		return sb.String(), emitted
	}

	// receiverTypeName 返回方法接收者的类型名：指针接收者 (*Name) 与值接收者 (Name) 均返回 Name，
	// 括号包裹的接收者（如 (Name)、((Name))、*(Name)）先解包再判定；无接收者或接收者为其他类型
	// （如泛型、选择器表达式）时返回空串。
	unwrapParen := func(expr ast.Expr) ast.Expr {
		for {
			p, ok := expr.(*ast.ParenExpr)
			if !ok {
				return expr
			}
			expr = p.X
		}
	}
	receiverTypeName := func(decl *ast.FuncDecl) string {
		if decl.Recv == nil || len(decl.Recv.List) == 0 {
			return ""
		}
		switch t := unwrapParen(decl.Recv.List[0].Type).(type) {
		case *ast.StarExpr:
			if ident, ok := unwrapParen(t.X).(*ast.Ident); ok {
				return ident.Name
			}
		case *ast.Ident:
			return t.Name
		}
		return ""
	}

	// isGeneratedMethod 判断方法是否为生成代码（需被移除并重新生成）。
	// 同时识别指针接收者 (*EntityName) 与值接收者 (EntityName)：用户手写的值接收者
	// ToDO/ToEntity 视为对生成方法的覆盖，一并移除，避免同名方法共存导致编译失败。
	isGeneratedMethod := func(decl *ast.FuncDecl) bool {
		recvName := receiverTypeName(decl)
		// 仅 ToDO（Entity 上）和 ToEntity（DO 上）为生成方法
		if recvName == entityName && decl.Name.Name == "ToDO" {
			return true
		}
		if recvName == doName && decl.Name.Name == "ToEntity" {
			return true
		}
		return false
	}

	// 预扫描包名引用：oldGenRefs 为旧生成代码引用的包名候选，usedRefs 为新生成代码与保留的用户代码引用的包名候选。
	// 旧生成代码引用过、而 usedRefs 中不再出现的 import spec 在重建时移除，避免产物 imported and not used。
	oldGenRefs := make(map[string]bool)
	usedRefs := make(map[string]bool)
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok == token.IMPORT {
				continue
			}
			if d.Tok == token.TYPE {
				for _, spec := range d.Specs {
					if isGeneratedTypeSpec(spec) {
						collectSelectorRefs(spec, oldGenRefs)
					} else {
						collectSelectorRefs(spec, usedRefs)
					}
				}
				continue
			}
			collectSelectorRefs(d, usedRefs)
		case *ast.FuncDecl:
			if isGeneratedMethod(d) {
				collectSelectorRefs(d, oldGenRefs)
			} else {
				collectSelectorRefs(d, usedRefs)
			}
		default:
			collectSelectorRefs(decl, usedRefs)
		}
	}
	genFile, err := parser.ParseFile(token.NewFileSet(), "", "package p\n\n"+entityCode+"\n\n"+doCode+"\n", 0)
	if err != nil {
		return fmt.Errorf("生成代码存在语法错误: %v", err)
	}
	collectSelectorRefs(genFile, usedRefs)

	neededSet := make(map[string]bool, len(neededImports))
	for _, p := range neededImports {
		neededSet[p] = true
	}
	removedSpecs := make(map[ast.Spec]bool)
	droppedImportDecls := make(map[*ast.GenDecl]bool)
	for _, decl := range file.Decls {
		d, ok := decl.(*ast.GenDecl)
		if !ok || d.Tok != token.IMPORT {
			continue
		}
		removed := 0
		for _, spec := range d.Specs {
			impSpec, ok := spec.(*ast.ImportSpec)
			if !ok {
				continue
			}
			name := importLocalName(impSpec)
			path, _ := strconv.Unquote(impSpec.Path.Value)
			if name != "" && oldGenRefs[name] && !usedRefs[name] && !neededSet[path] {
				removedSpecs[spec] = true
				removed++
			}
		}
		if removed > 0 && removed == len(d.Specs) {
			droppedImportDecls[d] = true
		}
	}

	// 骨架保持式分类：以原文件顶层声明序列为骨架，逐个槽位（slot）收集，顺序即原顺序。
	// 生成声明槽位只保留其前置游离注释前缀，正文留到组装阶段用新内容替换；
	// 用户槽位保存完整正文（含前置游离注释、声明本体与同行尾注释），原位原样输出。
	// import 声明单独收集，统一前置到 package 行之后。
	type importEntry struct {
		decl       *ast.GenDecl
		lead       string // 前置游离注释
		start, end int    // 声明区间（含 Doc 与同行尾注释）
	}
	type slot struct {
		isGen  bool    // 是否为生成声明槽位（原位替换）
		hasGen bool    // 该槽位是否承载生成声明（混合 type 块承载块内生成类型时为 true）
		kind   genKind // isGen 为 true 时有效
		text   string  // 用户槽位：完整正文；生成槽位：仅保留的前置游离注释前缀
	}
	var imports []importEntry
	var slots []slot
	carrierIdx := make(map[genKind]int) // 生成声明种类 → 承载它的槽位索引
	seenGen := make(map[genKind]bool)   // 已建立槽位的生成声明种类（重复出现时去重）
	existingImports := make(map[string]bool)

	prev := pkgEnd
	pending := "" // 被移除声明之前的游离注释，顺延给下一个保留声明
	takeLead := func(lead string) string {
		s := strings.TrimLeft(pending+lead, " \t\r\n")
		pending = ""
		return s
	}

	for i, decl := range file.Decls {
		start := declStarts[i]
		end := lineCommentEnd(off(decl.End()), nextStart(i))
		lead := string(origContent[prev:start])
		prev = end
		body := string(origContent[start:end])

		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok == token.IMPORT {
				if droppedImportDecls[d] {
					pending += lead
					continue
				}
				imports = append(imports, importEntry{decl: d, lead: takeLead(lead), start: start, end: end})
				// 记录现有文件已导入的包路径，供 neededImports 校验缺失时补充。
				// 仅统计无别名的导入：别名导入（含 _ 与 . 导入）绑定的是非默认包名，
				// 生成代码引用的默认包名（如 time.Time 的 time）仍不可用，
				// 不记入已存在，允许补充标准导入（两种导入可合法共存）。
				for _, spec := range d.Specs {
					impSpec, ok := spec.(*ast.ImportSpec)
					if !ok || removedSpecs[spec] || impSpec.Name != nil {
						continue
					}
					if path, err := strconv.Unquote(impSpec.Path.Value); err == nil {
						existingImports[path] = true
					}
				}
				continue
			}
			if d.Tok == token.TYPE {
				// 统计块内 Spec：有效的生成 Spec、用户 Spec、不再生成的生成 Spec
				genCount, userCount, dropCount := 0, 0, 0
				for _, spec := range d.Specs {
					if k, ok := generatedTypeKind(spec); ok {
						if genText[k] == "" {
							dropCount++
						} else {
							genCount++
						}
					} else {
						userCount++
					}
				}
				switch {
				case genCount == 0 && dropCount == 0:
					// 纯用户 type 声明块：整块原位保留
					slots = append(slots, slot{text: takeLead(lead) + body})
				case genCount == 0 && userCount == 0:
					// 块内仅含不再生成的生成类型：整块移除，前置游离注释顺延
					pending += lead
				case genCount == 0:
					// 块内含用户类型，仅剔除不再生成的生成 Spec
					rendered, _ := renderTypeBlock(d, start, end, seenGen)
					slots = append(slots, slot{text: takeLead(lead) + rendered})
				case len(d.Specs) == 1 && userCount == 0 && dropCount == 0:
					// 单一生成类型（标准的独立 type 声明，或仅含一个生成类型的括号块）：
					// 以独立声明形态就地替换，不保留括号块，与首次生成的形态一致
					k, _ := generatedTypeKind(d.Specs[0])
					if seenGen[k] {
						pending += lead
						continue
					}
					seenGen[k] = true
					slots = append(slots, slot{isGen: true, hasGen: true, kind: k, text: takeLead(lead)})
					carrierIdx[k] = len(slots) - 1
				default:
					// 混合 type 块（含用户类型，或含多个生成类型）：保留块形态，
					// 块内生成 Spec 就地替换、不再生成的生成 Spec 与重复生成的 Spec 剔除，
					// 用户 Spec 与游离注释原样保留
					rendered, emitted := renderTypeBlock(d, start, end, seenGen)
					if len(emitted) == 0 && userCount == 0 {
						// 块内生成 Spec 均为前序重复：整块移除，前置游离注释顺延
						pending += lead
						continue
					}
					slots = append(slots, slot{hasGen: len(emitted) > 0, text: takeLead(lead) + rendered})
					for k := range emitted {
						seenGen[k] = true
						carrierIdx[k] = len(slots) - 1
					}
				}
				continue
			}
			// 其他 GenDecl（var/const）：整块原位保留
			slots = append(slots, slot{text: takeLead(lead) + body})
		case *ast.FuncDecl:
			if isGeneratedMethod(d) {
				k := genToEntity
				if receiverTypeName(d) == entityName {
					k = genToDO
				}
				// 生成代码不含该声明（genToText 为空）或该种类已有槽位时不再输出，避免重名方法共存
				if genText[k] == "" || seenGen[k] {
					pending += lead
					continue
				}
				seenGen[k] = true
				slots = append(slots, slot{isGen: true, hasGen: true, kind: k, text: takeLead(lead)})
				carrierIdx[k] = len(slots) - 1
				continue
			}
			// 用户方法（含 Entity/DO 上的自定义方法）：不再归位，原位保留
			slots = append(slots, slot{text: takeLead(lead) + body})
		default:
			// 未知声明类型（防御性）：原样保留，避免任何用户代码丢失
			slots = append(slots, slot{text: takeLead(lead) + body})
		}
	}
	// 文件尾：最后一个声明之后的注释，以及顺延到末尾仍未被认领的游离注释
	tail := strings.TrimSpace(pending + string(origContent[prev:]))

	// 重建文件：文件头 + 原 package 行 + imports + Entity生成代码 + Entity自定义方法 + DO生成代码 + DO自定义方法 + 其他用户代码 + 文件尾注释
	// 生成代码需要的 import 缺失时自动补充：若文件仍有保留的 import 声明则合并进第一个
	// （避免形成两个 import 块），否则新建一个 import 声明。
	var missingImports []string
	for _, p := range neededImports {
		if !existingImports[p] {
			missingImports = append(missingImports, p)
		}
	}
	// renderImport 输出保留的 import 声明：按源码偏移剔除 removedSpecs（连同所在整行），
	// 并把 missing 合并进来——已为 import (…) 块时在右括号前插入新 spec，其余原文（含注释）不变；
	// 单个 import "x" 时展开为 import (…) 块，保留原块 Doc 注释与各 spec 原文（含尾注释）。
	renderImport := func(e importEntry, missing []string) string {
		d := e.decl
		var newLines strings.Builder
		for _, p := range missing {
			newLines.WriteString("\t")
			newLines.WriteString(strconv.Quote(p))
			newLines.WriteString("\n")
		}
		if !d.Lparen.IsValid() {
			if len(missing) == 0 {
				return string(origContent[e.start:e.end])
			}
			var sb strings.Builder
			tailFrom := off(d.End())
			for _, spec := range d.Specs {
				from, to := specSpan(spec)
				sb.WriteString("\t")
				sb.WriteString(string(origContent[from:to]))
				sb.WriteString("\n")
				if to > tailFrom {
					tailFrom = to
				}
			}
			if tailFrom > e.end {
				tailFrom = e.end
			}
			sb.WriteString(newLines.String())
			return string(origContent[e.start:off(d.Pos())]) + "import (\n" + sb.String() + ")" + string(origContent[tailFrom:e.end])
		}
		var sb strings.Builder
		last := e.start
		for _, spec := range d.Specs {
			if !removedSpecs[spec] {
				continue
			}
			from, to := lineSpan(specSpan(spec))
			sb.WriteString(string(origContent[last:from]))
			last = to
		}
		rparen := off(d.Rparen)
		sb.WriteString(string(origContent[last:rparen]))
		sb.WriteString(newLines.String())
		sb.WriteString(string(origContent[rparen:e.end]))
		return sb.String()
	}
	var importDecls []string
	for i, e := range imports {
		var missing []string
		if i == 0 {
			missing = missingImports
		}
		importDecls = append(importDecls, e.lead+renderImport(e, missing))
	}
	if len(imports) == 0 && len(missingImports) > 0 {
		importDecls = append(importDecls, buildImportDecl(missingImports))
	}

	// 计算缺失生成声明的落点。配对：Entity 结构体↔ToDO、DO 结构体↔ToEntity。
	// 结构体缺失补在其方法之前、方法缺失补在其结构体之后；两个都缺或都无配对时，
	// 追加到最后一个生成声明之后（无任何生成声明时置于首个用户声明之前）。
	// beforeMap[i] 表示在原槽位 slots[i] 之前插入的生成声明（i == len(slots) 表示置于末尾）。
	beforeMap := make(map[int][]genKind)
	var unpaired []genKind
	for _, pc := range []struct {
		self, partner genKind
		after         bool
	}{
		{genEntType, genToDO, false},
		{genToDO, genEntType, true},
		{genDoType, genToEntity, false},
		{genToEntity, genDoType, true},
	} {
		if _, ok := carrierIdx[pc.self]; ok || genText[pc.self] == "" {
			continue // 已存在，或新代码不生成该声明
		}
		if pidx, ok := carrierIdx[pc.partner]; ok {
			pos := pidx
			if pc.after {
				pos = pidx + 1
			}
			beforeMap[pos] = append(beforeMap[pos], pc.self)
			continue
		}
		unpaired = append(unpaired, pc.self)
	}
	if len(unpaired) > 0 {
		sort.Slice(unpaired, func(i, j int) bool { return unpaired[i] < unpaired[j] })
		base := 0
		for i, s := range slots {
			if s.hasGen {
				base = i + 1
			}
		}
		beforeMap[base] = append(beforeMap[base], unpaired...)
	}

	// 按骨架顺序输出各槽位：生成声明槽位在原位替换为新内容，用户槽位原样，缺失的生成声明按落点插入。
	var out []string
	for i := 0; i <= len(slots); i++ {
		for _, k := range beforeMap[i] {
			out = append(out, genText[k])
		}
		if i == len(slots) {
			break
		}
		if s := slots[i]; s.isGen {
			out = append(out, s.text+genText[s.kind])
		} else {
			out = append(out, s.text)
		}
	}

	// 重建产物：原文件头 + 原 package 行 + imports + 骨架槽位（原序）+ 文件尾注释。
	// 存量文件 package 声明之前的原文（build tags、文件级注释）按原文前置保留，不做任何补写。
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
	for i, s := range out {
		if i > 0 {
			buf.WriteString("\n\n")
		}
		buf.WriteString(s)
	}
	if tail != "" {
		if len(out) > 0 {
			buf.WriteString("\n\n")
		}
		buf.WriteString(tail)
	}
	buf.WriteString("\n")

	return writeGeneratedFile(filePath, buf.Bytes())
}
