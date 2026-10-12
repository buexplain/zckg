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

// quitDocExamples 直接读取正式文档，避免复制示例后与 Markdown 失去同步。
func quitDocExamples(t *testing.T) []string {
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
	if len(matches) != 4 {
		t.Fatalf("正式文档应包含 4 段 Go 示例，实际 %d", len(matches))
	}
	examples := make([]string, len(matches))
	for i, match := range matches {
		examples[i] = match[1]
	}
	return examples
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
	for name, content := range map[string]string{"go.mod": mod, "main.go": source} {
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

// TestDocExamples_Compile 从 Markdown 提取全部四段示例，补齐片段外壳后执行真实 Go 编译。
func TestDocExamples_Compile(t *testing.T) {
	examples := quitDocExamples(t)
	names := []string{"basic", "levels", "manual_shutdown", "http"}
	const prelude = "package main\nimport (\"log/slog\"; \"os\"; \"github.com/buexplain/zckg/zcquit\")\n"
	for i, name := range names {
		t.Run(name, func(t *testing.T) {
			source := examples[i]
			switch i {
			case 1:
				source = prelude + "func main() {\n" + source + "\n}\n"
			case 2:
				source = prelude + source + "\nfunc healthCheck() error { return nil }\n"
			}
			buildQuitDocExample(t, source)
		})
	}
}

// TestDocExamples_BasicWaitsForWorker 锁死真实示例的等待 handler、资源级别和启动门顺序，删除等待即失败。
func TestDocExamples_BasicWaitsForWorker(t *testing.T) {
	source, err := format.Source([]byte(quitDocExamples(t)[0]))
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
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("创建端口占用监听器: %v", err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Errorf("关闭端口占用监听器: %v", err)
		}
	})
	source := quitDocExamples(t)[3]
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
