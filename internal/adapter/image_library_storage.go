package adapter

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	imagelibraryapp "xianyu-go/internal/application/imagelibrary"
	"xianyu-go/internal/replyimage"
)

// imageLibraryFileLimit 限定单张资源中心图片的最大字节数，与本地回复图片读取上限保持一致。
const imageLibraryFileLimit = imagelibraryapp.MaxImageBytes

// defaultImageLibraryRoot 返回资源中心共享图片目录：回复图片根目录下的保留子目录。
// 它与账号私有目录共用同一个根，发送时由本地图片加载器在同一个沙箱内解析。
func defaultImageLibraryRoot() string {
	return filepath.Join(defaultReplyImageRoot(), replyimage.LibraryDirectoryName)
}

// ImageLibraryStorage 把资源中心图片文件保存在受限目录中，文件名由服务生成且写入后不再变化。
type ImageLibraryStorage struct {
	// rootPath 是资源中心图片目录的绝对或相对路径，只由组合根提供。
	rootPath string
	// now 返回当前时间，用于生成可排序的文件名前缀。
	now func() time.Time
}

// NewImageLibraryStorage 构造资源中心图片文件存储；空目录在写入时返回明确错误。
func NewImageLibraryStorage(rootPath string) *ImageLibraryStorage {
	return &ImageLibraryStorage{rootPath: strings.TrimSpace(rootPath), now: time.Now}
}

// Save 写入图片字节并返回不可变文件名；文件已存在时重新生成名称，最多尝试 5 次。
func (s *ImageLibraryStorage) Save(ctx context.Context, data []byte, mimeType string) (string, error) {
	if err := s.validate(); err != nil { // err 表示存储目录未配置。
		return "", err
	}
	if err := ctx.Err(); err != nil { // err 是调用方取消原因；取消时不创建目录或文件。
		return "", err
	}
	if len(data) == 0 || len(data) > imageLibraryFileLimit {
		return "", errors.New("资源中心图片为空或超过 10 MiB")
	}
	// extension 是媒体类型对应的文件扩展名，只用于磁盘可读性，不参与图片格式判定。
	extension, extensionErr := imageLibraryExtension(mimeType)
	if extensionErr != nil {
		return "", extensionErr
	}
	if err := os.MkdirAll(s.rootPath, 0o755); err != nil { // err 是目录创建错误。
		return "", errors.New("资源中心图片目录不可用")
	}
	// root、rootErr 保存受限目录句柄及打开错误，所有文件操作都在该句柄内完成。
	root, rootErr := os.OpenRoot(s.rootPath)
	if rootErr != nil {
		return "", errors.New("资源中心图片目录不可用")
	}
	defer root.Close()
	// attempt 是文件名冲突时的重试序号，避免并发上传写入同一个文件。
	for attempt := 0; attempt < 5; attempt++ {
		// fileName 是本次尝试的不可变文件名；randomErr 表示随机后缀生成失败。
		fileName, randomErr := s.newFileName(extension)
		if randomErr != nil {
			return "", randomErr
		}
		// file、openErr 保存独占创建的图片文件；已存在说明名称冲突，需要重新生成。
		file, openErr := root.OpenFile(fileName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if openErr != nil {
			if errors.Is(openErr, os.ErrExist) {
				continue
			}
			return "", errors.New("资源中心图片写入失败")
		}
		// writeErr 保存整段写入结果；写入失败时关闭并删除半成品文件。
		_, writeErr := file.Write(data)
		// closeErr 是关闭文件句柄的错误，写失败时优先保留写入错误。
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			_ = root.Remove(fileName)
			if cancelErr := ctx.Err(); cancelErr != nil { // cancelErr 保留取消类别。
				return "", cancelErr
			}
			return "", errors.New("资源中心图片写入失败")
		}
		return fileName, nil
	}
	return "", errors.New("资源中心图片文件名冲突")
}

// Read 在受限目录内读取图片字节，拒绝目录、符号链接目标以外的越界路径和超限文件。
func (s *ImageLibraryStorage) Read(ctx context.Context, fileName string) ([]byte, error) {
	if err := s.validate(); err != nil { // err 表示存储目录未配置。
		return nil, err
	}
	if err := ctx.Err(); err != nil { // err 是调用方取消原因；取消时不访问文件系统。
		return nil, err
	}
	if err := validateImageLibraryFileName(fileName); err != nil { // err 表示文件名越界或非法。
		return nil, err
	}
	// root、rootErr 保存受限目录句柄及打开错误。
	root, rootErr := os.OpenRoot(s.rootPath)
	if rootErr != nil {
		return nil, imagelibraryapp.ErrNotFound
	}
	defer root.Close()
	// metadata、statErr 提前拒绝目录和特殊文件，避免普通读取阻塞。
	metadata, statErr := root.Stat(fileName)
	if statErr != nil || !metadata.Mode().IsRegular() {
		return nil, imagelibraryapp.ErrNotFound
	}
	if metadata.Size() <= 0 || metadata.Size() > imageLibraryFileLimit {
		return nil, errors.New("资源中心图片为空或超过 10 MiB")
	}
	// file、openErr 保存只读文件句柄；Root 阻止相对路径和链接越过图片目录。
	file, openErr := root.OpenFile(fileName, os.O_RDONLY, 0)
	if openErr != nil {
		return nil, imagelibraryapp.ErrNotFound
	}
	defer file.Close()
	// data、readErr 保存限量且可取消的内存读取结果；额外一字节用于检测读取期间的文件增长。
	data, readErr := io.ReadAll(io.LimitReader(replyImageContextReader{ctx: ctx, reader: file}, imageLibraryFileLimit+1))
	if readErr != nil {
		if cancelErr := ctx.Err(); cancelErr != nil { // cancelErr 保留取消类别。
			return nil, cancelErr
		}
		return nil, errors.New("资源中心图片读取失败")
	}
	if len(data) == 0 || len(data) > imageLibraryFileLimit {
		return nil, errors.New("资源中心图片为空或超过 10 MiB")
	}
	return data, nil
}

// Remove 删除图片文件；文件已不存在时视为删除成功，便于用户重试删除。
func (s *ImageLibraryStorage) Remove(ctx context.Context, fileName string) error {
	if err := s.validate(); err != nil { // err 表示存储目录未配置。
		return err
	}
	if err := ctx.Err(); err != nil { // err 是调用方取消原因；取消时不访问文件系统。
		return err
	}
	if err := validateImageLibraryFileName(fileName); err != nil { // err 表示文件名越界或非法。
		return err
	}
	// root、rootErr 保存受限目录句柄及打开错误；目录不存在说明文件已不存在。
	root, rootErr := os.OpenRoot(s.rootPath)
	if rootErr != nil {
		return nil
	}
	defer root.Close()
	// removeErr 是删除结果；已不存在同样视为成功。
	removeErr := root.Remove(fileName)
	if removeErr == nil || errors.Is(removeErr, os.ErrNotExist) {
		return nil
	}
	return errors.New("资源中心图片删除失败")
}

// newFileName 生成 `img-<纳秒时间>-<8 字节随机十六进制>.<扩展名>` 形式的不可变文件名。
func (s *ImageLibraryStorage) newFileName(extension string) (string, error) {
	// suffix 是 8 字节随机十六进制后缀，避免并发上传生成同名文件。
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil { // err 表示系统随机源不可用。
		return "", errors.New("生成资源中心图片文件名失败")
	}
	return fmt.Sprintf("img-%d-%s.%s", s.now().UnixNano(), hex.EncodeToString(suffix), extension), nil
}

// validate 检查存储目录是否已配置。
func (s *ImageLibraryStorage) validate() error {
	if s == nil || s.rootPath == "" {
		return errors.New("资源中心图片存储未初始化")
	}
	if s.now == nil {
		s.now = time.Now
	}
	return nil
}

// imageLibraryExtension 把受支持的媒体类型映射为文件扩展名。
func imageLibraryExtension(mimeType string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/png":
		return "png", nil
	case "image/jpeg", "image/jpg":
		return "jpg", nil
	case "image/gif":
		return "gif", nil
	default:
		return "", errors.New("资源中心图片必须为 PNG、JPEG 或 GIF")
	}
}

// validateImageLibraryFileName 要求文件名是单级普通文件名，拒绝目录穿越和平台特殊分隔符。
func validateImageLibraryFileName(fileName string) error {
	if fileName == "" || fileName == "." || fileName == ".." || strings.ContainsAny(fileName, `/\:`) || strings.TrimSpace(fileName) != fileName {
		return errors.New("资源中心图片文件名无效")
	}
	return nil
}

// libraryReference 返回业务配置保存的共享图片引用文本。
func libraryReference(fileName string) string {
	return replyimage.LibraryReference(fileName)
}

// 编译期检查存储适配器提供应用层要求的全部文件能力。
var _ imagelibraryapp.Storage = (*ImageLibraryStorage)(nil)
