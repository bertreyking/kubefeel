package api

import (
	"fmt"
	"net/http"
	"testing"
)

func TestBuildGrafanaDashboardDefinitionBuildsStarterWhenDefinitionEmpty(t *testing.T) {
	dashboard, err := buildGrafanaDashboardDefinition("业务总览", []string{"ops", "prod"}, "")
	if err != nil {
		t.Fatalf("build starter dashboard: %v", err)
	}

	if dashboard["title"] != "业务总览" {
		t.Fatalf("expected title to be kept, got %#v", dashboard["title"])
	}

	tags, ok := dashboard["tags"].([]string)
	if !ok || len(tags) != 2 {
		t.Fatalf("expected normalized tags, got %#v", dashboard["tags"])
	}

	panels, ok := dashboard["panels"].([]any)
	if !ok || len(panels) == 0 {
		t.Fatalf("expected starter panel, got %#v", dashboard["panels"])
	}
}

func TestBuildGrafanaDashboardDefinitionNormalizesImportedJSON(t *testing.T) {
	raw := `{
		"dashboard": {
			"id": 12,
			"uid": "old-uid",
			"title": "old",
			"version": 9,
			"panels": []
		}
	}`

	dashboard, err := buildGrafanaDashboardDefinition("新标题", []string{"biz", "biz", "ops"}, raw)
	if err != nil {
		t.Fatalf("build dashboard from json: %v", err)
	}

	if dashboard["title"] != "新标题" {
		t.Fatalf("expected title to be overwritten, got %#v", dashboard["title"])
	}
	if _, ok := dashboard["id"]; ok {
		t.Fatalf("expected id to be removed, got %#v", dashboard["id"])
	}
	if _, ok := dashboard["uid"]; ok {
		t.Fatalf("expected uid to be removed, got %#v", dashboard["uid"])
	}
	if dashboard["version"] != 0 {
		t.Fatalf("expected version to reset to 0, got %#v", dashboard["version"])
	}

	tags, ok := dashboard["tags"].([]string)
	if !ok || len(tags) != 2 {
		t.Fatalf("expected deduplicated tags, got %#v", dashboard["tags"])
	}
}

func TestValidateGrafanaProxyRequestAllowsReadAndQueryPaths(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "root get", method: http.MethodGet, path: "/"},
		{name: "dashboard get", method: http.MethodGet, path: "/d/kubefeel/demo"},
		{name: "datasource query post", method: http.MethodPost, path: "/api/ds/query"},
		{name: "datasource proxy post", method: http.MethodPost, path: "/api/datasources/proxy/uid/prometheus/api/v1/query_range"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, err := validateGrafanaProxyRequest(test.method, test.path)
			if err != nil {
				t.Fatalf("expected request to be allowed, got err=%v", err)
			}
			if status != http.StatusOK {
				t.Fatalf("expected status 200, got %d", status)
			}
		})
	}
}

func TestValidateGrafanaProxyRequestRejectsMutatingPaths(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
	}{
		{name: "dashboard write", method: http.MethodPost, path: "/api/dashboards/db", expectedStatus: http.StatusForbidden},
		{name: "folder delete", method: http.MethodDelete, path: "/api/folders/ops", expectedStatus: http.StatusMethodNotAllowed},
		{name: "org update", method: http.MethodPut, path: "/api/org/preferences", expectedStatus: http.StatusMethodNotAllowed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, err := validateGrafanaProxyRequest(test.method, test.path)
			if err == nil {
				t.Fatalf("expected request to be rejected")
			}
			if status != test.expectedStatus {
				t.Fatalf("expected status %d, got %d", test.expectedStatus, status)
			}
		})
	}
}

func TestIsGrafanaSessionAuthError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "session rotate payload",
			err:      fmt.Errorf(`search failed: {"message":"Unauthorized","messageId":"session.token.rotate","statusCode":401}`),
			expected: true,
		},
		{
			name:     "plain unauthorized",
			err:      fmt.Errorf("Unauthorized"),
			expected: true,
		},
		{
			name:     "non auth failure",
			err:      fmt.Errorf("dashboard not found"),
			expected: false,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := isGrafanaSessionAuthError(test.err); actual != test.expected {
				t.Fatalf("expected %v, got %v", test.expected, actual)
			}
		})
	}
}
