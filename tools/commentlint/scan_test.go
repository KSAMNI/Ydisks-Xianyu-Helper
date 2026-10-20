package main

import (
	"go/ast"
	"os"
	"path/filepath"
	"testing"
)

// writeFixture 在临时根目录下创建夹具文件，必要时创建父目录。
func writeFixture(t *testing.T, root, relative, content string) {
	t.Helper()
	// full 是夹具文件的绝对路径。
	full := filepath.Join(root, filepath.FromSlash(relative))
	// mkdirErr 保存该步骤返回的错误，用于断言预期的成功或失败路径。
	if mkdirErr := os.MkdirAll(filepath.Dir(full), 0o755); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	// writeErr 保存该步骤返回的错误，用于断言预期的成功或失败路径。
	if writeErr := os.WriteFile(full, []byte(content), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
}

// TestInspectGoFileReportsEveryDeclarationKind 覆盖函数、变量、类型、结构体/接口字段、匿名字段、短变量与 range 变量的缺注释识别，以及有注释时的放行。
func TestInspectGoFileReportsEveryDeclarationKind(t *testing.T) {
	// root 是夹具根目录。
	root := t.TempDir()
	writeFixture(t, root, "bad.go", `package bad

import "fmt"

var _ = 1
var plain int
const grouped = 1
type Thing struct {
	Named string
	fmt.Stringer
}
type Iface interface {
	Method()
}
func missing() {
	local := 1
	for index, value := range []int{1} {
		_, _ = index, value
	}
	_ = local
}
`)
	writeFixture(t, root, "good.go", `package good

// plain 是带中文说明的变量。
var plain int

// Thing 是带中文说明的类型。
type Thing struct {
	// Named 是带中文说明的字段。
	Named string
}

// ok 是带中文说明的函数。
func ok() {
	// local 是带中文说明的短变量。
	local := 1
	_ = local
}
`)
	// findings、inspectErr 是缺注释夹具的扫描结果。
	findings, inspectErr := inspectGoFile(root, filepath.Join(root, "bad.go"))
	if inspectErr != nil {
		t.Fatal(inspectErr)
	}
	// kinds 统计各声明类别的命中次数。
	kinds := map[string]int{}
	// finding 是当前扫描记录。
	for _, finding := range findings {
		kinds[finding.Kind]++
	}
	// kind 是当前遍历到的元素，用于逐项断言或处理。
	for _, kind := range []string{"function", "var", "const", "type", "local-variable", "range-variable"} {
		if kinds[kind] == 0 {
			t.Fatalf("缺少类别 %s: %+v", kind, findings)
		}
	}
	// goodFindings 是完整注释夹具的扫描结果，必须为空。
	goodFindings, goodErr := inspectGoFile(root, filepath.Join(root, "good.go"))
	if goodErr != nil || len(goodFindings) != 0 {
		t.Fatalf("good findings=%+v err=%v", goodFindings, goodErr)
	}
}

// TestCollectFindingsWalksTreeAndSkipsExcluded 覆盖递归扫描、生成文件/忽略目录/冻结文件的跳过、排序和语法错误传播。
func TestCollectFindingsWalksTreeAndSkipsExcluded(t *testing.T) {
	// root 是夹具根目录。
	root := t.TempDir()
	// missing 是缺少注释的最小源码。
	const missing = "package p\nvar x int\n"
	writeFixture(t, root, "b.go", missing)
	writeFixture(t, root, "a.go", missing)
	writeFixture(t, root, "gen.gen.go", missing)
	writeFixture(t, root, "notes.txt", "ignored")
	writeFixture(t, root, "node_modules/dep/x.go", missing)
	writeFixture(t, root, "internal/webui/static/x.go", missing)
	writeFixture(t, root, "internal/browser/slider.go", missing)
	// findings、collectErr 是整棵树的扫描结果。
	findings, collectErr := collectFindings(root)
	if collectErr != nil {
		t.Fatal(collectErr)
	}
	sortFindings(findings)
	if len(findings) != 2 || findings[0].File != "a.go" || findings[1].File != "b.go" {
		t.Fatalf("findings=%+v", findings)
	}
	writeFixture(t, root, "broken.go", "package p\nfunc (")
	// brokenErr 保存该步骤返回的错误，用于断言预期的成功或失败路径。
	if _, brokenErr := collectFindings(root); brokenErr == nil {
		t.Fatal("语法错误应向上传播")
	}
	// templateErr 保存该步骤返回的错误，用于断言预期的成功或失败路径。
	if _, templateErr := collectTemplateCommentFindings(root); templateErr == nil {
		t.Fatal("模板审计遇到语法错误应失败")
	}
	// missingErr 保存该步骤返回的错误，用于断言预期的成功或失败路径。
	if _, missingErr := collectFindings(filepath.Join(root, "absent")); missingErr == nil {
		t.Fatal("不存在的根目录应失败")
	}
}

// TestCollectTemplateCommentFindingsSortsResults 覆盖整棵树的模板化注释扫描与排序。
func TestCollectTemplateCommentFindingsSortsResults(t *testing.T) {
	// root 是夹具根目录。
	root := t.TempDir()
	writeFixture(t, root, "z.go", "package p\n\n// err 表示错误\nvar err error\n")
	writeFixture(t, root, "a.go", "package p\n\n// count 表示数量\nvar count int\n")
	writeFixture(t, root, "internal/browser/token_captcha.go", "package p\n\n// err 表示错误\nvar err error\n")
	// findings、scanErr 是扫描结果。
	findings, scanErr := collectTemplateCommentFindings(root)
	if scanErr != nil || len(findings) != 2 || findings[0].File != "a.go" {
		t.Fatalf("findings=%+v err=%v", findings, scanErr)
	}
}

// TestShouldSkipDirectoryAndFrozenFiles 覆盖目录忽略规则与冻结文件清单。
func TestShouldSkipDirectoryAndFrozenFiles(t *testing.T) {
	// root 是扫描根目录。
	root := t.TempDir()
	// cases 保存相对目录到是否跳过的期望。
	cases := map[string]bool{".git": true, "data": true, "data/sub": true, "vendor/x": true, "internal/webui/static": true, "internal/server": false, "database": false}
	// relative、want 分别是相对目录与期望结果。
	for relative, want := range cases {
		// got 保存本次调用的返回值，用于与期望结果比较。
		if got := shouldSkipDirectory(filepath.Join(root, filepath.FromSlash(relative)), root); got != want {
			t.Fatalf("%s got %v want %v", relative, got, want)
		}
	}
	if !isFrozenGoFile("internal/browser/slider.go") || isFrozenGoFile("internal/browser/other.go") {
		t.Fatal("冻结文件清单判断错误")
	}
}

// TestHelpersCoverChineseDetectionNamesAndSorting 覆盖汉字检测、匿名函数名、稳定排序的全部比较分支。
func TestHelpersCoverChineseDetectionNamesAndSorting(t *testing.T) {
	if !containsChinese("含有中文") || containsChinese("english only") {
		t.Fatal("containsChinese 判断错误")
	}
	if funcName(nil) != "anonymous" || funcName(&ast.Ident{Name: ""}) != "anonymous" || funcName(&ast.Ident{Name: "named"}) != "named" {
		t.Fatal("funcName 判断错误")
	}
	// findings 覆盖按文件、行号、类别、名称依次比较的排序。
	findings := []Finding{
		{File: "b", Line: 1, Kind: "k", Name: "n"},
		{File: "a", Line: 2, Kind: "k", Name: "n"},
		{File: "a", Line: 1, Kind: "z", Name: "n"},
		{File: "a", Line: 1, Kind: "k", Name: "z"},
		{File: "a", Line: 1, Kind: "k", Name: "a"},
	}
	sortFindings(findings)
	// want 是按文件、行号、类别、名称依次比较后的期望顺序。
	want := []Finding{
		{File: "a", Line: 1, Kind: "k", Name: "a"},
		{File: "a", Line: 1, Kind: "k", Name: "z"},
		{File: "a", Line: 1, Kind: "z", Name: "n"},
		{File: "a", Line: 2, Kind: "k", Name: "n"},
		{File: "b", Line: 1, Kind: "k", Name: "n"},
	}
	// index 是当前遍历到的元素，用于逐项断言或处理。
	for index := range want {
		if findings[index] != want[index] {
			t.Fatalf("index %d got %+v want %+v", index, findings[index], want[index])
		}
	}
}
