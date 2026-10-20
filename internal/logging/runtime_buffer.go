package logging

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// defaultRuntimeBufferCapacity 是未显式配置容量时进程内保留的运行时日志条数。
	defaultRuntimeBufferCapacity = 2000
	// maxRuntimeBufferCapacity 是单个运行时日志缓冲允许的最大条数，避免异常配置占用过多进程内存。
	maxRuntimeBufferCapacity = 20000
)

// RuntimeEntry 是运行时日志缓冲保存的一条记录。
// Attrs 已经过本包的脱敏处理器，因此不会包含 Cookie、Token、x5sec 或密码明文。
type RuntimeEntry struct {
	// Seq 是进程内单调递增的记录序号，从 1 开始，作为前端增量读取游标。
	Seq uint64
	// Time 是记录产生时间。
	Time time.Time
	// Level 是记录等级。
	Level slog.Level
	// Message 是记录消息。
	Message string
	// Attrs 是记录携带的已脱敏属性，同时包含创建 logger 时绑定的属性和单条记录属性。
	Attrs []slog.Attr
}

// RuntimeSnapshot 是运行时日志缓冲在某一时刻的一致快照，边界值与记录来自同一次加锁读取。
type RuntimeSnapshot struct {
	// Entries 是按序号升序排列的记录，且每条记录的序号都严格大于调用方传入的 since。
	Entries []RuntimeEntry
	// OldestSeq 是快照时刻缓冲区中最早记录的序号；缓冲区为空时为零。
	OldestSeq uint64
	// NextSeq 是快照时刻最后一条记录的序号；缓冲区为空时为零。
	NextSeq uint64
	// Dropped 是缓冲启动以来因容量上限被覆盖的累计记录数。
	Dropped uint64
	// Capacity 是缓冲可保留的最大记录数。
	Capacity int
}

// RuntimeBuffer 是进程内固定容量的运行时日志环形缓冲。
// 并发约束：mu 保护 entries、start、size、nextSeq 和 dropped 五个字段；
// 所有读写都在锁内完成，持锁期间不得调用外部代码或执行阻塞 I/O。
type RuntimeBuffer struct {
	// mu 保护环形缓冲的全部可变字段。
	mu sync.RWMutex
	// entries 是固定长度的环形存储，长度由 capacity 决定。
	entries []RuntimeEntry
	// capacity 是 entries 的长度，也是缓冲可保留的最大记录数。
	capacity int
	// start 是当前最早有效记录在 entries 中的下标。
	start int
	// size 是当前有效记录数，取值不超过 capacity。
	size int
	// nextSeq 是最近一次分配的记录序号。
	nextSeq uint64
	// dropped 是因容量上限被覆盖的累计记录数。
	dropped uint64
}

// NewRuntimeBuffer 构造指定容量的运行时日志缓冲。
// capacity 非正数时使用 defaultRuntimeBufferCapacity，超过 maxRuntimeBufferCapacity 时收敛到上限。
func NewRuntimeBuffer(capacity int) *RuntimeBuffer {
	// resolvedCapacity 是实际生效的容量，始终为正数且不超过进程内存上限。
	resolvedCapacity := capacity
	if resolvedCapacity <= 0 {
		resolvedCapacity = defaultRuntimeBufferCapacity
	}
	if resolvedCapacity > maxRuntimeBufferCapacity {
		resolvedCapacity = maxRuntimeBufferCapacity
	}
	return &RuntimeBuffer{entries: make([]RuntimeEntry, resolvedCapacity), capacity: resolvedCapacity}
}

// Append 追加一条已脱敏记录并返回其序号；缓冲区已满时覆盖最早的记录并累计丢弃计数。
// nil 接收者返回零值序号，调用方无需先判断缓冲是否启用。
func (b *RuntimeBuffer) Append(now time.Time, level slog.Level, message string, attrs []slog.Attr) uint64 {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextSeq++
	// entry 是本次写入的记录副本；attrs 由调用方构造，缓冲不再修改其内容。
	entry := RuntimeEntry{Seq: b.nextSeq, Time: now, Level: level, Message: message, Attrs: attrs}
	if b.size < b.capacity {
		b.entries[(b.start+b.size)%b.capacity] = entry
		b.size++
		return entry.Seq
	}
	// 缓冲区已满：覆盖最早记录并把起点推进一格，保持环形顺序不变。
	b.entries[b.start] = entry
	b.start = (b.start + 1) % b.capacity
	b.dropped++
	return entry.Seq
}

// Snapshot 返回序号严格大于 since 的全部记录及缓冲区边界，保证边界与记录来自同一次读取。
func (b *RuntimeBuffer) Snapshot(since uint64) RuntimeSnapshot {
	if b == nil {
		return RuntimeSnapshot{}
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	// snapshot 保存锁内计算完成的边界值，Entries 在匹配阶段再填充。
	snapshot := RuntimeSnapshot{NextSeq: b.nextSeq, Dropped: b.dropped, Capacity: b.capacity}
	if b.size == 0 {
		return snapshot
	}
	snapshot.OldestSeq = b.entries[b.start].Seq
	// entries 保存按时间顺序复制的匹配记录，使调用方在锁外不会读到被覆盖的槽位。
	entries := make([]RuntimeEntry, 0, b.size)
	// offset 是当前遍历到的环形缓冲逻辑下标，从最早的记录开始。
	for offset := 0; offset < b.size; offset++ {
		// entry 是当前逻辑位置的记录副本。
		entry := b.entries[(b.start+offset)%b.capacity]
		if entry.Seq <= since {
			continue
		}
		entries = append(entries, entry)
	}
	snapshot.Entries = entries
	return snapshot
}

// Len 返回缓冲区当前保留的记录数。
func (b *RuntimeBuffer) Len() int {
	if b == nil {
		return 0
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.size
}

// Reset 清空全部记录、序号和丢弃计数，仅用于测试隔离或显式重置。
// 重置后持有旧序号游标的调用方需要重新从零开始读取。
func (b *RuntimeBuffer) Reset() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.start = 0
	b.size = 0
	b.nextSeq = 0
	b.dropped = 0
}

// runtimeBufferHolder 保存进程级运行时日志缓冲；nil 表示当前进程未启用日志缓冲。
var runtimeBufferHolder atomic.Pointer[RuntimeBuffer]

// EnableRuntimeBuffer 启用进程级运行时日志缓冲并返回其唯一实例；重复调用会替换旧缓冲且不迁移旧记录。
func EnableRuntimeBuffer(capacity int) *RuntimeBuffer {
	// buffer 是本次启用的新缓冲，由组合根或测试持有引用。
	buffer := NewRuntimeBuffer(capacity)
	runtimeBufferHolder.Store(buffer)
	return buffer
}

// DisableRuntimeBuffer 停用进程级运行时日志缓冲，已保留的记录随之丢弃。
func DisableRuntimeBuffer() {
	runtimeBufferHolder.Store(nil)
}

// CurrentRuntimeBuffer 返回当前启用的进程级运行时日志缓冲；未启用时返回 nil。
func CurrentRuntimeBuffer() *RuntimeBuffer {
	return runtimeBufferHolder.Load()
}

// runtimeBufferHandler 在底层处理器写出前把记录追加到进程级运行时日志缓冲。
// 它位于脱敏处理器内侧，因此写入缓冲的属性已经完成凭证清理。
type runtimeBufferHandler struct {
	// next 是实际写出记录的底层处理器。
	next slog.Handler
	// attrs 保存创建 logger 时绑定的已脱敏属性，追加记录时需要与单条记录属性合并。
	attrs []slog.Attr
	// group 是当前分组前缀，空值表示尚未打开任何分组。
	group string
}

// Enabled 沿用底层处理器的等级过滤，避免为被过滤的记录构造缓冲属性。
func (h *runtimeBufferHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle 先把脱敏后的记录追加到运行时缓冲，再交给底层处理器写出。
func (h *runtimeBufferHandler) Handle(ctx context.Context, record slog.Record) error {
	// buffer 是当前进程启用的运行时缓冲；未启用时跳过全部额外分配。
	if buffer := runtimeBufferHolder.Load(); buffer != nil {
		// attrs 保存绑定属性与单条记录属性的合并结果，容量按两者之和预留。
		attrs := make([]slog.Attr, 0, len(h.attrs)+record.NumAttrs())
		attrs = append(attrs, h.attrs...)
		record.Attrs( /* collectRecordAttrs 展开单条记录属性并补齐当前分组前缀。 */ func(attr slog.Attr) bool {
			attrs = append(attrs, h.prefixed(attr))
			return true
		})
		buffer.Append(record.Time, record.Level, record.Message, attrs)
	}
	return h.next.Handle(ctx, record)
}

// WithAttrs 记住底层处理器绑定的属性副本，并继续向缓冲透传。
func (h *runtimeBufferHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// added 保存按当前分组前缀展开后的新增绑定属性。
	added := make([]slog.Attr, 0, len(attrs))
	// attr 是本次需要绑定到 logger 的属性。
	for _, attr := range attrs {
		added = append(added, h.prefixed(attr))
	}
	// combined 保存历史绑定属性与新增属性的合并结果，供后续记录复用。
	combined := make([]slog.Attr, 0, len(h.attrs)+len(added))
	combined = append(combined, h.attrs...)
	combined = append(combined, added...)
	return &runtimeBufferHandler{next: h.next.WithAttrs(attrs), attrs: combined, group: h.group}
}

// WithGroup 记录分组前缀，使缓冲中的属性键保留嵌套分组语义。
func (h *runtimeBufferHandler) WithGroup(name string) slog.Handler {
	// prefix 是本次打开分组后的完整前缀；已有前缀时使用点号拼接。
	prefix := name
	if h.group != "" {
		prefix = h.group + "." + name
	}
	return &runtimeBufferHandler{next: h.next.WithGroup(name), attrs: h.attrs, group: prefix}
}

// prefixed 按当前分组前缀展开一条属性，保证缓冲中的键名不因分组而与其他属性冲突。
func (h *runtimeBufferHandler) prefixed(attr slog.Attr) slog.Attr {
	if h.group == "" {
		return attr
	}
	return slog.Attr{Key: h.group + "." + attr.Key, Value: attr.Value}
}
