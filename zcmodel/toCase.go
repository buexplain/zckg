package zcmodel

import (
	"bytes"
	"strings"
	"unicode"
)

// formatJSONTag 根据列名和命名风格生成 JSON tag 的值
// 支持的风格：lowerCamel、upperCamel、lowerSnake、upperSnake、lowerKebab、upperKebab
func formatJSONTag(colName string, jsonTagValueCase NameCase) string {
	words := splitWords(colName)
	switch jsonTagValueCase {
	case NameCaseLowerCamel:
		return toLowerCamel(words)
	case NameCaseUpperCamel:
		return toUpperCamel(words)
	case NameCaseLowerSnake:
		return toLowerSnake(words)
	case NameCaseUpperSnake:
		return toUpperSnake(words)
	case NameCaseLowerKebab:
		return toLowerKebab(words)
	case NameCaseUpperKebab:
		return toUpperKebab(words)
	default:
		return ""
	}
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

// lowerFirst 将名称首字符转为小写，用于由结构体名推导互转方法的参数名。
// 首字符按 []rune 处理，避免多字节字符（如中文）被按字节截断产生残缺字节。
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
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
