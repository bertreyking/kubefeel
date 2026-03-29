package provision

import (
	"fmt"
	"strings"
)

const (
	ImageRegistryPresetUpstream   = "upstream"
	ImageRegistryPresetDaocloud   = "daocloud"
	ImageRegistryPresetAliyunACR  = "aliyun-acr"
	ImageRegistryPresetCustom     = "custom"
	defaultDaocloudRegistryPrefix = "docker.m.daocloud.io"
)

type ImageRegistryPreset struct {
	Key              string `json:"key"`
	Label            string `json:"label"`
	Description      string `json:"description"`
	RequiresRegistry bool   `json:"requiresRegistry"`
	Placeholder      string `json:"placeholder,omitempty"`
	DefaultRegistry  string `json:"defaultRegistry,omitempty"`
	Recommended      bool   `json:"recommended"`
}

type imageRepositoryOverrides struct {
	DisplayValue string
	Kube         string
	GCR          string
	Docker       string
	Quay         string
	Github       string
}

func BuiltinImageRegistryPresets() []ImageRegistryPreset {
	presets := []ImageRegistryPreset{
		{
			Key:         ImageRegistryPresetUpstream,
			Label:       "默认上游",
			Description: "保持 Kubespray 默认仓库，直接从官方源拉取镜像。",
		},
		{
			Key:             ImageRegistryPresetDaocloud,
			Label:           "DaoCloud 代理",
			Description:     "开箱即用，适合直接代理 registry.k8s.io、docker.io、quay.io、ghcr.io。",
			DefaultRegistry: defaultDaocloudRegistryPrefix,
			Recommended:     true,
		},
		{
			Key:              ImageRegistryPresetAliyunACR,
			Label:            "Aliyun ACR",
			Description:      "适合提前将 Kubespray 所需镜像同步到阿里云 ACR 命名空间，再统一替换镜像前缀。",
			RequiresRegistry: true,
			Placeholder:      "例如：registry.cn-hangzhou.aliyuncs.com/your-namespace",
		},
		{
			Key:              ImageRegistryPresetCustom,
			Label:            "自定义前缀",
			Description:      "手动指定统一镜像前缀，平台会同时覆盖 registry.k8s.io、docker.io、quay.io、ghcr.io、gcr.io。",
			RequiresRegistry: true,
			Placeholder:      "例如：harbor.example.com/k8s",
		},
	}

	result := make([]ImageRegistryPreset, len(presets))
	copy(result, presets)
	return result
}

func ResolveImageRegistryPreset(rawPreset, rawRegistry string) (ImageRegistryPreset, error) {
	key := normalizeImageRegistryPreset(rawPreset, rawRegistry)
	for _, preset := range BuiltinImageRegistryPresets() {
		if preset.Key == key {
			return preset, nil
		}
	}

	return ImageRegistryPreset{}, fmt.Errorf("未找到镜像源方案 %q", key)
}

func normalizeImageRegistryPreset(rawPreset, rawRegistry string) string {
	switch raw := stringsToLowerTrim(rawPreset); raw {
	case ImageRegistryPresetUpstream,
		ImageRegistryPresetDaocloud,
		ImageRegistryPresetAliyunACR,
		ImageRegistryPresetCustom:
		return raw
	}

	if stringsToLowerTrim(rawRegistry) != "" {
		return ImageRegistryPresetCustom
	}

	return ImageRegistryPresetUpstream
}

func NormalizeImageRegistryPresetForJob(rawPreset, rawRegistry string) string {
	return normalizeImageRegistryPreset(rawPreset, rawRegistry)
}

func buildImageRepositoryOverrides(rawPreset, rawRegistry string) (ImageRegistryPreset, imageRepositoryOverrides, error) {
	preset, err := ResolveImageRegistryPreset(rawPreset, rawRegistry)
	if err != nil {
		return ImageRegistryPreset{}, imageRepositoryOverrides{}, err
	}

	switch preset.Key {
	case ImageRegistryPresetUpstream:
		return preset, imageRepositoryOverrides{}, nil
	case ImageRegistryPresetDaocloud:
		return preset, imageRepositoryOverrides{
			DisplayValue: defaultDaocloudRegistryPrefix,
			Kube:         defaultDaocloudRegistryPrefix,
			GCR:          defaultDaocloudRegistryPrefix,
			Docker:       defaultDaocloudRegistryPrefix,
			Quay:         defaultDaocloudRegistryPrefix,
			Github:       defaultDaocloudRegistryPrefix,
		}, nil
	case ImageRegistryPresetAliyunACR, ImageRegistryPresetCustom:
		value, err := normalizeImageRegistry(rawRegistry)
		if err != nil {
			return ImageRegistryPreset{}, imageRepositoryOverrides{}, err
		}
		if value == "" {
			return ImageRegistryPreset{}, imageRepositoryOverrides{}, fmt.Errorf("请填写镜像仓库地址")
		}

		return preset, imageRepositoryOverrides{
			DisplayValue: value,
			Kube:         value,
			GCR:          value,
			Docker:       value,
			Quay:         value,
			Github:       value,
		}, nil
	default:
		return ImageRegistryPreset{}, imageRepositoryOverrides{}, fmt.Errorf("未找到镜像源方案 %q", preset.Key)
	}
}

func stringsToLowerTrim(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}
