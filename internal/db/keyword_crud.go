package db

import (
	"context"
	"database/sql"
	"errors"
)

// KeywordRow 是完整的非敏感关键词持久化记录，不携带账号凭证。
type KeywordRow struct {
	// ID 是关键词规则主键。
	ID int64 `json:"id"`
	// CookieID 是规则所属账号标识。
	CookieID string
	// Keyword 是第一条表达式的兼容单值字段。
	Keyword string
	// Expressions 是规则保存的完整表达式集合。
	Expressions []string
	// MatchType 是 contains 或 regexp 匹配模式。
	MatchType string
	// Reply 是文字回复内容。
	Reply string
	// ItemID 是逗号分隔的商品范围字段。
	ItemID string
	// Type 是 text 或 image 回复类型。
	Type string
	// ImageURL 是图片回复地址。
	ImageURL string
}

// keywordQueryer 是查询规则快照所需的最小数据库能力，允许复用普通连接和调用方事务。
type keywordQueryer interface {
	// QueryContext 在 ctx 取消时终止参数化 query，返回由调用方关闭的结果集。
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// AllRows 返回 cookieID 下按主键排序的完整规则；查询不读取账号凭证。
func (k *Keywords) AllRows(ctx context.Context, cookieID string) ([]KeywordRow, error) {
	return readKeywordRows(ctx, k.DB, cookieID)
}

// readKeywordRows 从 queryer 读取账号规则；在事务中使用时快照受账号行锁保护。
func readKeywordRows(ctx context.Context, queryer keywordQueryer, cookieID string) ([]KeywordRow, error) {
	// rows、err 保存当前账号的关键词结果集及读取错误。
	rows, err := queryer.QueryContext(ctx,
		`SELECT id, keyword, reply, COALESCE(item_id,''), COALESCE(type,'text'), COALESCE(image_url,''),
			COALESCE(keyword_expressions,''), COALESCE(match_type,'contains')
		 FROM keywords WHERE cookie_id=? ORDER BY id`, cookieID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// result 保存完整规则集合，空结果保持原有 nil 语义。
	var result []KeywordRow
	for rows.Next() {
		// row 是当前持久化行，扫描后附加账号标识。
		var row KeywordRow
		// rawExpressions 是尚未解码的表达式 JSON。
		var rawExpressions string
		// err 是单行字段扫描失败，任何失败都终止当前快照读取。
		if err := rows.Scan(&row.ID, &row.Keyword, &row.Reply, &row.ItemID, &row.Type, &row.ImageURL, &rawExpressions, &row.MatchType); err != nil {
			return nil, err
		}
		row.MatchType = normalizeKeywordMatchType(row.MatchType)
		row.Expressions = decodeKeywordExpressions(rawExpressions, row.Keyword, row.MatchType)
		if len(row.Expressions) > 0 {
			row.Keyword = row.Expressions[0]
		}
		row.CookieID = cookieID
		result = append(result, row)
	}
	return result, rows.Err()
}

// beginKeywordWrite 开启由调用方提交或回滚的事务，并只锁定 cookieID 的非敏感账号行。
// 同账号的更新和批量替换串行读取当前配置；SQLite 先取得写锁，其他方言使用 FOR UPDATE。
func (k *Keywords) beginKeywordWrite(ctx context.Context, cookieID string) (*sql.Tx, error) {
	// tx、err 保存账号级关键词事务及开启错误。
	tx, err := k.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	// query 只读取账号标识，不接触 Cookie、Token 或加密元数据。
	query := `SELECT id FROM cookies WHERE id=?`
	if k.Dialect == DialectMySQL || k.Dialect == DialectPostgres {
		query += ` FOR UPDATE`
	} else {
		// lockErr 保存 SQLite 写锁申请错误，避免读快照后升级锁与其他写入竞争。
		if _, lockErr := tx.ExecContext(ctx, `UPDATE cookies SET id=id WHERE id=?`, cookieID); lockErr != nil {
			_ = tx.Rollback()
			return nil, lockErr
		}
	}
	// existingID 是锁定的账号标识，存在性确认失败时不允许继续写入。
	var existingID string
	// err 保存行锁查询错误，缺少账号时回滚并返回稳定的不存在错误。
	if err := tx.QueryRowContext(ctx, query, cookieID).Scan(&existingID); err != nil {
		_ = tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return tx, nil
}

// UpdateByID 完整覆盖 row 所指规则；内部调用方已明确提供最终字段。
func (k *Keywords) UpdateByID(ctx context.Context, row KeywordRow) error {
	return k.UpdateWithCurrent(ctx, row.CookieID, row.ID, func(KeywordRow) (KeywordRow, error) {
		return row, nil
	})
}

// UpdateWithCurrent 在账号事务内读取 id 对应规则，由 build 合并并校验后原子写回。
// build 只进行内存计算，不得调用外部 I/O 或重入数据库；错误原样返回并回滚，身份字段不可被回调改写。
func (k *Keywords) UpdateWithCurrent(ctx context.Context, cookieID string, id int64, build func(KeywordRow) (KeywordRow, error)) error {
	// tx、err 保存当前账号的互斥写事务。
	tx, err := k.beginKeywordWrite(ctx, cookieID)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// rows、err 保存受锁保护的当前规则快照。
	rows, err := readKeywordRows(ctx, tx, cookieID)
	if err != nil {
		return err
	}
	// current 是待更新的账号内规则，其他账号的相同 ID 不会进入结果集。
	for _, current := range rows {
		if current.ID != id {
			continue
		}
		// row、buildErr 保存应用层完成合并和校验后的结果。
		row, buildErr := build(current)
		if buildErr != nil {
			return buildErr
		}
		// keyword、expressionsJSON、matchType 是最终持久化字段，正则保留原文。
		keyword, expressionsJSON, matchType := keywordPersistenceFields(row.Keyword, row.Expressions, row.MatchType)
		if row.Type == "" {
			row.Type = "text"
		}
		// updateErr 保存 SQL 写入错误；行已在同一事务确认存在，无变化更新也视为成功。
		if _, updateErr := tx.ExecContext(ctx, `UPDATE keywords
			SET keyword=?,reply=?,item_id=?,type=?,image_url=?,keyword_expressions=?,match_type=?
			WHERE id=? AND cookie_id=?`, keyword, row.Reply, nullable(row.ItemID), row.Type, nullable(row.ImageURL), expressionsJSON, matchType, id, cookieID); updateErr != nil {
			return updateErr
		}
		return tx.Commit()
	}
	return ErrNotFound
}

// Add 创建 cookieID 下的旧格式包含匹配规则，并返回生成的主键。
func (k *Keywords) Add(ctx context.Context, cookieID, keyword, reply, itemID, kwType, imageURL string) (int64, error) {
	return k.AddWithExpressions(ctx, cookieID, []string{keyword}, reply, itemID, kwType, "contains", imageURL)
}

// AddWithExpressions 写入 cookieID 的完整匹配配置和回复字段，expressions 按 OR 语义共享一条规则。
func (k *Keywords) AddWithExpressions(ctx context.Context, cookieID string, expressions []string, reply, itemID, kwType, matchType, imageURL string) (int64, error) {
	// firstKeyword 保存兼容旧字段的首表达式。
	firstKeyword := ""
	if len(expressions) > 0 {
		firstKeyword = expressions[0]
	}
	// keyword、expressionsJSON、normalizedMatchType 保存数据库写入字段。
	keyword, expressionsJSON, normalizedMatchType := keywordPersistenceFields(firstKeyword, expressions, matchType)
	if kwType == "" {
		kwType = "text"
	}
	// tx、err 让新增和读取当前配置后的批量替换使用相同账号锁。
	tx, err := k.beginKeywordWrite(ctx, cookieID)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// id、insertErr 保存新增规则的主键及数据库错误。
	id, insertErr := insertReturningID(ctx, tx, k.Dialect,
		`INSERT INTO keywords (cookie_id, keyword, reply, item_id, type, image_url, keyword_expressions, match_type) VALUES (?,?,?,?,?,?,?,?)`,
		cookieID, keyword, reply, nullable(itemID), kwType, nullable(imageURL), expressionsJSON, normalizedMatchType)
	if insertErr != nil {
		return 0, insertErr
	}
	// err 保存提交失败，未提交的主键不能作为成功结果返回。
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// ReplaceForCookie 原子覆盖 cookieID 的全部关键词，rows 是内部调用方明确提供的完整目标集合。
func (k *Keywords) ReplaceForCookie(ctx context.Context, cookieID string, rows []KeywordRow) error {
	return k.ReplaceWithCurrent(ctx, cookieID, func([]KeywordRow) ([]KeywordRow, error) {
		return rows, nil
	})
}

// ReplaceWithCurrent 在账号事务内把现有规则交给 build，校验成功后才删除和重建。
// build 不得进行外部 I/O；任一校验、插入或提交错误使整个替换失败，原规则保持不变。
func (k *Keywords) ReplaceWithCurrent(ctx context.Context, cookieID string, build func([]KeywordRow) ([]KeywordRow, error)) error {
	// tx、err 保存账号级写事务，阻止兼容校验和删除之间被其他更新插入。
	tx, err := k.beginKeywordWrite(ctx, cookieID)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// current、err 保存事务内的现有规则快照。
	current, err := readKeywordRows(ctx, tx, cookieID)
	if err != nil {
		return err
	}
	// rows、err 保存应用层确认不会丢失兼容信息的完整替换集合。
	rows, err := build(current)
	if err != nil {
		return err
	}
	// err 保存清理旧规则失败，任何错误都会回滚整批操作。
	if _, err := tx.ExecContext(ctx, `DELETE FROM keywords WHERE cookie_id=?`, cookieID); err != nil {
		return err
	}
	// row 是当前待重建规则，不允许请求改变所属账号。
	for _, row := range rows {
		// keyword、expressionsJSON、matchType 保存当前规则的表达式持久化字段。
		keyword, expressionsJSON, matchType := keywordPersistenceFields(row.Keyword, row.Expressions, row.MatchType)
		if row.Type == "" {
			row.Type = "text"
		}
		// insertErr 保存当前插入失败原因；失败时由事务回滚全部旧数据删除。
		if _, insertErr := tx.ExecContext(ctx,
			`INSERT INTO keywords (cookie_id, keyword, reply, item_id, type, image_url, keyword_expressions, match_type) VALUES (?,?,?,?,?,?,?,?)`,
			cookieID, keyword, row.Reply, nullable(row.ItemID), row.Type, nullable(row.ImageURL), expressionsJSON, matchType); insertErr != nil {
			return insertErr
		}
	}
	return tx.Commit()
}

// DeleteByID 删除 cookieID 下的 id，目标不存在时返回 ErrNotFound。
func (k *Keywords) DeleteByID(ctx context.Context, cookieID string, id int64) error {
	// tx、err 与更新共享账号锁，避免目标在更新快照读取后被删除。
	tx, err := k.beginKeywordWrite(ctx, cookieID)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// result、err 保存账号隔离删除的执行结果。
	result, err := tx.ExecContext(ctx, `DELETE FROM keywords WHERE id=? AND cookie_id=?`, id, cookieID)
	if err != nil {
		return err
	}
	// affected、rowsErr 用于区分删除成功与账号内目标不存在。
	affected, rowsErr := result.RowsAffected()
	if rowsErr != nil {
		return rowsErr
	}
	if affected == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// DeleteByIndex 按 cookieID 内主键升序的零基 index 删除规则，保持历史索引接口兼容。
func (k *Keywords) DeleteByIndex(ctx context.Context, cookieID string, index int) error {
	// tx、err 保证索引解析和删除不会与同账号批量替换交错。
	tx, err := k.beginKeywordWrite(ctx, cookieID)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// rows、err 保存该账号的有序主键列表。
	rows, err := tx.QueryContext(ctx, `SELECT id FROM keywords WHERE cookie_id=? ORDER BY id`, cookieID)
	if err != nil {
		return err
	}
	defer rows.Close()
	// ids 保存全部主键，消费完结果集后再执行删除。
	var ids []int64
	for rows.Next() {
		// id 是当前扫描到的账号内规则标识。
		var id int64
		// err 保存当前主键的扫描错误，禁止使用不完整索引列表删除。
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	// err 保存结果集遍历错误，读取不完整时拒绝执行删除。
	if err := rows.Err(); err != nil {
		return err
	}
	if index < 0 || index >= len(ids) {
		return ErrNotFound
	}
	// err 保存索引定位后的删除错误，成功时才提交当前事务。
	if _, err := tx.ExecContext(ctx, `DELETE FROM keywords WHERE id=? AND cookie_id=?`, ids[index], cookieID); err != nil {
		return err
	}
	return tx.Commit()
}
