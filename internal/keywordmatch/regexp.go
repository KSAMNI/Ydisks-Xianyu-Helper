// Package keywordmatch 提供关键词回复的正则表达式编译规则。
// 保存时校验与运行时匹配必须共用同一入口，避免两侧对同一表达式给出不同结论。
package keywordmatch

import (
	"regexp"
	"sync"
)

// maxCachedPatterns 是已编译正则缓存的条目上限；超过后整体清空重建，防止无限增长。
const maxCachedPatterns = 1024

// compiledCache 保存表达式文本到已编译正则的映射；*regexp.Regexp 可被多个 goroutine 并发使用。
// compiledCacheMu 保护 compiledCache 的读写，持锁期间不做编译。
var (
	compiledCache   = make(map[string]*regexp.Regexp)
	compiledCacheMu sync.RWMutex
)

// CompileExpression 编译一条关键词正则表达式并默认忽略大小写。
// expression 是用户填写的原始表达式，首尾空白原样保留；使用 (?i) 前缀而不是外层分组包裹，
// 因此 "a)|(b" 这类不配对括号会编译失败，而不会逃出大小写不敏感范围。
// 表达式内部仍可用 (?-i) 等 RE2 内联标志显式覆盖。成功的结果会被缓存，失败不缓存。
func CompileExpression(expression string) (*regexp.Regexp, error) {
	// pattern 原样作为缓存键与编译输入：正则中的首尾空白属于表达式语义，裁剪会改写匹配结果（如转义空格）。
	pattern := expression
	compiledCacheMu.RLock()
	// cached 是命中的已编译正则。
	cached, found := compiledCache[pattern]
	compiledCacheMu.RUnlock()
	if found {
		return cached, nil
	}
	// compiled、err 保存本次编译结果。
	compiled, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return nil, err
	}
	compiledCacheMu.Lock()
	if len(compiledCache) >= maxCachedPatterns {
		compiledCache = make(map[string]*regexp.Regexp)
	}
	compiledCache[pattern] = compiled
	compiledCacheMu.Unlock()
	return compiled, nil
}
