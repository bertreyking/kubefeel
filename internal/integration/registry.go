package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	defaultRepositoryLimit = 200
	defaultArtifactLimit   = 500
	defaultTagsPerRepo     = 50
)

type RegistryProbe struct {
	CatalogCountHint int    `json:"catalogCountHint"`
	Message          string `json:"message"`
}

type RegistryArtifact struct {
	Repository string     `json:"repository"`
	Tag        string     `json:"tag"`
	Digest     string     `json:"digest"`
	BuildTime  *time.Time `json:"buildTime,omitempty"`
}

type RegistryArtifactVersion struct {
	Tag       string     `json:"tag"`
	Digest    string     `json:"digest,omitempty"`
	BuildTime *time.Time `json:"buildTime,omitempty"`
}

type RegistryImage struct {
	Name            string                    `json:"name"`
	Repository      string                    `json:"repository"`
	VersionCount    int                       `json:"versionCount"`
	LatestBuildTime *time.Time                `json:"latestBuildTime,omitempty"`
	Versions        []RegistryArtifactVersion `json:"versions"`
}

type RegistryImageSpace struct {
	Name         string          `json:"name"`
	ImageCount   int             `json:"imageCount"`
	VersionCount int             `json:"versionCount"`
	Images       []RegistryImage `json:"images"`
}

type RegistryArtifactList struct {
	Items          []RegistryArtifact   `json:"items"`
	ImageSpaces    []RegistryImageSpace `json:"imageSpaces,omitempty"`
	RepositoryHint int                  `json:"repositoryHint"`
	Truncated      bool                 `json:"truncated"`
}

type RegistryClient struct {
	client *endpointClient
}

func NewRegistryClient(cfg EndpointConfig) (*RegistryClient, error) {
	client, err := newEndpointClient(cfg)
	if err != nil {
		return nil, err
	}

	return &RegistryClient{client: client}, nil
}

func (c *RegistryClient) Test(ctx context.Context) (RegistryProbe, error) {
	if _, _, err := c.client.DoBytes(ctx, "GET", "/v2/", nil, nil, "registry:catalog:*"); err != nil {
		return RegistryProbe{}, err
	}

	repositories, _, err := c.listCatalog(ctx, 3, "")
	if err != nil {
		return RegistryProbe{}, err
	}

	return RegistryProbe{
		CatalogCountHint: len(repositories),
		Message:          "仓库 API 已连通，可继续读取镜像目录。",
	}, nil
}

func (c *RegistryClient) ListArtifacts(
	ctx context.Context,
	namespace string,
	search string,
	limit int,
) (RegistryArtifactList, error) {
	if limit <= 0 {
		limit = defaultArtifactLimit
	}

	prefix := normalizeNamespaceFilter(namespace)
	query := strings.ToLower(strings.TrimSpace(search))
	repositories, truncated, err := c.listCatalog(ctx, defaultRepositoryLimit, prefix)
	if err != nil {
		return RegistryArtifactList{}, err
	}

	items := make([]RegistryArtifact, 0, limit)
	artifactTruncated := truncated
	for _, repository := range repositories {
		tags, tagTruncated, tagErr := c.listTags(ctx, repository, defaultTagsPerRepo)
		if tagErr != nil {
			if strings.Contains(strings.ToLower(tagErr.Error()), "manifest unknown") {
				continue
			}
			return RegistryArtifactList{}, tagErr
		}
		if tagTruncated {
			artifactTruncated = true
		}

		sort.SliceStable(tags, func(i, j int) bool {
			return tags[i] > tags[j]
		})

		for _, tag := range tags {
			if query != "" && !strings.Contains(strings.ToLower(repository), query) && !strings.Contains(strings.ToLower(tag), query) {
				continue
			}

			metadata, metadataErr := c.inspectTag(ctx, repository, tag)
			if metadataErr != nil {
				metadata = RegistryArtifact{Repository: repository, Tag: tag}
			}

			items = append(items, RegistryArtifact{
				Repository: repository,
				Tag:        tag,
				Digest:     metadata.Digest,
				BuildTime:  metadata.BuildTime,
			})
			if len(items) >= limit {
				artifactTruncated = true
				break
			}
		}

		if len(items) >= limit {
			break
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		left := items[i].BuildTime
		right := items[j].BuildTime
		switch {
		case left == nil && right == nil:
			if items[i].Repository == items[j].Repository {
				return items[i].Tag > items[j].Tag
			}
			return items[i].Repository < items[j].Repository
		case left == nil:
			return false
		case right == nil:
			return true
		default:
			return left.After(*right)
		}
	})

	return RegistryArtifactList{
		Items:          items,
		ImageSpaces:    buildRegistryImageSpaces(items),
		RepositoryHint: len(repositories),
		Truncated:      artifactTruncated,
	}, nil
}

func buildRegistryImageSpaces(items []RegistryArtifact) []RegistryImageSpace {
	spaceMap := make(map[string]map[string][]RegistryArtifactVersion)

	for _, item := range items {
		repository := strings.TrimSpace(item.Repository)
		if repository == "" {
			continue
		}

		segments := strings.Split(repository, "/")
		spaceName := "未分组"
		if len(segments) > 1 {
			spaceName = segments[0]
		}

		if _, ok := spaceMap[spaceName]; !ok {
			spaceMap[spaceName] = make(map[string][]RegistryArtifactVersion)
		}

		spaceMap[spaceName][repository] = append(spaceMap[spaceName][repository], RegistryArtifactVersion{
			Tag:       item.Tag,
			Digest:    item.Digest,
			BuildTime: item.BuildTime,
		})
	}

	spaces := make([]RegistryImageSpace, 0, len(spaceMap))
	for spaceName, imageMap := range spaceMap {
		images := make([]RegistryImage, 0, len(imageMap))
		versionTotal := 0

		for repository, versions := range imageMap {
			sort.SliceStable(versions, func(i, j int) bool {
				left := versions[i].BuildTime
				right := versions[j].BuildTime
				switch {
				case left == nil && right == nil:
					return versions[i].Tag > versions[j].Tag
				case left == nil:
					return false
				case right == nil:
					return true
				default:
					return left.After(*right)
				}
			})

			segments := strings.Split(repository, "/")
			imageName := repository
			if len(segments) > 1 {
				imageName = strings.Join(segments[1:], "/")
			}

			images = append(images, RegistryImage{
				Name:            imageName,
				Repository:      repository,
				VersionCount:    len(versions),
				LatestBuildTime: versions[0].BuildTime,
				Versions:        versions,
			})
			versionTotal += len(versions)
		}

		sort.SliceStable(images, func(i, j int) bool {
			return images[i].Name < images[j].Name
		})

		spaces = append(spaces, RegistryImageSpace{
			Name:         spaceName,
			ImageCount:   len(images),
			VersionCount: versionTotal,
			Images:       images,
		})
	}

	sort.SliceStable(spaces, func(i, j int) bool {
		return spaces[i].Name < spaces[j].Name
	})

	return spaces
}

func (c *RegistryClient) listCatalog(
	ctx context.Context,
	limit int,
	namespace string,
) ([]string, bool, error) {
	collected := []string{}
	last := ""
	truncated := false

	for len(collected) < limit {
		pageSize := limit - len(collected)
		if pageSize > 50 {
			pageSize = 50
		}

		query := url.Values{}
		query.Set("n", fmt.Sprintf("%d", pageSize))
		if last != "" {
			query.Set("last", last)
		}

		var payload struct {
			Repositories []string `json:"repositories"`
		}
		if err := c.client.DoJSON(ctx, "GET", "/v2/_catalog", query, nil, "registry:catalog:*", &payload); err != nil {
			return nil, false, err
		}

		if len(payload.Repositories) == 0 {
			break
		}

		for _, repository := range payload.Repositories {
			if namespace != "" && !strings.HasPrefix(repository, namespace+"/") && repository != namespace {
				continue
			}
			collected = append(collected, repository)
			if len(collected) >= limit {
				truncated = true
				break
			}
		}

		last = payload.Repositories[len(payload.Repositories)-1]
		if len(payload.Repositories) < pageSize {
			break
		}
	}

	return collected, truncated, nil
}

func (c *RegistryClient) listTags(ctx context.Context, repository string, limit int) ([]string, bool, error) {
	collected := []string{}
	last := ""
	truncated := false

	for len(collected) < limit {
		pageSize := limit - len(collected)
		if pageSize > 50 {
			pageSize = 50
		}

		query := url.Values{}
		query.Set("n", fmt.Sprintf("%d", pageSize))
		if last != "" {
			query.Set("last", last)
		}

		var payload struct {
			Name string   `json:"name"`
			Tags []string `json:"tags"`
		}
		scope := fmt.Sprintf("repository:%s:pull", repository)
		path := fmt.Sprintf("/v2/%s/tags/list", strings.TrimPrefix(repository, "/"))
		if err := c.client.DoJSON(ctx, "GET", path, query, nil, scope, &payload); err != nil {
			return nil, false, err
		}

		if len(payload.Tags) == 0 {
			break
		}

		collected = append(collected, payload.Tags...)
		if len(payload.Tags) < pageSize {
			break
		}

		last = payload.Tags[len(payload.Tags)-1]
		if len(collected) >= limit {
			truncated = true
			break
		}
	}

	return uniqueStrings(collected), truncated, nil
}

func (c *RegistryClient) inspectTag(ctx context.Context, repository string, tag string) (RegistryArtifact, error) {
	manifest, err := c.fetchManifest(ctx, repository, tag)
	if err != nil {
		return RegistryArtifact{}, err
	}

	if manifest.MediaType == "application/vnd.oci.image.index.v1+json" || manifest.MediaType == "application/vnd.docker.distribution.manifest.list.v2+json" {
		childDigest := manifest.preferredChildDigest()
		if childDigest == "" {
			return RegistryArtifact{
				Repository: repository,
				Tag:        tag,
				Digest:     manifest.Digest,
			}, nil
		}

		manifest, err = c.fetchManifest(ctx, repository, childDigest)
		if err != nil {
			return RegistryArtifact{}, err
		}
	}

	artifact := RegistryArtifact{
		Repository: repository,
		Tag:        tag,
		Digest:     manifest.Digest,
	}
	if manifest.ConfigDigest == "" {
		return artifact, nil
	}

	body, _, err := c.client.DoBytes(
		ctx,
		"GET",
		fmt.Sprintf("/v2/%s/blobs/%s", strings.TrimPrefix(repository, "/"), manifest.ConfigDigest),
		nil,
		nil,
		fmt.Sprintf("repository:%s:pull", repository),
	)
	if err != nil {
		return artifact, nil
	}

	var payload struct {
		Created string `json:"created"`
		History []struct {
			Created string `json:"created"`
		} `json:"history"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return artifact, nil
	}

	if parsed := parseBuildTime(payload.Created); parsed != nil {
		artifact.BuildTime = parsed
		return artifact, nil
	}

	for _, entry := range payload.History {
		if parsed := parseBuildTime(entry.Created); parsed != nil {
			artifact.BuildTime = parsed
			return artifact, nil
		}
	}

	return artifact, nil
}

type registryManifest struct {
	MediaType    string
	Digest       string
	ConfigDigest string
	Manifests    []registryManifestDescriptor
}

type registryManifestDescriptor struct {
	Digest   string `json:"digest"`
	Platform struct {
		Architecture string `json:"architecture"`
		OS           string `json:"os"`
	} `json:"platform"`
}

func (m registryManifest) preferredChildDigest() string {
	for _, item := range m.Manifests {
		if item.Platform.Architecture == "amd64" && item.Platform.OS == "linux" {
			return item.Digest
		}
	}
	if len(m.Manifests) > 0 {
		return m.Manifests[0].Digest
	}

	return ""
}

func (c *RegistryClient) fetchManifest(ctx context.Context, repository string, reference string) (registryManifest, error) {
	headers := map[string]string{
		"Accept": strings.Join([]string{
			"application/vnd.oci.image.index.v1+json",
			"application/vnd.oci.image.manifest.v1+json",
			"application/vnd.docker.distribution.manifest.list.v2+json",
			"application/vnd.docker.distribution.manifest.v2+json",
		}, ", "),
	}
	body, responseHeaders, err := c.client.DoBytes(
		ctx,
		"GET",
		fmt.Sprintf("/v2/%s/manifests/%s", strings.TrimPrefix(repository, "/"), reference),
		nil,
		headers,
		fmt.Sprintf("repository:%s:pull", repository),
	)
	if err != nil {
		return registryManifest{}, err
	}

	var payload struct {
		MediaType string `json:"mediaType"`
		Config    struct {
			Digest string `json:"digest"`
		} `json:"config"`
		Manifests []registryManifestDescriptor `json:"manifests"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return registryManifest{}, err
	}

	digest := strings.TrimSpace(responseHeaders.Get("Docker-Content-Digest"))
	if digest == "" && strings.HasPrefix(reference, "sha256:") {
		digest = reference
	}

	return registryManifest{
		MediaType:    payload.MediaType,
		Digest:       digest,
		ConfigDigest: payload.Config.Digest,
		Manifests:    payload.Manifests,
	}, nil
}

func normalizeNamespaceFilter(value string) string {
	return strings.Trim(strings.TrimSpace(value), "/")
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func parseBuildTime(raw string) *time.Time {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil
	}

	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err == nil {
		return &parsed
	}

	parsed, err = time.Parse(time.RFC3339, value)
	if err == nil {
		return &parsed
	}

	return nil
}
