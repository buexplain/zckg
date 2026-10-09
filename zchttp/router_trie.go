package zchttp

import (
	"fmt"
	"strings"
)

// routeNode 是基数树的节点，按 path 段逐层下探：
//   - static 子节点对应静态字面量段，匹配时优先于参数段
//   - param 子节点对应 {name}/{name?} 参数段，同一节点同一位置仅允许一个参数名
//   - catchAll 子节点对应 {name...} 通配尾段，优先级最低；它必为末段，只用 paramName/entries[0]/regEntry
//   - entries[0] 为按完整段数命中时的 entry；entries[1] 为尾部可选参数被省略时的 entry
type routeNode struct {
	static    map[string]*routeNode
	param     *routeNode
	catchAll  *routeNode
	paramName string         // param/catchAll 子节点的参数名（用于冲突检测）
	optional  bool           // param 子节点是否为可选参数 {name?}
	entries   [2]*routeEntry // [0]=完整命中, [1]=可选参数省略
	regEntry  *routeEntry    // 首个经过本节点的 entry，用于中间节点冲突时的位置提示
}

// cloneRouteNode 深拷贝路由树节点结构，entry 只读共享；用于 Any 注册的无副作用预检。
func cloneRouteNode(node *routeNode) *routeNode {
	if node == nil {
		return nil
	}
	cloned := &routeNode{
		static:    make(map[string]*routeNode, len(node.static)),
		paramName: node.paramName,
		optional:  node.optional,
		entries:   node.entries,
		regEntry:  node.regEntry,
	}
	for segment, child := range node.static {
		cloned.static[segment] = cloneRouteNode(child)
	}
	cloned.param = cloneRouteNode(node.param)
	cloned.catchAll = cloneRouteNode(node.catchAll)
	return cloned
}

// insertRoute 将一条路由按段插入基数树（静态段、参数段与通配尾段同树）。
// 遇可选参数段时，省略分支登记在 param 节点的 entries[1]，
// 同时继续下探以便后续段命中 entries[0]；通配尾段登记在独立的 catchAll 槽，
// 与同节点的 static/param 槽共存。
// 各类冲突（终点重复、参数名或通配名不一致、可选性不一致）立即 panic，消息包含冲突双方 handler 位置。
func insertRoute(root *routeNode, segments []routeSegment, entry *routeEntry, method, path string) {
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
		if seg.catchAll {
			if node.catchAll == nil {
				node.catchAll = &routeNode{paramName: seg.name}
			} else if node.catchAll.paramName != seg.name {
				routeConflictPanic(method, path, node.catchAll.regEntry, entry,
					fmt.Sprintf("catch-all name conflict at same position: {%s...} vs {%s...}", node.catchAll.paramName, seg.name))
			}
			if node.catchAll.regEntry == nil {
				node.catchAll.regEntry = entry
			}
			node = node.catchAll
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

// routeConflictPanic 以与静态路由冲突一致的格式 panic；
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

// matchPath 在基数树上逐段扫描匹配请求路径，同节点按 静态段 > 参数段 > 通配尾段 依次尝试，
// 前者分支失败时回溯。路径耗尽时按 终点 entry > 可选参数省略分支（param 子节点 optional 且
// entries[1] 非空）> 通配零段命中 的顺序兜底；通配零段命中不追加捕获值。
// 静态与参数分支均失败时，通配槽捕获本层进入时的剩余路径（去掉前导 "/"，可含多段），
// 不能用切出首段后的剩余部分，否则丢失通配起点后的第一段。
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
		if n.catchAll != nil {
			return n.catchAll.entries[0], captured
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
	if n.catchAll != nil {
		// 参数分支的 append 可能已写入共享底层数组的后续槽位，但未修改 captured 前缀，此处覆盖同一槽位无害
		return n.catchAll.entries[0], append(captured, path[1:])
	}
	return nil, nil
}
