package helm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"multikube-manager/internal/kube"

	helmaction "helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	helmrelease "helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

type DeployRequest struct {
	Namespace       string
	CreateNamespace bool
	ReleaseName     string
	RepoURL         string
	ChartName       string
	Version         string
	Values          string
}

type DeployResult struct {
	Operation   string `json:"operation"`
	ReleaseName string `json:"releaseName"`
	Namespace   string `json:"namespace"`
	Chart       string `json:"chart"`
	Version     string `json:"version"`
	Revision    int    `json:"revision"`
	Status      string `json:"status"`
	Notes       string `json:"notes"`
}

func Deploy(ctx context.Context, runtime *kube.Runtime, kubeconfig string, request DeployRequest) (DeployResult, error) {
	workdir, err := os.MkdirTemp("", "kubefeel-helm-*")
	if err != nil {
		return DeployResult{}, fmt.Errorf("创建 Helm 工作目录失败: %w", err)
	}
	defer os.RemoveAll(workdir)

	if err := os.MkdirAll(filepath.Join(workdir, "repository-cache"), 0o755); err != nil {
		return DeployResult{}, fmt.Errorf("初始化 Helm 缓存目录失败: %w", err)
	}

	if err := os.WriteFile(filepath.Join(workdir, "config"), []byte(kubeconfig), 0o600); err != nil {
		return DeployResult{}, fmt.Errorf("写入 kubeconfig 失败: %w", err)
	}

	if err := os.WriteFile(filepath.Join(workdir, "repositories.yaml"), []byte("repositories: []\n"), 0o600); err != nil {
		return DeployResult{}, fmt.Errorf("初始化 Helm 仓库配置失败: %w", err)
	}

	settings := cli.New()
	settings.KubeConfig = filepath.Join(workdir, "config")
	settings.RepositoryConfig = filepath.Join(workdir, "repositories.yaml")
	settings.RepositoryCache = filepath.Join(workdir, "repository-cache")
	settings.RegistryConfig = filepath.Join(workdir, "registry.json")
	settings.SetNamespace(request.Namespace)

	actionConfig := new(helmaction.Configuration)
	if err := actionConfig.Init(settings.RESTClientGetter(), request.Namespace, "secret", func(string, ...any) {}); err != nil {
		return DeployResult{}, fmt.Errorf("初始化 Helm 客户端失败: %w", err)
	}

	if err := ensureNamespace(ctx, runtime, request.Namespace, request.CreateNamespace); err != nil {
		return DeployResult{}, err
	}

	values := map[string]any{}
	if strings.TrimSpace(request.Values) != "" {
		if err := yaml.Unmarshal([]byte(request.Values), &values); err != nil {
			return DeployResult{}, fmt.Errorf("解析 values YAML 失败: %w", err)
		}
	}

	chartOptions := helmaction.ChartPathOptions{
		RepoURL: request.RepoURL,
		Version: strings.TrimSpace(request.Version),
	}

	chartPath, err := chartOptions.LocateChart(request.ChartName, settings)
	if err != nil {
		return DeployResult{}, fmt.Errorf("拉取 Helm Chart 失败: %w", err)
	}

	chartRef, err := loader.Load(chartPath)
	if err != nil {
		return DeployResult{}, fmt.Errorf("加载 Helm Chart 失败: %w", err)
	}

	release, exists, err := loadRelease(actionConfig, request.ReleaseName)
	if err != nil {
		return DeployResult{}, err
	}

	if !exists {
		install := helmaction.NewInstall(actionConfig)
		install.ReleaseName = request.ReleaseName
		install.Namespace = request.Namespace
		install.CreateNamespace = false
		install.Timeout = 5 * time.Minute
		install.Wait = false

		release, err = install.Run(chartRef, values)
		if err != nil {
			return DeployResult{}, fmt.Errorf("安装 Helm 应用失败: %w", err)
		}

		return serializeReleaseResult("installed", request, release), nil
	}

	upgrade := helmaction.NewUpgrade(actionConfig)
	upgrade.Namespace = request.Namespace
	upgrade.ResetValues = true
	upgrade.Timeout = 5 * time.Minute
	upgrade.Wait = false

	release, err = upgrade.Run(release.Name, chartRef, values)
	if err != nil {
		return DeployResult{}, fmt.Errorf("升级 Helm 应用失败: %w", err)
	}

	return serializeReleaseResult("upgraded", request, release), nil
}

func ensureNamespace(ctx context.Context, runtime *kube.Runtime, namespace string, create bool) error {
	if strings.TrimSpace(namespace) == "" {
		return fmt.Errorf("namespace is required")
	}

	_, err := runtime.Dynamic.Resource(namespaceGVR).Get(ctx, namespace, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("检查目标 namespace 失败: %w", err)
	}
	if !create {
		return fmt.Errorf("namespace %q 不存在，请先创建或勾选自动创建", namespace)
	}

	object := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Namespace",
			"metadata": map[string]any{
				"name": namespace,
				"labels": map[string]any{
					"app.kubernetes.io/managed-by": "kubefeel",
				},
			},
		},
	}

	if _, err := runtime.Dynamic.Resource(namespaceGVR).Create(ctx, object, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("创建 namespace %q 失败: %w", namespace, err)
	}

	return nil
}

func loadRelease(config *helmaction.Configuration, releaseName string) (*helmrelease.Release, bool, error) {
	getter := helmaction.NewGet(config)
	release, err := getter.Run(releaseName)
	if err == nil {
		return release, true, nil
	}
	if errors.Is(err, driver.ErrReleaseNotFound) || strings.Contains(strings.ToLower(err.Error()), "release: not found") {
		return nil, false, nil
	}

	return nil, false, fmt.Errorf("查询 Helm Release 失败: %w", err)
}

func serializeReleaseResult(operation string, request DeployRequest, release *helmrelease.Release) DeployResult {
	status := ""
	version := strings.TrimSpace(request.Version)
	revision := 0
	notes := ""
	if release != nil {
		status = release.Info.Status.String()
		revision = release.Version
		notes = strings.TrimSpace(release.Info.Notes)
		if version == "" && release.Chart != nil && release.Chart.Metadata != nil {
			version = release.Chart.Metadata.Version
		}
	}

	return DeployResult{
		Operation:   operation,
		ReleaseName: request.ReleaseName,
		Namespace:   request.Namespace,
		Chart:       request.ChartName,
		Version:     version,
		Revision:    revision,
		Status:      status,
		Notes:       notes,
	}
}

var namespaceGVR = schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}
