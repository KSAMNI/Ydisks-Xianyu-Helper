package adapter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	imagelibraryapp "xianyu-go/internal/application/imagelibrary"
)

// TestImageLibraryStorageRoundTrip 验证资源中心图片文件保存、读取与删除的完整往返。
func TestImageLibraryStorageRoundTrip(t *testing.T) {
	// root 是当前测试独占的资源中心图片目录。
	root := t.TempDir()
	// storage 是被测文件存储。
	storage := NewImageLibraryStorage(root)
	// ctx 控制本地文件操作。
	ctx := context.Background()
	// fileName、saveErr 保存写入文件名及错误。
	fileName, saveErr := storage.Save(ctx, []byte("png-bytes"), "image/png")
	if saveErr != nil {
		t.Fatalf("保存图片失败: %v", saveErr)
	}
	if !strings.HasSuffix(fileName, ".png") || strings.ContainsAny(fileName, `/\`) {
		t.Fatalf("文件名不合法: %q", fileName)
	}
	// data、readErr 保存读取结果及错误。
	data, readErr := storage.Read(ctx, fileName)
	if readErr != nil || string(data) != "png-bytes" {
		t.Fatalf("读取图片异常: %q err=%v", string(data), readErr)
	}
	if removeErr := storage.Remove(ctx, fileName); removeErr != nil { // removeErr 是删除结果。
		t.Fatalf("删除图片失败: %v", removeErr)
	}
	if _, missingErr := storage.Read(ctx, fileName); !errors.Is(missingErr, imagelibraryapp.ErrNotFound) { // missingErr 是缺失读取错误。
		t.Fatalf("删除后读取未返回未找到: %v", missingErr)
	}
	if againErr := storage.Remove(ctx, fileName); againErr != nil { // againErr 验证重复删除幂等。
		t.Fatalf("重复删除应幂等: %v", againErr)
	}
}

// TestImageLibraryStorageRejectsInvalidInput 验证文件存储拒绝越界文件名、非法格式与超限内容。
func TestImageLibraryStorageRejectsInvalidInput(t *testing.T) {
	// storage 是被测文件存储。
	storage := NewImageLibraryStorage(t.TempDir())
	// ctx 控制本地文件操作。
	ctx := context.Background()
	if _, err := storage.Save(ctx, nil, "image/png"); err == nil { // err 应拒绝空内容。
		t.Fatal("空图片未被拒绝")
	}
	if _, err := storage.Save(ctx, []byte("x"), "image/webp"); err == nil { // err 应拒绝不支持的媒体类型。
		t.Fatal("不支持的媒体类型未被拒绝")
	}
	// invalid 保存必须被拒绝的文件名。
	invalid := []string{"", ".", "..", "../a.png", `a\b.png`, "sub/a.png", " C:1.png"}
	// name 是当前待校验文件名。
	for _, name := range invalid {
		if _, err := storage.Read(ctx, name); err == nil { // err 应拒绝非法文件名。
			t.Fatalf("非法文件名未被拒绝: %q", name)
		}
	}
	// emptyStorage 验证未配置目录时返回明确错误。
	emptyStorage := NewImageLibraryStorage("  ")
	if _, err := emptyStorage.Save(ctx, []byte("x"), "image/png"); err == nil { // err 应拒绝未配置目录。
		t.Fatal("未配置目录未被拒绝")
	}
	// cancelled、cancel 构造已取消的调用上下文，验证不访问文件系统。
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := storage.Save(cancelled, []byte("x"), "image/png"); err == nil { // err 应保留取消类别。
		t.Fatal("已取消的保存未被拒绝")
	}
}

// TestImageLibraryRepositoryCRUD 验证图片元数据适配器的创建、读取、改名、删除与引用计数。
func TestImageLibraryRepositoryCRUD(t *testing.T) {
	// store、cleanup 是独立 SQLite 仓储与清理入口。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是被测元数据适配器。
	repository := NewImageLibraryRepository(store)
	// ctx 控制本地仓储调用。
	ctx := context.Background()
	// assetID、createErr 保存新建图片标识及错误。
	assetID, createErr := repository.Create(ctx, imagelibraryapp.Asset{Name: "封面", FileName: "img-1.png", MIMEType: "image/png", SizeBytes: 4, Width: 2, Height: 2})
	if createErr != nil {
		t.Fatalf("创建图片失败: %v", createErr)
	}
	// asset、getErr 保存读取结果及错误。
	asset, getErr := repository.Get(ctx, assetID)
	if getErr != nil || asset.Name != "封面" || asset.Reference != "_library/img-1.png" {
		t.Fatalf("读取图片异常: %+v err=%v", asset, getErr)
	}
	// list、listErr 保存列表结果及错误。
	list, listErr := repository.List(ctx)
	if listErr != nil || len(list) != 1 {
		t.Fatalf("列表异常: %+v err=%v", list, listErr)
	}
	if taken, err := repository.NameTaken(ctx, "封面", 0); err != nil || !taken { // taken、err 验证名称占用。
		t.Fatalf("名称占用检查异常: %v %v", taken, err)
	}
	if renameErr := repository.Rename(ctx, assetID, "新封面"); renameErr != nil { // renameErr 是改名结果。
		t.Fatalf("改名失败: %v", renameErr)
	}
	if _, conflictErr := repository.Create(ctx, imagelibraryapp.Asset{Name: "新封面", FileName: "img-2.png", MIMEType: "image/png", SizeBytes: 4, Width: 2, Height: 2}); !errors.Is(conflictErr, imagelibraryapp.ErrNameTaken) { // conflictErr 验证重名映射。
		t.Fatalf("重名创建未返回名称占用: %v", conflictErr)
	}
	// counts、countsErr 保存引用次数统计结果。
	counts, countsErr := repository.ReferenceCounts(ctx)
	if countsErr != nil || len(counts) != 0 {
		t.Fatalf("空引用统计异常: %+v err=%v", counts, countsErr)
	}
	if deleteErr := repository.Delete(ctx, assetID); deleteErr != nil { // deleteErr 是删除结果。
		t.Fatalf("删除失败: %v", deleteErr)
	}
	if _, missingErr := repository.Get(ctx, assetID); !errors.Is(missingErr, imagelibraryapp.ErrNotFound) { // missingErr 验证缺失映射。
		t.Fatalf("缺失读取未返回未找到: %v", missingErr)
	}
	if deleteAgain := repository.Delete(ctx, assetID); !errors.Is(deleteAgain, imagelibraryapp.ErrNotFound) { // deleteAgain 验证重复删除映射。
		t.Fatalf("重复删除未返回未找到: %v", deleteAgain)
	}
}

// TestLocalReplyImageLoaderReadsLibraryReference 验证共享引用直接从资源中心目录读取，不需要账号私有目录。
func TestLocalReplyImageLoaderReadsLibraryReference(t *testing.T) {
	// root 是回复图片根目录，共享图片写入其保留子目录。
	root := t.TempDir()
	// libraryDir 是资源中心共享图片目录。
	libraryDir := filepath.Join(root, "_library")
	if err := os.MkdirAll(libraryDir, 0o755); err != nil { // err 是测试目录创建错误。
		t.Fatalf("创建共享目录失败: %v", err)
	}
	// pngBytes 是带合法 PNG 文件头的测试图片。
	pngBytes := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\nIDATx\x9cc\x00\x01\x00\x00\x05\x00\x01\r\n-\xb4\x00\x00\x00\x00IEND\xaeB`\x82")
	if err := os.WriteFile(filepath.Join(libraryDir, "img-1.png"), pngBytes, 0o644); err != nil { // err 是测试图片写入错误。
		t.Fatalf("写入共享图片失败: %v", err)
	}
	// loader 是被测本地图片加载器。
	loader := newLocalReplyImageLoader(root)
	// data、contentType、filename、loadErr 保存共享图片读取结果。
	data, contentType, filename, loadErr := loader(context.Background(), "acc1", "_library/img-1.png")
	if loadErr != nil || len(data) == 0 || contentType != "image/png" || filename != "img-1.png" {
		t.Fatalf("共享图片读取失败: contentType=%q filename=%q err=%v", contentType, filename, loadErr)
	}
	if _, _, _, missingErr := loader(context.Background(), "acc1", "_library/missing.png"); missingErr == nil { // missingErr 验证缺失共享图片被拒绝。
		t.Fatal("缺失共享图片未被拒绝")
	}
	if _, _, _, traversalErr := loader(context.Background(), "acc1", "_library/../secret.png"); traversalErr == nil { // traversalErr 验证共享引用不能越界。
		t.Fatal("共享引用越界未被拒绝")
	}
}
