package webui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// TestResourceCenterBrowserIntegration 用 t 验证真实嵌入 SPA 的资源分类、历史地址与首次回复图片保存；只连接本地夹具。
func TestResourceCenterBrowserIntegration(t *testing.T) {
	if os.Getenv("RUN_BROWSER_INTEGRATION") != "1" {
		t.Skip("设置 RUN_BROWSER_INTEGRATION=1 执行资源中心浏览器回归")
	}
	// runtime、err 是当前测试独占的浏览器驱动和启动结果。
	runtime, err := playwright.Run()
	templateBrowserCheck(t, err, "启动资源中心测试驱动")
	t.Cleanup(func() { // 测试结束时同步停止驱动，不保留后台进程。
		templateBrowserCheck(t, runtime.Stop(), "关闭资源中心测试驱动")
	})
	// browser 是无真实账号的本地 Chromium，生命周期短于驱动。
	browser, err := runtime.Chromium.Launch(playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(true)})
	templateBrowserCheck(t, err, "启动资源中心测试浏览器")
	t.Cleanup(func() { // 关闭浏览器和它拥有的页面。
		templateBrowserCheck(t, browser.Close(), "关闭资源中心测试浏览器")
	})
	// saved 是 HTTP 夹具向测试发送已保存草稿的单次缓冲通道；不关闭，随测试释放。
	saved := make(chan map[string]any, 1)
	// server 为本次测试提供实际构建资源和受控 API，由 Cleanup 关闭并等待 HTTP 请求。
	server := resourceCenterBrowserServer(t, saved)
	// page 是桌面宽度的真实生产 SPA，不修改布局 CSS。
	page, err := browser.NewPage(playwright.BrowserNewPageOptions{Viewport: &playwright.Size{Width: 1440, Height: 1000}})
	templateBrowserCheck(t, err, "创建资源中心测试页面")
	page.SetDefaultTimeout(7000)
	templateBrowserCheck(t, page.Route("**/*", func(route playwright.Route) { // route 是页面单次请求；外网访问一律拒绝。
		if strings.HasPrefix(route.Request().URL(), server.URL+"/") {
			_ = route.Continue()
			return
		}
		_ = route.Abort()
	}), "隔离真实平台请求")
	_, err = page.Goto(server.URL + "/app/library")
	templateBrowserCheck(t, err, "访问原本地资源库地址")
	templateBrowserCheck(t, page.Locator("button[role='tab']:has-text('商品模板')[aria-selected='true']").WaitFor(), "旧地址默认商品模板")
	// oldEntries、err 验证原菜单已合并，历史 URL 不是第二个导航入口。
	oldEntries, err := page.Locator("nav button[aria-label='本地资源库']").Count()
	templateBrowserCheck(t, err, "统计旧导航入口")
	if oldEntries != 0 {
		t.Fatal("侧边栏不应再保留本地资源库入口")
	}
	templateBrowserCheck(t, page.Locator("button[role='tab']:has-text('商品图片')").Click(), "切换商品图片")
	templateBrowserCheck(t, page.Locator("p:has-text('用于商品发布的主图和规格图')").WaitFor(), "展示商品图片用途")
	templateBrowserCheck(t, page.Locator("button[role='tab']:has-text('消息图片')").Click(), "切换消息图片")
	templateBrowserCheck(t, page.Locator("img[alt='使用说明 缩略图']").WaitFor(), "展示已有消息图片")
	_, err = page.Goto(server.URL + "/app/image-library")
	templateBrowserCheck(t, err, "访问原资源中心地址")
	templateBrowserCheck(t, page.Locator("button[role='tab']:has-text('消息图片')[aria-selected='true']").WaitFor(), "旧图片地址默认消息图片")
	templateBrowserCheck(t, page.Locator("nav button[aria-label='自动化规则']").Click(), "打开自动化规则")
	templateBrowserCheck(t, page.Locator("button:has-text('商品首次回复')").Click(), "打开商品首次回复")
	templateBrowserCheck(t, page.Locator("button[title='编辑']").Click(), "编辑首次回复规则")
	_, err = page.Locator("label:has-text('图片来源（可选）') select").SelectOption(playwright.SelectOptionValues{Values: &[]string{"local"}})
	templateBrowserCheck(t, err, "切换已上传图片来源")
	_, err = page.Locator("select:has(option[value='_library/guide.png'])").SelectOption(playwright.SelectOptionValues{Values: &[]string{"_library/guide.png"}})
	templateBrowserCheck(t, err, "选择资源中心图片")
	templateBrowserCheck(t, page.Locator(".modal-container img[alt='使用说明 缩略图']").WaitFor(), "显示已选图片预览")
	templateBrowserCheck(t, page.Locator(".modal-footer button:has-text('保存')").Click(), "保存纯图片首次回复")
	select {
	case body := <-saved: // body 是实际前端 adapter 发出的请求，不是手写模拟的提交。
		if body["reply_image_path"] != "_library/guide.png" || body["reply_image_url"] != "" || body["reply_content"] != "" {
			t.Fatal("首次回复必须保存选中图片并清空旧 URL，不能误要求文字")
		}
		if _, exists := body["imageSource"]; exists { // exists 检查临时来源状态是否错误进入传输契约。
			t.Fatal("图片来源临时状态不得写入接口")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("未收到首次回复图片保存请求")
	}
	templateBrowserCheck(t, page.Locator(".modal-container").WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateDetached}), "保存完成关闭编辑器")
}

// resourceCenterBrowserServer 为 t 创建只用本地固定响应的服务，将保存的规则发送给 saved；不读写真实数据库或凭证。
func resourceCenterBrowserServer(t *testing.T, saved chan<- map[string]any) *httptest.Server {
	t.Helper()
	// assets、err 是应用实际嵌入的前端构建资源。
	assets, err := Static()
	templateBrowserCheck(t, err, "读取资源中心构建资源")
	// mux 只服务静态资源与本地夹具，不代理任何外部平台。
	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(assets))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { // w 返回构建后的 SPA；r 允许两个历史地址及客户端导航。
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/health" {
			resourceCenterBrowserAPI(w, r, saved)
			return
		}
		// index、openErr 是实际构建首页及读取错误。
		index, openErr := assets.Open("index.html")
		if openErr != nil {
			http.Error(w, "缺少前端构建", http.StatusInternalServerError)
			return
		}
		defer index.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.Copy(w, index)
	})
	// server 的请求 goroutine 由 httptest 持有并在关闭时等待。
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// resourceCenterBrowserAPI 用 w/r 响应真实 SPA 需要的非敏感夹具；保存请求经 saved 送回测试断言。
func resourceCenterBrowserAPI(w http.ResponseWriter, r *http.Request, saved chan<- map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	// body 是当前端点的固定响应，默认空数组满足不相关的列表读取。
	body := `[]`
	switch r.URL.Path {
	case "/api/v1/session":
		body = `{"authenticated":true,"initialized":true,"is_admin":true}`
	case "/health":
		body = `{"status":"ok","version":"test","commit":"local"}`
	case "/api/v1/accounts/details":
		body = `[{"id":"a1","remark":"测试账号","enabled":true}]`
	case "/api/v1/image-library":
		body = `{"items":[{"id":7,"name":"使用说明","reference":"_library/guide.png","mime_type":"image/png","size_bytes":100,"width":1,"height":1,"usage_count":0}]}`
	case "/api/v1/image-library/7/content":
		w.Header().Set("Content-Type", "image/svg+xml")
		body = `<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16"><rect width="16" height="16" fill="blue"/></svg>`
	case "/api/v1/reply-rules/greetings":
		body = `[{"id":1,"cookie_id":"a1","name":"欢迎语","enabled":true,"reply_content":"","reply_image_url":"https://example.test/old.png","reply_image_path":"","item_ids":["i1"]}]`
	case "/api/v1/reply-rules/greetings/1":
		// draft、decodeErr 捕获真实保存请求，非法 JSON 作为夹具失败响应而非成功。
		var draft map[string]any
		// decodeErr 表示保存载荷无法解析，此时返回失败且不通知测试保存成功。
		if decodeErr := json.NewDecoder(r.Body).Decode(&draft); decodeErr != nil {
			http.Error(w, "请求 JSON 无效", http.StatusBadRequest)
			return
		}
		draft["id"] = 1
		select {
		case saved <- draft:
		default:
		}
		_ = json.NewEncoder(w).Encode(draft)
		return
	case "/api/v1/delivery-templates":
		body = `{"data":[]}`
	case "/api/v1/automation-rules":
		body = `{"data":[],"total":0,"total_pages":1,"page":1,"page_size":20,"trigger_counts":{}}`
	case "/api/v1/automation-issues":
		body = `{"runs":[],"pending_tasks":[]}`
	case "/api/v1/reply-rules/defaults":
		body = `{}`
	}
	_, _ = io.WriteString(w, body)
}
