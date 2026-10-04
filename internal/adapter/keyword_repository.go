package adapter

import (
	"context"
	"errors"

	keywordsapp "xianyu-go/internal/application/keywords"
	"xianyu-go/internal/db"
)

// KeywordRepository 将关键词和指定商品回复数据库操作适配为应用层 Port。
type KeywordRepository struct {
	// store 保存数据库聚合入口；敏感账号字段不会被本适配器读取。
	store *db.Store
}

// NewKeywordRepository 创建关键词数据库适配器。
func NewKeywordRepository(store *db.Store) *KeywordRepository {
	return &KeywordRepository{store: store}
}

// List 查询指定用户账号的关键词规则，并转换为应用模型。
func (r *KeywordRepository) List(ctx context.Context, userID int64, cookieID string) ([]keywordsapp.Keyword, error) {
	// err 表示账号归属校验失败，阻止跨用户读取规则。
	if err := r.authorize(ctx, userID, cookieID); err != nil {
		return nil, err
	}
	// rows 保存数据库返回的关键词规则行。
	rows, err := r.store.Keywords.AllRows(ctx, cookieID)
	if err != nil {
		return nil, err
	}
	// result 保存转换后的应用层关键词模型，不携带数据库对象。
	result := make([]keywordsapp.Keyword, 0, len(rows))
	// row 表示当前待转换的关键词数据库行。
	for _, row := range rows {
		result = append(result, keywordModel(row))
	}
	return result, nil
}

// Add 创建一条指定用户账号的关键词规则，并兼容只提供旧 Keyword 字段的调用方。
func (r *KeywordRepository) Add(ctx context.Context, userID int64, cookieID string, draft keywordsapp.Draft) (int64, error) {
	// err 表示账号归属校验失败。
	if err := r.authorize(ctx, userID, cookieID); err != nil {
		return 0, err
	}
	// expressions 保存新多表达式字段；旧调用方未提供时回退为单元素集合。
	expressions := draft.Expressions
	if len(expressions) == 0 {
		expressions = []string{draft.Keyword}
	}
	return r.store.Keywords.AddWithExpressions(ctx, cookieID, expressions, draft.Reply, draft.ItemID, draft.Type, draft.MatchType, draft.ImageURL)
}

// Replace 在数据库事务内转换现有规则并运行应用层 build，成功后才整体覆盖。
func (r *KeywordRepository) Replace(ctx context.Context, userID int64, cookieID string, build func([]keywordsapp.Keyword) ([]keywordsapp.Draft, error)) error {
	// err 表示账号归属校验失败。
	if err := r.authorize(ctx, userID, cookieID); err != nil {
		return err
	}
	return r.store.Keywords.ReplaceWithCurrent(ctx, cookieID, func(current []db.KeywordRow) ([]db.KeywordRow, error) {
		// models 是事务快照对应的非敏感应用模型。
		models := make([]keywordsapp.Keyword, 0, len(current))
		// row 是当前待转换的持久化快照。
		for _, row := range current {
			models = append(models, keywordModel(row))
		}
		// drafts、err 保存应用层决定的完整替换批次或兼容校验错误。
		drafts, err := build(models)
		if err != nil {
			return nil, err
		}
		// rows 是已验证批次的持久化字段集合，账号由事务边界统一指定。
		rows := make([]db.KeywordRow, 0, len(drafts))
		// draft 是当前应用层确认可替换的规则。
		for _, draft := range drafts {
			rows = append(rows, keywordDraftRow(draft))
		}
		return rows, nil
	})
}

// Update 在同一数据库事务内读取现有规则，运行 build 合并缺省字段并写回。
func (r *KeywordRepository) Update(ctx context.Context, userID int64, cookieID string, id int64, build func(keywordsapp.Keyword) (keywordsapp.Draft, error)) error {
	// err 表示账号归属校验失败。
	if err := r.authorize(ctx, userID, cookieID); err != nil {
		return err
	}
	// err 保存原子更新或应用回调的错误，不把语法拒绝变为基础设施故障。
	err := r.store.Keywords.UpdateWithCurrent(ctx, cookieID, id, func(current db.KeywordRow) (db.KeywordRow, error) {
		// draft、buildErr 保存继承缺省匹配字段后通过校验的规则。
		draft, buildErr := build(keywordModel(current))
		if buildErr != nil {
			return db.KeywordRow{}, buildErr
		}
		return keywordDraftRow(draft), nil
	})
	if errors.Is(err, db.ErrNotFound) {
		return keywordsapp.ErrNotFound
	}
	return err
}

// keywordDraftRow 将已通过应用校验的 draft 转为写入字段，不接受调用方覆盖账号或主键。
func keywordDraftRow(draft keywordsapp.Draft) db.KeywordRow {
	return db.KeywordRow{Keyword: draft.Keyword, Expressions: draft.Expressions, MatchType: draft.MatchType,
		Reply: draft.Reply, ItemID: draft.ItemID, Type: draft.Type, ImageURL: draft.ImageURL}
}

// DeleteByID 按 ID 删除指定用户账号的一条关键词规则。
func (r *KeywordRepository) DeleteByID(ctx context.Context, userID int64, cookieID string, id int64) error {
	// err 表示账号归属校验失败。
	if err := r.authorize(ctx, userID, cookieID); err != nil {
		return err
	}
	// err 表示数据库删除错误或目标规则不存在。
	err := r.store.Keywords.DeleteByID(ctx, cookieID, id)
	if errors.Is(err, db.ErrNotFound) {
		return keywordsapp.ErrNotFound
	}
	return err
}

// DeleteByIndex 按稳定 ID 顺序的零基索引删除关键词规则。
func (r *KeywordRepository) DeleteByIndex(ctx context.Context, userID int64, cookieID string, index int) error {
	// err 表示账号归属校验失败。
	if err := r.authorize(ctx, userID, cookieID); err != nil {
		return err
	}
	// err 表示数据库删除错误或索引没有对应规则。
	err := r.store.Keywords.DeleteByIndex(ctx, cookieID, index)
	if errors.Is(err, db.ErrNotFound) {
		return keywordsapp.ErrNotFound
	}
	return err
}

// ListItemReplies 查询当前用户所有账号的商品回复，并保持数据库返回顺序。
func (r *KeywordRepository) ListItemReplies(ctx context.Context, userID int64) ([]keywordsapp.ItemReply, error) {
	// err 表示适配器依赖或用户身份校验失败。
	if err := r.validateUser(userID); err != nil {
		return nil, err
	}
	// cookieIDs 保存当前用户拥有的账号标识，不包含账号凭证。
	cookieIDs, err := r.store.Cookies.ListOwnedIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	// result 保存跨账号聚合后的商品回复应用模型。
	result := make([]keywordsapp.ItemReply, 0)
	// cookieID 表示当前遍历的用户账号。
	for _, cookieID := range cookieIDs {
		// queryErr 表示当前账号商品回复读取失败。
		// rows 保存当前账号的商品回复数据库行。
		rows, queryErr := r.store.ItemReps.AllForUser(ctx, cookieID)
		if queryErr != nil {
			return nil, queryErr
		}
		// row 表示当前待转换的商品回复行。
		for _, row := range rows {
			result = append(result, itemReplyModel(row))
		}
	}
	return result, nil
}

// GetItemReply 读取指定用户账号和商品的商品回复。
func (r *KeywordRepository) GetItemReply(ctx context.Context, userID int64, cookieID, itemID string) (keywordsapp.ItemReply, error) {
	// err 表示账号归属校验失败。
	if err := r.authorize(ctx, userID, cookieID); err != nil {
		return keywordsapp.ItemReply{}, err
	}
	// row、err 保存商品回复数据库行及读取错误。
	row, err := r.store.ItemReps.Get(ctx, cookieID, itemID)
	if errors.Is(err, db.ErrNotFound) {
		return keywordsapp.ItemReply{}, keywordsapp.ErrNotFound
	}
	if err != nil {
		return keywordsapp.ItemReply{}, err
	}
	return itemReplyModel(*row), nil
}

// SetItemReply 覆盖指定用户账号和商品的商品回复。
func (r *KeywordRepository) SetItemReply(ctx context.Context, userID int64, cookieID, itemID, content string) error {
	// err 表示账号归属校验失败。
	if err := r.authorize(ctx, userID, cookieID); err != nil {
		return err
	}
	return r.store.ItemReps.Set(ctx, cookieID, itemID, content)
}

// DeleteItemReply 删除指定用户账号和商品的商品回复。
func (r *KeywordRepository) DeleteItemReply(ctx context.Context, userID int64, cookieID, itemID string) error {
	// err 表示账号归属校验失败。
	if err := r.authorize(ctx, userID, cookieID); err != nil {
		return err
	}
	return r.store.ItemReps.Delete(ctx, cookieID, itemID)
}

// authorize 只查询账号归属，不读取或解密任何凭证字段。
func (r *KeywordRepository) authorize(ctx context.Context, userID int64, cookieID string) error {
	// err 表示适配器依赖或用户身份校验失败。
	if err := r.validateUser(userID); err != nil {
		return err
	}
	if cookieID == "" {
		return keywordsapp.ErrInvalidInput
	}
	// owned、err 保存当前用户对账号的直接归属结果及查询错误。
	owned, err := r.store.Cookies.ExistsOwned(ctx, userID, cookieID)
	if err != nil {
		return err
	}
	if owned {
		return nil
	}
	// ownerID、err 保存账号所有者标识及读取错误，用于区分不存在和跨用户访问。
	ownerID, err := r.store.Cookies.GetOwnerID(ctx, cookieID)
	if errors.Is(err, db.ErrNotFound) {
		return keywordsapp.ErrNotFound
	}
	if err != nil {
		return err
	}
	if ownerID != userID {
		return keywordsapp.ErrForbidden
	}
	return nil
}

// validateUser 检查数据库适配器和用户身份是否可用。
func (r *KeywordRepository) validateUser(userID int64) error {
	if r == nil || r.store == nil || r.store.Cookies == nil || r.store.Keywords == nil || r.store.ItemReps == nil {
		return errors.New("关键词数据库适配器未初始化")
	}
	if userID <= 0 {
		return keywordsapp.ErrInvalidUser
	}
	return nil
}

// keywordModel 将数据库关键词行转换为应用模型。
func keywordModel(row db.KeywordRow) keywordsapp.Keyword {
	return keywordsapp.Keyword{ID: row.ID, CookieID: row.CookieID, Keyword: row.Keyword, Expressions: row.Expressions, MatchType: row.MatchType, Reply: row.Reply, ItemID: row.ItemID, Type: row.Type, ImageURL: row.ImageURL}
}

// itemReplyModel 将数据库商品回复行转换为应用模型。
func itemReplyModel(row db.ItemReply) keywordsapp.ItemReply {
	return keywordsapp.ItemReply{ItemID: row.ItemID, CookieID: row.CookieID, ReplyContent: row.ReplyContent}
}

var _ keywordsapp.Repository = (*KeywordRepository)(nil)
