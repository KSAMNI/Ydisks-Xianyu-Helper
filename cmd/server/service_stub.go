//go:build !windows

package main

import (
	"context"
	"errors"
)

// errPlatformServiceUnsupported 是非 Windows 平台收到 -service 参数时返回的固定错误；
// 这些平台由 systemd 或 launchd 直接以普通前台进程启动服务端，不存在进程内服务注册。
var errPlatformServiceUnsupported = errors.New("当前平台不支持 -service；请使用系统服务管理器启动普通进程")

// runPlatformService 是非 Windows 平台的服务模式存根：忽略服务名和启动函数，始终返回
// errPlatformServiceUnsupported，由调用方输出错误并以非零状态退出。
func runPlatformService(_ string, _ func(context.Context) error) error {
	return errPlatformServiceUnsupported
}
