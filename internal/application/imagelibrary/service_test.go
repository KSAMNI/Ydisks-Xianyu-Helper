package imagelibrary

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeRepository 是资源中心图片元数据端口的测试替身，按资产标识保存内存记录。
type fakeRepository struct {
	// assets 保存按标识索引的图片元数据。
	assets map[int64]Asset
	// nextID 是下一个分配的图片标识。
	nextID int64
	// counts 保存引用文本到业务引用次数的映射。
	counts map[string]int
	// createErr 表示创建元数据时应返回的错误。
	createErr error
	// renameErr 表示改名时应返回的错误。
	renameErr error
	// deleteErr 表示删除元数据时应返回的错误。
	deleteErr error
	// listErr 表示列表查询时应返回的错误。
	listErr error
	// countsErr 表示引用次数查询时应返回的错误。
	countsErr error
}

// List 返回当前保存的全部图片元数据。
func (r *fakeRepository) List(context.Context) ([]Asset, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	// result 是按标识顺序收集的图片元数据。
	result := make([]Asset, 0, len(r.assets))
	for id := int64(1); id < r.nextID; id++ { // id 是当前遍历的图片标识。
		// asset、ok 是当前标识对应的图片及其是否存在。
		asset, ok := r.assets[id]
		if ok {
			result = append(result, asset)
		}
	}
	return result, nil
}

// Get 按标识读取图片元数据，缺失时返回 ErrNotFound。
func (r *fakeRepository) Get(_ context.Context, assetID int64) (Asset, error) {
	// asset、ok 是目标图片及其是否存在。
	asset, ok := r.assets[assetID]
	if !ok {
		return Asset{}, ErrNotFound
	}
	return asset, nil
}

// Create 保存图片元数据并返回新标识。
func (r *fakeRepository) Create(_ context.Context, asset Asset) (int64, error) {
	if r.createErr != nil {
		return 0, r.createErr
	}
	r.nextID++
	asset.ID = r.nextID
	r.assets[asset.ID] = asset
	return asset.ID, nil
}

// Rename 只更新展示名。
func (r *fakeRepository) Rename(_ context.Context, assetID int64, name string) error {
	if r.renameErr != nil {
		return r.renameErr
	}
	// asset、ok 是目标图片及其是否存在。
	asset, ok := r.assets[assetID]
	if !ok {
		return ErrNotFound
	}
	asset.Name = name
	r.assets[assetID] = asset
	return nil
}

// Delete 删除图片元数据。
func (r *fakeRepository) Delete(_ context.Context, assetID int64) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	delete(r.assets, assetID)
	return nil
}

// NameTaken 判断展示名是否已被其他图片占用。
func (r *fakeRepository) NameTaken(_ context.Context, name string, excludeID int64) (bool, error) {
	for id, asset := range r.assets { // id 是当前图片标识；asset 是当前图片元数据。
		if id != excludeID && asset.Name == name {
			return true, nil
		}
	}
	return false, nil
}

// ReferenceCounts 返回引用文本到业务引用次数的映射。
func (r *fakeRepository) ReferenceCounts(context.Context) (map[string]int, error) {
	if r.countsErr != nil {
		return nil, r.countsErr
	}
	if r.counts == nil {
		return map[string]int{}, nil
	}
	return r.counts, nil
}

// fakeStorage 是资源中心图片文件端口的测试替身，按文件名保存内存字节。
type fakeStorage struct {
	// files 保存文件名到图片字节的映射。
	files map[string][]byte
	// nextName 是下一次保存返回的文件名。
	nextName string
	// saveErr 表示保存文件时应返回的错误。
	saveErr error
	// removeErr 表示删除文件时应返回的错误。
	removeErr error
	// removed 保存被删除的文件名，便于断言清理行为。
	removed []string
}

// Save 保存图片字节并返回预设文件名。
func (s *fakeStorage) Save(_ context.Context, data []byte, _ string) (string, error) {
	if s.saveErr != nil {
		return "", s.saveErr
	}
	if s.files == nil {
		s.files = map[string][]byte{}
	}
	// name 是本次保存使用的文件名。
	name := s.nextName
	if name == "" {
		name = "img-test.png"
	}
	s.files[name] = append([]byte(nil), data...)
	return name, nil
}

// Read 读取图片字节，缺失时返回 ErrNotFound。
func (s *fakeStorage) Read(_ context.Context, fileName string) ([]byte, error) {
	// data、ok 是目标文件内容及其是否存在。
	data, ok := s.files[fileName]
	if !ok {
		return nil, ErrNotFound
	}
	return data, nil
}

// Remove 删除图片文件并记录被删除的文件名。
func (s *fakeStorage) Remove(_ context.Context, fileName string) error {
	if s.removeErr != nil {
		return s.removeErr
	}
	s.removed = append(s.removed, fileName)
	delete(s.files, fileName)
	return nil
}

// newTestService 构造带固定时钟的资源中心服务，返回服务与两个测试替身。
func newTestService() (*Service, *fakeRepository, *fakeStorage) {
	// repository 是图片元数据测试替身。
	repository := &fakeRepository{assets: map[int64]Asset{}, nextID: 0}
	// storage 是图片文件测试替身。
	storage := &fakeStorage{files: map[string][]byte{}}
	// service 是被测应用服务，固定时钟保证默认名称可断言。
	service := NewService(repository, storage)
	service.now = func() time.Time { return time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC) }
	return service, repository, storage
}

// TestUploadCreatesAssetWithDefaultName 验证上传会生成默认名称、稳定引用与引用计数。
func TestUploadCreatesAssetWithDefaultName(t *testing.T) {
	// service、repository、storage 是被测服务与测试替身。
	service, _, storage := newTestService()
	// asset、err 保存上传结果及错误。
	asset, err := service.Upload(context.Background(), UploadInput{Data: []byte("png"), MIMEType: "image/png", Width: 10, Height: 20})
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	if asset.Name != "图片 20261010-120000" || asset.Reference != "_library/img-test.png" || asset.SizeBytes != 3 || asset.UsageCount != 0 {
		t.Fatalf("上传结果异常: %+v", asset)
	}
	if string(storage.files["img-test.png"]) != "png" {
		t.Fatalf("文件未写入: %+v", storage.files)
	}
}

// TestUploadAppendsSuffixOnNameConflict 验证重名上传自动追加序号而不是直接失败。
func TestUploadAppendsSuffixOnNameConflict(t *testing.T) {
	// service、repository、_ 是被测服务与测试替身。
	service, repository, _ := newTestService()
	repository.assets[1] = Asset{ID: 1, Name: "说明图", Reference: "_library/old.png"}
	repository.nextID = 1
	// asset、err 保存重名上传结果及错误。
	asset, err := service.Upload(context.Background(), UploadInput{Name: " 说明图 ", Data: []byte("x"), MIMEType: "image/jpeg", Width: 1, Height: 1})
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	if asset.Name != "说明图 (2)" {
		t.Fatalf("重名未追加序号: %+v", asset)
	}
}

// TestUploadRejectsInvalidPayload 验证上传会拒绝非法格式、空内容和超限尺寸。
func TestUploadRejectsInvalidPayload(t *testing.T) {
	// service、_、_ 是被测服务与测试替身。
	service, _, _ := newTestService()
	// invalid 保存必须被拒绝的上传输入。
	invalid := []UploadInput{
		{Data: nil, MIMEType: "image/png", Width: 1, Height: 1},
		{Data: []byte("x"), MIMEType: "image/webp", Width: 1, Height: 1},
		{Data: []byte("x"), MIMEType: "image/png", Width: 0, Height: 1},
		{Data: make([]byte, MaxImageBytes+1), MIMEType: "image/png", Width: 1, Height: 1},
	}
	// input 是当前待校验的上传输入。
	for _, input := range invalid {
		// err 是本次上传应返回的校验错误。
		var validationErr *ValidationError
		if _, err := service.Upload(context.Background(), input); !errors.As(err, &validationErr) { // err 是本次上传应返回的校验错误。
			t.Fatalf("非法上传未被拒绝: %+v err=%v", input, err)
		}
	}
}

// TestUploadCleansUpFileWhenMetadataFails 验证元数据写入失败时删除刚写入的文件。
func TestUploadCleansUpFileWhenMetadataFails(t *testing.T) {
	// service、repository、storage 是被测服务与测试替身。
	service, repository, storage := newTestService()
	repository.createErr = errors.New("db down")
	if _, err := service.Upload(context.Background(), UploadInput{Data: []byte("x"), MIMEType: "image/gif", Width: 1, Height: 1}); err == nil { // err 是元数据失败时应返回的错误。
		t.Fatal("元数据失败应返回错误")
	}
	if len(storage.removed) != 1 || len(storage.files) != 0 {
		t.Fatalf("未清理半成品文件: %+v", storage)
	}
}

// TestRenameValidatesNameAndConflict 验证改名会裁剪空白、拒绝非法名称并检查重名。
func TestRenameValidatesNameAndConflict(t *testing.T) {
	// service、repository、_ 是被测服务与测试替身。
	service, repository, _ := newTestService()
	repository.assets[1] = Asset{ID: 1, Name: "旧名", Reference: "_library/a.png"}
	repository.assets[2] = Asset{ID: 2, Name: "已占用", Reference: "_library/b.png"}
	repository.nextID = 3
	// asset、err 保存改名结果及错误。
	asset, err := service.Rename(context.Background(), 1, "  新名  ")
	if err != nil || asset.Name != "新名" {
		t.Fatalf("改名失败: %+v err=%v", asset, err)
	}
	if _, err := service.Rename(context.Background(), 1, "已占用"); !errors.Is(err, ErrNameTaken) { // err 是重名改名错误。
		t.Fatalf("重名未拒绝: %v", err)
	}
	if _, err := service.Rename(context.Background(), 1, strings.Repeat("长", MaxNameRunes+1)); err == nil { // err 是超长名称校验错误。
		t.Fatal("超长名称未拒绝")
	}
	if _, err := service.Rename(context.Background(), 1, "  "); err == nil { // err 是空名称校验错误。
		t.Fatal("空名称未拒绝")
	}
	if _, err := service.Rename(context.Background(), 99, "新名"); !errors.Is(err, ErrNotFound) { // err 是缺失图片改名错误。
		t.Fatalf("缺失图片未返回未找到: %v", err)
	}
}

// TestDeleteRequiresForceWhenReferenced 验证仍被引用的图片需要显式确认才能删除。
func TestDeleteRequiresForceWhenReferenced(t *testing.T) {
	// service、repository、storage 是被测服务与测试替身。
	service, repository, storage := newTestService()
	repository.assets[1] = Asset{ID: 1, Name: "被引用", FileName: "a.png", Reference: "_library/a.png"}
	repository.nextID = 2
	repository.counts = map[string]int{"_library/a.png": 2}
	storage.files["a.png"] = []byte("x")
	if err := service.Delete(context.Background(), 1, false); !errors.Is(err, ErrImageInUse) { // err 是被引用图片删除错误。
		t.Fatalf("被引用图片未阻止删除: %v", err)
	}
	if err := service.Delete(context.Background(), 1, true); err != nil { // err 是强制删除结果。
		t.Fatalf("强制删除失败: %v", err)
	}
	if len(storage.files) != 0 || len(repository.assets) != 0 {
		t.Fatalf("删除未清理文件与元数据: %+v %+v", storage.files, repository.assets)
	}
}

// TestDeleteKeepsMetadataWhenFileRemovalFails 验证文件删除失败时保留元数据，便于用户重试。
func TestDeleteKeepsMetadataWhenFileRemovalFails(t *testing.T) {
	// service、repository、storage 是被测服务与测试替身。
	service, repository, storage := newTestService()
	repository.assets[1] = Asset{ID: 1, Name: "文件异常", FileName: "a.png", Reference: "_library/a.png"}
	repository.nextID = 2
	storage.removeErr = errors.New("io error")
	if err := service.Delete(context.Background(), 1, false); err == nil { // err 是文件删除失败错误。
		t.Fatal("文件删除失败应返回错误")
	}
	if len(repository.assets) != 1 {
		t.Fatal("文件删除失败不应删除元数据")
	}
}

// TestContentAndListReturnUsageCount 验证内容读取返回已校验字节，列表附带引用次数。
func TestContentAndListReturnUsageCount(t *testing.T) {
	// service、repository、storage 是被测服务与测试替身。
	service, repository, storage := newTestService()
	repository.assets[1] = Asset{ID: 1, Name: "封面", FileName: "a.png", Reference: "_library/a.png", MIMEType: "image/png"}
	repository.nextID = 2
	repository.counts = map[string]int{"_library/a.png": 3}
	storage.files["a.png"] = []byte("png")
	// content、err 保存图片内容读取结果及错误。
	content, err := service.Content(context.Background(), 1)
	if err != nil || string(content.Data) != "png" || content.MIMEType != "image/png" {
		t.Fatalf("内容读取失败: %+v err=%v", content, err)
	}
	// assets、listErr 保存附带引用次数的图片列表。
	assets, listErr := service.List(context.Background())
	if listErr != nil || len(assets) != 1 || assets[0].UsageCount != 3 {
		t.Fatalf("列表引用次数异常: %+v err=%v", assets, listErr)
	}
	if _, err := service.Content(context.Background(), 99); !errors.Is(err, ErrNotFound) { // err 是缺失内容读取错误。
		t.Fatalf("缺失内容未返回未找到: %v", err)
	}
}

// TestServiceRejectsMissingPorts 验证装配不完整时所有用例返回明确错误。
func TestServiceRejectsMissingPorts(t *testing.T) {
	// broken 是缺少存储端口的服务实例。
	broken := NewService(&fakeRepository{}, nil)
	if _, err := broken.List(context.Background()); err == nil { // err 是缺少端口时的列表错误。
		t.Fatal("缺少端口时列表应失败")
	}
	// empty 是零值服务，nil 端口同样必须被拒绝。
	var empty *Service
	if _, err := empty.Upload(context.Background(), UploadInput{}); err == nil { // err 是零值服务的上传错误。
		t.Fatal("零值服务应失败")
	}
}
