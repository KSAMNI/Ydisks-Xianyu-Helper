package keywords

import (
	"context"
	"strings"

	"xianyu-go/internal/replyimage"
)

// ItemReplyDraft 是商品默认图文修改输入；缺省图片字段保留，显式空串清除。
type ItemReplyDraft struct {
	// ReplyContent 是商品命中后的完整正文，空正文且无图片表示使用账号兜底。
	ReplyContent string
	// ReplyImageURL 是可选的网络图片来源修改。
	ReplyImageURL *string
	// ReplyImagePath 是可选的账号图片目录内相对路径修改。
	ReplyImagePath *string
}

// SetItemReplyDraft 校验 userID、cookieID 和 itemID 后，在事务中合并 draft 与已有商品配置。
func (s *Service) SetItemReplyDraft(ctx context.Context, userID int64, cookieID, itemID string, draft ItemReplyDraft) error {
	if err := s.validate(userID, cookieID); err != nil { // err 是依赖或调用方标识错误。
		return err
	}
	if strings.TrimSpace(itemID) == "" {
		return &ValidationError{Message: "商品ID不能为空"}
	}
	return s.repository.UpdateItemReply(ctx, userID, cookieID, itemID, func(current ItemReply) (ItemReply, error) {
		// current 是账号事务中的最新配置，旧客户端未提供图片字段时保持原值。
		current.ReplyContent = draft.ReplyContent
		if draft.ReplyImageURL != nil {
			current.ReplyImageURL = *draft.ReplyImageURL
		}
		if draft.ReplyImagePath != nil {
			current.ReplyImagePath = *draft.ReplyImagePath
		}
		if err := replyimage.ValidateSource(current.ReplyImageURL, current.ReplyImagePath); err != nil { // err 只包含可安全展示的输入提示。
			return ItemReply{}, &ValidationError{Message: err.Error()}
		}
		return current, nil
	})
}
