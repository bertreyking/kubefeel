package helm

import (
	"os"
	"path/filepath"
)

type AppTemplate struct {
	Key             string `json:"key"`
	Label           string `json:"label"`
	Description     string `json:"description"`
	Category        string `json:"category"`
	RepoURL         string `json:"repoURL"`
	ChartName       string `json:"chartName"`
	DefaultVersion  string `json:"defaultVersion"`
	ReleaseNameHint string `json:"releaseNameHint"`
	NamespaceHint   string `json:"namespaceHint"`
	Values          string `json:"values"`
	LocalChartPath  string `json:"-"`
}

func BuiltinTemplates() []AppTemplate {
	return []AppTemplate{
		{
			Key:             "nginx",
			Label:           "Nginx",
			Description:     "轻量 Web 服务入口，内置本地 Helm Starter Chart，默认可直接部署。",
			Category:        "web",
			RepoURL:         "",
			ChartName:       "kubefeel-nginx-starter",
			ReleaseNameHint: "nginx",
			NamespaceHint:   "default",
			LocalChartPath:  localChartPath("nginx-starter"),
			Values: `service:
  type: ClusterIP
replicaCount: 1
`,
		},
		{
			Key:             "redis",
			Label:           "Redis",
			Description:     "常用缓存服务，适合会话、缓存和轻量队列场景。",
			Category:        "middleware",
			RepoURL:         "https://charts.bitnami.com/bitnami",
			ChartName:       "redis",
			ReleaseNameHint: "redis",
			NamespaceHint:   "default",
			Values: `architecture: standalone
auth:
  enabled: false
`,
		},
		{
			Key:             "mysql",
			Label:           "MySQL",
			Description:     "单实例 MySQL 模版，适合测试环境或轻量业务场景。",
			Category:        "database",
			RepoURL:         "https://charts.bitnami.com/bitnami",
			ChartName:       "mysql",
			ReleaseNameHint: "mysql",
			NamespaceHint:   "default",
			Values: `auth:
  rootPassword: change-me
primary:
  persistence:
    enabled: true
    size: 8Gi
`,
		},
		{
			Key:             "prometheus",
			Label:           "Prometheus",
			Description:     "常用监控采集组件，适合快速补齐指标采集能力。",
			Category:        "observability",
			RepoURL:         "https://prometheus-community.github.io/helm-charts",
			ChartName:       "prometheus",
			ReleaseNameHint: "prometheus",
			NamespaceHint:   "monitoring",
			Values: `server:
  persistentVolume:
    enabled: true
    size: 10Gi
alertmanager:
  enabled: true
`,
		},
		{
			Key:             "grafana",
			Label:           "Grafana",
			Description:     "可视化看板组件，可与现有 Prometheus 或 VictoriaMetrics 搭配使用。",
			Category:        "observability",
			RepoURL:         "https://grafana.github.io/helm-charts",
			ChartName:       "grafana",
			ReleaseNameHint: "grafana",
			NamespaceHint:   "monitoring",
			Values: `service:
  type: ClusterIP
persistence:
  enabled: true
  size: 10Gi
`,
		},
	}
}

func LookupTemplate(key string) (AppTemplate, bool) {
	for _, item := range BuiltinTemplates() {
		if item.Key == key {
			return item, true
		}
	}

	return AppTemplate{}, false
}

func localChartPath(name string) string {
	workingDir, err := os.Getwd()
	if err != nil {
		return filepath.Join("charts", name)
	}

	return filepath.Join(workingDir, "charts", name)
}
