package zchttp

import (
	"fmt"
	"strings"
)

// routeNode 是参数路由基数树的节点，按 path 段逐层下探：
//   - static 子节点对应静态字面量段，匹配时优先于参数段
//   - param 子节点对应 {name}/{name?} 参数段，同一节点同一位置仅允许一个参数名
//   - entries[0] 为按完整段数命中时的 entry；entries[1] 为尾部可选参数被省略时的 entry
type routeNode struct {
	static    map[string]*routeNode
	param     *routeNode
	paramName string         // param 子节点的参数名（用于冲突检测）
	optional  bool           // param 子节点是否为可选参数 {name?}
	entries   [2]*routeEntry // [0]=参数命中, [1]=可选参数省略
	regEntry  *routeEntry    // 首个经过本节点的 entry，用于中间节点冲突时的位置提示
}

// insertParamRoute 将一条参数路由按段插入基数树。
// 遇可选参数段时，省略分支登记在 param 节点的 entries[1]，
// 同时继续下探以便后续段命中 entries[0]。
// 各类冲突（终点重复、参数名不一致、可选性不一致）立即 panic，消息包含冲突双方 handler 位置。
func insertParamRoute(root *routeNode, segments []routeSegment, entry *routeEntry, method, path string) {
	node := root
	for _, seg := range segments {
		if !seg.isParam {
			child := node.static[seg.literal]
			if child == nil {
				child = &routeNode{static: make(map[string]*routeNode)}
				node.static[seg.literal] = child
			}
			node = child
			continue
		}
		if node.param == nil {
			node.param = &routeNode{static: make(map[string]*routeNode), paramName: seg.name, optional: seg.optional}
		} else if node.param.paramName != seg.name {
			routeConflictPanic(method, path, node.param.regEntry, entry,
				fmt.Sprintf("parameter name conflict at same position: {%s} vs {%s}", node.param.paramName, seg.name))
		} else if node.param.optional != seg.optional {
			routeConflictPanic(method, path, node.param.regEntry, entry,
				fmt.Sprintf("parameter {%s} optionality conflict: required vs optional", seg.name))
		}
		if seg.optional {
			if node.param.entries[1] != nil {
				routeConflictPanic(method, path, node.param.entries[1], entry, "")
			}
			node.param.entries[1] = entry
		}
		if node.param.regEntry == nil {
			node.param.regEntry = entry
		}
		node = node.param
	}
	if node.entries[0] != nil {
		routeConflictPanic(method, path, node.entries[0], entry, "")
	}
	node.entries[0] = entry
	if node.regEntry == nil {
		node.regEntry = entry
	}
}

// routeConflictPanic 以与精确路由冲突一致的格式 panic；
// existing 可能为 nil（中间节点冲突且该节点尚无终点 entry），此时仅提示冲突原因与新路由位置
func routeConflictPanic(method, path string, existing, incoming *routeEntry, reason string) {
	if reason == "" {
		reason = "route conflict"
	}
	if existing == nil {
		panic(fmt.Sprintf(
			"%s: %s %s registered by %s (%s:%d)",
			reason, method, path,
			incoming.handlerName, incoming.handlerFile, incoming.handlerLine,
		))
	}
	panic(fmt.Sprintf(
		"%s: %s %s already registered by %s (%s:%d), conflicting with %s (%s:%d)",
		reason, method, path,
		existing.handlerName, existing.handlerFile, existing.handlerLine,
		incoming.handlerName, incoming.handlerFile, incoming.handlerLine,
	))
}

// matchPath 在基数树上逐段扫描匹配请求路径，静态段优先于参数段，
// 静态分支失败时回溯尝试参数分支。路径耗尽时若本节点无终点 entry，
// 再尝试尾部可选参数省略分支（param 子节点 optional 且 entries[1] 非空）。
// 相比预先 strings.Split 整个路径，本实现直接以子串切片递进，
// 匹配过程除捕获参数外不产生分配（热路径优化）。
// path 为归一化路径（"/" 开头、无末尾 "/"），根路径应传空串；
// captured 累积按注册顺序捕获的参数值；被省略的尾部可选参数不追加。
func (n *routeNode) matchPath(path string, captured []string) (*routeEntry, []string) {
	if path == "" {
		if n.entries[0] != nil {
			return n.entries[0], captured
		}
		if n.param != nil && n.param.optional && n.param.entries[1] != nil {
			return n.param.entries[1], captured
		}
		return nil, nil
	}
	// path 以 "/" 开头：取首尾两个 "/" 之间（或至路径末尾）的一段，
	// 剩余部分仍保持 "/" 开头形态递进，无需切分出完整段切片
	rest := path[1:]
	var seg string
	if idx := strings.IndexByte(rest, '/'); idx == -1 {
		seg, rest = rest, ""
	} else {
		seg, rest = rest[:idx], rest[idx:]
	}
	if child, ok := n.static[seg]; ok {
		if e, c := child.matchPath(rest, captured); e != nil {
			return e, c
		}
	}
	if n.param != nil {
		if e, c := n.param.matchPath(rest, append(captured, seg)); e != nil {
			return e, c
		}
	}
	return nil, nil
}
