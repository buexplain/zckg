package zcmodel

import (
	"errors"
	"fmt"
	"go/build"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Generate 根据表信息和列定义生成 Entity 结构体、DO 结构体及两者的互转方法（ToDO/ToEntity），
// 并将生成代码写入 OutputDir 下的 Go 文件。
// 处理流程：
//  1. 校验数据库方言、表名（防路径穿越）、表名推导的结构体名（须为导出标识符）、输出文件名
//     （Input.FileName 或表名，不得触发 Go 文件名构建规则）、列映射 tag 名和 JSON tag 命名风格；
//  2. 复制列信息（nil 列报错），为副本中未显式指定的字段补充字段名（ToPascalCase）、字段类型（formatStructFieldType）
//     和 JSON tag 值（formatJSONTag），不改动调用方传入的数据；
//  3. 检测转换后字段名为空/非法标识符/未导出/与生成方法 ToDO、ToEntity 同名/重复的列并报错，
//     避免产出无法编译或无法映射的代码；
//  4. 生成 Entity（值类型字段）和 DO（any 类型字段）结构体及互转方法；
//  5. 将生成代码写入 {OutputDir}/{文件名}.go（OutputDir 为空时报错），已存在的文件采用「骨架保持式合并」——
//     顶层声明的排列结构不变，生成声明（Entity/DO 结构体、ToDO/ToEntity 方法）在原位替换，用户代码原位保留；
//     落盘前对完整内容做 go/format 自校验，非法产物直接报错不写文件。
func Generate(input Input) error {
	// 校验数据库方言
	switch input.Dialect {
	case DialectMysql, DialectPostgres, DialectSqlite:
	default:
		return fmt.Errorf("不支持的数据库方言: %s", input.Dialect)
	}
	// 校验表名：默认用作文件名，必须拒绝路径穿越与非法文件名字符
	if err := validateTableName(input.TableName); err != nil {
		return err
	}
	// 结构体名由表名推导，前置校验其为合法的导出标识符（如 public.users、_2fa、_用户 均不满足）
	baseName := ToPascalCase(input.TableName)
	if !token.IsIdentifier(baseName) || !token.IsExported(baseName) {
		return fmt.Errorf("表名 %s 推导出的结构体名前缀 %q 不是合法的导出 Go 标识符", input.TableName, baseName)
	}
	fileName, err := resolveFileName(input.FileName, input.TableName)
	if err != nil {
		return err
	}
	if err := validateColumnTagName(input.ColumnTagName); err != nil {
		return err
	}

	// 复制列信息：后续补全只作用于副本，避免原地修改调用方传入的 Column
	columns := make([]Column, len(input.Columns))
	for i, c := range input.Columns {
		if c == nil {
			return fmt.Errorf("列[%d] 为 nil", i)
		}
		columns[i] = *c
	}

	// 处理json tag的值
	if input.JsonTagValueCase != "" {
		if input.JsonTagValueCase.IsValid() == false {
			return fmt.Errorf("不支持的 json tag 命名风格: %s", input.JsonTagValueCase)
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
			columns[i].StructFieldInfo.Name = ToPascalCase(columns[i].Name)
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

	// 字段名合法性检测：空字段名、非法标识符（如数字开头）、未导出标识符、与生成方法同名、转换后重名
	// 都会产出无法编译或无法使用的代码，直接报错
	seenNames := make(map[string]string, len(columns))
	for _, c := range columns {
		if c.StructFieldInfo.Name == "" {
			return fmt.Errorf("列 %q 转换后的字段名为空", c.Name)
		}
		// 前置拦截非法标识符（如数字开头列名转换出的 2faCode），
		// 给出带列名上下文的明确错误，避免依赖落盘语法自校验兜底导致根因难定位
		if !token.IsIdentifier(c.StructFieldInfo.Name) {
			return fmt.Errorf("列 %q 转换后的字段名 %q 不是合法的 Go 标识符", c.Name, c.StructFieldInfo.Name)
		}
		// 未导出字段（如中文列名、显式指定的 _）会被 json 与 zcdb 映射静默跳过，
		// 且 _ 无法在 ToDO/ToEntity 中被引用
		if !token.IsExported(c.StructFieldInfo.Name) {
			return fmt.Errorf("列 %q 转换后的字段名 %q 不是导出标识符（首字符须为大写字母），请通过 StructFieldInfo.Name 指定导出名", c.Name, c.StructFieldInfo.Name)
		}
		// Entity 上生成 ToDO 方法、DO 上生成 ToEntity 方法，两个结构体字段集合相同，同名字段一律拒绝
		if c.StructFieldInfo.Name == "ToDO" || c.StructFieldInfo.Name == "ToEntity" {
			return fmt.Errorf("列 %q 转换后的字段名 %q 与生成方法同名，请通过 StructFieldInfo.Name 改名", c.Name, c.StructFieldInfo.Name)
		}
		if prev, ok := seenNames[c.StructFieldInfo.Name]; ok {
			return fmt.Errorf("列 %q 与 %q 转换后的字段名重复: %q", prev, c.Name, c.StructFieldInfo.Name)
		}
		seenNames[c.StructFieldInfo.Name] = c.Name
	}

	// 生成 Entity 和 DO 结构体及互转方法
	entityName := baseName + "Entity"
	doName := baseName + "DO"

	tableComment := "表，"
	if input.TableComment != "" {
		tableComment = input.TableComment + "，"
	}
	// 数据库名为空时只呈现表名，避免出现 .table 这样的残缺限定名
	qualifiedName := input.TableName
	if input.Database != "" {
		qualifiedName = input.Database + "." + input.TableName
	}

	entityComment := fmt.Sprintf("%s %s %sentity结构体，常用于数据库读取操作。", entityName, qualifiedName, tableComment)
	doComment := fmt.Sprintf("%s %s %sdo结构体，常用于数据库写入操作。", doName, qualifiedName, tableComment)

	entityStruct := buildStruct(entityName, columns, false, entityComment, input.ColumnTagName, input.Indexes)
	doStruct := buildStruct(doName, columns, true, doComment, input.ColumnTagName, input.Indexes)
	toDOMethod := buildToDOMethod(entityName, doName, columns)
	toEntityMethod := buildToEntityMethod(entityName, doName, columns)

	entityCode := entityStruct + "\n\n" + toDOMethod
	doCode := doStruct + "\n\n" + toEntityMethod

	if input.OutputDir == "" {
		return errors.New("输出目录为空")
	}
	if err := os.MkdirAll(input.OutputDir, 0755); err != nil {
		return fmt.Errorf("创建输出目录失败: %v", err)
	}

	filePath := filepath.Join(input.OutputDir, fileName+".go")
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
// 不得为空、不得为 "." / ".."、不得含路径分隔符（/、\）与 Windows 非法文件名字符（<>:"|?*），
// 且首字符须为 ASCII 字母或下划线——表名将推导结构体名，非 ASCII（如中文）或数字开头
// 会推导出无法编译的标识符，前置报错给出明确根因。
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
	// 首字符按 rune 取，多字节字符（如中文）整体落入非法分支，不会被按字节截断漏检
	first := []rune(name)[0]
	if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z') || first == '_') {
		return fmt.Errorf("表名首字符必须为 ASCII 字母或下划线: %s", name)
	}
	return nil
}

// resolveFileName 确定输出文件名（不含 .go）：name 为空时取表名，否则使用调用方显式指定的 name。
// 显式指定的文件名同样拒绝 "." / ".."、路径分隔符、Windows 非法文件名字符与多余的 .go 扩展名；
// 无论来源，文件名触发 Go 文件名构建规则（见 goFileNameConflict）时报错，
// 来源为表名时提示通过 Input.FileName 改名。
func resolveFileName(name, tableName string) (string, error) {
	if name == "" {
		if reason := goFileNameConflict(tableName); reason != "" {
			return "", fmt.Errorf("表名 %s 用作文件名 %s.go 会触发 Go 文件名规则（%s），请通过 Input.FileName 指定文件名", tableName, tableName, reason)
		}
		return tableName, nil
	}
	if name == "." || name == ".." {
		return "", fmt.Errorf("文件名非法: %s", name)
	}
	if strings.ContainsAny(name, `/\<>:"|?*`) {
		return "", fmt.Errorf("文件名包含非法字符: %s", name)
	}
	if strings.HasSuffix(name, ".go") {
		return "", fmt.Errorf("文件名不应包含 .go 扩展名: %s", name)
	}
	if reason := goFileNameConflict(name); reason != "" {
		return "", fmt.Errorf("文件名 %s.go 会触发 Go 文件名规则（%s）", name, reason)
	}
	return name, nil
}

// goFileNameConflict 返回 {name}.go 触发的 Go 文件名规则说明，不触发时返回空串：
// 以 _ 或 . 开头被 go 工具忽略；_test 后缀被识别为测试文件；_GOOS / _GOARCH / _GOOS_GOARCH 后缀
// 被识别为平台构建约束。平台约束借助 go/build 判定：GOOS/GOARCH 置空的 Context 不匹配任何平台，
// 带平台后缀的文件名在读取文件内容之前即被排除，因此无需维护平台清单。
func goFileNameConflict(name string) string {
	if strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
		return "以 _ 或 . 开头的文件会被 go 工具忽略"
	}
	if strings.HasSuffix(name, "_test") {
		return "_test 后缀会使其被识别为测试文件"
	}
	ctxt := build.Context{
		OpenFile: func(string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("package p\n")), nil
		},
	}
	if ok, err := ctxt.MatchFile(".", name+".go"); err == nil && !ok {
		return "文件名后缀会被识别为 GOOS/GOARCH 平台构建约束"
	}
	return ""
}

// validateColumnTagName 校验列映射 tag 名可作为 reflect.StructTag 的 key：
// 不得含空格与控制字符、冒号、双引号、DEL，也不得含会截断 tag 字面量的反引号；空串表示不生成列 tag。
func validateColumnTagName(name string) error {
	for i := 0; i < len(name); i++ {
		b := name[i]
		if b <= ' ' || b == ':' || b == '"' || b == 0x7f || b == '`' {
			return fmt.Errorf("列映射 tag 名非法: %q", name)
		}
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
