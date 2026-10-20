package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// scriptedHTTPRuntime 是按脚本返回各阶段结果的 HTTP 服务替身，同时记录阶段调用顺序。
type scriptedHTTPRuntime struct {
	// bindErr、startErr、waitErr、stopErr 分别是 Bind、Start、Wait、Stop 阶段的预置结果。
	bindErr, startErr, waitErr, stopErr error
	// calls 按发生顺序记录被调用的阶段名称。
	calls []string
}

// Bind 记录绑定阶段并返回预置错误。
func (runtime *scriptedHTTPRuntime) Bind() error {
	runtime.calls = append(runtime.calls, "bind")
	return runtime.bindErr
}

// Start 记录监听启动阶段并返回预置错误。
func (runtime *scriptedHTTPRuntime) Start(context.Context) error {
	runtime.calls = append(runtime.calls, "start")
	return runtime.startErr
}

// Wait 记录等待阶段并返回预置错误。
func (runtime *scriptedHTTPRuntime) Wait() error {
	runtime.calls = append(runtime.calls, "wait")
	return runtime.waitErr
}

// Stop 记录关闭阶段并返回预置错误。
func (runtime *scriptedHTTPRuntime) Stop(context.Context) error {
	runtime.calls = append(runtime.calls, "stop")
	return runtime.stopErr
}

// scriptedCoordinator 是按脚本返回启动与关闭结果的应用生命周期替身。
type scriptedCoordinator struct {
	// startErr、closeErr 分别是 Start 与 Close 的预置结果。
	startErr, closeErr error
	// closed 记录 Close 是否被调用。
	closed bool
}

// Start 返回预置启动错误。
func (coordinator *scriptedCoordinator) Start(context.Context) error { return coordinator.startErr }

// Close 记录关闭并返回预置错误。
func (coordinator *scriptedCoordinator) Close(context.Context) error {
	coordinator.closed = true
	return coordinator.closeErr
}

// TestRunServerLifecycleStages 覆盖生命周期的绑定失败、组件启动失败、监听失败和正常退出四条路径。
func TestRunServerLifecycleStages(t *testing.T) {
	// logger 丢弃输出，避免污染测试日志。
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// cases 列出每个阶段的失败注入和期望结果。
	cases := []struct {
		// name 是子场景名称。
		name string
		// http、app 是注入的 HTTP 与应用替身。
		http *scriptedHTTPRuntime
		app  *scriptedCoordinator
		// wantErr 为期望错误关键词，空串表示必须成功。
		wantErr string
		// wantClosed 表示应用协调器是否应被关闭。
		wantClosed bool
	}{
		{"bind", &scriptedHTTPRuntime{bindErr: errors.New("port busy")}, &scriptedCoordinator{}, "绑定 HTTP 服务失败", false},
		{"app-start", &scriptedHTTPRuntime{}, &scriptedCoordinator{startErr: errors.New("boom")}, "启动应用生命周期失败", false},
		{"http-start", &scriptedHTTPRuntime{startErr: errors.New("listen")}, &scriptedCoordinator{}, "启动 HTTP 服务失败", true},
		{"ok", &scriptedHTTPRuntime{}, &scriptedCoordinator{}, "", true},
		{"wait-error", &scriptedHTTPRuntime{waitErr: errors.New("serve")}, &scriptedCoordinator{}, "serve", true},
	}
	// tc 是当前执行的生命周期场景。
	for _, tc := range cases {
		// lifecycleErr 保存该场景的生命周期返回值。
		lifecycleErr := runServerLifecycle(context.Background(), serverRuntime{httpServer: tc.http, lifecycleCoordinator: tc.app}, logger)
		if tc.wantErr == "" && lifecycleErr != nil {
			t.Fatalf("%s: unexpected err %v", tc.name, lifecycleErr)
		}
		if tc.wantErr != "" && (lifecycleErr == nil || !strings.Contains(lifecycleErr.Error(), tc.wantErr)) {
			t.Fatalf("%s: err=%v want %q", tc.name, lifecycleErr, tc.wantErr)
		}
		if tc.app.closed != tc.wantClosed {
			t.Fatalf("%s: closed=%v want %v", tc.name, tc.app.closed, tc.wantClosed)
		}
	}
}

// TestRollbackApplicationRuntimeHandlesNil 覆盖没有应用协调器时回滚为空操作。
func TestRollbackApplicationRuntimeHandlesNil(t *testing.T) {
	// rollbackErr 保存该步骤返回的错误，用于断言预期的成功或失败路径。
	if rollbackErr := rollbackApplicationRuntime(nil, time.Second); rollbackErr != nil {
		t.Fatal(rollbackErr)
	}
}

// TestIsRemoteDatabaseURL 覆盖远程数据库地址识别，SQLite 与本地路径不得被视为远程。
func TestIsRemoteDatabaseURL(t *testing.T) {
	// cases 保存地址到是否远程的期望映射。
	cases := map[string]bool{"mysql://u@tcp(h)/d": true, " POSTGRES://h/d": true, "postgresql://h/d": true, "pgx://h/d": true, "sqlite://x.db": false, "data/x.db": false, "": false}
	// raw、want 分别是待判断地址与期望结果。
	for raw, want := range cases {
		// got 保存本次调用的返回值，用于与期望结果比较。
		if got := isRemoteDatabaseURL(raw); got != want {
			t.Fatalf("%q got %v want %v", raw, got, want)
		}
	}
}

// isolateServerEnv 清空会影响启动准备阶段的进程环境变量，并由 t.Setenv 在测试结束后恢复。
func isolateServerEnv(t *testing.T) {
	t.Helper()
	// key 是需要隔离的环境变量名。
	for _, key := range []string{"XIANYU_DATA_KEY", "DATABASE_URL", "LOG_LEVEL", "LOG_FORMAT", "XIANYU_LOG_DIR", "PLAYWRIGHT_DRIVER_PATH", "PLAYWRIGHT_BROWSERS_PATH"} {
		t.Setenv(key, "")
	}
}

// TestPrepareServerStartupRemoteDatabaseRequiresKey 覆盖远程数据库未配置密钥时拒绝启动。
func TestPrepareServerStartupRemoteDatabaseRequiresKey(t *testing.T) {
	isolateServerEnv(t)
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		t.Skip("桌面平台默认有用户数据目录与密钥文件，该拒绝分支只在无默认数据目录的平台可达")
	}
	// opts 指向远程数据库但没有任何密钥来源。
	opts := serverOptions{dbURL: "mysql://u:p@tcp(h:3306)/d"}
	// startupErr 保存该步骤返回的错误，用于断言预期的成功或失败路径。
	if _, startupErr := prepareServerStartup(&opts); startupErr == nil || !strings.Contains(startupErr.Error(), "XIANYU_DATA_KEY") {
		t.Fatalf("err=%v", startupErr)
	}
}

// TestPrepareServerStartupDerivesPathsAndPolicies 覆盖数据目录派生、Playwright 路径、日志策略优先级与 DATABASE_URL 优先级。
func TestPrepareServerStartupDerivesPathsAndPolicies(t *testing.T) {
	isolateServerEnv(t)
	// dir 是显式工作目录。
	dir := t.TempDir()
	// opts 模拟安装包提供 runtime 根目录并指定日志选项。
	opts := serverOptions{workDir: dir, playwrightRuntimeRoot: filepath.Join(dir, "rt"), logLevel: "warn", logFormat: "json", dbPath: defaultDBPath}
	// startup、startupErr 是派生配置及其错误。
	startup, startupErr := prepareServerStartup(&opts)
	if startupErr != nil {
		t.Fatal(startupErr)
	}
	if !startup.explicitLogLevel || !startup.explicitLogFormat || startup.resolvedLogFormat != "json" || startup.dataDir != dir {
		t.Fatalf("startup=%+v", startup)
	}
	if opts.dataKeyFile != filepath.Join(dir, defaultDataKeyName) || opts.playwrightDriverDir == "" || opts.playwrightBrowserDir == "" {
		t.Fatalf("opts=%+v", opts)
	}
	if !strings.HasSuffix(startup.resolvedDBURL, filepath.FromSlash(defaultDBPath)) {
		t.Fatalf("db url=%q", startup.resolvedDBURL)
	}
	// 环境变量 DATABASE_URL 优先于命令行数据库地址。
	t.Setenv("DATABASE_URL", "sqlite://"+filepath.Join(dir, "env.db"))
	// envStartup 是 DATABASE_URL 生效后的派生配置。
	envStartup, envErr := prepareServerStartup(&serverOptions{workDir: dir, dbURL: "sqlite://ignored.db", verbose: true})
	if envErr != nil || !strings.HasSuffix(envStartup.resolvedDBURL, "env.db") || !envStartup.explicitLogLevel {
		t.Fatalf("env startup=%+v err=%v", envStartup, envErr)
	}
}

// TestPrepareServerStartupRejectsInvalidLogLevel 覆盖非法日志等级。
func TestPrepareServerStartupRejectsInvalidLogLevel(t *testing.T) {
	isolateServerEnv(t)
	// dir 是隔离工作目录。
	dir := t.TempDir()
	// startupErr 保存该步骤返回的错误，用于断言预期的成功或失败路径。
	if _, startupErr := prepareServerStartup(&serverOptions{workDir: dir, logLevel: "nope"}); startupErr == nil {
		t.Fatal("invalid log level should fail")
	}
}

// TestOpenServerInfrastructureRejectsBadDatabase 覆盖数据库无法打开时释放日志并返回错误。
func TestOpenServerInfrastructureRejectsBadDatabase(t *testing.T) {
	isolateServerEnv(t)
	// startup 指向不存在的目录下的 SQLite 路径，使打开失败。
	startup := serverStartupConfig{resolvedDBURL: "sqlite://" + filepath.Join(t.TempDir(), "missing", "dir", "x.db")}
	// openErr 保存该步骤返回的错误，用于断言预期的成功或失败路径。
	if _, openErr := openServerInfrastructure(context.Background(), startup, serverOptions{}); openErr == nil {
		t.Fatal("bad database should fail")
	}
}

// TestRunServerFullLifecycleStartsAndStopsOnCancel 以真实 SQLite 和随机端口启动整套服务，取消 Context 后应干净退出，并覆盖 ensure-admin 路径。
func TestRunServerFullLifecycleStartsAndStopsOnCancel(t *testing.T) {
	isolateServerEnv(t)
	// dir 是隔离的数据目录。
	dir := t.TempDir()
	// listener 先占位获取一个空闲端口再释放，避免固定端口冲突。
	listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
	if listenErr != nil {
		t.Fatal(listenErr)
	}
	// addr 是本次服务监听地址。
	addr := listener.Addr().String()
	_ = listener.Close()
	// ctx、cancel 控制服务运行时长。
	ctx, cancel := context.WithCancel(context.Background())
	// done 接收 runServer 的返回值。
	done := make(chan error, 1)
	go func() {
		done <- runServer(ctx, serverOptions{workDir: dir, dbPath: defaultDBPath, addr: addr, noBrowser: true, ensureAdmin: true, adminEmail: "a@example.com", adminPassword: "pw-123456", logLevel: "error"})
	}()
	// deadline 是等待端口可连接的截止时间。
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		// conn、dialErr 是探测服务是否已监听的结果。
		conn, dialErr := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	cancel()
	select {
	// runErr 保存该步骤返回的错误，用于断言预期的成功或失败路径。
	case runErr := <-done:
		if runErr != nil {
			t.Fatalf("runServer: %v", runErr)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("runServer did not stop after cancel")
	}
}
