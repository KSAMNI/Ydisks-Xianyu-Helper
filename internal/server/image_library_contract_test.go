package server

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// newImageLibraryTestPNG 生成一张合法 PNG，用于真实上传与内容读取契约校验。
func newImageLibraryTestPNG(t *testing.T) []byte {
	t.Helper()
	// buffer 保存 PNG 编码结果。
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 4, 3))); err != nil { // err 是测试图片编码错误。
		t.Fatalf("编码测试图片失败: %v", err)
	}
	return buffer.Bytes()
}

// newImageLibraryMultipart 构造包含图片与可选名称的上传请求体及媒体类型。
func newImageLibraryMultipart(t *testing.T, data []byte, name string) (*bytes.Buffer, string) {
	t.Helper()
	// body 保存 multipart 请求体。
	body := &bytes.Buffer{}
	// writer 负责写入图片字段和名称字段。
	writer := multipart.NewWriter(body)
	// part、partErr 保存图片表单文件写入器及错误。
	part, partErr := writer.CreateFormFile("file", "cover.png")
	if partErr != nil {
		t.Fatalf("创建上传字段失败: %v", partErr)
	}
	if _, err := part.Write(data); err != nil { // err 是测试图片写入请求体的错误。
		t.Fatalf("写入上传图片失败: %v", err)
	}
	if name != "" {
		if err := writer.WriteField("name", name); err != nil { // err 是名称字段写入错误。
			t.Fatalf("写入名称字段失败: %v", err)
		}
	}
	if err := writer.Close(); err != nil { // err 是 multipart 收尾错误。
		t.Fatalf("收尾 multipart 失败: %v", err)
	}
	return body, writer.FormDataContentType()
}

// TestOpenAPIImageLibraryResponses 验证资源中心图片列表、上传、改名、内容读取与删除的真实响应契约。
func TestOpenAPIImageLibraryResponses(t *testing.T) {
	// 上传会写入磁盘，必须在上传目录解析前指向测试独占目录。
	t.Setenv("XIANYU_UPLOAD_DIR", t.TempDir())
	// srv、_、cleanup 拥有隔离测试服务及资源释放责任。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler、sessionCookie 是真实路由树和认证会话。
	handler := srv.Router()
	// sessionCookie 是登录后供每次请求使用的认证凭证。
	sessionCookie := loginHelper(t, handler)
	// emptyList 是空图片库列表，响应必须返回空数组而不是 null。
	emptyList := serveOpenAPISuccess(t, handler, sessionCookie, http.MethodGet, "/api/v1/image-library", "")
	if !bytes.Contains(emptyList.Body.Bytes(), []byte(`"items":[]`)) {
		t.Fatalf("空列表响应异常: %s", emptyList.Body.String())
	}
	// uploadBody、contentType 是合法图片上传请求体及媒体类型。
	uploadBody, contentType := newImageLibraryMultipart(t, newImageLibraryTestPNG(t), "封面图")
	// uploadRequest 是上传资源中心图片的真实请求。
	uploadRequest := httptest.NewRequest(http.MethodPost, "/api/v1/image-library", uploadBody)
	uploadRequest.Header.Set("Content-Type", contentType)
	uploadRequest.AddCookie(sessionCookie)
	// uploadRecorder 捕获上传响应。
	uploadRecorder := httptest.NewRecorder()
	handler.ServeHTTP(uploadRecorder, uploadRequest)
	assertOpenAPIExpectedStatusResponse(t, uploadRequest, uploadRecorder, http.StatusCreated)
	// created 是上传成功后的图片 DTO。
	var created imageLibraryAssetResponse
	if err := json.Unmarshal(uploadRecorder.Body.Bytes(), &created); err != nil { // err 是上传响应解码错误。
		t.Fatalf("解码上传响应失败: %v", err)
	}
	if created.ID <= 0 || created.Name != "封面图" || created.Reference == "" || created.MIMEType != "image/png" || created.Width != 4 || created.Height != 3 {
		t.Fatalf("上传响应字段异常: %+v", created)
	}
	// listAfterUpload 验证列表返回刚上传的图片。
	listAfterUpload := serveOpenAPISuccess(t, handler, sessionCookie, http.MethodGet, "/api/v1/image-library", "")
	if !bytes.Contains(listAfterUpload.Body.Bytes(), []byte(created.Reference)) {
		t.Fatalf("列表未包含新图片: %s", listAfterUpload.Body.String())
	}
	// renamePath 是图片改名入口。
	renamePath := "/api/v1/image-library/" + strconv.FormatInt(created.ID, 10)
	// renamed 是改名成功后的图片 DTO。
	renamed := serveOpenAPISuccess(t, handler, sessionCookie, http.MethodPatch, renamePath, `{"name":"新封面"}`)
	if !bytes.Contains(renamed.Body.Bytes(), []byte(`"name":"新封面"`)) || !bytes.Contains(renamed.Body.Bytes(), []byte(created.Reference)) {
		t.Fatalf("改名响应异常: %s", renamed.Body.String())
	}
	// contentRecorder 捕获图片内容响应，状态码与媒体类型必须与契约一致。
	contentRequest := httptest.NewRequest(http.MethodGet, renamePath+"/content", nil)
	contentRequest.AddCookie(sessionCookie)
	// contentRecorder 保存二进制图片响应。
	contentRecorder := httptest.NewRecorder()
	handler.ServeHTTP(contentRecorder, contentRequest)
	assertOpenAPIRecordedSuccessResponse(t, contentRequest, contentRecorder)
	if contentRecorder.Header().Get("Content-Type") != "image/png" || contentRecorder.Body.Len() == 0 {
		t.Fatalf("图片内容响应异常: contentType=%q len=%d", contentRecorder.Header().Get("Content-Type"), contentRecorder.Body.Len())
	}
	// 引用该图片的卡券让删除需要显式确认。
	cardRequest := httptest.NewRequest(http.MethodPost, "/api/v1/cards", bytes.NewReader([]byte(`{"name":"图片卡","type":"image","image_path":"`+created.Reference+`","enabled":true}`)))
	cardRequest.Header.Set("Content-Type", "application/json")
	cardRequest.AddCookie(sessionCookie)
	// cardRecorder 捕获卡券创建响应。
	cardRecorder := httptest.NewRecorder()
	handler.ServeHTTP(cardRecorder, cardRequest)
	if cardRecorder.Code != http.StatusOK {
		t.Fatalf("创建引用图片的卡券失败: status=%d body=%s", cardRecorder.Code, cardRecorder.Body.String())
	}
	// referencedList 验证列表返回引用次数。
	referencedList := serveOpenAPISuccess(t, handler, sessionCookie, http.MethodGet, "/api/v1/image-library", "")
	if !bytes.Contains(referencedList.Body.Bytes(), []byte(`"usage_count":1`)) {
		t.Fatalf("引用次数未统计: %s", referencedList.Body.String())
	}
	// blockedRequest 验证仍被引用的图片未带 force 时返回冲突。
	blockedRequest := httptest.NewRequest(http.MethodDelete, renamePath, nil)
	blockedRequest.AddCookie(sessionCookie)
	// blockedRecorder 捕获冲突响应。
	blockedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(blockedRecorder, blockedRequest)
	if blockedRecorder.Code != http.StatusConflict {
		t.Fatalf("被引用图片删除未返回冲突: status=%d body=%s", blockedRecorder.Code, blockedRecorder.Body.String())
	}
	// deleteRequest 是带 force 的删除请求，成功时返回 204 空响应。
	deleteRequest := httptest.NewRequest(http.MethodDelete, renamePath+"?force=1", nil)
	deleteRequest.AddCookie(sessionCookie)
	// deleteRecorder 捕获删除响应。
	deleteRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deleteRecorder, deleteRequest)
	assertOpenAPISuccessResponse(t, deleteRequest, deleteRecorder)
	// missingRequest 验证删除后的内容读取返回未找到。
	missingRequest := httptest.NewRequest(http.MethodGet, renamePath+"/content", nil)
	missingRequest.AddCookie(sessionCookie)
	// missingRecorder 捕获未找到响应。
	missingRecorder := httptest.NewRecorder()
	handler.ServeHTTP(missingRecorder, missingRequest)
	if missingRecorder.Code != http.StatusNotFound {
		t.Fatalf("删除后读取未返回未找到: status=%d body=%s", missingRecorder.Code, missingRecorder.Body.String())
	}
	// invalidBody、invalidContentType 是非图片上传请求体，必须被拒绝。
	invalidBody, invalidContentType := newImageLibraryMultipart(t, []byte("not-an-image"), "")
	// invalidRequest 是非法图片上传请求。
	invalidRequest := httptest.NewRequest(http.MethodPost, "/api/v1/image-library", invalidBody)
	invalidRequest.Header.Set("Content-Type", invalidContentType)
	invalidRequest.AddCookie(sessionCookie)
	// invalidRecorder 捕获格式校验失败响应。
	invalidRecorder := httptest.NewRecorder()
	handler.ServeHTTP(invalidRecorder, invalidRequest)
	if invalidRecorder.Code != http.StatusBadRequest {
		t.Fatalf("非法图片未返回参数错误: status=%d body=%s", invalidRecorder.Code, invalidRecorder.Body.String())
	}
}
