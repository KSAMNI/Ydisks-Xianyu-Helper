// Package replyimage 定义默认回复图片引用的纯校验规则，不读取文件或环境配置。
package replyimage

import (
	"errors"
	"io/fs"
	"strings"
	"unicode"
)

// ValidatePath 校验 reference 是账号图片目录内的相对文件引用；空串表示不配置图片。
// 路径保留原文，禁止绝对路径、目录穿越和平台特殊分隔符；不检查文件是否存在。
func ValidatePath(reference string) error {
	if reference == "" {
		return nil
	}
	if len(reference) > 1024 || reference == "." || !fs.ValidPath(reference) || strings.ContainsAny(reference, `\:`) || strings.TrimSpace(reference) != reference {
		return errors.New("本地图片必须使用图片目录内的相对路径，不能包含盘符或目录穿越")
	}
	// character 是路径中的当前字符；控制字符不能进入文件名或错误诊断。
	for _, character := range reference {
		if unicode.IsControl(character) {
			return errors.New("本地图片路径不能包含控制字符")
		}
	}
	return nil
}

// ValidateSource 校验 imageURL 和 imagePath 互斥；仅本地路径使用新格式约束，旧 URL 行为保持不变。
func ValidateSource(imageURL, imagePath string) error {
	if imageURL != "" && imagePath != "" {
		return errors.New("图片 URL 和本地图片路径不能同时配置")
	}
	return ValidatePath(imagePath)
}

// LibraryDirectoryName 是资源中心共享图片在回复图片根目录下保留的目录名。
// 该目录由资源中心管理；共享引用写成 `<LibraryDirectoryName>/<file_name>`，账号私有目录不参与解析。
const LibraryDirectoryName = "_library"

// LibraryReference 返回资源中心图片 fileName 对应的共享引用文本。
// 调用方负责保证 fileName 是资源中心生成的单级文件名，本函数只拼接固定前缀。
func LibraryReference(fileName string) string {
	return LibraryDirectoryName + "/" + fileName
}

// LibraryFileName 判断 reference 是否为资源中心共享图片引用，并返回其中的文件名。
// 只接受 `_library/` 加单级文件名的形式，拒绝空文件名、子目录和目录穿越，避免共享引用绕过沙箱。
func LibraryFileName(reference string) (string, bool) {
	// directory 是引用的首级目录名；fileName 是共享图片文件名；found 表示引用是否含分隔符。
	directory, fileName, found := strings.Cut(reference, "/")
	if !found || directory != LibraryDirectoryName || fileName == "" || strings.Contains(fileName, "/") {
		return "", false
	}
	if ValidatePath(fileName) != nil {
		return "", false
	}
	return fileName, true
}
