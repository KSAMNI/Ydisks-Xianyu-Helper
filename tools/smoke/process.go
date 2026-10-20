package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// lockedBuffer 是并发安全的字节缓冲，供子进程的两个输出流同时写入。
type lockedBuffer struct {
	// mu 保护 buf；持锁期间只做内存读写。
	mu sync.Mutex
	// buf 保存累计输出。
	buf bytes.Buffer
}

// Write 追加子进程输出，实现 io.Writer。
func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String 返回当前累计输出的副本。
func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// serverProcess 封装被验证的服务进程及其输出缓冲。
type serverProcess struct {
	// cmd 是底层子进程。
	cmd *exec.Cmd
	// logs 收集子进程的标准输出与错误输出。
	logs *lockedBuffer
	// exited 在子进程退出后关闭；exitErr 保存其退出错误，关闭 exited 前写入。
	exited  chan struct{}
	exitErr error
}

// newServerProcess 启动 binary 并开始异步等待其退出。
// ctx 取消时不强杀进程，由调用方通过 stop 走优雅退出路径。
func newServerProcess(_ context.Context, binary string, args []string, dir string) (*serverProcess, error) {
	// logs 收集子进程输出。
	logs := &lockedBuffer{}
	// cmd 是待启动的服务进程，固定在临时目录运行。
	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = logs, logs
	cmd.Env = isolatedEnv(os.Environ())
	// err 保存子进程启动错误，例如二进制不存在或无执行权限。
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动服务: %w", err)
	}
	// proc 保存进程句柄；等待协程是 exited 通道的唯一发送方。
	proc := &serverProcess{cmd: cmd, logs: logs, exited: make(chan struct{})}
	go func() {
		proc.exitErr = cmd.Wait()
		close(proc.exited)
	}()
	return proc, nil
}

// output 返回已收集的子进程输出，用于失败诊断。
func (p *serverProcess) output() string { return p.logs.String() }

// stop 发送 SIGTERM 并在 timeout 内等待进程退出，超时则强制结束并报错。
// 进程已退出时直接返回其退出状态；正常优雅退出（含被信号终止的退出码）视为成功。
func (p *serverProcess) stop(timeout time.Duration) error {
	select {
	case <-p.exited:
		return nil
	default:
	}
	// err 保存向子进程发送终止信号的错误。
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("发送终止信号: %w", err)
	}
	// timer 限制优雅退出的等待时间。
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-p.exited:
		// exitCode 为 0 表示优雅退出，其他退出码代表停止流程异常。
		var exitErr *exec.ExitError
		if errors.As(p.exitErr, &exitErr) && exitErr.ExitCode() != 0 {
			return fmt.Errorf("服务退出码 %d\n%s", exitErr.ExitCode(), p.output())
		}
		return p.exitErr
	case <-timer.C:
		_ = p.cmd.Process.Kill()
		<-p.exited
		return fmt.Errorf("服务在 %s 内未能优雅退出", timeout)
	}
}

// freeLocalAddr 让系统分配一个空闲的本地 TCP 端口并返回 127.0.0.1:端口。
func freeLocalAddr() (string, error) {
	// listener 仅用于探测空闲端口，取得地址后立即关闭。
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("分配本地端口: %w", err)
	}
	// addr 是系统分配的监听地址，关闭监听器后端口暂时空闲。
	addr := listener.Addr().String()
	// err 保存释放探测端口时的错误。
	if err := listener.Close(); err != nil {
		return "", fmt.Errorf("释放探测端口: %w", err)
	}
	return addr, nil
}

// waitHealthy 轮询 base 的 /health 直到返回 200、超时、上下文取消或服务提前退出。
// exited 在服务进程退出时关闭，可为 nil。
func waitHealthy(ctx context.Context, base string, timeout time.Duration, exited <-chan struct{}) error {
	// deadline 是等待就绪的截止时间。
	deadline := time.Now().Add(timeout)
	// client 使用短超时，避免单次探测拖慢轮询。
	client := &http.Client{Timeout: 2 * time.Second}
	for {
		// resp、err 分别是本次健康探测的响应与传输错误；仅探测成功时才检查状态码。
		if resp, err := client.Get(base + "/health"); err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s 内健康检查未通过", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-exited:
			return errors.New("服务在就绪前退出")
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// isolatedEnv 从 environ 中移除会让服务连接真实数据库、复用真实密钥或改变监听行为的变量，
// 保证冒烟只使用命令行指定的临时库和临时密钥文件。
func isolatedEnv(environ []string) []string {
	// blocked 是必须清除的环境变量名。
	blocked := []string{"DATABASE_URL", "XIANYU_DATA_KEY", "XIANYU_ADMIN_PASSWORD"}
	// result 保存过滤后的环境。
	result := make([]string, 0, len(environ)+1)
	// entry 是 KEY=VALUE 形式的单个环境变量。
	for _, entry := range environ {
		// drop 表示该变量是否需要清除。
		drop := false
		// name 是需要清除的变量名。
		for _, name := range blocked {
			if strings.HasPrefix(entry, name+"=") {
				drop = true
			}
		}
		if !drop {
			result = append(result, entry)
		}
	}
	return append(result, "LOG_LEVEL=info")
}
