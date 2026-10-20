package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// ErrImageAssetNameTaken 表示资源中心图片的显示名已被占用。
// 名称在数据库层由唯一索引保证，服务层在写入前也会做一次可读的预检查。
var ErrImageAssetNameTaken = errors.New("资源中心图片名称已存在")

// ImageAssetRow 是资源中心图片的元数据行。
// FileName 是不可变的磁盘文件名；业务配置通过 `_library/<file_name>` 引用它，因此改名不影响引用。
type ImageAssetRow struct {
	// ID 是图片的稳定持久化标识。
	ID int64
	// Name 是用户可编辑的展示名。
	Name string
	// FileName 是图片库目录内的实际文件名，写入后不再变化。
	FileName string
	// MIMEType 是按图片头识别出的媒体类型，只允许 png、jpeg、gif。
	MIMEType string
	// SizeBytes 是图片文件字节数，用于展示和上限复核。
	SizeBytes int64
	// Width 是图片像素宽度。
	Width int
	// Height 是图片像素高度。
	Height int
	// CreatedAt 是上传时间文本，保持数据库原样以避免方言时间解析差异。
	CreatedAt string
	// UpdatedAt 是最近改名时间文本。
	UpdatedAt string
}

// ImageAssets 提供资源中心图片元数据的窄持久化能力。
type ImageAssets struct {
	// DB 是共享连接池，只由本仓储执行图片元数据 SQL。
	DB *sql.DB
	// Dialect 标识当前数据库方言，用于占位符与唯一冲突识别。
	Dialect Dialect
}

// List 按最近上传优先返回全部资源中心图片。
func (a *ImageAssets) List(ctx context.Context) ([]ImageAssetRow, error) {
	// rows、err 保存图片元数据游标及查询错误。
	rows, err := a.DB.QueryContext(ctx, `SELECT id,name,file_name,mime_type,size_bytes,width,height,created_at,updated_at FROM image_assets ORDER BY created_at DESC,id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// result 汇总全部图片元数据，读取失败时不返回部分结果。
	result := make([]ImageAssetRow, 0, 16)
	for rows.Next() {
		// row 是当前扫描出的图片元数据。
		var row ImageAssetRow
		if err := rows.Scan(&row.ID, &row.Name, &row.FileName, &row.MIMEType, &row.SizeBytes, &row.Width, &row.Height, &row.CreatedAt, &row.UpdatedAt); err != nil { // err 是当前行扫描错误。
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// Get 读取单张图片元数据；记录缺失时返回 ErrNotFound。
func (a *ImageAssets) Get(ctx context.Context, assetID int64) (ImageAssetRow, error) {
	// row 是查询到的图片元数据。
	var row ImageAssetRow
	// err 是单行查询错误；无记录时映射为未找到。
	err := a.DB.QueryRowContext(ctx, `SELECT id,name,file_name,mime_type,size_bytes,width,height,created_at,updated_at FROM image_assets WHERE id=?`, assetID).
		Scan(&row.ID, &row.Name, &row.FileName, &row.MIMEType, &row.SizeBytes, &row.Width, &row.Height, &row.CreatedAt, &row.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ImageAssetRow{}, ErrNotFound
	}
	if err != nil {
		return ImageAssetRow{}, err
	}
	return row, nil
}

// Create 写入图片元数据并返回新标识；显示名或文件名重复时返回 ErrImageAssetNameTaken。
// PostgreSQL 不支持 LastInsertId，因此统一通过 insertReturningID 按方言取回自增标识。
func (a *ImageAssets) Create(ctx context.Context, row ImageAssetRow) (int64, error) {
	// assetID、err 保存新图片标识及写入错误。
	assetID, err := insertReturningID(ctx, a.DB, a.Dialect, `INSERT INTO image_assets (name,file_name,mime_type,size_bytes,width,height) VALUES (?,?,?,?,?,?)`,
		row.Name, row.FileName, row.MIMEType, row.SizeBytes, row.Width, row.Height)
	if err != nil {
		if isImageAssetUniqueViolation(err) {
			return 0, ErrImageAssetNameTaken
		}
		return 0, err
	}
	return assetID, nil
}

// Rename 只更新展示名，保持文件名与已有业务引用不变。
func (a *ImageAssets) Rename(ctx context.Context, assetID int64, name string) error {
	// result、err 保存改名结果及执行错误。
	result, err := a.DB.ExecContext(ctx, `UPDATE image_assets SET name=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, name, assetID)
	if err != nil {
		if isImageAssetUniqueViolation(err) {
			return ErrImageAssetNameTaken
		}
		return err
	}
	// affected、affectedErr 表示实际更新的行数；0 行说明图片已被并发删除。
	affected, affectedErr := result.RowsAffected()
	if affectedErr != nil {
		return affectedErr
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete 删除图片元数据；返回受影响行数供调用方区分已删除与并发缺失。
func (a *ImageAssets) Delete(ctx context.Context, assetID int64) error {
	// result、err 保存删除结果及执行错误。
	result, err := a.DB.ExecContext(ctx, `DELETE FROM image_assets WHERE id=?`, assetID)
	if err != nil {
		return err
	}
	// affected、affectedErr 表示实际删除行数；0 行说明图片已被并发删除。
	affected, affectedErr := result.RowsAffected()
	if affectedErr != nil {
		return affectedErr
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// NameTaken 判断展示名是否已被其他图片占用，excludeID 用于改名时排除自身。
func (a *ImageAssets) NameTaken(ctx context.Context, name string, excludeID int64) (bool, error) {
	// count 是满足名称条件且不是 excludeID 的记录数。
	var count int
	if err := a.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM image_assets WHERE name=? AND id<>?`, name, excludeID).Scan(&count); err != nil { // err 是名称占用查询错误。
		return false, err
	}
	return count > 0, nil
}

// ReferenceCounts 统计每张本地图片引用被卡片、发货模板和默认回复使用的次数。
// 返回键是配置中保存的原始引用文本；调用方按 `_library/<file_name>` 查找对应图片。
func (a *ImageAssets) ReferenceCounts(ctx context.Context) (map[string]int, error) {
	// rows、err 保存按引用文本聚合的统计游标及查询错误。
	rows, err := a.DB.QueryContext(ctx, `SELECT refs.ref, COUNT(*) FROM (
SELECT image_path AS ref FROM cards WHERE image_path<>''
UNION ALL SELECT image_path FROM delivery_template_messages WHERE COALESCE(image_path,'')<>''
UNION ALL SELECT reply_image_path FROM default_replies WHERE COALESCE(reply_image_path,'')<>''
UNION ALL SELECT reply_image_path FROM item_replay WHERE COALESCE(reply_image_path,'')<>''
) AS refs GROUP BY refs.ref`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// counts 保存引用文本到引用次数的映射。
	counts := make(map[string]int, 32)
	for rows.Next() {
		// reference 是配置中保存的图片引用文本；total 是它的引用次数。
		var reference string
		// total 是当前引用文本在全部业务表中的出现次数。
		var total int
		if err := rows.Scan(&reference, &total); err != nil { // err 是引用次数行扫描错误。
			return nil, err
		}
		counts[reference] = total
	}
	return counts, rows.Err()
}

// isImageAssetUniqueViolation 按方言识别唯一索引冲突，避免把并发改名失败当成基础设施故障。
func isImageAssetUniqueViolation(err error) bool {
	// message 是驱动返回的错误文本；三种方言使用不同的关键字表达唯一冲突。
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint") || strings.Contains(message, "duplicate entry") || strings.Contains(message, "duplicate key value")
}
