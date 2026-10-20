package runtime

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	composition "xianyu-go/internal/composition"
	"xianyu-go/internal/db"
)

// failingQRAccountLogin 在二维码登录成功持久化时返回固定错误，其余方法沿用成功替身。
type failingQRAccountLogin struct {
	// runtimeAccountLoginFake 提供其余登录端口的成功行为。
	runtimeAccountLoginFake
	// err 是持久化失败原因。
	err error
}

// PersistQRLoginSuccess 返回预置错误，模拟扫码结果持久化失败。
func (f failingQRAccountLogin) PersistQRLoginSuccess(context.Context, int64, string, map[string]any, string) (composition.CookieLoginResult, error) {
	return composition.CookieLoginResult{}, f.err
}

// TestAccountLoginTransportPropagatesPersistError 验证扫码持久化失败时传输层返回空结果并原样透传错误。
func TestAccountLoginTransportPropagatesPersistError(t *testing.T) {
	// boom 是持久化失败原因。
	boom := errors.New("persist failed")
	// transport 包装失败的账号登录服务。
	transport := accountLoginTransport{service: failingQRAccountLogin{err: boom}}
	// result、err 是传输层转换结果。
	result, err := transport.PersistQRLoginSuccess(context.Background(), 1, "s", nil, "")
	if !errors.Is(err, boom) || result.AccountID != "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

// TestBuildRuntimeWithBrowserEnabledOnlyConstructs 验证启用浏览器时只登记生命周期组件而不启动 Chromium。
func TestBuildRuntimeWithBrowserEnabledOnlyConstructs(t *testing.T) {
	// sqlDB、dialect、openErr 是临时库连接、方言与打开错误。
	sqlDB, dialect, openErr := db.Open(context.Background(), filepath.Join(t.TempDir(), "browser.db"))
	if openErr != nil {
		t.Fatal(openErr)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	// built、err 是装配结果；不调用 Start，因此不会启动浏览器。
	built, err := BuildRuntime(
		RuntimeOptions{NoBrowser: false, WebDir: t.TempDir(), Addr: "127.0.0.1:0"},
		RuntimeInfrastructure{Store: db.NewStore(sqlDB, dialect), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))},
	)
	if err != nil || built.HTTPServer == nil || built.Lifecycle == nil {
		t.Fatalf("built=%+v err=%v", built, err)
	}
}
