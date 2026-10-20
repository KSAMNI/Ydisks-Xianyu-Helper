//go:build !windows

package main

import (
	"context"
	"errors"
	"testing"
)

// TestRunPlatformServiceStubReturnsUnsupportedSentinel 验证非 Windows 存根不调用启动函数，并返回可用 errors.Is 识别的固定错误。
func TestRunPlatformServiceStubReturnsUnsupportedSentinel(t *testing.T) {
	// called 记录存根是否误执行了服务启动函数；服务模式在本平台不得启动服务端。
	called := false
	// err 是存根返回的服务模式错误。
	err := runPlatformService("test", func(context.Context) error {
		called = true
		return nil
	})
	if !errors.Is(err, errPlatformServiceUnsupported) {
		t.Fatalf("err=%v want errPlatformServiceUnsupported", err)
	}
	if called {
		t.Fatal("非 Windows 存根不应调用服务启动函数")
	}
}
