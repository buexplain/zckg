package zcquit_test

import (
	"context"
	"fmt"
	"go/format"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// docExample 表示文档中的一个完整 Go 示例程序。
// 文档约定：go 块必须是可直接编译运行的完整程序；伪代码与反例使用 text 块。
type docExample struct {
	name   string // 首行「// 示例 N：...」注释，供失败信息定位
	source string
}

// quitDocExamples 直接读取正式文档，按出现顺序返回全部 go 代码块，
// 避免把示例复制进测试后与 Markdown 失去同步。
func quitDocExamples(t *testing.T) []docExample {
	t.Helper()
	path, err := filepath.Abs("docs/zcquit.md")
	if err != nil {
		t.Fatalf("解析文档路径: %v", err)
	}
	doc, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s: %v", path, err)
	}
	matches := regexp.MustCompile("(?ms)^```go\n(.*?)^```[ \t]*$").FindAllStringSubmatch(strings.ReplaceAll(string(doc), "\r\n", "\n"), -1)
	if len(matches) < 3 {
		t.Fatalf("正式文档应至少包含 3 段完整 Go 示例，实际 %d", len(matches))
	}
	examples := make([]docExample, len(matches))
	for i, match := range matches {
		examples[i] = docExample{name: docExampleName(match[1], i), source: match[1]}
	}
	return examples
}

// docExampleName 取示例首行注释，缺失时回退为序号。
func docExampleName(source string, index int) string {
	for _, line := range strings.Split(source, "\n") {
		if strings.HasPrefix(line, "// 示例 ") {
			return line
		}
		if strings.HasPrefix(line, "package ") {
			break
		}
	}
	return fmt.Sprintf("第 %d 段示例", index+1)
}

// findDocExample 按内容特征定位唯一示例；数量不符即失败，防止示例被误删后测试空转。
func findDocExample(t *testing.T, examples []docExample, want string, match func(docExample) bool) docExample {
	t.Helper()
	var found []docExample
	for _, example := range examples {
		if match(example) {
			found = append(found, example)
		}
	}
	if len(found) != 1 {
		t.Fatalf("文档中应有唯一的 %s 示例，实际匹配 %d 段", want, len(found))
	}
	return found[0]
}

// buildQuitDocExample 在独立临时 module 内编译真实示例，产物仅用于本次测试。
func buildQuitDocExample(t *testing.T, source string) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("解析仓库路径: %v", err)
	}
	dir := t.TempDir()
	mod := fmt.Sprintf("module example.test/zcquitdocs\n\ngo 1.25.11\n\nrequire github.com/buexplain/zckg v0.0.0\n\nreplace github.com/buexplain/zckg => %q\n", filepath.ToSlash(root))
	// 文档示例省略 package 声明，便于直接粘贴进现有 main.go；校验时补齐为完整程序。
	for name, content := range map[string]string{"go.mod": mod, "main.go": "package main\n\n" + source} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatalf("写入 %s: %v", name, err)
		}
	}
	binary := filepath.Join(dir, "example.exe")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", binary, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	cmd.WaitDelay = time.Second
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("文档示例编译失败: %v (context=%v)\n%.4096s", err, ctx.Err(), output)
	}
	return binary
}

// TestDocExamples_CompletePrograms 锁死文档约定：每个 go 块都是省略 package 声明的完整示例，
// 必须自带 import 与 func main，且不得再声明 package（由 buildQuitDocExample 补齐）。
func TestDocExamples_CompletePrograms(t *testing.T) {
	for i, example := range quitDocExamples(t) {
		t.Run(fmt.Sprintf("example-%d", i+1), func(t *testing.T) {
			if !strings.Contains(example.name, "示例") {
				t.Fatalf("示例缺少「// 示例 N：」首行注释，名称: %q", example.name)
			}
			if !strings.Contains(example.source, "\nimport (\n") {
				t.Fatalf("示例必须包含 import 块，实际:\n%.4096s", example.source)
			}
			if !strings.Contains(example.source, "\nfunc main() {") {
				t.Fatalf("示例必须包含 func main()，实际:\n%.4096s", example.source)
			}
			for _, line := range strings.Split(example.source, "\n") {
				if strings.HasPrefix(line, "package ") {
					t.Fatalf("示例不应声明 package（校验时统一补齐），实际行: %q", line)
				}
			}
		})
	}
}

// TestDocExamples_Compile 编译文档中的全部 Go 示例，每个示例均为独立完整程序。
func TestDocExamples_Compile(t *testing.T) {
	for i, example := range quitDocExamples(t) {
		t.Run(fmt.Sprintf("example-%d", i+1), func(t *testing.T) {
			buildQuitDocExample(t, example.source)
		})
	}
}

// TestDocExamples_BasicWaitsForWorker 锁死基本示例的等待 handler、资源级别和启动门顺序，删除等待即失败。
func TestDocExamples_BasicWaitsForWorker(t *testing.T) {
	example := findDocExample(t, quitDocExamples(t), "基本用法", func(ex docExample) bool {
		return strings.Contains(ex.source, "<-workerDone") && strings.Contains(ex.source, "zcquit.AddSigHandler(0")
	})
	source, err := format.Source([]byte("package main\n\n" + example.source))
	if err != nil {
		t.Fatalf("格式化基本示例: %v", err)
	}
	got := string(source)
	// 精确匹配完整注册调用，不能由注释中提及 workerDone 或 main 中独立等待冒充。
	waitHandler := "zcquit.AddSigHandler(0, func(sig os.Signal) {\n\t\t<-workerDone\n\t\tslog.Info(\"业务协程已结束\", \"signal\", sig)\n\t})"
	previous := -1
	for _, want := range []string{
		"workerDone := make(chan struct{})",
		"go runServer(workerStart, workerDone)",
		waitHandler,
		"zcquit.AddSigHandler(1, func(sig os.Signal) {",
		"close(workerStart)",
		"zcquit.Listen()",
	} {
		position := strings.Index(got, want)
		if strings.Count(got, want) != 1 || position <= previous {
			t.Fatalf("基本示例缺少唯一且顺序正确的 %q\n%.4096s", want, got)
		}
		previous = position
	}
	for _, want := range []string{
		"func runServer(start <-chan struct{}, done chan<- struct{}) {\n\tdefer close(done)",
		"select {\n\tcase <-zcquit.GetCtx().Done():\n\t\treturn // 尚未放行就退出，无业务需要收尾\n\tcase <-start:\n\t}",
	} {
		if strings.Count(got, want) != 1 {
			t.Fatalf("worker 缺少完成通知或启动前取消分支 %q\n%.4096s", want, got)
		}
	}
}

// TestDocExamples_HTTPPortConflict 用随机本机占用端口运行真实示例，验证启动失败后全部级别恰好清理一次并正常返回。
func TestDocExamples_HTTPPortConflict(t *testing.T) {
	example := findDocExample(t, quitDocExamples(t), "HTTP 多级清理", func(ex docExample) bool {
		return strings.Contains(ex.source, `"net/http"`) && strings.Contains(ex.source, `Addr: ":8080"`)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("创建端口占用监听器: %v", err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Errorf("关闭端口占用监听器: %v", err)
		}
	})
	source := example.source
	// 注册必须先于服务 goroutine 启动，不能靠调度碰巧晚执行错误分支。
	start := strings.Index(source, "go func() {")
	for _, level := range []int{0, 1, 2} {
		registration := fmt.Sprintf("zcquit.AddSigHandler(%d,", level)
		if strings.Count(source, registration) != 1 || strings.Index(source, registration) >= start {
			t.Fatalf("HTTP 启动前应完成唯一的注册 %q\n%.4096s", registration, source)
		}
	}
	const address = `Addr: ":8080"`
	if strings.Count(source, address) != 1 {
		t.Fatalf("HTTP 示例应有唯一的地址配置 %q\n%.4096s", address, source)
	}
	// 仅替换运行地址，不改错误分支或清理逻辑，也不占用固定 8080。
	source = strings.Replace(source, address, "Addr: "+strconv.Quote(listener.Addr().String()), 1)
	binary := buildQuitDocExample(t, source)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary)
	cmd.Dir = filepath.Dir(binary)
	cmd.WaitDelay = time.Second
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("HTTP 启动失败后示例未正常返回: %v (context=%v)\n%.4096s", err, ctx.Err(), output)
	}
	if want := "listen tcp " + listener.Addr().String() + ": bind:"; !strings.Contains(string(output), want) {
		t.Fatalf("未观察到占用地址的 bind 错误 %q\n%.4096s", want, output)
	}
	messages := []string{
		"HTTP 服务器异常退出",
		"通知网关摘除本节点...",
		"正在关闭 HTTP 服务器...",
		"正在关闭数据库连接...",
		"正在关闭 Redis 连接...",
		"程序已退出",
	}
	positions := make([]int, len(messages))
	for i, message := range messages {
		// 示例沿用默认 slog/log 输出；按行匹配完整消息，允许后接结构化属性。
		pattern := regexp.MustCompile(`(?m)^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} (?:INFO|ERROR) ` + regexp.QuoteMeta(message) + `(?: [a-zA-Z_][a-zA-Z0-9_]*=[^\r\n]*)?\r?$`)
		matches := pattern.FindAllIndex(output, -1)
		if len(matches) != 1 {
			t.Fatalf("日志 %q 应恰好出现一次，实际 %d\n%.4096s", message, len(matches), output)
		}
		positions[i] = matches[0][0]
	}
	// level 2 的两个 handler 可任意排序，但均须晚于 level 1、早于 main 返回。
	for _, order := range [][]int{{0, 1, 2, 3, 5}, {0, 1, 2, 4, 5}} {
		for i := 1; i < len(order); i++ {
			if positions[order[i-1]] >= positions[order[i]] {
				t.Fatalf("清理日志顺序错误: %q 应先于 %q\n%.4096s", messages[order[i-1]], messages[order[i]], output)
			}
		}
	}
}
