package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// findRuntimeAttr 返回记录中指定键的属性；键不存在时返回空属性与 false。
func findRuntimeAttr(entry RuntimeEntry, key string) (slog.Attr, bool) {
	// attr 表示当前遍历到的记录属性。
	for _, attr := range entry.Attrs {
		if attr.Key == key {
			return attr, true
		}
	}
	return slog.Attr{}, false
}

// TestRuntimeBufferSnapshotReportsBoundsAndFilters 验证环形覆盖、游标过滤和空接收者行为。
func TestRuntimeBufferSnapshotReportsBoundsAndFilters(t *testing.T) {
	// buffer 是容量为 3 的测试缓冲，用于验证第五条记录覆盖最早记录。
	buffer := NewRuntimeBuffer(3)
	// index 表示当前写入的序号计数，取值 1 到 5。
	for index := 1; index <= 5; index++ {
		buffer.Append(time.Unix(int64(index), 0), slog.LevelInfo, "记录", nil)
	}
	if buffer.Len() != 3 {
		t.Fatalf("覆盖后长度=%d want 3", buffer.Len())
	}
	// snapshot 是从零游标读取的完整快照，应只包含最后三条记录。
	snapshot := buffer.Snapshot(0)
	if len(snapshot.Entries) != 3 || snapshot.Entries[0].Seq != 3 || snapshot.Entries[2].Seq != 5 {
		t.Fatalf("完整快照异常: %+v", snapshot.Entries)
	}
	if snapshot.OldestSeq != 3 || snapshot.NextSeq != 5 || snapshot.Dropped != 2 || snapshot.Capacity != 3 {
		t.Fatalf("快照边界异常: %+v", snapshot)
	}
	// filtered 是从序号 4 之后读取的增量快照，应只返回第五条记录。
	filtered := buffer.Snapshot(4)
	if len(filtered.Entries) != 1 || filtered.Entries[0].Seq != 5 {
		t.Fatalf("增量快照异常: %+v", filtered.Entries)
	}
	// empty 是游标已追上末尾时的空增量结果，边界值仍需保留。
	empty := buffer.Snapshot(5)
	if len(empty.Entries) != 0 || empty.NextSeq != 5 || empty.OldestSeq != 3 {
		t.Fatalf("末尾增量快照异常: %+v", empty)
	}
	// nilBuffer 是未初始化的缓冲接收者，所有方法都必须安全返回零值。
	var nilBuffer *RuntimeBuffer
	if nilBuffer.Append(time.Now(), slog.LevelInfo, "忽略", nil) != 0 || nilBuffer.Len() != 0 || len(nilBuffer.Snapshot(0).Entries) != 0 {
		t.Fatal("空接收者应返回零值而不写入")
	}
	nilBuffer.Reset()
}

// TestNewRuntimeBufferClampsCapacity 验证容量缺省和上限收敛，避免异常配置占用过多内存。
func TestNewRuntimeBufferClampsCapacity(t *testing.T) {
	// zeroBuffer 是容量为零时回退到默认容量的缓冲。
	zeroBuffer := NewRuntimeBuffer(0)
	// negativeBuffer 是容量为负时回退到默认容量的缓冲。
	negativeBuffer := NewRuntimeBuffer(-5)
	// oversizedBuffer 是超过进程上限时收敛到最大容量的缓冲。
	oversizedBuffer := NewRuntimeBuffer(maxRuntimeBufferCapacity + 100)
	if zeroBuffer.Snapshot(0).Capacity != defaultRuntimeBufferCapacity || negativeBuffer.Snapshot(0).Capacity != defaultRuntimeBufferCapacity {
		t.Fatalf("默认容量回退异常: zero=%d negative=%d", zeroBuffer.Snapshot(0).Capacity, negativeBuffer.Snapshot(0).Capacity)
	}
	if oversizedBuffer.Snapshot(0).Capacity != maxRuntimeBufferCapacity {
		t.Fatalf("容量上限收敛异常: %d", oversizedBuffer.Snapshot(0).Capacity)
	}
}

// TestRuntimeBufferResetClearsAllState 验证重置会清空记录、序号和丢弃计数。
func TestRuntimeBufferResetClearsAllState(t *testing.T) {
	// buffer 是写入两条记录后待重置的缓冲。
	buffer := NewRuntimeBuffer(2)
	buffer.Append(time.Now(), slog.LevelInfo, "第一条", nil)
	buffer.Append(time.Now(), slog.LevelWarn, "第二条", nil)
	buffer.Reset()
	// snapshot 是重置后的快照，记录与边界都应回到初始状态。
	snapshot := buffer.Snapshot(0)
	if buffer.Len() != 0 || len(snapshot.Entries) != 0 || snapshot.NextSeq != 0 || snapshot.OldestSeq != 0 || snapshot.Dropped != 0 {
		t.Fatalf("重置后状态异常: %+v", snapshot)
	}
}

// TestRuntimeBufferHandlerCapturesRedactedRecords 验证缓冲捕获的是脱敏后记录，且绑定属性一并保留。
func TestRuntimeBufferHandlerCapturesRedactedRecords(t *testing.T) {
	// buffer 是本次测试启用的进程级运行时缓冲。
	buffer := EnableRuntimeBuffer(8)
	t.Cleanup(DisableRuntimeBuffer)
	// output 捕获同时写往底层处理器的日志文本。
	var output bytes.Buffer
	// logger 是绑定账号与子系统属性后写入一条失败日志的实例。
	logger := NewLogger(&output, "text").With("account", "acc-1", "subsys", "automation")
	logger.Error("MTOP 响应失败", "api", "mtop.trade.order", "cookie", "secret-cookie-value", "status", 500)
	// snapshot 是缓冲中本次写入的记录集合。
	snapshot := buffer.Snapshot(0)
	if len(snapshot.Entries) != 1 {
		t.Fatalf("缓冲记录数=%d want 1", len(snapshot.Entries))
	}
	// entry 是本次写入的唯一记录。
	entry := snapshot.Entries[0]
	if entry.Seq != 1 || entry.Level != slog.LevelError || entry.Message != "MTOP 响应失败" {
		t.Fatalf("记录基本字段异常: %+v", entry)
	}
	// accountAttr、ok 是绑定账号属性的取值结果。
	accountAttr, ok := findRuntimeAttr(entry, "account")
	if !ok || accountAttr.Value.String() != "acc-1" {
		t.Fatalf("绑定属性未进入缓冲: %+v", entry.Attrs)
	}
	// subsysAttr、ok 是绑定子系统属性的取值结果，供来源分类使用。
	subsysAttr, ok := findRuntimeAttr(entry, "subsys")
	if !ok || subsysAttr.Value.String() != "automation" {
		t.Fatalf("子系统属性未进入缓冲: %+v", entry.Attrs)
	}
	// cookieAttr、ok 是敏感属性的脱敏结果。
	cookieAttr, ok := findRuntimeAttr(entry, "cookie")
	if !ok || cookieAttr.Value.String() != "<redacted>" {
		t.Fatalf("敏感属性未脱敏: %+v", entry.Attrs)
	}
	// statusAttr、ok 是数字属性的保留结果。
	statusAttr, ok := findRuntimeAttr(entry, "status")
	if !ok || statusAttr.Value.Int64() != 500 {
		t.Fatalf("数字属性未保留: %+v", entry.Attrs)
	}
	if !strings.Contains(output.String(), "MTOP 响应失败") {
		t.Fatalf("底层处理器仍应写出日志: %s", output.String())
	}
}

// TestRuntimeBufferHandlerHonorsLevelAndGroupPrefix 验证缓冲沿用等级过滤并保留分组前缀。
func TestRuntimeBufferHandlerHonorsLevelAndGroupPrefix(t *testing.T) {
	defer Level.Set(slog.LevelInfo)
	// buffer 是本次测试启用的进程级运行时缓冲。
	buffer := EnableRuntimeBuffer(8)
	t.Cleanup(DisableRuntimeBuffer)
	// output 捕获同时写往底层处理器的日志文本。
	var output bytes.Buffer
	// logger 是在信息等级下打开分组的实例，用于验证低等级记录被过滤。
	logger := NewLogger(&output, "text").WithGroup("scope").With("phase", "one")
	logger.Debug("被过滤的调试记录", "key", "value")
	if len(buffer.Snapshot(0).Entries) != 0 {
		t.Fatalf("低等级记录不应进入缓冲: %+v", buffer.Snapshot(0).Entries)
	}
	Level.Set(slog.LevelDebug)
	logger.Debug("进入缓冲的记录", "key", "value")
	// snapshot 是启用调试等级后写入的记录集合。
	snapshot := buffer.Snapshot(0)
	if len(snapshot.Entries) != 1 {
		t.Fatalf("调试记录数=%d want 1", len(snapshot.Entries))
	}
	// prefixedKey、ok 是分组内单条记录属性展开后的键名。
	prefixedKey, ok := findRuntimeAttr(snapshot.Entries[0], "scope.key")
	if !ok || prefixedKey.Value.String() != "value" {
		t.Fatalf("分组前缀未应用到记录属性: %+v", snapshot.Entries[0].Attrs)
	}
	// prefixedBound、ok 是分组内绑定属性展开后的键名。
	prefixedBound, ok := findRuntimeAttr(snapshot.Entries[0], "scope.phase")
	if !ok || prefixedBound.Value.String() != "one" {
		t.Fatalf("分组前缀未应用到绑定属性: %+v", snapshot.Entries[0].Attrs)
	}
}

// TestDisableRuntimeBufferSkipsCapture 验证未启用缓冲时日志照常写出且不产生捕获。
func TestDisableRuntimeBufferSkipsCapture(t *testing.T) {
	EnableRuntimeBuffer(4)
	DisableRuntimeBuffer()
	if CurrentRuntimeBuffer() != nil {
		t.Fatal("停用后不应保留进程级缓冲")
	}
	// output 捕获未启用缓冲时仍应写出的日志文本。
	var output bytes.Buffer
	// logger 是未启用缓冲时创建的实例，必须与普通日志行为一致。
	logger := NewLogger(&output, "text")
	logger.Warn("普通日志", "account", "acc-2")
	if !strings.Contains(output.String(), "普通日志") {
		t.Fatalf("未启用缓冲时仍应写出日志: %s", output.String())
	}
}

// TestRuntimeBufferConcurrentAppendAndSnapshot 验证并发追加与快照在容量上限内保持自洽。
func TestRuntimeBufferConcurrentAppendAndSnapshot(t *testing.T) {
	// buffer 是容量为 32 的并发测试缓冲。
	buffer := NewRuntimeBuffer(32)
	// writers 是并发写入的协程数量。
	const writers = 8
	// perWriter 是每个协程写入的记录条数。
	const perWriter = 50
	// done 用于等待全部写入协程结束。
	var done sync.WaitGroup
	done.Add(writers)
	// writerIndex 表示当前启动的写入协程序号。
	for writerIndex := 0; writerIndex < writers; writerIndex++ {
		go func( /* index 是当前协程序号，仅用于区分写入内容。 */ index int) {
			defer done.Done()
			// appendIndex 表示当前协程写入的记录计数。
			for appendIndex := 0; appendIndex < perWriter; appendIndex++ {
				buffer.Append(time.Now(), slog.LevelInfo, "并发记录", []slog.Attr{slog.Int("writer", index)})
			}
		}(writerIndex)
	}
	// reader 表示并发读取快照的协程序号，用于覆盖读锁路径。
	for reader := 0; reader < 4; reader++ {
		// snapshot 是并发读取到的快照，记录数与序号边界必须自洽。
		snapshot := buffer.Snapshot(0)
		if len(snapshot.Entries) > 32 || snapshot.NextSeq < uint64(len(snapshot.Entries)) {
			t.Fatalf("并发快照边界异常: entries=%d next=%d", len(snapshot.Entries), snapshot.NextSeq)
		}
		// entryIndex 表示当前检查的快照记录下标。
		for entryIndex := 1; entryIndex < len(snapshot.Entries); entryIndex++ {
			if snapshot.Entries[entryIndex].Seq <= snapshot.Entries[entryIndex-1].Seq {
				t.Fatalf("并发快照序号未严格递增: %d", snapshot.Entries[entryIndex].Seq)
			}
		}
	}
	done.Wait()
	// final 是全部写入结束后的最终快照。
	final := buffer.Snapshot(0)
	if len(final.Entries) != 32 || final.NextSeq != writers*perWriter {
		t.Fatalf("并发写入结果异常: entries=%d next=%d", len(final.Entries), final.NextSeq)
	}
	if final.Dropped != uint64(writers*perWriter-32) {
		t.Fatalf("并发丢弃计数异常: %d", final.Dropped)
	}
	// previousSeq 保存遍历到的上一条记录序号，用于校验最终快照严格递增。
	previousSeq := uint64(0)
	// entry 是当前待校验的最终记录。
	for _, entry := range final.Entries {
		if entry.Seq <= previousSeq {
			t.Fatalf("最终快照序号未严格递增: %d", entry.Seq)
		}
		previousSeq = entry.Seq
	}
}

// TestRuntimeBufferHandlerNestsGroupPrefixes 验证连续打开分组时缓冲属性键拼接完整前缀。
func TestRuntimeBufferHandlerNestsGroupPrefixes(t *testing.T) {
	// buffer 是本次测试启用的进程级运行时缓冲。
	buffer := EnableRuntimeBuffer(4)
	t.Cleanup(DisableRuntimeBuffer)
	// output 捕获同时写往底层处理器的日志文本。
	var output bytes.Buffer
	// logger 是连续打开两个分组的实例。
	logger := NewLogger(&output, "text").WithGroup("outer").WithGroup("inner")
	logger.Warn("嵌套分组记录", "key", "value")
	// snapshot 是包含嵌套分组记录的缓冲快照。
	snapshot := buffer.Snapshot(0)
	if len(snapshot.Entries) != 1 {
		t.Fatalf("嵌套分组记录数=%d want 1", len(snapshot.Entries))
	}
	// nestedKey、ok 是嵌套分组展开后的完整键名。
	nestedKey, ok := findRuntimeAttr(snapshot.Entries[0], "outer.inner.key")
	if !ok || nestedKey.Value.String() != "value" {
		t.Fatalf("嵌套分组前缀未正确拼接: %+v", snapshot.Entries[0].Attrs)
	}
}
