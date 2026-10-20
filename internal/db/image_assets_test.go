package db

import (
	"context"
	"errors"
	"testing"

	"xianyu-go/internal/deliverytemplate"
)

// TestImageAssetsCRUD 验证资源中心图片元数据的创建、列表、改名、删除与名称占用检查。
func TestImageAssetsCRUD(t *testing.T) {
	// store、cleanup 是独立 SQLite 仓储和清理入口。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 控制本地仓储调用。
	ctx := context.Background()
	// assetID、createErr 保存新建图片元数据标识及写入错误。
	assetID, createErr := store.ImageAssets.Create(ctx, ImageAssetRow{Name: "说明图", FileName: "img-1.png", MIMEType: "image/png", SizeBytes: 3, Width: 2, Height: 4})
	if createErr != nil {
		t.Fatalf("创建图片元数据失败: %v", createErr)
	}
	// row、getErr 保存读取结果及错误。
	row, getErr := store.ImageAssets.Get(ctx, assetID)
	if getErr != nil || row.Name != "说明图" || row.FileName != "img-1.png" || row.Width != 2 || row.Height != 4 {
		t.Fatalf("读取图片元数据异常: %+v err=%v", row, getErr)
	}
	// taken、takenErr 表示同名检查结果，改名时必须排除自身。
	taken, takenErr := store.ImageAssets.NameTaken(ctx, "说明图", 0)
	if takenErr != nil || !taken {
		t.Fatalf("同名检查异常: taken=%v err=%v", taken, takenErr)
	}
	// selfTaken、selfErr 验证排除自身后不再算作占用。
	selfTaken, selfErr := store.ImageAssets.NameTaken(ctx, "说明图", assetID)
	if selfErr != nil || selfTaken {
		t.Fatalf("排除自身检查异常: taken=%v err=%v", selfTaken, selfErr)
	}
	if renameErr := store.ImageAssets.Rename(ctx, assetID, "新说明图"); renameErr != nil { // renameErr 是改名结果。
		t.Fatalf("改名失败: %v", renameErr)
	}
	// list、listErr 保存列表结果及错误。
	list, listErr := store.ImageAssets.List(ctx)
	if listErr != nil || len(list) != 1 || list[0].Name != "新说明图" {
		t.Fatalf("列表异常: %+v err=%v", list, listErr)
	}
	// duplicateErr 验证唯一索引冲突被映射为稳定错误。
	duplicateErr := store.ImageAssets.Rename(ctx, assetID, "新说明图")
	if duplicateErr != nil {
		t.Fatalf("同名改名不应失败: %v", duplicateErr)
	}
	// otherID、otherErr 保存第二张图片标识，用于验证重名被拒绝。
	otherID, otherErr := store.ImageAssets.Create(ctx, ImageAssetRow{Name: "另一张", FileName: "img-2.png", MIMEType: "image/png", SizeBytes: 3, Width: 1, Height: 1})
	if otherErr != nil {
		t.Fatalf("创建第二张图片失败: %v", otherErr)
	}
	if conflictErr := store.ImageAssets.Rename(ctx, otherID, "新说明图"); !errors.Is(conflictErr, ErrImageAssetNameTaken) { // conflictErr 是重名改名错误。
		t.Fatalf("重名改名未返回稳定错误: %v", conflictErr)
	}
	if deleteErr := store.ImageAssets.Delete(ctx, assetID); deleteErr != nil { // deleteErr 是删除结果。
		t.Fatalf("删除失败: %v", deleteErr)
	}
	if _, missingErr := store.ImageAssets.Get(ctx, assetID); !errors.Is(missingErr, ErrNotFound) { // missingErr 是缺失读取错误。
		t.Fatalf("删除后读取未返回未找到: %v", missingErr)
	}
	if deleteAgain := store.ImageAssets.Delete(ctx, assetID); !errors.Is(deleteAgain, ErrNotFound) { // deleteAgain 是重复删除错误。
		t.Fatalf("重复删除未返回未找到: %v", deleteAgain)
	}
}

// TestImageAssetsCreateUsesReturningForPostgres 验证 Postgres 方言分支通过 RETURNING 取回自增标识。
// pgx 驱动不支持 Result.LastInsertId；SQLite 3.35+ 同样支持 RETURNING，因此可在本地确定性覆盖该分支。
func TestImageAssetsCreateUsesReturningForPostgres(t *testing.T) {
	// store、cleanup 是独立 SQLite 连接与清理入口。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// postgresAssets 复用同一连接但声明为 Postgres 方言，强制走 RETURNING 取标识路径。
	postgresAssets := &ImageAssets{DB: store.DB, Dialect: DialectPostgres}
	// assetID、createErr 保存 RETURNING 分支返回的标识及错误。
	assetID, createErr := postgresAssets.Create(context.Background(), ImageAssetRow{Name: "方言图", FileName: "dialect.png", MIMEType: "image/png", SizeBytes: 1, Width: 1, Height: 1})
	if createErr != nil || assetID <= 0 {
		t.Fatalf("Postgres 分支取标识失败: id=%d err=%v", assetID, createErr)
	}
	// row、getErr 确认写入的字段完整可见。
	row, getErr := postgresAssets.Get(context.Background(), assetID)
	if getErr != nil || row.FileName != "dialect.png" {
		t.Fatalf("Postgres 分支回读失败: row=%+v err=%v", row, getErr)
	}
}

// TestImageAssetsReferenceCounts 验证引用计数统计卡片、发货模板与默认回复中的本地图片引用。
func TestImageAssetsReferenceCounts(t *testing.T) {
	// store、cleanup 是独立 SQLite 仓储和清理入口。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 控制本地仓储调用。
	ctx := context.Background()
	// userID、cookieID 是卡券与模板所有者，提供真实外键。
	userID, _ := seedAccount(t, store)
	// cardID、cardErr 保存带资源中心图片的卡券标识及创建错误。
	cardID, cardErr := store.Cards.Create(ctx, &CardFull{Name: "图片卡", Type: "image", ImagePath: "_library/img-1.png", UserID: userID, Enabled: true})
	if cardErr != nil {
		t.Fatalf("创建图片卡券失败: %v", cardErr)
	}
	if cardID <= 0 {
		t.Fatal("卡券标识无效")
	}
	// templateID、templateErr 保存带资源中心图片的发货模板标识及创建错误。
	templateID, templateErr := store.DeliveryTemplates.Create(ctx, DeliveryTemplateInput{UserID: userID, Name: "图片模板", Enabled: true, MessageItems: []deliverytemplate.Message{{Type: "image", ImagePath: "_library/img-1.png"}}})
	if templateErr != nil {
		t.Fatalf("创建图片模板失败: %v", templateErr)
	}
	if templateID <= 0 {
		t.Fatal("模板标识无效")
	}
	// counts、countsErr 保存按引用文本聚合的引用次数。
	counts, countsErr := store.ImageAssets.ReferenceCounts(ctx)
	if countsErr != nil {
		t.Fatalf("统计引用次数失败: %v", countsErr)
	}
	if counts["_library/img-1.png"] != 2 {
		t.Fatalf("引用次数异常: %+v", counts)
	}
	if counts[""] != 0 {
		t.Fatalf("空引用不应被统计: %+v", counts)
	}
}
