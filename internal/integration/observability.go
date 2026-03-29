package integration

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

type ObservabilityProbe struct {
	Message string `json:"message"`
	Version string `json:"version"`
}

type ObservabilityClient struct {
	client *endpointClient
}

func NewObservabilityClient(cfg EndpointConfig) (*ObservabilityClient, error) {
	client, err := newEndpointClient(cfg)
	if err != nil {
		return nil, err
	}

	return &ObservabilityClient{client: client}, nil
}

func (c *ObservabilityClient) BaseURL() string {
	return c.client.BaseURL()
}

func (c *ObservabilityClient) Test(ctx context.Context, kind string) (ObservabilityProbe, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "prometheus":
		return c.testPrometheus(ctx, "/api/v1/status/buildinfo")
	case "victoriametrics":
		if probe, err := c.testPrometheus(ctx, "/api/v1/status/buildinfo"); err == nil {
			return probe, nil
		}
		if probe, err := c.testPrometheus(ctx, "/prometheus/api/v1/status/buildinfo"); err == nil {
			return probe, nil
		}
		if _, _, err := c.client.DoBytes(ctx, "GET", "/health", nil, nil, ""); err == nil {
			return ObservabilityProbe{
				Message: "VictoriaMetrics 健康检查通过，可继续接入查询入口。",
			}, nil
		}
		if _, _, err := c.client.DoBytes(ctx, "GET", "/-/healthy", nil, nil, ""); err == nil {
			return ObservabilityProbe{
				Message: "VictoriaMetrics 健康检查通过，可继续接入查询入口。",
			}, nil
		}
		return ObservabilityProbe{}, fmt.Errorf("victoriametrics endpoint check failed")
	case "grafana":
		var payload struct {
			Version  string `json:"version"`
			Database string `json:"database"`
		}
		if err := c.client.DoJSON(ctx, "GET", "/api/health", nil, nil, "", &payload); err != nil {
			return ObservabilityProbe{}, err
		}

		detail := "Grafana 已连通，可作为嵌入式仪表盘入口。"
		if strings.TrimSpace(payload.Database) != "" {
			detail = fmt.Sprintf("Grafana 已连通，数据库状态 %s。", payload.Database)
		}
		return ObservabilityProbe{
			Message: detail,
			Version: strings.TrimSpace(payload.Version),
		}, nil
	default:
		return ObservabilityProbe{}, fmt.Errorf("unsupported observability kind")
	}
}

func (c *ObservabilityClient) testPrometheus(ctx context.Context, rawPath string) (ObservabilityProbe, error) {
	var payload struct {
		Status string `json:"status"`
		Data   struct {
			Version string `json:"version"`
			Branch  string `json:"branch"`
		} `json:"data"`
	}
	if err := c.client.DoJSON(ctx, "GET", rawPath, nil, nil, "", &payload); err != nil {
		return ObservabilityProbe{}, err
	}

	version := strings.TrimSpace(payload.Data.Version)
	message := "指标查询接口可用。"
	if version != "" {
		message = fmt.Sprintf("指标查询接口可用，当前版本 %s。", version)
	}

	return ObservabilityProbe{
		Message: message,
		Version: version,
	}, nil
}

func BuildGrafanaEmbedPath(basePath string) string {
	trimmed := strings.TrimSpace(basePath)
	if trimmed == "" {
		return "/dashboards"
	}
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		if parsed, err := url.Parse(trimmed); err == nil {
			trimmed = parsed.Path
			if parsed.RawQuery != "" {
				trimmed += "?" + parsed.RawQuery
			}
		}
	}
	if !strings.HasPrefix(trimmed, "/") {
		trimmed = "/" + trimmed
	}
	return trimmed
}
