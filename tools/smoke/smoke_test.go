package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// fakeServer 是行为可定制的假服务，模拟真实 xianyu-server 的会话与列表接口。
type fakeServer struct {
	// mu 保护 initialized、loggedIn。
	mu sync.Mutex
	// initialized、loggedIn 分别表示是否已初始化管理员、当前 Cookie 是否已登录。
	initialized, loggedIn bool
	// password 是初始化时设置的密码。
	password string
	// healthBody 是 /health 的响应体。
	healthBody string
	// breakPath 非空时，该路径返回 500，用于验证失败定位。
	breakPath string
	// skipRootMarker 为 true 时前端首页缺少 React 挂载点。
	skipRootMarker bool
	// keepSessionAfterLogout 为 true 时登出后仍保持登录。
	keepSessionAfterLogout bool
	// acceptWrongPassword 为 true 时错误密码也能登录。
	acceptWrongPassword bool
	// openWithoutAuth 为 true 时列表接口不要求登录。
	openWithoutAuth bool
	// initFails 为 true 时初始化接口返回 500。
	initFails bool
	// preInitialized 为 true 时一开始就报告已初始化。
	preInitialized bool
}

// newFakeServer 创建遵循正常协议的假服务。
func newFakeServer() *fakeServer { return &fakeServer{healthBody: `{"status":"ok","database":"ok"}`} }

// handler 返回假服务的 HTTP 处理器。
func (f *fakeServer) handler() http.Handler {
	// mux 注册冒烟会访问的全部路径。
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(f.healthBody)) })
	mux.HandleFunc("/api/v1/session", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		// username 仅在已登录时返回。
		username := ""
		if f.loggedIn {
			username = "admin"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"authenticated": f.loggedIn, "initialized": f.initialized || f.preInitialized, "username": username})
	})
	mux.HandleFunc("/api/v1/session/initialize", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		// body 是请求中的初始化密码。
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if f.initFails {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		f.initialized, f.loggedIn, f.password = true, true, body["password"]
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/api/v1/session/login", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		// body 是请求中的登录凭据。
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["password"] != f.password && !f.acceptWrongPassword {
			http.Error(w, `{"error":"bad"}`, http.StatusUnauthorized)
			return
		}
		f.loggedIn = true
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/api/v1/session/logout", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if !f.keepSessionAfterLogout {
			f.loggedIn = false
		}
		_, _ = w.Write([]byte(`{}`))
	})
	// path 是需要登录的核心列表接口。
	for _, path := range listEndpoints {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if !f.loggedIn && !f.openWithoutAuth {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			if r.URL.Path == f.breakPath {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte(`[]`))
		})
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		if f.skipRootMarker {
			_, _ = w.Write([]byte(`<html></html>`))
			return
		}
		_, _ = w.Write([]byte(`<div id="root"></div>`))
	})
	return mux
}

// TestRunChecksHappyPath 验证正常服务通过全部检查。
func TestRunChecksHappyPath(t *testing.T) {
	// server 是遵循协议的假服务。
	server := httptest.NewServer(newFakeServer().handler())
	defer server.Close()
	// err 保存对正常假服务执行全部检查的结果，期望为空。
	if err := runChecks(context.Background(), server.URL, "pw-123456"); err != nil {
		t.Fatalf("正常服务应通过: %v", err)
	}
}

// TestRunChecksDetectsRegressions 验证每类服务端回归都会让冒烟失败并指明步骤。
func TestRunChecksDetectsRegressions(t *testing.T) {
	// cases 是被破坏的行为及期望出现在错误中的步骤名。
	cases := []struct {
		name   string
		mutate func(*fakeServer)
		want   string
	}{
		{"健康检查未声明ok", func(f *fakeServer) { f.healthBody = `{"status":"degraded"}` }, "健康检查"},
		{"全新库已初始化", func(f *fakeServer) { f.preInitialized = true }, "初始化前会话"},
		{"列表接口500", func(f *fakeServer) { f.breakPath = "/api/v1/orders" }, "核心列表接口"},
		{"登出后仍登录", func(f *fakeServer) { f.keepSessionAfterLogout = true }, "登出后会话"},
		{"错误密码可登录", func(f *fakeServer) { f.acceptWrongPassword = true }, "错误密码登录"},
		{"未登录可访问受保护接口", func(f *fakeServer) { f.openWithoutAuth = true }, "未登录访问受保护接口"},
		{"初始化失败", func(f *fakeServer) { f.initFails = true }, "首次初始化管理员"},
		{"首页缺少挂载点", func(f *fakeServer) { f.skipRootMarker = true }, "前端首页"},
	}
	// tc 是当前破坏场景。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// fake 是按场景破坏后的假服务。
			fake := newFakeServer()
			tc.mutate(fake)
			// server 是按场景破坏后的假服务的 HTTP 测试服务器。
			server := httptest.NewServer(fake.handler())
			defer server.Close()
			// err 应指明失败步骤。
			err := runChecks(context.Background(), server.URL, "pw-123456")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("期望包含 %q，实际 %v", tc.want, err)
			}
		})
	}
}

// TestRunChecksUnreachable 验证服务不可达时返回带步骤的错误。
func TestRunChecksUnreachable(t *testing.T) {
	// server 创建后立即关闭，得到一个不可达地址。
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()
	// err 保存对不可达服务执行检查的结果，期望指明健康检查步骤。
	if err := runChecks(context.Background(), server.URL, "pw"); err == nil || !strings.Contains(err.Error(), "健康检查") {
		t.Fatalf("实际 %v", err)
	}
}

// TestExpectStatusInvalidJSONSession 验证会话响应不是 JSON 时给出明确错误。
func TestExpectStatusInvalidJSONSession(t *testing.T) {
	// server 的会话接口返回非 JSON。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("not json")) }))
	defer server.Close()
	// err 保存对非 JSON 会话响应的解析结果，期望给出明确错误。
	if _, err := newSmokeClient(server.URL).session(context.Background(), "步骤"); err == nil || !strings.Contains(err.Error(), "合法 JSON") {
		t.Fatalf("实际 %v", err)
	}
}

// TestSnippetTruncates 验证长响应体被截断且短响应体原样返回。
func TestSnippetTruncates(t *testing.T) {
	// got 是短响应体经 snippet 处理后的结果，期望原样返回。
	if got := snippet([]byte("abc")); got != "abc" {
		t.Fatalf("got %q", got)
	}
	// got 是超长响应体经 snippet 处理后的结果，期望被截断并带省略号。
	if got := snippet([]byte(strings.Repeat("x", 300))); !strings.HasSuffix(got, "…") || len([]rune(got)) != 201 {
		t.Fatalf("got len %d", len([]rune(got)))
	}
}

// TestWaitHealthy 覆盖就绪、超时、取消和进程提前退出四个分支。
func TestWaitHealthy(t *testing.T) {
	// ready 是前两次返回 503、之后返回 200 的服务。
	var hits int
	// ready 是前两次返回 503、之后返回 200 的假服务。
	ready := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer ready.Close()
	// err 保存等待最终就绪的结果，期望为空。
	if err := waitHealthy(context.Background(), ready.URL, 10*time.Second, nil); err != nil {
		t.Fatalf("应最终就绪: %v", err)
	}
	// never 永远返回 503。
	never := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer never.Close()
	// err 保存永远未就绪服务的等待结果，期望超时错误。
	if err := waitHealthy(context.Background(), never.URL, 300*time.Millisecond, nil); err == nil || !strings.Contains(err.Error(), "未通过") {
		t.Fatalf("超时实际 %v", err)
	}
	// cancelled 是已取消的上下文。
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	// err 保存上下文已取消时的等待结果，期望为 context.Canceled。
	if err := waitHealthy(cancelled, never.URL, 10*time.Second, nil); err != context.Canceled {
		t.Fatalf("取消实际 %v", err)
	}
	// exited 是已关闭的退出通道，模拟服务提前退出。
	exited := make(chan struct{})
	close(exited)
	// err 保存服务提前退出时的等待结果，期望指明就绪前退出。
	if err := waitHealthy(context.Background(), never.URL, 10*time.Second, exited); err == nil || !strings.Contains(err.Error(), "就绪前退出") {
		t.Fatalf("提前退出实际 %v", err)
	}
}

// TestFreeLocalAddr 验证返回本地回环地址。
func TestFreeLocalAddr(t *testing.T) {
	// addr 是分配到的地址。
	addr, err := freeLocalAddr()
	if err != nil || !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("addr=%q err=%v", addr, err)
	}
}

// TestLockedBufferConcurrent 验证缓冲在并发写入下完整保留内容。
func TestLockedBufferConcurrent(t *testing.T) {
	// buf 是被测缓冲；wg 等待所有写入协程。
	buf := &lockedBuffer{}
	// wg 等待所有并发写入协程结束。
	var wg sync.WaitGroup
	// i 是并发写入协程的序号，仅用于控制协程数量。
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = buf.Write([]byte("ab")) }()
	}
	wg.Wait()
	if buf.String() != strings.Repeat("ab", 10) {
		t.Fatalf("got %q", buf.String())
	}
}

// TestIsolatedEnvRemovesSensitiveVariables 验证真实数据库与密钥变量被清除且其他变量保留。
func TestIsolatedEnvRemovesSensitiveVariables(t *testing.T) {
	// got 是过滤后的环境。
	got := isolatedEnv([]string{"DATABASE_URL=mysql://x", "XIANYU_DATA_KEY=k", "XIANYU_ADMIN_PASSWORD=p", "PATH=/bin", "DATABASE_URL_OTHER=keep"})
	// joined 是便于断言的拼接文本。
	joined := strings.Join(got, "\n")
	// bad 是不应出现在隔离环境中的敏感变量前缀。
	for _, bad := range []string{"DATABASE_URL=", "XIANYU_DATA_KEY=", "XIANYU_ADMIN_PASSWORD="} {
		if strings.Contains(joined, bad) {
			t.Fatalf("不应保留 %s", bad)
		}
	}
	if !strings.Contains(joined, "PATH=/bin") || !strings.Contains(joined, "DATABASE_URL_OTHER=keep") || !strings.Contains(joined, "LOG_LEVEL=info") {
		t.Fatalf("环境不完整: %v", got)
	}
}

// TestMain 在 GO_SMOKE_HELPER 非空时把测试二进制当作模拟服务进程运行，避免 -addr 等参数被测试框架拒绝。
func TestMain(m *testing.M) {
	// mode 为空表示正常测试运行。
	if mode := os.Getenv("GO_SMOKE_HELPER"); mode != "" {
		helperMain(mode)
	}
	os.Exit(m.Run())
}

// helperMain 按 mode 模拟服务进程行为，并总以 os.Exit 结束。
func helperMain(mode string) {
	switch mode {
	case "exit3":
		os.Exit(3)
	case "graceful":
		// sig 接收终止信号后以 0 退出。
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM)
		println("ready")
		<-sig
		os.Exit(0)
	case "stubborn":
		signal.Ignore(syscall.SIGTERM)
		time.Sleep(time.Minute)
	case "serve":
		// 以假服务监听 -addr 指定地址，收到 SIGTERM 后优雅退出，用于覆盖 run 的成功路径。
		var addr string
		// i 是命令行参数下标。
		for i, arg := range os.Args {
			if arg == "-addr" && i+1 < len(os.Args) {
				addr = os.Args[i+1]
			}
		}
		// server 是监听 addr 的假服务。
		server := &http.Server{Addr: addr, Handler: newFakeServer().handler()}
		go func() { _ = server.ListenAndServe() }()
		// sig 接收终止信号；缓冲为 1 以免信号在等待前丢失。
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM)
		<-sig
		_ = server.Close()
		os.Exit(0)
	case "badexit":
		// 收到终止信号后以非 0 退出，模拟停止流程异常。
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGTERM)
		<-sig
		os.Exit(5)
	}
	os.Exit(0)
}

// startHelper 以指定模式启动辅助子进程。
func startHelper(t *testing.T, mode string) *serverProcess {
	t.Helper()
	t.Setenv("GO_SMOKE_HELPER", mode)
	// proc 是辅助子进程封装。
	proc, err := newServerProcess(context.Background(), os.Args[0], nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return proc
}

// TestServerProcessStopGraceful 验证优雅退出成功，且重复 stop 在进程已退出后仍返回成功。
func TestServerProcessStopGraceful(t *testing.T) {
	// proc 是会响应 SIGTERM 的子进程。
	proc := startHelper(t, "graceful")
	time.Sleep(300 * time.Millisecond)
	// err 保存优雅退出的停止结果，期望为空。
	if err := proc.stop(10 * time.Second); err != nil {
		t.Fatalf("优雅退出应成功: %v", err)
	}
	// err 保存进程已退出后重复停止的结果，期望为空。
	if err := proc.stop(time.Second); err != nil {
		t.Fatalf("重复停止应成功: %v", err)
	}
}

// TestServerProcessStopTimeoutKills 验证忽略信号的进程超时后被强制结束并报错。
func TestServerProcessStopTimeoutKills(t *testing.T) {
	// proc 是忽略 SIGTERM 的子进程。
	proc := startHelper(t, "stubborn")
	time.Sleep(300 * time.Millisecond)
	// err 保存忽略信号进程的停止结果，期望超时强杀错误。
	if err := proc.stop(500 * time.Millisecond); err == nil || !strings.Contains(err.Error(), "未能优雅退出") {
		t.Fatalf("实际 %v", err)
	}
}

// TestServerProcessStopNonZeroExit 验证退出码非 0 被识别为停止异常。
func TestServerProcessStopNonZeroExit(t *testing.T) {
	// proc 是收到信号后以 5 退出的子进程。
	proc := startHelper(t, "badexit")
	time.Sleep(300 * time.Millisecond)
	// err 保存非零退出码进程的停止结果，期望包含退出码。
	if err := proc.stop(10 * time.Second); err == nil || !strings.Contains(err.Error(), "退出码 5") {
		t.Fatalf("实际 %v", err)
	}
}

// TestRunFailsWhenServerExitsEarlyAndMissingBinary 验证服务启动即退出与缺少参数、二进制不存在的错误路径。
func TestRunFailsWhenServerExitsEarlyAndMissingBinary(t *testing.T) {
	// err 保存缺少 -bin 参数时的运行结果，期望提示缺少参数。
	if err := run(context.Background(), config{}); err == nil || !strings.Contains(err.Error(), "-bin") {
		t.Fatalf("缺少参数实际 %v", err)
	}
	// err 保存二进制不存在时的运行结果，期望提示启动服务失败。
	if err := run(context.Background(), config{binary: "/nonexistent/xianyu-server", startTimeout: time.Second, stopTimeout: time.Second}); err == nil || !strings.Contains(err.Error(), "启动服务") {
		t.Fatalf("不存在的二进制实际 %v", err)
	}
	// 以会立即退出的辅助进程作为“服务”，应报告就绪前退出。
	t.Setenv("GO_SMOKE_HELPER", "exit3")
	// err 是启动即退出的服务产生的错误。
	err := run(context.Background(), config{binary: os.Args[0], startTimeout: 10 * time.Second, stopTimeout: time.Second})
	if err == nil || !strings.Contains(err.Error(), "等待服务就绪") {
		t.Fatalf("实际 %v", err)
	}
}

// TestStartServerArguments 验证 -no-browser 只在显式要求时附加，并始终使用临时库与独立密钥文件。
func TestStartServerArguments(t *testing.T) {
	t.Setenv("GO_SMOKE_HELPER", "exit3")
	// noBrowser 表示该轮是否显式要求关闭浏览器自动化。
	for _, noBrowser := range []bool{false, true} {
		// proc 是按配置启动的辅助进程。
		proc, err := startServer(context.Background(), config{binary: os.Args[0], noBrowser: noBrowser}, t.TempDir(), "127.0.0.1:1")
		if err != nil {
			t.Fatal(err)
		}
		// args 是实际传给子进程的参数。
		args := strings.Join(proc.cmd.Args, " ")
		<-proc.exited
		if strings.Contains(args, "-no-browser") != noBrowser || !strings.Contains(args, "-data-key-file") || !strings.Contains(args, "smoke.db") {
			t.Fatalf("noBrowser=%v 参数 %s", noBrowser, args)
		}
	}
}

// TestRunSuccessAgainstHelperServer 验证 run 的完整成功路径：启动、就绪、全部检查、优雅退出与临时目录清理。
func TestRunSuccessAgainstHelperServer(t *testing.T) {
	t.Setenv("GO_SMOKE_HELPER", "serve")
	// err 保存完整冒烟流程的运行结果，期望为空。
	if err := run(context.Background(), config{binary: os.Args[0], startTimeout: 20 * time.Second, stopTimeout: 10 * time.Second}); err != nil {
		t.Fatalf("完整流程应通过: %v", err)
	}
}
