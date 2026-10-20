// Package imagelibrary 定义资源中心图片的上传、改名、删除、查询和内容读取用例。
// 本包只编排业务规则，不直接访问数据库、磁盘或 HTTP；文件与元数据通过消费者定义的窄端口注入。
package imagelibrary

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"xianyu-go/internal/replyimage"
)

// MaxImageBytes 是单张资源中心图片允许的最大字节数，与本地回复图片读取上限保持一致。
const MaxImageBytes = 10 << 20

// MaxNameRunes 是图片显示名允许的最大字符数，避免超长名称破坏列表与下拉展示。
const MaxNameRunes = 100

// ErrNotFound 表示资源中心图片或它的磁盘文件不存在。
var ErrNotFound = errors.New("资源中心图片不存在")

// ErrNameTaken 表示新的图片显示名已被其他图片占用。
var ErrNameTaken = errors.New("资源中心图片名称已存在")

// ErrImageInUse 表示图片仍被卡片、发货模板或默认回复引用，需要调用方显式确认后删除。
var ErrImageInUse = errors.New("资源中心图片仍被业务配置引用")

// ValidationError 表示上传或改名输入不满足稳定的业务约束。
type ValidationError struct {
	// Message 是可直接展示给用户的稳定校验提示，不包含磁盘路径等基础设施细节。
	Message string
}

// Error 返回资源中心图片输入校验提示。
func (e *ValidationError) Error() string {
	if e == nil {
		return "资源中心图片输入校验失败"
	}
	return e.Message
}

// Asset 是应用层使用的资源中心图片模型，与数据库行和 HTTP DTO 解耦。
type Asset struct {
	// ID 是图片的稳定标识，前端用它发起改名、删除和内容读取。
	ID int64
	// Name 是用户可编辑的展示名。
	Name string
	// FileName 是图片库目录内不可变的文件名。
	FileName string
	// Reference 是业务配置应保存的图片引用文本，固定为 `_library/<file_name>`。
	Reference string
	// MIMEType 是按图片头识别的媒体类型。
	MIMEType string
	// SizeBytes 是图片字节数。
	SizeBytes int64
	// Width 是图片像素宽度。
	Width int
	// Height 是图片像素高度。
	Height int
	// CreatedAt 是上传时间文本。
	CreatedAt string
	// UpdatedAt 是最近改名时间文本。
	UpdatedAt string
	// UsageCount 是当前卡片、发货模板和默认回复引用该图片的总次数。
	UsageCount int
}

// Content 是图片二进制内容及其响应元数据。
type Content struct {
	// Data 是已读取的图片字节，最多 MaxImageBytes。
	Data []byte
	// MIMEType 是读取时校验过的媒体类型。
	MIMEType string
	// Name 是图片展示名，用于下载文件名提示。
	Name string
}

// UploadInput 是已由 HTTP 边界读取并解码的图片上传输入。
type UploadInput struct {
	// Name 是用户期望的显示名；空值表示由服务按时间生成。
	Name string
	// Data 是图片字节，长度上限由 MaxImageBytes 约束。
	Data []byte
	// MIMEType 是按图片头识别出的媒体类型，只接受 png、jpeg、gif。
	MIMEType string
	// Width 是图片像素宽度，必须为正数。
	Width int
	// Height 是图片像素高度，必须为正数。
	Height int
}

// Repository 定义资源中心图片元数据所需的最小持久化能力。
type Repository interface {
	// List 按最近上传优先返回全部图片元数据。
	List(ctx context.Context) ([]Asset, error)
	// Get 读取单张图片元数据；缺失时返回 ErrNotFound。
	Get(ctx context.Context, assetID int64) (Asset, error)
	// Create 保存图片元数据并返回新标识；显示名冲突时返回 ErrNameTaken。
	Create(ctx context.Context, asset Asset) (int64, error)
	// Rename 只更新展示名；显示名冲突时返回 ErrNameTaken，记录缺失时返回 ErrNotFound。
	Rename(ctx context.Context, assetID int64, name string) error
	// Delete 删除图片元数据；记录缺失时返回 ErrNotFound。
	Delete(ctx context.Context, assetID int64) error
	// NameTaken 判断展示名是否被其他图片占用，excludeID 用于改名时排除自身。
	NameTaken(ctx context.Context, name string, excludeID int64) (bool, error)
	// ReferenceCounts 返回按引用文本聚合的业务引用次数。
	ReferenceCounts(ctx context.Context) (map[string]int, error)
}

// Storage 定义资源中心图片文件的持久化能力，实现方负责目录沙箱与文件名生成。
type Storage interface {
	// Save 保存图片字节并返回不可变磁盘文件名。
	Save(ctx context.Context, data []byte, mimeType string) (string, error)
	// Read 读取图片字节；文件缺失时返回 ErrNotFound。
	Read(ctx context.Context, fileName string) ([]byte, error)
	// Remove 删除图片文件；文件已不存在时视为删除成功。
	Remove(ctx context.Context, fileName string) error
}

// Service 编排资源中心图片的校验、元数据与磁盘文件一致性。
type Service struct {
	// repository 保存图片元数据端口。
	repository Repository
	// storage 保存图片文件端口。
	storage Storage
	// now 返回当前时间，测试可注入固定时钟以稳定生成默认名称。
	now func() time.Time
}

// NewService 创建资源中心应用服务；空端口会在调用时返回明确装配错误。
func NewService(repository Repository, storage Storage) *Service {
	return &Service{repository: repository, storage: storage, now: time.Now}
}

// List 返回全部图片并附带业务引用次数，供资源中心列表和下拉选择展示。
func (s *Service) List(ctx context.Context) ([]Asset, error) {
	if err := s.validate(); err != nil { // err 表示服务端口未装配完整。
		return nil, err
	}
	// assets 是仓储返回的图片元数据列表。
	assets, err := s.repository.List(ctx)
	if err != nil {
		return nil, err
	}
	// counts 是按引用文本聚合的业务引用次数。
	counts, err := s.repository.ReferenceCounts(ctx)
	if err != nil {
		return nil, err
	}
	// index 是当前待补充引用次数的图片下标。
	for index := range assets {
		assets[index].UsageCount = counts[assets[index].Reference]
	}
	return assets, nil
}

// Upload 校验并保存一张图片，返回写入后的图片元数据。
func (s *Service) Upload(ctx context.Context, input UploadInput) (Asset, error) {
	if err := s.validate(); err != nil { // err 表示服务端口未装配完整。
		return Asset{}, err
	}
	// mimeType 是校验后的媒体类型；图片格式只接受 PNG、JPEG 和 GIF。
	mimeType, formatErr := validateImagePayload(input.Data, input.MIMEType, input.Width, input.Height)
	if formatErr != nil {
		return Asset{}, formatErr
	}
	// name 是最终展示名；空输入由时间生成，重名自动追加序号。
	name, nameErr := s.uniqueUploadName(ctx, input.Name)
	if nameErr != nil {
		return Asset{}, nameErr
	}
	// fileName 是存储端口生成的不可变磁盘文件名。
	fileName, saveErr := s.storage.Save(ctx, input.Data, mimeType)
	if saveErr != nil {
		return Asset{}, fmt.Errorf("保存资源中心图片失败: %w", saveErr)
	}
	// asset 是待写入的元数据；文件名一旦写入不再变化，改名只影响 Name。
	asset := Asset{
		Name: name, FileName: fileName, Reference: replyimage.LibraryReference(fileName),
		MIMEType: mimeType, SizeBytes: int64(len(input.Data)), Width: input.Width, Height: input.Height,
	}
	// assetID、createErr 保存新建元数据标识及写入错误。
	assetID, createErr := s.repository.Create(ctx, asset)
	if createErr != nil {
		// cleanupErr 尽力删除刚写入的文件，避免元数据失败后留下孤儿文件。
		if cleanupErr := s.storage.Remove(ctx, fileName); cleanupErr != nil {
			return Asset{}, fmt.Errorf("%w: %w", createErr, cleanupErr)
		}
		return Asset{}, createErr
	}
	return s.load(ctx, assetID)
}

// Rename 校验新名称并只更新展示名，已有业务引用不受影响。
func (s *Service) Rename(ctx context.Context, assetID int64, name string) (Asset, error) {
	if err := s.validate(); err != nil { // err 表示服务端口未装配完整。
		return Asset{}, err
	}
	// normalized 是去掉首尾空白并通过字符校验的展示名。
	normalized, nameErr := validateName(name)
	if nameErr != nil {
		return Asset{}, nameErr
	}
	// asset 先确认图片存在，缺失时返回未找到而不是名称冲突。
	if _, existsErr := s.repository.Get(ctx, assetID); existsErr != nil {
		return Asset{}, existsErr
	}
	// taken、takenErr 表示新名称是否已被其他图片占用。
	taken, takenErr := s.repository.NameTaken(ctx, normalized, assetID)
	if takenErr != nil {
		return Asset{}, takenErr
	}
	if taken {
		return Asset{}, ErrNameTaken
	}
	if err := s.repository.Rename(ctx, assetID, normalized); err != nil { // err 表示改名写入失败。
		return Asset{}, err
	}
	return s.load(ctx, assetID)
}

// Delete 删除图片元数据与磁盘文件；仍被业务配置引用且 force 为假时拒绝删除。
func (s *Service) Delete(ctx context.Context, assetID int64, force bool) error {
	if err := s.validate(); err != nil { // err 表示服务端口未装配完整。
		return err
	}
	// asset 是待删除的图片元数据，用于定位磁盘文件名与引用次数。
	asset, err := s.load(ctx, assetID)
	if err != nil {
		return err
	}
	if asset.UsageCount > 0 && !force {
		return ErrImageInUse
	}
	// 先删除文件再删除元数据：文件删除失败时元数据仍在，用户可以直接重试。
	if removeErr := s.storage.Remove(ctx, asset.FileName); removeErr != nil {
		return fmt.Errorf("删除资源中心图片文件失败: %w", removeErr)
	}
	return s.repository.Delete(ctx, assetID)
}

// Content 读取图片二进制内容，供资源中心列表缩略图和其他页面的预览使用。
func (s *Service) Content(ctx context.Context, assetID int64) (Content, error) {
	if err := s.validate(); err != nil { // err 表示服务端口未装配完整。
		return Content{}, err
	}
	// asset 是待读取内容的图片元数据。
	asset, err := s.load(ctx, assetID)
	if err != nil {
		return Content{}, err
	}
	// data 是受上限约束的图片字节。
	data, readErr := s.storage.Read(ctx, asset.FileName)
	if readErr != nil {
		return Content{}, readErr
	}
	return Content{Data: data, MIMEType: asset.MIMEType, Name: asset.Name}, nil
}

// load 读取单张图片并补充引用次数，保持所有返回路径的模型字段一致。
func (s *Service) load(ctx context.Context, assetID int64) (Asset, error) {
	if assetID <= 0 {
		return Asset{}, ErrNotFound
	}
	// asset 是仓储返回的图片元数据。
	asset, err := s.repository.Get(ctx, assetID)
	if err != nil {
		return Asset{}, err
	}
	// counts 是按引用文本聚合的业务引用次数；缺失键表示暂未被引用。
	counts, err := s.repository.ReferenceCounts(ctx)
	if err != nil {
		return Asset{}, err
	}
	asset.UsageCount = counts[asset.Reference]
	return asset, nil
}

// uniqueUploadName 生成上传图片的展示名，重名时追加递增序号，最多尝试 50 次。
func (s *Service) uniqueUploadName(ctx context.Context, rawName string) (string, error) {
	// base 是去除首尾空白后的期望名称；空值使用时间戳生成默认名。
	base := strings.TrimSpace(rawName)
	if base == "" {
		base = "图片 " + s.now().Format("20060102-150405")
	}
	// candidate 是当前尝试的展示名；limit 是序号尝试上限。
	candidate := base
	// attempt 是当前尝试的序号，0 表示原始名称。
	for attempt := 0; attempt < 50; attempt++ {
		// normalized 是当前候选名通过字符校验后的文本。
		normalized, nameErr := validateName(candidate)
		if nameErr != nil {
			return "", nameErr
		}
		// taken、takenErr 表示当前候选名是否已被占用。
		taken, takenErr := s.repository.NameTaken(ctx, normalized, 0)
		if takenErr != nil {
			return "", takenErr
		}
		if !taken {
			return normalized, nil
		}
		candidate = fmt.Sprintf("%s (%d)", base, attempt+2)
	}
	return "", ErrNameTaken
}

// validate 检查应用服务端口是否装配完整。
func (s *Service) validate() error {
	if s == nil || s.repository == nil || s.storage == nil {
		return errors.New("资源中心图片服务未初始化")
	}
	if s.now == nil {
		s.now = time.Now
	}
	return nil
}

// validateName 校验展示名：去空白后非空、不超过 MaxNameRunes 且不含控制字符。
func validateName(rawName string) (string, error) {
	// name 是去掉首尾空白后的展示名。
	name := strings.TrimSpace(rawName)
	if name == "" {
		return "", &ValidationError{Message: "图片名称不能为空"}
	}
	if utf8.RuneCountInString(name) > MaxNameRunes {
		return "", &ValidationError{Message: fmt.Sprintf("图片名称不能超过 %d 个字符", MaxNameRunes)}
	}
	// character 是名称中的当前字符，控制字符会破坏列表展示和日志。
	for _, character := range name {
		if unicode.IsControl(character) {
			return "", &ValidationError{Message: "图片名称不能包含控制字符"}
		}
	}
	return name, nil
}

// validateImagePayload 校验图片字节、媒体类型与像素尺寸，返回规范化后的媒体类型。
func validateImagePayload(data []byte, mimeType string, width, height int) (string, error) {
	if len(data) == 0 {
		return "", &ValidationError{Message: "图片内容为空"}
	}
	if len(data) > MaxImageBytes {
		return "", &ValidationError{Message: "图片不能超过 10 MiB"}
	}
	// normalized 是按字节头识别的媒体类型；调用方声明的类型只作为交叉校验。
	normalized := strings.ToLower(strings.TrimSpace(mimeType))
	if normalized == "image/jpg" {
		normalized = "image/jpeg"
	}
	if normalized != "image/png" && normalized != "image/jpeg" && normalized != "image/gif" {
		return "", &ValidationError{Message: "图片必须为 PNG、JPEG 或 GIF"}
	}
	if width <= 0 || height <= 0 {
		return "", &ValidationError{Message: "图片尺寸无效"}
	}
	return normalized, nil
}
