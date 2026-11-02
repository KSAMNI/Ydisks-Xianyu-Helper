package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestItemGreetingLocalImageContract 用 t 验证真实 HTTP→应用→仓储的纯图片创建、列表回读、来源切换和错误契约。
func TestItemGreetingLocalImageContract(t *testing.T) {
	// srv、store、cleanup 提供完整路由与隔离数据库，不接触真实账号。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是完整路由，session 为测试管理员会话。
	handler := srv.Router()
	// session 只用于本地 httptest 身份校验。
	session := loginHelper(t, handler)
	// insertErr 建立拥有者账号，无需平台登录。
	if _, insertErr := store.DB.ExecContext(context.Background(), `INSERT INTO cookies(id,value,user_id) VALUES ('local-image','fixture',1)`); insertErr != nil {
		t.Fatal(insertErr)
	}
	// created 必须成功保存无文字无URL的共享图片。
	created := greetingRequest(t, handler, session, http.MethodPost, "/api/v1/reply-rules/greetings", `{"cookie_id":"local-image","enabled":true,"reply_content":"","reply_image_path":"_library/guide.png","item_ids":["i1"]}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("纯图片创建失败: %d %s", created.Code, created.Body.String())
	}
	// rule 是具名响应，验证新增路径没有在传输适配器丢失。
	var rule itemGreetingResponse
	// decodeErr 检查响应符合新增字段形状。
	if decodeErr := json.Unmarshal(created.Body.Bytes(), &rule); decodeErr != nil || rule.ReplyImagePath != "_library/guide.png" || rule.ReplyImageURL != "" {
		t.Fatalf("纯图片响应不正确: %v", decodeErr)
	}
	// listed 覆盖仓储与 adapter 的回读转换，不仅检查创建回显。
	listed := greetingRequest(t, handler, session, http.MethodGet, "/api/v1/reply-rules/greetings", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"reply_image_path":"_library/guide.png"`) {
		t.Fatal("列表未回读已保存图片")
	}
	// path 是当前规则更新端点。
	path := "/api/v1/reply-rules/greetings/" + jsonID(rule.ID)
	// invalid 是不允许持久化的双来源或越界路径。
	for _, invalid := range []string{
		`{"enabled":true,"reply_content":"","reply_image_url":"https://example.test/a.png","reply_image_path":"_library/guide.png","item_ids":["i1"]}`,
		`{"enabled":true,"reply_content":"","reply_image_path":"../secret","item_ids":["i1"]}`,
	} {
		// rejected 必须返回参数错误并保持原规则不变。
		rejected := greetingRequest(t, handler, session, http.MethodPut, path, invalid)
		if rejected.Code != http.StatusBadRequest {
			t.Fatalf("非法图片来源未拒绝: %d", rejected.Code)
		}
	}
	// updated 验证旧URL请求省略新字段时可以正常切回URL并清空本地图。
	updated := greetingRequest(t, handler, session, http.MethodPut, path, `{"enabled":true,"reply_content":"","reply_image_url":"https://example.test/a.png","item_ids":["i1"]}`)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"reply_image_path":""`) {
		t.Fatalf("URL兼容更新失败: %d", updated.Code)
	}
}

// TestItemGreetingImageDeletionProtection 用 t 验证真实上传图片仅被首次回复引用时，统计和删除冲突仍生效。
func TestItemGreetingImageDeletionProtection(t *testing.T) {
	t.Setenv("XIANYU_UPLOAD_DIR", t.TempDir())
	// srv、store、cleanup 拥有独立数据库和图片目录，不接触生产素材。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是实际资源中心与首次回复路由。
	handler := srv.Router()
	// session 是本地测试管理会话。
	session := loginHelper(t, handler)
	// insertErr 为首次回复创建所属账号。
	if _, insertErr := store.DB.ExecContext(context.Background(), `INSERT INTO cookies(id,value,user_id) VALUES ('image-owner','fixture',1)`); insertErr != nil {
		t.Fatal(insertErr)
	}
	// body、contentType 构造合法 PNG 上传，不直接伪造图片元数据。
	body, contentType := newImageLibraryMultipart(t, newImageLibraryTestPNG(t), "首次回复图")
	// request 是经过真实 handler 的上传请求。
	request := httptest.NewRequest(http.MethodPost, "/api/v1/image-library", body)
	request.Header.Set("Content-Type", contentType)
	request.AddCookie(session)
	// recorder 捕获上传后的资源标识。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assertOpenAPIExpectedStatusResponse(t, request, recorder, http.StatusCreated)
	// asset 是真实上传结果，用其引用绑定首次回复。
	var asset imageLibraryAssetResponse
	// decodeErr 确保有效读取服务端生成的文件引用。
	if decodeErr := json.Unmarshal(recorder.Body.Bytes(), &asset); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	// created 使用停用规则验证引用保护与是否启用无关。
	created := greetingRequest(t, handler, session, http.MethodPost, "/api/v1/reply-rules/greetings", `{"cookie_id":"image-owner","enabled":false,"reply_content":"","reply_image_path":"`+asset.Reference+`","item_ids":["i1"]}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("引用首次回复创建失败: %d", created.Code)
	}
	// listed 必须统计首次回复的独立引用。
	listed := greetingRequest(t, handler, session, http.MethodGet, "/api/v1/image-library", "")
	if !strings.Contains(listed.Body.String(), `"usage_count":1`) {
		t.Fatal("资源中心未统计首次回复引用")
	}
	// blocked 在没有 force 确认时必须拒绝删除被引用图片。
	blocked := greetingRequest(t, handler, session, http.MethodDelete, "/api/v1/image-library/"+jsonID(asset.ID), "")
	if blocked.Code != http.StatusConflict {
		t.Fatalf("首次回复引用没有阻止误删: %d", blocked.Code)
	}
}
