package logs

import (
	"errors"
	"testing"
	"time"
)

// fakeSource 是记录调用参数的测试日志来源，用于验证查询用例的过滤与游标语义。
type fakeSource struct {
	// snapshot 是本次测试返回给用例的固定快照。
	snapshot Snapshot
	// since 保存用例实际传入的排他游标，供游标透传断言使用。
	since uint64
}

// Snapshot 按传入游标过滤预设快照，并记录调用方实际使用的游标。
func (s *fakeSource) Snapshot(since uint64) Snapshot {
	s.since = since
	// filtered 是模拟真实来源按游标过滤后的记录集合。
	filtered := make([]Record, 0, len(s.snapshot.Records))
	// record 是当前待过滤的预设记录。
	for _, record := range s.snapshot.Records {
		if record.Seq > since {
			filtered = append(filtered, record)
		}
	}
	return Snapshot{
		Records:   filtered,
		OldestSeq: s.snapshot.OldestSeq,
		NextSeq:   s.snapshot.NextSeq,
		Dropped:   s.snapshot.Dropped,
		Capacity:  s.snapshot.Capacity,
	}
}

// newRecord 构造一条带常见具名字段与附加属性的测试记录。
func newRecord(seq uint64, level Level, source Source, message string) Record {
	return Record{
		Seq:       seq,
		Time:      time.Unix(int64(seq), 0),
		Level:     level,
		Source:    source,
		Message:   message,
		Method:    "POST",
		Path:      "/api/v1/orders",
		Status:    500,
		API:       "mtop.trade.order.render",
		Category:  "system",
		Error:     "请求失败",
		AccountID: "acc-1",
		Details:   []Detail{{Key: "ret", Value: "FAIL_SYS_TIMEOUT"}},
	}
}

// TestQueryFiltersByLevelSourceAndKeyword 验证等级、来源和关键字三个过滤维度。
func TestQueryFiltersByLevelSourceAndKeyword(t *testing.T) {
	// source 是返回四条不同等级和来源记录的测试来源。
	source := &fakeSource{snapshot: Snapshot{
		Records: []Record{
			newRecord(1, LevelDebug, SourceHTTP, "调试记录"),
			newRecord(2, LevelInfo, SourceMTop, "信息记录"),
			newRecord(3, LevelWarn, SourceMTop, "警告记录"),
			newRecord(4, LevelError, SourceOther, "错误记录"),
		},
		OldestSeq: 1, NextSeq: 4, Dropped: 0, Capacity: 10,
	}}
	// service 是本次测试的运行日志查询用例。
	service := NewService(source)
	// page、err 分别是按最低警告等级加平台来源过滤后的结果与错误。
	page, err := service.Query(Query{MinLevel: "warn", Sources: []string{" MTOP "}})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(page.Entries) != 1 || page.Entries[0].Seq != 3 {
		t.Fatalf("等级与来源过滤异常: %+v", page.Entries)
	}
	if page.NextCursor != 3 || page.OldestSeq != 1 || page.Capacity != 10 {
		t.Fatalf("分页边界异常: %+v", page)
	}
	// keywordPage 是按附加属性值过滤后的结果，验证关键字覆盖 Details。
	keywordPage, err := service.Query(Query{Keyword: "fail_sys_timeout"})
	if err != nil {
		t.Fatalf("关键字查询失败: %v", err)
	}
	if len(keywordPage.Entries) != 4 {
		t.Fatalf("附加属性关键字过滤异常: %+v", keywordPage.Entries)
	}
	// apiPage 是按平台接口名过滤后的结果，验证关键字覆盖 API 字段。
	apiPage, err := service.Query(Query{Keyword: "mtop.trade"})
	if err != nil {
		t.Fatalf("接口关键字查询失败: %v", err)
	}
	if len(apiPage.Entries) != 4 {
		t.Fatalf("接口名关键字过滤异常: %+v", apiPage.Entries)
	}
}

// TestQueryNormalizesLimitAndCursor 验证条数归一化、游标推进和游标透传。
func TestQueryNormalizesLimitAndCursor(t *testing.T) {
	// records 是本次测试使用的六条信息等级记录。
	records := make([]Record, 0, 6)
	// index 表示当前构造的记录序号，取值 1 到 6。
	for index := 1; index <= 6; index++ {
		records = append(records, newRecord(uint64(index), LevelInfo, SourceHTTP, "记录"))
	}
	// source 是返回上述记录的测试来源。
	source := &fakeSource{snapshot: Snapshot{Records: records, OldestSeq: 1, NextSeq: 6, Capacity: 10}}
	// service 是本次测试的运行日志查询用例。
	service := NewService(source)
	// defaultPage 是未指定条数时使用默认上限的结果。
	defaultPage, err := service.Query(Query{Since: 2})
	if err != nil {
		t.Fatalf("默认条数查询失败: %v", err)
	}
	if source.since != 2 || len(defaultPage.Entries) != 4 || defaultPage.NextCursor != 6 {
		t.Fatalf("默认条数或游标异常: since=%d page=%+v", source.since, defaultPage)
	}
	// limitedPage 是条数上限小于可用记录时的结果，游标应停在最后一条已返回记录。
	limitedPage, err := service.Query(Query{Since: 1, Limit: 2})
	if err != nil {
		t.Fatalf("限量查询失败: %v", err)
	}
	if len(limitedPage.Entries) != 2 || limitedPage.NextCursor != 3 {
		t.Fatalf("限量查询游标异常: %+v", limitedPage)
	}
	// clampedPage 是超过硬上限时的结果，最多返回 MaxLimit 条。
	clampedPage, err := service.Query(Query{Limit: MaxLimit + 100})
	if err != nil {
		t.Fatalf("超限查询失败: %v", err)
	}
	if len(clampedPage.Entries) != len(records) {
		t.Fatalf("硬上限收敛异常: %d", len(clampedPage.Entries))
	}
	// negativePage 是负条数时的结果，应回退到默认上限。
	negativePage, err := service.Query(Query{Limit: -3})
	if err != nil {
		t.Fatalf("负条数查询失败: %v", err)
	}
	if len(negativePage.Entries) != len(records) {
		t.Fatalf("负条数回退异常: %d", len(negativePage.Entries))
	}
}

// TestQueryAdvancesCursorWhenNothingMatches 验证没有匹配记录时游标推进到来源末尾且不倒退。
func TestQueryAdvancesCursorWhenNothingMatches(t *testing.T) {
	// source 是只含信息等级记录的测试来源。
	source := &fakeSource{snapshot: Snapshot{
		Records:   []Record{newRecord(7, LevelInfo, SourceHTTP, "记录")},
		OldestSeq: 7, NextSeq: 9, Dropped: 4, Capacity: 10,
	}}
	// service 是本次测试的运行日志查询用例。
	service := NewService(source)
	// page 是按错误等级过滤后没有命中记录的结果。
	page, err := service.Query(Query{Since: 5, MinLevel: "error"})
	if err != nil {
		t.Fatalf("空结果查询失败: %v", err)
	}
	if len(page.Entries) != 0 || page.NextCursor != 9 || page.Dropped != 4 {
		t.Fatalf("空结果游标异常: %+v", page)
	}
	// stalePage 是来源边界落后于请求游标时的结果；来源重启后必须回到较小游标而不是长期返回空页。
	stalePage, err := service.Query(Query{Since: 100, MinLevel: "error"})
	if err != nil {
		t.Fatalf("越界游标查询失败: %v", err)
	}
	if stalePage.NextCursor != 9 {
		t.Fatalf("越界游标未回落到来源末尾: %d", stalePage.NextCursor)
	}
}

// TestQueryRejectsInvalidLevel 验证无效等级过滤返回明确错误而不静默放行。
func TestQueryRejectsInvalidLevel(t *testing.T) {
	// service 是本次测试的运行日志查询用例。
	service := NewService(&fakeSource{})
	// page、err 分别是无效等级查询的结果与错误。
	page, err := service.Query(Query{MinLevel: "verbose"})
	if !errors.Is(err, ErrInvalidLevel) {
		t.Fatalf("无效等级应返回 ErrInvalidLevel: %v", err)
	}
	if len(page.Entries) != 0 {
		t.Fatalf("无效等级不应返回记录: %+v", page.Entries)
	}
}

// TestQueryWithoutSourceReturnsEmptyPage 验证缺少日志来源时查询安全返回空页。
func TestQueryWithoutSourceReturnsEmptyPage(t *testing.T) {
	// page、err 分别是不带来源的查询结果与错误。
	page, err := NewService(nil).Query(Query{})
	if err != nil || len(page.Entries) != 0 || page.NextCursor != 0 {
		t.Fatalf("缺少来源时结果异常: page=%+v err=%v", page, err)
	}
	// nilService 是未初始化的查询用例接收者，必须与缺少来源时行为一致。
	var nilService *Service
	// nilPage、nilErr 分别是空接收者查询的结果与错误。
	nilPage, nilErr := nilService.Query(Query{})
	if nilErr != nil || len(nilPage.Entries) != 0 {
		t.Fatalf("空接收者结果异常: page=%+v err=%v", nilPage, nilErr)
	}
}

// TestQueryFirstReadReturnsNewestRecords 验证首次读取返回最近的一批记录而不是最旧的一批。
func TestQueryFirstReadReturnsNewestRecords(t *testing.T) {
	// records 是序号 1 到 10 的测试记录。
	records := make([]Record, 0, 10)
	// index 表示当前构造的记录序号。
	for index := 1; index <= 10; index++ {
		records = append(records, newRecord(uint64(index), LevelInfo, SourceHTTP, "记录"))
	}
	// source 是返回上述记录的测试来源。
	source := &fakeSource{snapshot: Snapshot{Records: records, OldestSeq: 1, NextSeq: 10, Capacity: 10}}
	// service 是本次测试的运行日志查询用例。
	service := NewService(source)
	// page 是首次读取三条记录的结果。
	page, err := service.Query(Query{Limit: 3})
	if err != nil {
		t.Fatalf("首次读取失败: %v", err)
	}
	// seqs 是首次读取返回的序号序列，必须是序号最大的三条。
	seqs := recordSeqs(page.Entries)
	if len(seqs) != 3 || seqs[0] != 8 || seqs[2] != 10 {
		t.Fatalf("首次读取未返回最近记录: %v", seqs)
	}
	if page.NextCursor != 10 {
		t.Fatalf("首次读取游标异常: %d", page.NextCursor)
	}
	// source.snapshot 追加第 11 条记录，模拟首次读取之后新增的日志。
	source.snapshot = Snapshot{Records: append(records, newRecord(11, LevelInfo, SourceHTTP, "记录")), OldestSeq: 1, NextSeq: 11, Capacity: 10}
	// incrementalPage 是从首次游标继续读取的结果，必须只包含新增记录。
	incrementalPage, err := service.Query(Query{Since: page.NextCursor, Limit: 3})
	if err != nil {
		t.Fatalf("增量读取失败: %v", err)
	}
	// incrementalSeqs 是增量读取返回的序号序列，必须只包含首次读取之后新增的记录。
	if incrementalSeqs := recordSeqs(incrementalPage.Entries); len(incrementalSeqs) != 1 || incrementalSeqs[0] != 11 {
		t.Fatalf("增量读取异常: %v", incrementalSeqs)
	}
}

// TestQueryRecoversFromStaleCursorAfterSourceRestart 验证来源重启后旧游标仍能重新读到最新记录。
func TestQueryRecoversFromStaleCursorAfterSourceRestart(t *testing.T) {
	// records 是重启后新来源保留的三条记录，序号从 1 重新开始。
	records := []Record{
		newRecord(1, LevelError, SourceMTop, "重启后记录"),
		newRecord(2, LevelError, SourceMTop, "重启后记录"),
		newRecord(3, LevelError, SourceMTop, "重启后记录"),
	}
	// source 是模拟服务端重启后的测试来源。
	source := &fakeSource{snapshot: Snapshot{Records: records, OldestSeq: 1, NextSeq: 3, Capacity: 2000}}
	// service 是本次测试的运行日志查询用例。
	service := NewService(source)
	// page 是携带旧进程高游标查询的结果。
	page, err := service.Query(Query{Since: 1200})
	if err != nil {
		t.Fatalf("旧游标查询失败: %v", err)
	}
	// seqs 是旧游标查询返回的序号序列，必须覆盖新来源的全部记录。
	if seqs := recordSeqs(page.Entries); len(seqs) != 3 || seqs[0] != 1 || seqs[2] != 3 {
		t.Fatalf("旧游标未重新读到最新记录: %v", seqs)
	}
	if page.NextCursor != 3 {
		t.Fatalf("旧游标未回落到新来源末尾: %d", page.NextCursor)
	}
}

// recordSeqs 提取记录序号，便于断言增量顺序。
func recordSeqs(records []Record) []uint64 {
	// seqs 保存按输入顺序提取的序号。
	seqs := make([]uint64, 0, len(records))
	// record 是当前待提取序号的记录。
	for _, record := range records {
		seqs = append(seqs, record.Seq)
	}
	return seqs
}

// TestQueryLevelSourcesAndKeywordBoundaries 验证等级、来源与关键字的边界分支。
func TestQueryLevelSourcesAndKeywordBoundaries(t *testing.T) {
	// records 是包含两种等级与来源的测试记录。
	records := []Record{
		newRecord(1, LevelDebug, SourceHTTP, "调试记录"),
		newRecord(2, LevelError, SourceMTop, "错误记录"),
	}
	// source 是返回上述记录的测试来源。
	source := &fakeSource{snapshot: Snapshot{Records: records, OldestSeq: 1, NextSeq: 2, Capacity: 10}}
	// service 是本次测试的运行日志查询用例。
	service := NewService(source)
	// levelCases 覆盖全部合法等级过滤值，空值表示不过滤。
	levelCases := []struct {
		// name 是用例名称。
		name string
		// level 是等级过滤文本。
		level string
		// want 是期望命中的记录数。
		want int
	}{
		{name: "不过滤", level: "", want: 2},
		{name: "调试等级", level: "debug", want: 2},
		{name: "信息等级", level: "info", want: 1},
		{name: "警告等级", level: "warn", want: 1},
		{name: "错误等级", level: "error", want: 1},
	}
	// levelCase 是当前待验证的等级过滤用例。
	for _, levelCase := range levelCases {
		// page、err 分别是当前等级过滤的查询结果与错误。
		page, err := service.Query(Query{MinLevel: levelCase.level})
		if err != nil {
			t.Fatalf("%s 查询失败: %v", levelCase.name, err)
		}
		if len(page.Entries) != levelCase.want {
			t.Fatalf("%s 命中 %d 条 want %d", levelCase.name, len(page.Entries), levelCase.want)
		}
	}
	// emptySourcesPage 是只包含空白来源项时的结果，空白项应被忽略并视为不过滤。
	emptySourcesPage, err := service.Query(Query{Sources: []string{"  ", ""}})
	if err != nil {
		t.Fatalf("空白来源查询失败: %v", err)
	}
	if len(emptySourcesPage.Entries) != 2 {
		t.Fatalf("空白来源项应被忽略: %d", len(emptySourcesPage.Entries))
	}
	// unmatchedPage 是关键字没有任何命中时的结果。
	unmatchedPage, err := service.Query(Query{Keyword: "绝对不会命中的关键字"})
	if err != nil {
		t.Fatalf("无命中关键字查询失败: %v", err)
	}
	if len(unmatchedPage.Entries) != 0 {
		t.Fatalf("无命中关键字不应返回记录: %d", len(unmatchedPage.Entries))
	}
}
