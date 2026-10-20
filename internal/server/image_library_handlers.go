package server

import (
	"bytes"
	"errors"
	"image"
	_ "image/gif"  // 注册资源中心图片的 GIF 头校验。
	_ "image/jpeg" // 注册资源中心图片的 JPEG 头校验。
	_ "image/png"  // 注册资源中心图片的 PNG 头校验。
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	imagelibraryapp "xianyu-go/internal/application/imagelibrary"
)

// maxImageLibraryUploadBytes 是单张资源中心图片允许的原始字节上限，单位为字节。
const maxImageLibraryUploadBytes = imagelibraryapp.MaxImageBytes

// maxImageLibraryMultipartBytes 是图片上传请求的总体上限，额外空间用于 multipart 元数据。
const maxImageLibraryMultipartBytes = maxImageLibraryUploadBytes + (1 << 20)

// imageLibraryAssetResponse 是资源中心图片的具名响应 DTO。
type imageLibraryAssetResponse struct {
	// ID 是图片稳定标识。
	ID int64 `json:"id"`
	// Name 是用户可编辑的展示名。
	Name string `json:"name"`
	// Reference 是其他配置应保存的图片引用文本。
	Reference string `json:"reference"`
	// MIMEType 是图片媒体类型。
	MIMEType string `json:"mime_type"`
	// SizeBytes 是图片字节数。
	SizeBytes int64 `json:"size_bytes"`
	// Width 是图片像素宽度。
	Width int `json:"width"`
	// Height 是图片像素高度。
	Height int `json:"height"`
	// UsageCount 是当前被卡片、发货模板和默认回复引用的次数。
	UsageCount int `json:"usage_count"`
	// CreatedAt 是上传时间文本。
	CreatedAt string `json:"created_at"`
	// UpdatedAt 是最近改名时间文本。
	UpdatedAt string `json:"updated_at"`
}

// imageLibraryListResponse 是资源中心图片列表的具名响应 DTO。
type imageLibraryListResponse struct {
	// Items 是按最近上传优先排序的图片列表。
	Items []imageLibraryAssetResponse `json:"items"`
}

// imageLibraryRenameRequest 是图片改名请求的具名 DTO。
type imageLibraryRenameRequest struct {
	// Name 是新的图片展示名。
	Name string `json:"name"`
}

// imageLibraryOperationResponse 是资源中心图片删除等无附加数据操作的具名响应 DTO。
type imageLibraryOperationResponse struct {
	// Success 表示删除操作已经完成。
	Success bool `json:"success"`
}

// newImageLibraryAssetResponse 将应用层图片模型转换为 HTTP DTO。
func newImageLibraryAssetResponse(asset imagelibraryapp.Asset) imageLibraryAssetResponse {
	return imageLibraryAssetResponse{
		ID: asset.ID, Name: asset.Name, Reference: asset.Reference, MIMEType: asset.MIMEType,
		SizeBytes: asset.SizeBytes, Width: asset.Width, Height: asset.Height,
		UsageCount: asset.UsageCount, CreatedAt: asset.CreatedAt, UpdatedAt: asset.UpdatedAt,
	}
}

// listImageLibrary 返回全部资源中心图片及其业务引用次数。
func (s *Server) listImageLibrary(w http.ResponseWriter, r *http.Request) {
	// assets、err 保存应用服务返回的图片列表及查询错误。
	assets, err := s.imageLibraryApplication().List(r.Context())
	if err != nil {
		s.writeImageLibraryError(w, err)
		return
	}
	// response 是列表响应；空列表也返回空数组，避免前端区分 null。
	response := imageLibraryListResponse{Items: make([]imageLibraryAssetResponse, 0, len(assets))}
	// asset 是当前待转换的应用层图片模型。
	for _, asset := range assets {
		response.Items = append(response.Items, newImageLibraryAssetResponse(asset))
	}
	writeJSON(w, http.StatusOK, response)
}

// uploadImageLibrary 读取 multipart 图片、校验格式并保存到资源中心。
func (s *Server) uploadImageLibrary(w http.ResponseWriter, r *http.Request) {
	if !parseMultipartRequest(w, r, maxImageLibraryMultipartBytes, maxImageLibraryMultipartBytes, "图片上传内容不能超过 11 MiB") {
		return
	}
	// file、header、fileErr 保存上传文件句柄、原始头信息和读取错误。
	file, header, fileErr := r.FormFile("file")
	if fileErr != nil {
		writeErr(w, http.StatusBadRequest, "请选择要上传的图片")
		return
	}
	defer file.Close()
	// data、readErr 保存受上限约束的图片字节；额外一字节用于识别超限。
	data, readErr := io.ReadAll(io.LimitReader(file, maxImageLibraryUploadBytes+1))
	if readErr != nil {
		writeErr(w, http.StatusBadRequest, "读取上传图片失败")
		return
	}
	if len(data) == 0 || len(data) > maxImageLibraryUploadBytes {
		writeErr(w, http.StatusBadRequest, "图片为空或超过 10 MiB")
		return
	}
	// config、format、decodeErr 按实际图片头验证格式和尺寸，不信任文件扩展名。
	config, format, decodeErr := image.DecodeConfig(bytes.NewReader(data))
	if decodeErr != nil || config.Width <= 0 || config.Height <= 0 || (format != "png" && format != "jpeg" && format != "gif") {
		writeErr(w, http.StatusBadRequest, "图片必须为有效的 PNG、JPEG 或 GIF")
		return
	}
	// name 优先使用用户填写的名称，其次回退到上传文件名（去掉扩展名）。
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = strings.TrimSpace(strings.TrimSuffix(filepath.Base(header.Filename), filepath.Ext(header.Filename)))
	}
	// asset、err 保存新建图片元数据及写入错误。
	asset, err := s.imageLibraryApplication().Upload(r.Context(), imagelibraryapp.UploadInput{
		Name: name, Data: data, MIMEType: "image/" + format, Width: config.Width, Height: config.Height,
	})
	if err != nil {
		s.writeImageLibraryError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, newImageLibraryAssetResponse(asset))
}

// renameImageLibrary 只修改图片展示名，已有业务引用保持有效。
func (s *Server) renameImageLibrary(w http.ResponseWriter, r *http.Request) {
	// assetID、idErr 保存路径中的图片标识及解析错误。
	assetID, idErr := parseImageLibraryID(r)
	if idErr != nil {
		writeErr(w, http.StatusNotFound, "资源中心图片不存在")
		return
	}
	// request 是改名请求的具名 DTO。
	var request imageLibraryRenameRequest
	if err := decodeJSON(r, &request); err != nil { // err 是请求体解码错误。
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// asset、err 保存改名后的图片元数据及错误。
	asset, err := s.imageLibraryApplication().Rename(r.Context(), assetID, request.Name)
	if err != nil {
		s.writeImageLibraryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newImageLibraryAssetResponse(asset))
}

// deleteImageLibrary 删除图片；仍被引用时需要 force 参数显式确认。
func (s *Server) deleteImageLibrary(w http.ResponseWriter, r *http.Request) {
	// assetID、idErr 保存路径中的图片标识及解析错误。
	assetID, idErr := parseImageLibraryID(r)
	if idErr != nil {
		writeErr(w, http.StatusNotFound, "资源中心图片不存在")
		return
	}
	// force 表示用户已在界面上确认删除仍被引用的图片。
	force := r.URL.Query().Get("force") == "1" || strings.EqualFold(r.URL.Query().Get("force"), "true")
	if err := s.imageLibraryApplication().Delete(r.Context(), assetID, force); err != nil { // err 是删除用例错误，按类别映射状态码。
		s.writeImageLibraryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, imageLibraryOperationResponse{Success: true})
}

// imageLibraryContent 返回图片二进制内容，供资源中心缩略图和其他页面预览使用。
func (s *Server) imageLibraryContent(w http.ResponseWriter, r *http.Request) {
	// assetID、idErr 保存路径中的图片标识及解析错误。
	assetID, idErr := parseImageLibraryID(r)
	if idErr != nil {
		writeErr(w, http.StatusNotFound, "资源中心图片不存在")
		return
	}
	// content、err 保存图片字节与媒体类型及读取错误。
	content, err := s.imageLibraryApplication().Content(r.Context(), assetID)
	if err != nil {
		s.writeImageLibraryError(w, err)
		return
	}
	w.Header().Set("Content-Type", content.MIMEType)
	// 文件内容与标识绑定且不会变化，允许浏览器长期缓存缩略图。
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content.Data)
}

// parseImageLibraryID 解析路径中的图片标识，非正整数视为不存在。
func parseImageLibraryID(r *http.Request) (int64, error) {
	// assetID 是路径参数中的图片标识。
	assetID, err := strconv.ParseInt(chi.URLParam(r, "asset_id"), 10, 64)
	if err != nil || assetID <= 0 {
		return 0, errors.New("资源中心图片标识无效")
	}
	return assetID, nil
}

// writeImageLibraryError 把资源中心图片应用错误映射为统一 HTTP 错误响应。
// 基础设施错误只回稳定提示，具体原因写入服务端诊断日志，避免把磁盘路径暴露给浏览器。
func (s *Server) writeImageLibraryError(w http.ResponseWriter, err error) {
	// validationErr 指向可直接展示的输入校验错误。
	var validationErr *imagelibraryapp.ValidationError
	switch {
	case errors.Is(err, imagelibraryapp.ErrNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, imagelibraryapp.ErrNameTaken):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, imagelibraryapp.ErrImageInUse):
		writeErr(w, http.StatusConflict, "图片仍被卡片、发货模板或默认回复引用，确认后可以强制删除")
	case errors.As(err, &validationErr):
		writeErr(w, http.StatusBadRequest, validationErr.Error())
	default:
		if s != nil && s.Logger != nil {
			s.Logger.Warn("资源中心图片操作失败", "err", err)
		}
		writeErr(w, http.StatusInternalServerError, "资源中心图片操作失败")
	}
}
