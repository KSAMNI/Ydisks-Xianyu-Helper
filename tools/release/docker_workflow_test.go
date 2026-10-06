package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	yaml3 "github.com/oasdiff/yaml3"
)

// dockerWorkflowFixture 只解析镜像发布安全边界所需字段，避免测试依赖无关步骤顺序。
type dockerWorkflowFixture struct {
	// On 保存手动触发输入；默认关闭的 latest 必须由用户显式开启。
	On struct {
		// Dispatch 保存 workflow_dispatch 的输入定义。
		Dispatch struct {
			// Inputs 按名称记录输入类型和默认值。
			Inputs map[string]struct {
				// Type 必须为 boolean，避免字符串被当作开启状态。
				Type string `yaml:"type"`
				// Default 固定为 false，普通手动构建不能更新 latest。
				Default bool `yaml:"default"`
			} `yaml:"inputs"`
		} `yaml:"workflow_dispatch"`
	} `yaml:"on"`
	// Jobs 按任务标识保存依赖和发布步骤。
	Jobs map[string]dockerWorkflowJob `yaml:"jobs"`
}

// dockerWorkflowJob 保存依赖、审批环境与发布命令，用于静态门禁回归。
type dockerWorkflowJob struct {
	// Needs 指向必须成功的上游任务，保证测试和双架构健康检查不被跳过。
	Needs string `yaml:"needs"`
	// If 保存触发条件，latest 只能由 main 的手动输入开启。
	If string `yaml:"if"`
	// Environment 复用正式发布环境，保留仓库配置的审批策略。
	Environment struct {
		// Name 是既有的正式发布环境名称。
		Name string `yaml:"name"`
	} `yaml:"environment"`
	// Concurrency 与正式 release 共用写入锁，避免并发更新 latest。
	Concurrency struct {
		// Group 是跨工作流的 latest 发布互斥组。
		Group string `yaml:"group"`
		// Cancel 不得取消正在进行的正式发布。
		Cancel bool `yaml:"cancel-in-progress"`
	} `yaml:"concurrency"`
	// Steps 包含镜像构建、健康检查和标签命令。
	Steps []struct {
		// Run 是受测试检查的静态脚本，不执行平台发布。
		Run string `yaml:"run"`
		// Uses 记录摘要下载等标准 action。
		Uses string `yaml:"uses"`
	} `yaml:"steps"`
}

// TestDockerLatestPublishKeepsVerificationGates 验证 latest 显式授权、主分支、测试链、审批和本次摘要来源。
func TestDockerLatestPublishKeepsVerificationGates(t *testing.T) {
	// source、readErr 从仓库读取真实工作流，而不是复制一份测试样本。
	source, readErr := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "docker-publish.yml"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	// workflow、parseErr 保存 YAML 结构，语法错误必须阻止发布变更。
	var workflow dockerWorkflowFixture
	// parseErr 表示工作流结构或字段类型无效，此时拒绝任何发布。
	if parseErr := yaml3.Unmarshal(source, &workflow); parseErr != nil {
		t.Fatal(parseErr)
	}
	// input、exists 检查手动发布必须显式开启布尔输入。
	input, exists := workflow.On.Dispatch.Inputs["publish_latest"]
	if !exists || input.Type != "boolean" || input.Default {
		t.Fatal("latest 必须使用默认关闭的布尔输入")
	}
	// latest 是唯一新增的 latest 推广任务。
	latest := workflow.Jobs["publish-latest"]
	if latest.If != "github.event_name == 'workflow_dispatch' && inputs.publish_latest && github.ref == 'refs/heads/main'" {
		t.Fatal("latest 只能从 main 分支显式手动发布")
	}
	if latest.Needs != "publish" || workflow.Jobs["publish"].Needs != "build" || workflow.Jobs["build"].Needs != "verify" {
		t.Fatal("latest 必须等待测试、原生双架构构建及常规 manifest 发布")
	}
	if latest.Environment.Name != "production-release" || latest.Concurrency.Group != "production-release-publish" || latest.Concurrency.Cancel {
		t.Fatal("latest 不得绕过正式发布环境或并发互斥")
	}
	// script 合并推广步骤，只检查安全不变量，不执行网络或 Docker 命令。
	script := ""
	// downloads 记录是否从当前 workflow run 下载已验证的架构摘要。
	downloads := false
	for /* step 是 latest 推广中的当前静态步骤。 */ _, step := range latest.Steps {
		script += step.Run
		downloads = downloads || step.Uses == "actions/download-artifact@v8"
	}
	if !downloads || !strings.Contains(script, `"$current_sha" != "$GITHUB_SHA"`) || !strings.Contains(script, `"${#digest_files[@]}" -ne 2`) || !strings.Contains(script, `--tag "$image_name:latest" "${digest_args[@]}"`) {
		t.Fatal("latest 必须拒绝陈旧 main 并使用本次运行的两个镜像摘要")
	}
	// content 同时确认原生平台和容器内 Chromium/健康检查仍在发布链路中。
	content := string(source)
	for /* requirement 是此次标签修改不能移除的既有门禁。 */ _, requirement := range []string{"platform: linux/amd64", "platform: linux/arm64", "npm test --prefix frontend", "go test ./...", "--dump-dom about:blank", `"http://127.0.0.1:$host_port/health"`} {
		if !strings.Contains(content, requirement) {
			t.Fatalf("镜像工作流缺少既有验证门禁：%s", requirement)
		}
	}
}
