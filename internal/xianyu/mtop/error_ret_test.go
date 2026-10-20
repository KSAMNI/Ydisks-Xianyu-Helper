package mtop

import (
	"errors"
	"fmt"
	"testing"
)

// TestMTopErrorRetCopiesRetFromWrappedError 覆盖从错误链取最后一次平台 ret：返回副本、普通错误和 nil 返回 nil。
func TestMTopErrorRetCopiesRetFromWrappedError(t *testing.T) {
	// responseErr 是携带平台 ret 的统一 MTOP 响应错误。
	responseErr := &MTopResponseError{API: "mtop.x", Ret: []string{"FAIL_SYS_TOKEN_EXOIRED::令牌过期"}}
	// got 是从包装错误链取得的 ret 副本。
	got := mtopErrorRet(fmt.Errorf("wrap: %w", responseErr))
	if len(got) != 1 || got[0] != responseErr.Ret[0] {
		t.Fatalf("got=%v", got)
	}
	got[0] = "mutated"
	if responseErr.Ret[0] == "mutated" {
		t.Fatal("返回值必须是副本，不能与原错误共享底层数组")
	}
	if mtopErrorRet(errors.New("plain")) != nil || mtopErrorRet(nil) != nil {
		t.Fatal("非 MTOP 错误应返回 nil")
	}
}
