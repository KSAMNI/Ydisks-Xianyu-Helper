package adapter

import (
	"context"
	"errors"

	imagelibraryapp "xianyu-go/internal/application/imagelibrary"
	"xianyu-go/internal/db"
)

// ImageLibraryRepository 将资源中心图片元数据仓储适配为应用层消费者定义的端口。
type ImageLibraryRepository struct {
	// store 保存数据库聚合入口，但只通过 ImageAssets 子仓储访问图片元数据。
	store *db.Store
}

// NewImageLibraryRepository 构造资源中心图片元数据适配器；装配缺失会在方法调用时返回错误。
func NewImageLibraryRepository(store *db.Store) *ImageLibraryRepository {
	return &ImageLibraryRepository{store: store}
}

// List 查询全部资源中心图片并转换为应用模型。
func (r *ImageLibraryRepository) List(ctx context.Context) ([]imagelibraryapp.Asset, error) {
	// err 表示资源中心图片适配器依赖缺失时的装配错误。
	if err := r.validate(); err != nil {
		return nil, err
	}
	// records 是按最近上传优先返回的数据库行。
	records, err := r.store.ImageAssets.List(ctx)
	if err != nil {
		return nil, err
	}
	// result 是与数据库模型解耦的应用层图片列表。
	result := make([]imagelibraryapp.Asset, 0, len(records))
	// record 是当前待转换的数据库图片行。
	for _, record := range records {
		result = append(result, imageLibraryApplicationModel(record))
	}
	return result, nil
}

// Get 读取单张图片元数据，并把数据库缺失错误映射为应用层稳定错误。
func (r *ImageLibraryRepository) Get(ctx context.Context, assetID int64) (imagelibraryapp.Asset, error) {
	// err 表示资源中心图片适配器依赖缺失时的装配错误。
	if err := r.validate(); err != nil {
		return imagelibraryapp.Asset{}, err
	}
	// record、err 保存数据库图片行及读取错误。
	record, err := r.store.ImageAssets.Get(ctx, assetID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return imagelibraryapp.Asset{}, imagelibraryapp.ErrNotFound
		}
		return imagelibraryapp.Asset{}, err
	}
	return imageLibraryApplicationModel(record), nil
}

// Create 将已校验的应用图片模型转换为数据库行并返回新标识。
func (r *ImageLibraryRepository) Create(ctx context.Context, asset imagelibraryapp.Asset) (int64, error) {
	// err 表示资源中心图片适配器依赖缺失时的装配错误。
	if err := r.validate(); err != nil {
		return 0, err
	}
	// assetID、createErr 保存新图片标识及写入错误；唯一冲突按名称占用返回。
	assetID, createErr := r.store.ImageAssets.Create(ctx, db.ImageAssetRow{
		Name: asset.Name, FileName: asset.FileName, MIMEType: asset.MIMEType,
		SizeBytes: asset.SizeBytes, Width: asset.Width, Height: asset.Height,
	})
	if errors.Is(createErr, db.ErrImageAssetNameTaken) {
		return 0, imagelibraryapp.ErrNameTaken
	}
	return assetID, createErr
}

// Rename 只更新图片展示名，保持磁盘文件名与既有业务引用不变。
func (r *ImageLibraryRepository) Rename(ctx context.Context, assetID int64, name string) error {
	// err 表示资源中心图片适配器依赖缺失时的装配错误。
	if err := r.validate(); err != nil {
		return err
	}
	// renameErr 保存数据库改名结果；唯一冲突与记录缺失分别映射为应用层稳定错误。
	renameErr := r.store.ImageAssets.Rename(ctx, assetID, name)
	if errors.Is(renameErr, db.ErrImageAssetNameTaken) {
		return imagelibraryapp.ErrNameTaken
	}
	if errors.Is(renameErr, db.ErrNotFound) {
		return imagelibraryapp.ErrNotFound
	}
	return renameErr
}

// Delete 删除图片元数据，并把记录缺失映射为应用层稳定错误。
func (r *ImageLibraryRepository) Delete(ctx context.Context, assetID int64) error {
	// err 表示资源中心图片适配器依赖缺失时的装配错误。
	if err := r.validate(); err != nil {
		return err
	}
	// deleteErr 保存数据库删除结果；记录已被并发删除时按缺失返回。
	deleteErr := r.store.ImageAssets.Delete(ctx, assetID)
	if errors.Is(deleteErr, db.ErrNotFound) {
		return imagelibraryapp.ErrNotFound
	}
	return deleteErr
}

// NameTaken 判断展示名是否已被其他图片占用，excludeID 用于改名时排除自身。
func (r *ImageLibraryRepository) NameTaken(ctx context.Context, name string, excludeID int64) (bool, error) {
	// err 表示资源中心图片适配器依赖缺失时的装配错误。
	if err := r.validate(); err != nil {
		return false, err
	}
	return r.store.ImageAssets.NameTaken(ctx, name, excludeID)
}

// ReferenceCounts 返回按引用文本聚合的业务引用次数。
func (r *ImageLibraryRepository) ReferenceCounts(ctx context.Context) (map[string]int, error) {
	// err 表示资源中心图片适配器依赖缺失时的装配错误。
	if err := r.validate(); err != nil {
		return nil, err
	}
	return r.store.ImageAssets.ReferenceCounts(ctx)
}

// validate 检查适配器是否具备 Store 与 ImageAssets 子仓储。
func (r *ImageLibraryRepository) validate() error {
	if r == nil || r.store == nil || r.store.ImageAssets == nil {
		return errors.New("资源中心图片数据库适配器未初始化")
	}
	return nil
}

// imageLibraryApplicationModel 将数据库图片行转换为基础设施无关的应用模型。
func imageLibraryApplicationModel(record db.ImageAssetRow) imagelibraryapp.Asset {
	return imagelibraryapp.Asset{
		ID: record.ID, Name: record.Name, FileName: record.FileName, Reference: libraryReference(record.FileName),
		MIMEType: record.MIMEType, SizeBytes: record.SizeBytes, Width: record.Width, Height: record.Height,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
}

// 编译期检查适配器提供应用层要求的全部元数据能力。
var _ imagelibraryapp.Repository = (*ImageLibraryRepository)(nil)
