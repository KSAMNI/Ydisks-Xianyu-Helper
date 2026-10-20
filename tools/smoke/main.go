// Command smoke 启动真实的 xianyu-server 二进制并通过 HTTP 验证核心链路可用。
//
// 它不依赖真实闲鱼账号：使用临时 SQLite 数据库和随机本地端口，依次验证健康检查、
// 首次初始化、登录鉴权、核心列表接口、嵌入式前端页面以及优雅退出。
// 默认保持浏览器自动化开启；仅在明确确认 Chromium 不可用的环境才通过 -no-browser 关闭。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

// smokePassword 是冒烟使用的一次性管理员密码，只写入临时数据库，不对应任何真实环境。
const smokePassword = "smoke-test-password"

// config 保存冒烟运行的命令行参数。
type config struct {
	// binary 是待验证的 xianyu-server 可执行文件路径。
	binary string
	// noBrowser 表示明确确认 Chromium 不可用时给服务附加 -no-browser。
	noBrowser bool
	// startTimeout 是等待服务健康检查通过的最长时间。
	startTimeout time.Duration
	// stopTimeout 是发送终止信号后等待进程退出的最长时间。
	stopTimeout time.Duration
}

// main 解析参数、执行冒烟并用退出码反馈结果。
func main() {
	// cfg 保存解析后的冒烟参数。
	cfg := config{}
	flag.StringVar(&cfg.binary, "bin", "", "待验证的 xianyu-server 可执行文件路径")
	flag.BoolVar(&cfg.noBrowser, "no-browser", false, "仅在确认 Chromium 不可用时关闭浏览器自动化")
	flag.DurationVar(&cfg.startTimeout, "start-timeout", 90*time.Second, "等待服务就绪的最长时间")
	flag.DurationVar(&cfg.stopTimeout, "stop-timeout", 30*time.Second, "等待服务优雅退出的最长时间")
	flag.Parse()
	// ctx 在收到中断信号时取消，保证本地中断也会清理子进程。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// err 保存整个冒烟流程的失败原因，非空时以退出码 1 结束。
	if err := run(ctx, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "冒烟失败:", err)
		os.Exit(1)
	}
	fmt.Println("冒烟通过")
}

// run 在临时目录启动服务，执行全部检查并保证服务被停止。
// 返回的错误合并了检查失败与退出失败，两者都会让冒烟失败。
func run(ctx context.Context, cfg config) (err error) {
	if cfg.binary == "" {
		return errors.New("缺少 -bin 参数")
	}
	// workDir 是本次冒烟独享的数据目录，结束后整体删除。
	workDir, err := os.MkdirTemp("", "xianyu-smoke-*")
	if err != nil {
		return fmt.Errorf("创建临时目录: %w", err)
	}
	defer func() { err = errors.Join(err, os.RemoveAll(workDir)) }()
	// addr 是随机分配的本地监听地址。
	addr, err := freeLocalAddr()
	if err != nil {
		return err
	}
	// proc 是被验证的服务进程。
	proc, err := startServer(ctx, cfg, workDir, addr)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, proc.stop(cfg.stopTimeout)) }()
	// base 是服务的 HTTP 根地址。
	base := "http://" + addr
	// err 保存等待服务就绪的错误；失败时附带服务输出便于定位。
	if err := waitHealthy(ctx, base, cfg.startTimeout, proc.exited); err != nil {
		return fmt.Errorf("等待服务就绪: %w\n%s", err, proc.output())
	}
	return runChecks(ctx, base, smokePassword)
}

// startServer 以临时数据库启动服务进程；输出被收集用于失败时诊断。
func startServer(ctx context.Context, cfg config, workDir, addr string) (*serverProcess, error) {
	// args 是服务启动参数：随机端口、临时库、独立工作目录和独立数据密钥文件（避免读写真实密钥）。
	args := []string{"-addr", addr, "-db", filepath.Join(workDir, "smoke.db"), "-workdir", workDir, "-data-key-file", filepath.Join(workDir, "data.key")}
	if cfg.noBrowser {
		args = append(args, "-no-browser")
	}
	return newServerProcess(ctx, cfg.binary, args, workDir)
}
