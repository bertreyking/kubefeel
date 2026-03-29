package integration

type RepositoryProvider struct {
	Key                  string `json:"key"`
	Label                string `json:"label"`
	Description          string `json:"description"`
	EndpointHint         string `json:"endpointHint"`
	NamespaceLabel       string `json:"namespaceLabel"`
	NamespacePlaceholder string `json:"namespacePlaceholder"`
}

type ObservabilityKind struct {
	Key                  string `json:"key"`
	Label                string `json:"label"`
	Description          string `json:"description"`
	EndpointHint         string `json:"endpointHint"`
	DashboardCapable     bool   `json:"dashboardCapable"`
	DefaultDashboardPath string `json:"defaultDashboardPath,omitempty"`
}

func RepositoryProviders() []RepositoryProvider {
	return []RepositoryProvider{
		{
			Key:                  "registry",
			Label:                "Registry",
			Description:          "标准 OCI / Docker Registry 接入，适合原生 registry 或兼容 v2 API 的仓库。",
			EndpointHint:         "https://registry.example.com",
			NamespaceLabel:       "镜像空间前缀",
			NamespacePlaceholder: "team-a / library，可留空",
		},
		{
			Key:                  "harbor",
			Label:                "Harbor",
			Description:          "Harbor 仓库接入，建议填写 Harbor 的 Docker / OCI 域名。",
			EndpointHint:         "https://harbor.example.com",
			NamespaceLabel:       "镜像空间 / 前缀",
			NamespacePlaceholder: "library / team-a，可留空",
		},
		{
			Key:                  "nexus",
			Label:                "Nexus",
			Description:          "Nexus Docker Hosted / Proxy 仓库接入，建议填写 Docker 连接器地址。",
			EndpointHint:         "https://docker.nexus.example.com",
			NamespaceLabel:       "镜像空间前缀",
			NamespacePlaceholder: "project / backend，可留空",
		},
		{
			Key:                  "jfrog",
			Label:                "JFrog",
			Description:          "Artifactory Docker 仓库接入，建议填写 Docker / OCI 访问域名。",
			EndpointHint:         "https://docker.artifactory.example.com",
			NamespaceLabel:       "镜像空间前缀",
			NamespacePlaceholder: "docker-prod-local / team-a，可留空",
		},
	}
}

func ObservabilityKinds() []ObservabilityKind {
	return []ObservabilityKind{
		{
			Key:              "prometheus",
			Label:            "Prometheus",
			Description:      "Prometheus 查询地址，平台会校验构建信息和查询接口。",
			EndpointHint:     "https://prometheus.example.com",
			DashboardCapable: false,
		},
		{
			Key:              "victoriametrics",
			Label:            "VictoriaMetrics",
			Description:      "VictoriaMetrics 查询入口，建议填写带 /prometheus 前缀的查询地址。",
			EndpointHint:     "https://vmselect.example.com/select/0/prometheus",
			DashboardCapable: false,
		},
		{
			Key:                  "grafana",
			Label:                "Grafana",
			Description:          "Grafana 控制台入口，平台会通过同源代理嵌入仪表盘页面。",
			EndpointHint:         "https://grafana.example.com",
			DashboardCapable:     true,
			DefaultDashboardPath: "/dashboards",
		},
	}
}

func ValidRepositoryProvider(kind string) bool {
	for _, item := range RepositoryProviders() {
		if item.Key == kind {
			return true
		}
	}

	return false
}

func ValidObservabilityKind(kind string) bool {
	for _, item := range ObservabilityKinds() {
		if item.Key == kind {
			return true
		}
	}

	return false
}
