package api

import (
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
