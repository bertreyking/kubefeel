package provision

import (
	"fmt"
	"strings"

	apimachineryversion "k8s.io/apimachinery/pkg/util/version"
)

const DefaultProvisionTemplateKey = "compat-133"

type ProvisionTemplate struct {
	Key                  string `json:"key"`
	Label                string `json:"label"`
	Description          string `json:"description"`
	KubesprayVersion     string `json:"kubesprayVersion"`
	KubesprayImage       string `json:"kubesprayImage"`
	MinKubernetesVersion string `json:"minKubernetesVersion"`
	MaxKubernetesVersion string `json:"maxKubernetesVersion"`
	VersionHint          string `json:"versionHint"`
	LegacyVersionPrefix  bool   `json:"legacyVersionPrefix"`
	Recommended          bool   `json:"recommended"`
}

func BuiltinProvisionTemplates() []ProvisionTemplate {
	templates := []ProvisionTemplate{
		{
			Key:                  "compat-126",
			Label:                "兼容 1.26.x",
			Description:          "历史环境模板，适合存量 1.26 集群。",
			KubesprayVersion:     "v2.24.0",
			KubesprayImage:       "quay.io/kubespray/kubespray:v2.24.0",
			MinKubernetesVersion: "1.26.0",
			MaxKubernetesVersion: "1.26.99",
			VersionHint:          "1.26.x",
			LegacyVersionPrefix:  true,
		},
		{
			Key:                  "compat-127-128",
			Label:                "兼容 1.27-1.28",
			Description:          "稳态模板，适合 1.27.x 与 1.28.x。",
			KubesprayVersion:     "v2.25.0",
			KubesprayImage:       "quay.io/kubespray/kubespray:v2.25.0",
			MinKubernetesVersion: "1.27.0",
			MaxKubernetesVersion: "1.28.99",
			VersionHint:          "1.27.x / 1.28.x",
			LegacyVersionPrefix:  true,
		},
		{
			Key:                  "compat-129-130",
			Label:                "兼容 1.29-1.30",
			Description:          "过渡模板，适合 1.29.x 与 1.30.x。",
			KubesprayVersion:     "v2.26.0",
			KubesprayImage:       "quay.io/kubespray/kubespray:v2.26.0",
			MinKubernetesVersion: "1.29.0",
			MaxKubernetesVersion: "1.30.99",
			VersionHint:          "1.29.x / 1.30.x",
			LegacyVersionPrefix:  true,
		},
		{
			Key:                  "compat-131-132",
			Label:                "兼容 1.31-1.32",
			Description:          "当前稳态模板，适合 1.31.x 与 1.32.x。",
			KubesprayVersion:     "v2.28.0",
			KubesprayImage:       "quay.io/kubespray/kubespray:v2.28.0",
			MinKubernetesVersion: "1.31.0",
			MaxKubernetesVersion: "1.32.99",
			VersionHint:          "1.31.x / 1.32.x",
			Recommended:          true,
		},
		{
			Key:                  DefaultProvisionTemplateKey,
			Label:                "兼容 1.33.x",
			Description:          "最新版模板，适合 1.33.x。",
			KubesprayVersion:     "v2.29.0",
			KubesprayImage:       DefaultKubesprayImage,
			MinKubernetesVersion: "1.33.0",
			MaxKubernetesVersion: "1.33.99",
			VersionHint:          "1.33.x",
		},
	}

	result := make([]ProvisionTemplate, len(templates))
	copy(result, templates)
	return result
}

func ResolveProvisionTemplate(templateKey, rawVersion string) (ProvisionTemplate, string, error) {
	templates := BuiltinProvisionTemplates()
	normalizedVersion := normalizeKubernetesVersion(rawVersion)

	if normalizedVersion == "" {
		if template, ok := findProvisionTemplateByKey(templates, templateKey); ok {
			return template, normalizedVersion, nil
		}
		if template, ok := findProvisionTemplateByKey(templates, DefaultProvisionTemplateKey); ok {
			return template, normalizedVersion, nil
		}
		return ProvisionTemplate{}, "", fmt.Errorf("未找到默认 Kubespray 模板")
	}

	selectedVersion, err := apimachineryversion.ParseGeneric(normalizedVersion)
	if err != nil {
		return ProvisionTemplate{}, "", fmt.Errorf("Kubernetes 版本格式不正确，请填写类似 1.31.8 的版本号")
	}

	if strings.TrimSpace(templateKey) != "" {
		template, ok := findProvisionTemplateByKey(templates, templateKey)
		if !ok {
			return ProvisionTemplate{}, "", fmt.Errorf("未找到 Kubespray 模板 %q", templateKey)
		}
		if err := ensureVersionInTemplate(template, selectedVersion, normalizedVersion); err != nil {
			return ProvisionTemplate{}, "", err
		}
		return template, normalizedVersion, nil
	}

	for _, template := range templates {
		if err := ensureVersionInTemplate(template, selectedVersion, normalizedVersion); err == nil {
			return template, normalizedVersion, nil
		}
	}

	return ProvisionTemplate{}, "", fmt.Errorf(
		"当前内置模板尚未覆盖 Kubernetes %s，请选择 1.26.x 到 1.33.x 之间的版本",
		normalizedVersion,
	)
}

func findProvisionTemplateByKey(templates []ProvisionTemplate, key string) (ProvisionTemplate, bool) {
	for _, template := range templates {
		if template.Key == key {
			return template, true
		}
	}

	return ProvisionTemplate{}, false
}

func ensureVersionInTemplate(
	template ProvisionTemplate,
	selectedVersion *apimachineryversion.Version,
	rawVersion string,
) error {
	minimum := apimachineryversion.MustParseGeneric(template.MinKubernetesVersion)
	if selectedVersion.LessThan(minimum) {
		return fmt.Errorf(
			"模板 %s 仅支持 Kubernetes %s 到 %s，当前填写的是 %s",
			template.Label,
			template.MinKubernetesVersion,
			template.MaxKubernetesVersion,
			rawVersion,
		)
	}

	maximum := apimachineryversion.MustParseGeneric(template.MaxKubernetesVersion)
	if maximum.LessThan(selectedVersion) {
		return fmt.Errorf(
			"模板 %s 仅支持 Kubernetes %s 到 %s，当前填写的是 %s",
			template.Label,
			template.MinKubernetesVersion,
			template.MaxKubernetesVersion,
			rawVersion,
		)
	}

	return nil
}
