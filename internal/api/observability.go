package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httputil"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"multikube-manager/internal/integration"
	"multikube-manager/internal/model"

	"github.com/gin-gonic/gin"
)

type observabilitySourcePayload struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	Description   string `json:"description"`
	Endpoint      string `json:"endpoint"`
	Username      string `json:"username"`
	Secret        string `json:"secret"`
	DashboardPath string `json:"dashboardPath"`
	SkipTLSVerify bool   `json:"skipTLSVerify"`
}

type grafanaSessionCacheEntry struct {
	cookieHeader string
	expiresAt    time.Time
}

type grafanaSearchItem struct {
	ID          int      `json:"id"`
	UID         string   `json:"uid"`
	Title       string   `json:"title"`
	Type        string   `json:"type"`
	URL         string   `json:"url"`
	FolderID    int      `json:"folderId"`
	FolderUID   string   `json:"folderUid"`
	FolderTitle string   `json:"folderTitle"`
	Tags        []string `json:"tags"`
	IsStarred   bool     `json:"isStarred"`
}

type grafanaCatalogFolder struct {
	UID            string                    `json:"uid"`
	Title          string                    `json:"title"`
	IsGeneral      bool                      `json:"isGeneral"`
	DashboardCount int                       `json:"dashboardCount"`
	Dashboards     []grafanaCatalogDashboard `json:"dashboards"`
}

type grafanaCatalogDashboard struct {
	UID         string   `json:"uid"`
	Title       string   `json:"title"`
	URL         string   `json:"url"`
	FolderUID   string   `json:"folderUid"`
	FolderTitle string   `json:"folderTitle"`
	Tags        []string `json:"tags"`
	IsStarred   bool     `json:"isStarred"`
}

type grafanaFolderPayload struct {
	Title string `json:"title"`
}

type grafanaDashboardPayload struct {
	Title      string   `json:"title"`
	FolderUID  string   `json:"folderUid"`
	Tags       []string `json:"tags"`
	Definition string   `json:"definition"`
}

type grafanaFolderResponse struct {
	ID    int    `json:"id"`
	UID   string `json:"uid"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type grafanaFolderDetailResponse struct {
	ID      int    `json:"id"`
	UID     string `json:"uid"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Version int    `json:"version"`
}

type grafanaDashboardCreateResponse struct {
	ID      int    `json:"id"`
	UID     string `json:"uid"`
	URL     string `json:"url"`
	Status  string `json:"status"`
	Slug    string `json:"slug"`
	Version int    `json:"version"`
}

type grafanaDashboardDetailResponse struct {
	Dashboard map[string]any `json:"dashboard"`
	Meta      struct {
		FolderUID   string `json:"folderUid"`
		FolderTitle string `json:"folderTitle"`
		URL         string `json:"url"`
		Slug        string `json:"slug"`
		Provisioned bool   `json:"provisioned"`
		CanSave     bool   `json:"canSave"`
		CanEdit     bool   `json:"canEdit"`
		CanDelete   bool   `json:"canDelete"`
	} `json:"meta"`
}

var (
	grafanaAppURLPattern    = regexp.MustCompile(`"appUrl":"[^"]*"`)
	grafanaAppSubURLPattern = regexp.MustCompile(`"appSubUrl":"[^"]*"`)
)

func (s *Server) observabilityKindCatalog(c *gin.Context) {
	respondData(c, http.StatusOK, integration.ObservabilityKinds())
}

func (s *Server) listObservabilitySources(c *gin.Context) {
	var items []model.ObservabilitySource
	if err := s.db.Order("created_at desc").Find(&items).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "查询数据源失败")
		return
	}

	result := make([]gin.H, 0, len(items))
	for _, item := range items {
		result = append(result, serializeObservabilitySource(item))
	}

	respondData(c, http.StatusOK, result)
}

func (s *Server) testObservabilitySource(c *gin.Context) {
	var input observabilitySourcePayload
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, http.StatusBadRequest, "invalid observability payload")
		return
	}

	probe, err := s.probeObservabilitySource(c.Request.Context(), input)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	respondData(c, http.StatusOK, probe)
}

func (s *Server) testStoredObservabilitySource(c *gin.Context) {
	item, endpoint, err := s.loadObservabilityEndpoint(c, false)
	if err != nil {
		return
	}

	probe, err := s.probeObservabilitySource(c.Request.Context(), observabilitySourcePayload{
		Name:          item.Name,
		Type:          item.Type,
		Description:   item.Description,
		Endpoint:      item.Endpoint,
		Username:      item.Username,
		Secret:        endpoint.Secret,
		DashboardPath: item.DashboardPath,
		SkipTLSVerify: item.SkipTLSVerify,
	})
	if err != nil {
		item.Status = "error"
		item.LastError = err.Error()
		item.LastCheckedAt = nowPtr()
		_ = s.db.Save(item).Error
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	item.Status = "connected"
	item.LastError = ""
	item.LastCheckedAt = nowPtr()
	_ = s.db.Save(item).Error

	respondData(c, http.StatusOK, gin.H{
		"source": serializeObservabilitySource(*item),
		"probe":  probe,
	})
}

func (s *Server) createObservabilitySource(c *gin.Context) {
	var input observabilitySourcePayload
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, http.StatusBadRequest, "invalid observability payload")
		return
	}

	probe, err := s.probeObservabilitySource(c.Request.Context(), input)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	secretEncrypted := ""
	if strings.TrimSpace(input.Secret) != "" {
		secretEncrypted, err = s.kubeFactory.Encrypt(strings.TrimSpace(input.Secret))
		if err != nil {
			respondError(c, http.StatusInternalServerError, "加密数据源凭据失败")
			return
		}
	}

	item := model.ObservabilitySource{
		Name:            strings.TrimSpace(input.Name),
		Type:            strings.TrimSpace(input.Type),
		Description:     strings.TrimSpace(input.Description),
		Endpoint:        strings.TrimSpace(input.Endpoint),
		Username:        strings.TrimSpace(input.Username),
		SecretEncrypted: secretEncrypted,
		DashboardPath:   normalizeDashboardPath(input.DashboardPath, input.Type),
		SkipTLSVerify:   input.SkipTLSVerify,
		Status:          "connected",
		LastError:       "",
		LastCheckedAt:   nowPtr(),
	}

	if err := s.db.Create(&item).Error; err != nil {
		respondError(c, http.StatusBadRequest, "保存数据源失败，名称可能已存在")
		return
	}

	s.clearGrafanaSessionCache(item.ID)

	respondData(c, http.StatusCreated, gin.H{
		"source": serializeObservabilitySource(item),
		"probe":  probe,
	})
}

func (s *Server) updateObservabilitySource(c *gin.Context) {
	identifier, err := parseUintParam(c, "id")
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid datasource id")
		return
	}

	var item model.ObservabilitySource
	if err := s.db.First(&item, identifier).Error; err != nil {
		respondError(c, http.StatusNotFound, "datasource not found")
		return
	}

	var input observabilitySourcePayload
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, http.StatusBadRequest, "invalid observability payload")
		return
	}

	secretValue := strings.TrimSpace(input.Secret)
	if secretValue == "" && item.SecretEncrypted != "" {
		if decrypted, decryptErr := s.kubeFactory.Decrypt(item.SecretEncrypted); decryptErr == nil {
			secretValue = decrypted
		}
	}

	payload := observabilitySourcePayload{
		Name:          firstNonEmpty(input.Name, item.Name),
		Type:          firstNonEmpty(input.Type, item.Type),
		Description:   firstNonEmpty(input.Description, item.Description),
		Endpoint:      firstNonEmpty(input.Endpoint, item.Endpoint),
		Username:      firstNonEmpty(input.Username, item.Username),
		Secret:        secretValue,
		DashboardPath: firstNonEmpty(input.DashboardPath, item.DashboardPath),
		SkipTLSVerify: input.SkipTLSVerify,
	}

	probe, err := s.probeObservabilitySource(c.Request.Context(), payload)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	if strings.TrimSpace(input.Name) != "" {
		item.Name = strings.TrimSpace(input.Name)
	}
	if strings.TrimSpace(input.Type) != "" {
		item.Type = strings.TrimSpace(input.Type)
	}
	item.Description = strings.TrimSpace(input.Description)
	if strings.TrimSpace(input.Endpoint) != "" {
		item.Endpoint = strings.TrimSpace(input.Endpoint)
	}
	item.Username = strings.TrimSpace(payload.Username)
	item.DashboardPath = normalizeDashboardPath(payload.DashboardPath, payload.Type)
	item.SkipTLSVerify = payload.SkipTLSVerify
	item.Status = "connected"
	item.LastError = ""
	item.LastCheckedAt = nowPtr()

	secretEncrypted := ""
	if strings.TrimSpace(secretValue) != "" {
		secretEncrypted, err = s.kubeFactory.Encrypt(secretValue)
		if err != nil {
			respondError(c, http.StatusInternalServerError, "加密数据源凭据失败")
			return
		}
	}
	item.SecretEncrypted = secretEncrypted

	if err := s.db.Save(&item).Error; err != nil {
		respondError(c, http.StatusBadRequest, "更新数据源失败")
		return
	}

	s.clearGrafanaSessionCache(item.ID)

	respondData(c, http.StatusOK, gin.H{
		"source": serializeObservabilitySource(item),
		"probe":  probe,
	})
}

func (s *Server) deleteObservabilitySource(c *gin.Context) {
	identifier, err := parseUintParam(c, "id")
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid datasource id")
		return
	}

	if err := s.db.Delete(&model.ObservabilitySource{}, identifier).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "删除数据源失败")
		return
	}

	s.clearGrafanaSessionCache(identifier)

	respondNoContent(c)
}

func (s *Server) listGrafanaCatalog(c *gin.Context) {
	source, endpoint, err := s.loadObservabilityEndpoint(c, true)
	if err != nil {
		return
	}

	var items []grafanaSearchItem
	if err := s.withGrafanaSessionRetry(c.Request.Context(), source.ID, endpoint, func(sessionCookieHeader string) error {
		var innerErr error
		items, innerErr = listGrafanaSearchItems(c.Request.Context(), endpoint, sessionCookieHeader)
		return innerErr
	}); err != nil {
		source.Status = "error"
		source.LastError = err.Error()
		source.LastCheckedAt = nowPtr()
		_ = s.db.Save(source).Error
		respondError(c, http.StatusBadGateway, fmt.Sprintf("Grafana 目录加载失败: %v", err))
		return
	}

	source.Status = "connected"
	source.LastError = ""
	source.LastCheckedAt = nowPtr()
	_ = s.db.Save(source).Error

	folders, dashboardCount := buildGrafanaCatalog(items)

	respondData(c, http.StatusOK, gin.H{
		"folders":        folders,
		"folderCount":    len(folders),
		"dashboardCount": dashboardCount,
		"loadedAt":       time.Now(),
	})
}

func (s *Server) createGrafanaFolder(c *gin.Context) {
	source, endpoint, err := s.loadObservabilityEndpoint(c, true)
	if err != nil {
		return
	}

	var input grafanaFolderPayload
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, http.StatusBadRequest, "invalid grafana folder payload")
		return
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		respondError(c, http.StatusBadRequest, "目录名称不能为空")
		return
	}

	var response grafanaFolderResponse
	if err := s.withGrafanaSessionRetry(c.Request.Context(), source.ID, endpoint, func(sessionCookieHeader string) error {
		payload := map[string]any{
			"title": title,
		}
		return doGrafanaJSONRequest(
			c.Request.Context(),
			endpoint,
			sessionCookieHeader,
			http.MethodPost,
			"/api/folders",
			payload,
			&response,
		)
	}); err != nil {
		source.Status = "error"
		source.LastError = err.Error()
		source.LastCheckedAt = nowPtr()
		_ = s.db.Save(source).Error
		respondError(c, http.StatusBadGateway, fmt.Sprintf("Grafana 目录创建失败: %v", err))
		return
	}

	source.Status = "connected"
	source.LastError = ""
	source.LastCheckedAt = nowPtr()
	_ = s.db.Save(source).Error

	respondData(c, http.StatusCreated, gin.H{
		"id":    response.ID,
		"uid":   response.UID,
		"title": response.Title,
		"url":   response.URL,
	})
}

func (s *Server) updateGrafanaFolder(c *gin.Context) {
	source, endpoint, err := s.loadObservabilityEndpoint(c, true)
	if err != nil {
		return
	}

	folderUID := normalizeGrafanaFolderUID(c.Param("folderUid"))
	if folderUID == "" {
		respondError(c, http.StatusBadRequest, "默认目录不支持重命名")
		return
	}

	var input grafanaFolderPayload
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, http.StatusBadRequest, "invalid grafana folder payload")
		return
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		respondError(c, http.StatusBadRequest, "目录名称不能为空")
		return
	}

	var response grafanaFolderResponse
	if err := s.withGrafanaSessionRetry(c.Request.Context(), source.ID, endpoint, func(sessionCookieHeader string) error {
		folderDetail, err := getGrafanaFolderDetail(c.Request.Context(), endpoint, sessionCookieHeader, folderUID)
		if err != nil {
			return err
		}

		return doGrafanaJSONRequest(
			c.Request.Context(),
			endpoint,
			sessionCookieHeader,
			http.MethodPut,
			"/api/folders/"+folderUID,
			map[string]any{
				"title":     title,
				"version":   folderDetail.Version,
				"overwrite": true,
			},
			&response,
		)
	}); err != nil {
		source.Status = "error"
		source.LastError = err.Error()
		source.LastCheckedAt = nowPtr()
		_ = s.db.Save(source).Error
		respondError(c, http.StatusBadGateway, fmt.Sprintf("Grafana 目录更新失败: %v", err))
		return
	}

	source.Status = "connected"
	source.LastError = ""
	source.LastCheckedAt = nowPtr()
	_ = s.db.Save(source).Error

	respondData(c, http.StatusOK, gin.H{
		"id":    response.ID,
		"uid":   response.UID,
		"title": response.Title,
		"url":   response.URL,
	})
}

func (s *Server) deleteGrafanaFolder(c *gin.Context) {
	source, endpoint, err := s.loadObservabilityEndpoint(c, true)
	if err != nil {
		return
	}

	folderUID := normalizeGrafanaFolderUID(c.Param("folderUid"))
	if folderUID == "" {
		respondError(c, http.StatusBadRequest, "默认目录不支持删除")
		return
	}

	if err := s.withGrafanaSessionRetry(c.Request.Context(), source.ID, endpoint, func(sessionCookieHeader string) error {
		items, err := listGrafanaSearchItems(c.Request.Context(), endpoint, sessionCookieHeader)
		if err != nil {
			return err
		}
		for _, item := range items {
			if item.Type == "dash-db" && strings.TrimSpace(item.FolderUID) == folderUID {
				return httpError("目录下仍有仪表盘，请先迁移或删除目录内仪表盘")
			}
		}

		return doGrafanaJSONRequest(
			c.Request.Context(),
			endpoint,
			sessionCookieHeader,
			http.MethodDelete,
			"/api/folders/"+folderUID,
			nil,
			nil,
		)
	}); err != nil {
		status := http.StatusBadGateway
		if err.Error() == "目录下仍有仪表盘，请先迁移或删除目录内仪表盘" {
			status = http.StatusBadRequest
		} else {
			source.Status = "error"
			source.LastError = err.Error()
			source.LastCheckedAt = nowPtr()
			_ = s.db.Save(source).Error
		}
		respondError(c, status, fmt.Sprintf("Grafana 目录删除失败: %v", err))
		return
	}

	source.Status = "connected"
	source.LastError = ""
	source.LastCheckedAt = nowPtr()
	_ = s.db.Save(source).Error

	respondNoContent(c)
}

func (s *Server) createGrafanaDashboard(c *gin.Context) {
	source, endpoint, err := s.loadObservabilityEndpoint(c, true)
	if err != nil {
		return
	}

	var input grafanaDashboardPayload
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, http.StatusBadRequest, "invalid grafana dashboard payload")
		return
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		respondError(c, http.StatusBadRequest, "仪表盘名称不能为空")
		return
	}

	dashboard, err := buildGrafanaDashboardDefinition(title, input.Tags, input.Definition)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	requestBody := map[string]any{
		"dashboard": dashboard,
		"overwrite": false,
		"message":   "created via KubeFeel",
	}

	if folderUID := normalizeGrafanaFolderUID(input.FolderUID); folderUID != "" {
		requestBody["folderUid"] = folderUID
	}

	var response grafanaDashboardCreateResponse
	if err := s.withGrafanaSessionRetry(c.Request.Context(), source.ID, endpoint, func(sessionCookieHeader string) error {
		return doGrafanaJSONRequest(
			c.Request.Context(),
			endpoint,
			sessionCookieHeader,
			http.MethodPost,
			"/api/dashboards/db",
			requestBody,
			&response,
		)
	}); err != nil {
		source.Status = "error"
		source.LastError = err.Error()
		source.LastCheckedAt = nowPtr()
		_ = s.db.Save(source).Error
		respondError(c, http.StatusBadGateway, fmt.Sprintf("Grafana 仪表盘创建失败: %v", err))
		return
	}

	source.Status = "connected"
	source.LastError = ""
	source.LastCheckedAt = nowPtr()
	_ = s.db.Save(source).Error

	respondData(c, http.StatusCreated, gin.H{
		"id":        response.ID,
		"uid":       response.UID,
		"url":       response.URL,
		"status":    response.Status,
		"version":   response.Version,
		"title":     title,
		"folderUid": normalizeGrafanaFolderUID(input.FolderUID),
		"tags":      normalizeGrafanaTags(input.Tags),
	})
}

func (s *Server) getGrafanaDashboardMeta(c *gin.Context) {
	source, endpoint, err := s.loadObservabilityEndpoint(c, true)
	if err != nil {
		return
	}

	dashboardUID := strings.TrimSpace(c.Param("dashboardUid"))
	if dashboardUID == "" {
		respondError(c, http.StatusBadRequest, "invalid dashboard uid")
		return
	}

	var dashboardDetail grafanaDashboardDetailResponse
	if err := s.withGrafanaSessionRetry(c.Request.Context(), source.ID, endpoint, func(sessionCookieHeader string) error {
		var innerErr error
		dashboardDetail, innerErr = getGrafanaDashboardDetail(c.Request.Context(), endpoint, sessionCookieHeader, dashboardUID)
		return innerErr
	}); err != nil {
		source.Status = "error"
		source.LastError = err.Error()
		source.LastCheckedAt = nowPtr()
		_ = s.db.Save(source).Error
		respondError(c, http.StatusBadGateway, fmt.Sprintf("Grafana 仪表盘信息加载失败: %v", err))
		return
	}
	if dashboardDetail.Dashboard == nil {
		respondError(c, http.StatusBadGateway, "Grafana 仪表盘信息加载失败: 仪表盘内容为空")
		return
	}

	title, _ := dashboardDetail.Dashboard["title"].(string)
	tags := stringifyGrafanaTags(dashboardDetail.Dashboard["tags"])
	definition, err := serializeGrafanaDashboardDefinition(dashboardDetail.Dashboard)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "序列化 Grafana 仪表盘失败")
		return
	}

	source.Status = "connected"
	source.LastError = ""
	source.LastCheckedAt = nowPtr()
	_ = s.db.Save(source).Error

	respondData(c, http.StatusOK, gin.H{
		"uid":         dashboardUID,
		"title":       title,
		"url":         dashboardDetail.Meta.URL,
		"folderUid":   dashboardDetail.Meta.FolderUID,
		"folderTitle": dashboardDetail.Meta.FolderTitle,
		"tags":        tags,
		"provisioned": dashboardDetail.Meta.Provisioned,
		"canSave":     dashboardDetail.Meta.CanSave && dashboardDetail.Meta.CanEdit,
		"canDelete":   dashboardDetail.Meta.CanDelete,
		"definition":  definition,
	})
}

func (s *Server) updateGrafanaDashboard(c *gin.Context) {
	source, endpoint, err := s.loadObservabilityEndpoint(c, true)
	if err != nil {
		return
	}

	dashboardUID := strings.TrimSpace(c.Param("dashboardUid"))
	if dashboardUID == "" {
		respondError(c, http.StatusBadRequest, "invalid dashboard uid")
		return
	}

	var input grafanaDashboardPayload
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, http.StatusBadRequest, "invalid grafana dashboard payload")
		return
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		respondError(c, http.StatusBadRequest, "仪表盘名称不能为空")
		return
	}

	var response grafanaDashboardCreateResponse
	if err := s.withGrafanaSessionRetry(c.Request.Context(), source.ID, endpoint, func(sessionCookieHeader string) error {
		dashboardDetail, err := getGrafanaDashboardDetail(c.Request.Context(), endpoint, sessionCookieHeader, dashboardUID)
		if err != nil {
			return err
		}
		if dashboardDetail.Dashboard == nil {
			return httpError("Grafana 仪表盘更新失败: 仪表盘内容为空")
		}
		if dashboardDetail.Meta.Provisioned || !dashboardDetail.Meta.CanSave || !dashboardDetail.Meta.CanEdit {
			return httpError("当前仪表盘处于只读保护状态，不能直接修改；请先保存为自定义副本后再调整目录或标题")
		}

		dashboardDetail.Dashboard["title"] = title
		dashboardDetail.Dashboard["tags"] = normalizeGrafanaTags(input.Tags)

		requestBody := map[string]any{
			"dashboard": dashboardDetail.Dashboard,
			"overwrite": true,
			"message":   "updated via KubeFeel",
		}

		if folderUID := normalizeGrafanaFolderUID(input.FolderUID); folderUID != "" {
			requestBody["folderUid"] = folderUID
		} else {
			requestBody["folderId"] = 0
		}

		return doGrafanaJSONRequest(
			c.Request.Context(),
			endpoint,
			sessionCookieHeader,
			http.MethodPost,
			"/api/dashboards/db",
			requestBody,
			&response,
		)
	}); err != nil {
		status := http.StatusBadGateway
		if strings.Contains(err.Error(), "只读保护状态") || strings.Contains(err.Error(), "仪表盘内容为空") {
			status = http.StatusBadRequest
		} else {
			source.Status = "error"
			source.LastError = err.Error()
			source.LastCheckedAt = nowPtr()
			_ = s.db.Save(source).Error
		}
		respondError(c, status, fmt.Sprintf("Grafana 仪表盘更新失败: %v", err))
		return
	}

	source.Status = "connected"
	source.LastError = ""
	source.LastCheckedAt = nowPtr()
	_ = s.db.Save(source).Error

	respondData(c, http.StatusOK, gin.H{
		"id":        response.ID,
		"uid":       response.UID,
		"url":       response.URL,
		"status":    response.Status,
		"version":   response.Version,
		"title":     title,
		"folderUid": normalizeGrafanaFolderUID(input.FolderUID),
		"tags":      normalizeGrafanaTags(input.Tags),
	})
}

func (s *Server) deleteGrafanaDashboard(c *gin.Context) {
	source, endpoint, err := s.loadObservabilityEndpoint(c, true)
	if err != nil {
		return
	}

	dashboardUID := strings.TrimSpace(c.Param("dashboardUid"))
	if dashboardUID == "" {
		respondError(c, http.StatusBadRequest, "invalid dashboard uid")
		return
	}

	if err := s.withGrafanaSessionRetry(c.Request.Context(), source.ID, endpoint, func(sessionCookieHeader string) error {
		dashboardDetail, err := getGrafanaDashboardDetail(c.Request.Context(), endpoint, sessionCookieHeader, dashboardUID)
		if err != nil {
			return err
		}
		if dashboardDetail.Meta.Provisioned || !dashboardDetail.Meta.CanDelete {
			return httpError("当前仪表盘处于只读保护状态，不能直接删除；如需调整请先保存为自定义副本")
		}

		return doGrafanaJSONRequest(
			c.Request.Context(),
			endpoint,
			sessionCookieHeader,
			http.MethodDelete,
			"/api/dashboards/uid/"+dashboardUID,
			nil,
			nil,
		)
	}); err != nil {
		status := http.StatusBadGateway
		if strings.Contains(err.Error(), "只读保护状态") {
			status = http.StatusBadRequest
		} else {
			source.Status = "error"
			source.LastError = err.Error()
			source.LastCheckedAt = nowPtr()
			_ = s.db.Save(source).Error
		}
		respondError(c, status, fmt.Sprintf("Grafana 仪表盘删除失败: %v", err))
		return
	}

	source.Status = "connected"
	source.LastError = ""
	source.LastCheckedAt = nowPtr()
	_ = s.db.Save(source).Error

	respondNoContent(c)
}

func (s *Server) proxyGrafana(c *gin.Context) {
	requestedProxyPath := normalizeGrafanaProxyPath(c.Param("proxyPath"))
	if status, err := validateGrafanaProxyRequest(c.Request.Method, requestedProxyPath); err != nil {
		respondError(c, status, err.Error())
		return
	}

	source, endpoint, err := s.loadObservabilityEndpoint(c, true)
	if err != nil {
		return
	}

	targetURL, err := url.Parse(endpoint.Endpoint)
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid grafana endpoint")
		return
	}

	proxyPrefix := grafanaProxyPrefix(source.ID)
	sessionCookieHeader, err := s.ensureGrafanaSession(c.Request.Context(), source.ID, endpoint)
	if err != nil {
		source.Status = "error"
		source.LastError = err.Error()
		source.LastCheckedAt = nowPtr()
		_ = s.db.Save(source).Error
		respondError(c, http.StatusBadGateway, fmt.Sprintf("Grafana 会话初始化失败: %v", err))
		return
	}

	source.Status = "connected"
	source.LastError = ""
	source.LastCheckedAt = nowPtr()
	_ = s.db.Save(source).Error

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		proxyPath := requestedProxyPath
		if proxyPath == "" || proxyPath == "/" {
			proxyPath = normalizeDashboardPath(source.DashboardPath, "grafana")
		}
		req.URL.Path = joinProxyPath(targetURL.Path, proxyPath)
		req.Host = targetURL.Host
		req.Header.Del("Cookie")
		req.Header.Del("Origin")
		req.Header.Del("Referer")

		if sessionCookieHeader != "" {
			req.Header.Set("Cookie", sessionCookieHeader)
		} else if endpoint.Username != "" || endpoint.Secret != "" {
			if endpoint.Username == "" {
				req.Header.Set("Authorization", "Bearer "+endpoint.Secret)
			} else {
				token := base64.StdEncoding.EncodeToString([]byte(endpoint.Username + ":" + endpoint.Secret))
				req.Header.Set("Authorization", "Basic "+token)
			}
		}
	}
	proxy.Transport = &http.Transport{
		Proxy:           http.ProxyFromEnvironment,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: endpoint.SkipTLSVerify}, //nolint:gosec
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		s.refreshGrafanaSessionCacheFromResponse(source.ID, resp)
		if location := strings.TrimSpace(resp.Header.Get("Location")); strings.HasPrefix(location, "/") {
			resp.Header.Set("Location", proxyPrefix+location)
		}

		contentType := strings.ToLower(resp.Header.Get("Content-Type"))
		var body []byte
		bodyLoaded := false
		if resp.StatusCode == http.StatusUnauthorized {
			var readErr error
			body, readErr = io.ReadAll(resp.Body)
			if readErr != nil {
				return readErr
			}
			_ = resp.Body.Close()
			bodyLoaded = true
			if isGrafanaSessionAuthMessage(string(body)) {
				s.clearGrafanaSessionCache(source.ID)
			}
		}

		resp.Header.Del("Set-Cookie")
		// Grafana 默认会返回禁止 iframe 的响应头，平台的沉浸式仪表盘必须移除这些头才能正常嵌入。
		resp.Header.Del("X-Frame-Options")
		resp.Header.Del("Frame-Options")
		resp.Header.Del("Content-Security-Policy")
		resp.Header.Del("Content-Security-Policy-Report-Only")

		if strings.Contains(contentType, "text/html") {
			if !bodyLoaded {
				var readErr error
				body, readErr = io.ReadAll(resp.Body)
				if readErr != nil {
					return readErr
				}
				_ = resp.Body.Close()
			}
			rewritten := rewriteGrafanaHTML(body, proxyPrefix)
			resp.Body = io.NopCloser(bytes.NewReader(rewritten))
			resp.ContentLength = int64(len(rewritten))
			resp.Header.Set("Content-Length", strconv.Itoa(len(rewritten)))
			return nil
		}
		if bodyLoaded {
			resp.Body = io.NopCloser(bytes.NewReader(body))
			resp.ContentLength = int64(len(body))
			resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
		}
		return nil
	}
	proxy.ErrorHandler = func(writer http.ResponseWriter, request *http.Request, err error) {
		c.AbortWithStatusJSON(http.StatusBadGateway, apiError{Message: err.Error()})
	}

	proxy.ServeHTTP(c.Writer, c.Request)
}

func (s *Server) ensureGrafanaSession(
	ctx context.Context,
	sourceID uint,
	endpoint integration.EndpointConfig,
) (string, error) {
	if strings.TrimSpace(endpoint.Username) == "" || strings.TrimSpace(endpoint.Secret) == "" {
		return "", nil
	}

	if cookieHeader, ok := s.loadGrafanaSessionCache(sourceID); ok {
		return cookieHeader, nil
	}

	cookieHeader, expiresAt, err := loginGrafanaSession(ctx, endpoint)
	if err != nil {
		return "", err
	}
	s.storeGrafanaSessionCache(sourceID, cookieHeader, expiresAt)
	return cookieHeader, nil
}

func (s *Server) withGrafanaSessionRetry(
	ctx context.Context,
	sourceID uint,
	endpoint integration.EndpointConfig,
	fn func(sessionCookieHeader string) error,
) error {
	shouldRetry := strings.TrimSpace(endpoint.Username) != "" && strings.TrimSpace(endpoint.Secret) != ""
	attempts := 1
	if shouldRetry {
		attempts = 2
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		sessionCookieHeader, err := s.ensureGrafanaSession(ctx, sourceID, endpoint)
		if err != nil {
			return err
		}
		if err := fn(sessionCookieHeader); err != nil {
			lastErr = err
			if !shouldRetry || attempt == attempts-1 || !isGrafanaSessionAuthError(err) {
				return err
			}
			s.clearGrafanaSessionCache(sourceID)
			continue
		}
		return nil
	}

	return lastErr
}

func loginGrafanaSession(
	ctx context.Context,
	endpoint integration.EndpointConfig,
) (string, time.Time, error) {
	targetURL, err := url.Parse(endpoint.Endpoint)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("invalid grafana endpoint: %w", err)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return "", time.Time{}, err
	}

	client := &http.Client{
		Timeout: 20 * time.Second,
		Jar:     jar,
		Transport: &http.Transport{
			Proxy:           http.ProxyFromEnvironment,
			TLSClientConfig: &tls.Config{InsecureSkipVerify: endpoint.SkipTLSVerify}, //nolint:gosec
		},
	}

	payload, err := json.Marshal(map[string]string{
		"user":     endpoint.Username,
		"email":    endpoint.Username,
		"password": endpoint.Secret,
	})
	if err != nil {
		return "", time.Time{}, err
	}

	loginURL := targetURL.ResolveReference(&url.URL{Path: joinProxyPath(targetURL.Path, "/login")})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL.String(), bytes.NewReader(payload))
	if err != nil {
		return "", time.Time{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")

	response, err := client.Do(request)
	if err != nil {
		return "", time.Time{}, err
	}
	body, _ := io.ReadAll(io.LimitReader(response.Body, 32*1024))
	_ = response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 400 {
		return "", time.Time{}, fmt.Errorf("login failed: %s", strings.TrimSpace(string(body)))
	}

	cookies := jar.Cookies(targetURL)
	cookieHeader, expiresAt := buildCookieHeader(cookies)
	if cookieHeader == "" {
		return "", time.Time{}, fmt.Errorf("grafana session cookie missing")
	}

	validateURL := targetURL.ResolveReference(&url.URL{Path: joinProxyPath(targetURL.Path, "/api/user")})
	validateReq, err := http.NewRequestWithContext(ctx, http.MethodGet, validateURL.String(), nil)
	if err != nil {
		return "", time.Time{}, err
	}
	validateReq.Header.Set("Cookie", cookieHeader)
	validateResp, err := client.Do(validateReq)
	if err != nil {
		return "", time.Time{}, err
	}
	_, _ = io.Copy(io.Discard, validateResp.Body)
	_ = validateResp.Body.Close()
	if validateResp.StatusCode < 200 || validateResp.StatusCode >= 300 {
		return "", time.Time{}, fmt.Errorf("session validation failed: HTTP %d", validateResp.StatusCode)
	}

	return cookieHeader, expiresAt, nil
}

func (s *Server) loadGrafanaSessionCache(sourceID uint) (string, bool) {
	s.cacheMu.RLock()
	entry, ok := s.grafanaSessionCache[sourceID]
	s.cacheMu.RUnlock()
	if !ok {
		return "", false
	}
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		s.clearGrafanaSessionCache(sourceID)
		return "", false
	}
	return entry.cookieHeader, entry.cookieHeader != ""
}

func (s *Server) storeGrafanaSessionCache(sourceID uint, cookieHeader string, expiresAt time.Time) {
	s.cacheMu.Lock()
	s.grafanaSessionCache[sourceID] = grafanaSessionCacheEntry{
		cookieHeader: cookieHeader,
		expiresAt:    expiresAt,
	}
	s.cacheMu.Unlock()
}

func (s *Server) clearGrafanaSessionCache(sourceID uint) {
	s.cacheMu.Lock()
	delete(s.grafanaSessionCache, sourceID)
	s.cacheMu.Unlock()
}

func (s *Server) refreshGrafanaSessionCacheFromResponse(sourceID uint, response *http.Response) {
	if response == nil {
		return
	}
	cookieHeader, expiresAt := buildCookieHeader(response.Cookies())
	if cookieHeader == "" {
		return
	}
	s.storeGrafanaSessionCache(sourceID, cookieHeader, expiresAt)
}

func isGrafanaSessionAuthError(err error) bool {
	if err == nil {
		return false
	}
	return isGrafanaSessionAuthMessage(err.Error())
}

func isGrafanaSessionAuthMessage(message string) bool {
	lowered := strings.ToLower(strings.TrimSpace(message))
	if lowered == "" {
		return false
	}
	return strings.Contains(lowered, "session.token.rotate") ||
		strings.Contains(lowered, "\"statuscode\":401") ||
		strings.Contains(lowered, "statuscode\":401") ||
		strings.Contains(lowered, "unauthorized") ||
		strings.Contains(lowered, "invalid username or password") ||
		strings.Contains(lowered, "session validation failed: http 401")
}

func buildCookieHeader(cookies []*http.Cookie) (string, time.Time) {
	if len(cookies) == 0 {
		return "", time.Time{}
	}

	parts := make([]string, 0, len(cookies))
	expiresAt := time.Now().Add(30 * time.Minute)
	for _, cookie := range cookies {
		if cookie == nil || strings.TrimSpace(cookie.Name) == "" {
			continue
		}
		parts = append(parts, cookie.Name+"="+cookie.Value)
		if !cookie.Expires.IsZero() && cookie.Expires.Before(expiresAt) {
			expiresAt = cookie.Expires
		}
	}

	return strings.Join(parts, "; "), expiresAt
}

func grafanaProxyPrefix(sourceID uint) string {
	return "/api/observability/grafana/" + strconv.FormatUint(uint64(sourceID), 10)
}

func rewriteGrafanaHTML(body []byte, proxyPrefix string) []byte {
	content := string(body)
	baseHref := proxyPrefix + "/"
	content = strings.ReplaceAll(content, `<base href="/" />`, `<base href="`+baseHref+`" />`)
	content = strings.ReplaceAll(content, `<base href="/">`, `<base href="`+baseHref+`">`)
	content = grafanaAppURLPattern.ReplaceAllString(content, `"appUrl":"`+baseHref+`"`)
	content = grafanaAppSubURLPattern.ReplaceAllString(content, `"appSubUrl":"`+proxyPrefix+`"`)
	return []byte(content)
}

func listGrafanaSearchItems(
	ctx context.Context,
	endpoint integration.EndpointConfig,
	sessionCookieHeader string,
) ([]grafanaSearchItem, error) {
	targetURL, err := url.Parse(endpoint.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid grafana endpoint: %w", err)
	}

	query := url.Values{}
	query.Set("limit", "5000")
	requestURL := targetURL.ResolveReference(&url.URL{
		Path:     joinProxyPath(targetURL.Path, "/api/search"),
		RawQuery: query.Encode(),
	})

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	applyGrafanaAuthHeaders(request, endpoint, sessionCookieHeader)

	client := &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			Proxy:           http.ProxyFromEnvironment,
			TLSClientConfig: &tls.Config{InsecureSkipVerify: endpoint.SkipTLSVerify}, //nolint:gosec
		},
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 32*1024))
		return nil, fmt.Errorf("search failed: %s", strings.TrimSpace(string(body)))
	}

	var items []grafanaSearchItem
	if err := json.NewDecoder(response.Body).Decode(&items); err != nil {
		return nil, err
	}

	filtered := make([]grafanaSearchItem, 0, len(items))
	for _, item := range items {
		if item.Type == "dash-folder" || item.Type == "dash-db" {
			filtered = append(filtered, item)
		}
	}

	return filtered, nil
}

func doGrafanaJSONRequest(
	ctx context.Context,
	endpoint integration.EndpointConfig,
	sessionCookieHeader string,
	method string,
	rawPath string,
	payload any,
	target any,
) error {
	targetURL, err := url.Parse(endpoint.Endpoint)
	if err != nil {
		return fmt.Errorf("invalid grafana endpoint: %w", err)
	}

	var bodyReader io.Reader
	if payload != nil {
		body, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(body)
	}

	requestURL := targetURL.ResolveReference(&url.URL{
		Path: joinProxyPath(targetURL.Path, rawPath),
	})
	request, err := http.NewRequestWithContext(ctx, method, requestURL.String(), bodyReader)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	applyGrafanaAuthHeaders(request, endpoint, sessionCookieHeader)

	client := &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			Proxy:           http.ProxyFromEnvironment,
			TLSClientConfig: &tls.Config{InsecureSkipVerify: endpoint.SkipTLSVerify}, //nolint:gosec
		},
	}

	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 64*1024))
		return fmt.Errorf("%s", strings.TrimSpace(string(body)))
	}

	if target == nil {
		return nil
	}

	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		return err
	}

	return nil
}

func getGrafanaFolderDetail(
	ctx context.Context,
	endpoint integration.EndpointConfig,
	sessionCookieHeader string,
	folderUID string,
) (grafanaFolderDetailResponse, error) {
	var detail grafanaFolderDetailResponse
	err := doGrafanaJSONRequest(
		ctx,
		endpoint,
		sessionCookieHeader,
		http.MethodGet,
		"/api/folders/"+folderUID,
		nil,
		&detail,
	)
	return detail, err
}

func getGrafanaDashboardDetail(
	ctx context.Context,
	endpoint integration.EndpointConfig,
	sessionCookieHeader string,
	dashboardUID string,
) (grafanaDashboardDetailResponse, error) {
	var detail grafanaDashboardDetailResponse
	err := doGrafanaJSONRequest(
		ctx,
		endpoint,
		sessionCookieHeader,
		http.MethodGet,
		"/api/dashboards/uid/"+dashboardUID,
		nil,
		&detail,
	)
	return detail, err
}

func buildGrafanaCatalog(items []grafanaSearchItem) ([]grafanaCatalogFolder, int) {
	folderIndex := map[string]int{}
	folders := make([]grafanaCatalogFolder, 0, len(items))
	dashboardCount := 0

	ensureFolder := func(uid, title string, isGeneral bool) int {
		key := strings.TrimSpace(uid)
		if key == "" {
			key = "__general__"
		}
		if index, ok := folderIndex[key]; ok {
			if title != "" && folders[index].Title == "" {
				folders[index].Title = title
			}
			return index
		}

		folder := grafanaCatalogFolder{
			UID:        uid,
			Title:      title,
			IsGeneral:  isGeneral,
			Dashboards: []grafanaCatalogDashboard{},
		}
		if folder.Title == "" {
			folder.Title = "未分组"
		}
		folderIndex[key] = len(folders)
		folders = append(folders, folder)
		return len(folders) - 1
	}

	for _, item := range items {
		if item.Type == "dash-folder" {
			ensureFolder(item.UID, item.Title, false)
		}
	}

	for _, item := range items {
		if item.Type != "dash-db" {
			continue
		}

		dashboard := grafanaCatalogDashboard{
			UID:         item.UID,
			Title:       item.Title,
			URL:         item.URL,
			FolderUID:   item.FolderUID,
			FolderTitle: item.FolderTitle,
			Tags:        item.Tags,
			IsStarred:   item.IsStarred,
		}

		index := ensureFolder(item.FolderUID, item.FolderTitle, strings.TrimSpace(item.FolderUID) == "")
		folders[index].Dashboards = append(folders[index].Dashboards, dashboard)
		folders[index].DashboardCount = len(folders[index].Dashboards)
		dashboardCount += 1
	}

	slices.SortStableFunc(folders, func(left, right grafanaCatalogFolder) int {
		if left.IsGeneral != right.IsGeneral {
			if left.IsGeneral {
				return 1
			}
			return -1
		}
		return strings.Compare(strings.ToLower(left.Title), strings.ToLower(right.Title))
	})

	for index := range folders {
		slices.SortStableFunc(folders[index].Dashboards, func(left, right grafanaCatalogDashboard) int {
			if left.IsStarred != right.IsStarred {
				if left.IsStarred {
					return -1
				}
				return 1
			}
			return strings.Compare(strings.ToLower(left.Title), strings.ToLower(right.Title))
		})
	}

	return folders, dashboardCount
}

func buildGrafanaDashboardDefinition(
	title string,
	tags []string,
	rawDefinition string,
) (map[string]any, error) {
	normalizedTags := normalizeGrafanaTags(tags)
	trimmedDefinition := strings.TrimSpace(rawDefinition)
	if trimmedDefinition == "" {
		return buildStarterGrafanaDashboard(title, normalizedTags), nil
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmedDefinition), &payload); err != nil {
		return nil, fmt.Errorf("仪表盘 JSON 解析失败: %w", err)
	}

	dashboard := payload
	if nested, ok := payload["dashboard"].(map[string]any); ok {
		dashboard = nested
	}

	delete(dashboard, "id")
	delete(dashboard, "uid")
	delete(dashboard, "version")

	dashboard["title"] = title
	dashboard["editable"] = true
	dashboard["schemaVersion"] = 39
	dashboard["version"] = 0
	dashboard["tags"] = normalizedTags
	if _, ok := dashboard["time"]; !ok {
		dashboard["time"] = map[string]any{
			"from": "now-6h",
			"to":   "now",
		}
	}
	if _, ok := dashboard["timezone"]; !ok {
		dashboard["timezone"] = "browser"
	}
	if _, ok := dashboard["refresh"]; !ok {
		dashboard["refresh"] = "30s"
	}
	if _, ok := dashboard["panels"]; !ok {
		dashboard["panels"] = buildStarterGrafanaDashboard(title, normalizedTags)["panels"]
	}

	return dashboard, nil
}

func buildStarterGrafanaDashboard(title string, tags []string) map[string]any {
	return map[string]any{
		"title":         title,
		"editable":      true,
		"schemaVersion": 39,
		"version":       0,
		"timezone":      "browser",
		"refresh":       "30s",
		"tags":          tags,
		"time": map[string]any{
			"from": "now-6h",
			"to":   "now",
		},
		"panels": []any{
			map[string]any{
				"id":    1,
				"type":  "text",
				"title": "Dashboard Overview",
				"gridPos": map[string]any{
					"h": 8,
					"w": 24,
					"x": 0,
					"y": 0,
				},
				"options": map[string]any{
					"mode":    "markdown",
					"content": fmt.Sprintf("### %s\n\n通过 KubeFeel 创建的自定义仪表盘，可继续在 Grafana 中补充查询与图表。", title),
				},
				"transparent": true,
			},
		},
	}
}

func normalizeGrafanaTags(tags []string) []string {
	seen := make(map[string]struct{}, len(tags))
	result := make([]string, 0, len(tags))
	for _, tag := range tags {
		trimmed := strings.TrimSpace(tag)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func normalizeGrafanaFolderUID(uid string) string {
	trimmed := strings.TrimSpace(uid)
	switch trimmed {
	case "", "__general__":
		return ""
	default:
		return trimmed
	}
}

func stringifyGrafanaTags(raw any) []string {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}

	result := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func serializeGrafanaDashboardDefinition(dashboard map[string]any) (string, error) {
	cloned := cloneGrafanaDashboardMap(dashboard)
	delete(cloned, "id")
	delete(cloned, "uid")
	delete(cloned, "version")
	body, err := json.MarshalIndent(cloned, "", "  ")
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func cloneGrafanaDashboardMap(value map[string]any) map[string]any {
	body, err := json.Marshal(value)
	if err != nil {
		return value
	}

	var cloned map[string]any
	if err := json.Unmarshal(body, &cloned); err != nil {
		return value
	}
	return cloned
}

func applyGrafanaAuthHeaders(request *http.Request, endpoint integration.EndpointConfig, sessionCookieHeader string) {
	request.Header.Del("Cookie")
	request.Header.Del("Authorization")
	if sessionCookieHeader != "" {
		request.Header.Set("Cookie", sessionCookieHeader)
		return
	}

	if endpoint.Username == "" && endpoint.Secret != "" {
		request.Header.Set("Authorization", "Bearer "+endpoint.Secret)
		return
	}

	if endpoint.Username != "" || endpoint.Secret != "" {
		token := base64.StdEncoding.EncodeToString([]byte(endpoint.Username + ":" + endpoint.Secret))
		request.Header.Set("Authorization", "Basic "+token)
	}
}

func (s *Server) loadObservabilityEndpoint(
	c *gin.Context,
	requireGrafana bool,
) (*model.ObservabilitySource, integration.EndpointConfig, error) {
	identifier, err := parseUintParam(c, "id")
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid datasource id")
		return nil, integration.EndpointConfig{}, err
	}

	var item model.ObservabilitySource
	if err := s.db.First(&item, identifier).Error; err != nil {
		respondError(c, http.StatusNotFound, "datasource not found")
		return nil, integration.EndpointConfig{}, err
	}
	if requireGrafana && item.Type != "grafana" {
		respondError(c, http.StatusBadRequest, "selected datasource is not grafana")
		return nil, integration.EndpointConfig{}, httpError("selected datasource is not grafana")
	}

	secret, err := s.kubeFactory.Decrypt(item.SecretEncrypted)
	if err != nil && item.SecretEncrypted != "" {
		respondError(c, http.StatusInternalServerError, "解密数据源凭据失败")
		return nil, integration.EndpointConfig{}, err
	}

	return &item, integration.EndpointConfig{
		Endpoint:      item.Endpoint,
		Username:      item.Username,
		Secret:        secret,
		SkipTLSVerify: item.SkipTLSVerify,
	}, nil
}

func (s *Server) probeObservabilitySource(
	ctx context.Context,
	input observabilitySourcePayload,
) (integration.ObservabilityProbe, error) {
	if strings.TrimSpace(input.Name) == "" {
		return integration.ObservabilityProbe{}, httpError("name is required")
	}
	if !integration.ValidObservabilityKind(strings.TrimSpace(input.Type)) {
		return integration.ObservabilityProbe{}, httpError("unsupported observability kind")
	}
	if strings.TrimSpace(input.Endpoint) == "" {
		return integration.ObservabilityProbe{}, httpError("endpoint is required")
	}

	client, err := integration.NewObservabilityClient(integration.EndpointConfig{
		Endpoint:      strings.TrimSpace(input.Endpoint),
		Username:      strings.TrimSpace(input.Username),
		Secret:        strings.TrimSpace(input.Secret),
		SkipTLSVerify: input.SkipTLSVerify,
	})
	if err != nil {
		return integration.ObservabilityProbe{}, err
	}

	probe, err := client.Test(ctx, strings.TrimSpace(input.Type))
	if err != nil {
		return integration.ObservabilityProbe{}, err
	}
	if strings.TrimSpace(input.Type) == "grafana" &&
		strings.TrimSpace(input.Username) != "" &&
		strings.TrimSpace(input.Secret) != "" {
		sessionCookieHeader, _, err := loginGrafanaSession(ctx, integration.EndpointConfig{
			Endpoint:      strings.TrimSpace(input.Endpoint),
			Username:      strings.TrimSpace(input.Username),
			Secret:        strings.TrimSpace(input.Secret),
			SkipTLSVerify: input.SkipTLSVerify,
		})
		if err != nil {
			return integration.ObservabilityProbe{}, fmt.Errorf("grafana web login failed: %w", err)
		}
		if _, err := listGrafanaSearchItems(ctx, integration.EndpointConfig{
			Endpoint:      strings.TrimSpace(input.Endpoint),
			Username:      strings.TrimSpace(input.Username),
			Secret:        strings.TrimSpace(input.Secret),
			SkipTLSVerify: input.SkipTLSVerify,
		}, sessionCookieHeader); err != nil {
			return integration.ObservabilityProbe{}, fmt.Errorf("grafana catalog check failed: %w", err)
		}
		if err := probeGrafanaWorkspaceAccess(ctx, integration.EndpointConfig{
			Endpoint:      strings.TrimSpace(input.Endpoint),
			Username:      strings.TrimSpace(input.Username),
			Secret:        strings.TrimSpace(input.Secret),
			SkipTLSVerify: input.SkipTLSVerify,
		}, sessionCookieHeader, input.DashboardPath); err != nil {
			return integration.ObservabilityProbe{}, fmt.Errorf("grafana workspace path check failed: %w", err)
		}
		probe.Message = "Grafana 已连通，目录接口和默认入口均可访问。"
	} else if strings.TrimSpace(input.Type) == "grafana" {
		if _, err := listGrafanaSearchItems(ctx, integration.EndpointConfig{
			Endpoint:      strings.TrimSpace(input.Endpoint),
			Username:      strings.TrimSpace(input.Username),
			Secret:        strings.TrimSpace(input.Secret),
			SkipTLSVerify: input.SkipTLSVerify,
		}, ""); err != nil {
			return integration.ObservabilityProbe{}, fmt.Errorf("grafana catalog check failed: %w", err)
		}
		if err := probeGrafanaWorkspaceAccess(ctx, integration.EndpointConfig{
			Endpoint:      strings.TrimSpace(input.Endpoint),
			Username:      strings.TrimSpace(input.Username),
			Secret:        strings.TrimSpace(input.Secret),
			SkipTLSVerify: input.SkipTLSVerify,
		}, "", input.DashboardPath); err != nil {
			return integration.ObservabilityProbe{}, fmt.Errorf("grafana workspace path check failed: %w", err)
		}
		probe.Message = "Grafana 已连通，目录接口和默认入口均可访问。"
	}

	return probe, nil
}

func serializeObservabilitySource(item model.ObservabilitySource) gin.H {
	return gin.H{
		"id":             item.ID,
		"name":           item.Name,
		"type":           item.Type,
		"description":    item.Description,
		"endpoint":       item.Endpoint,
		"username":       item.Username,
		"dashboardPath":  item.DashboardPath,
		"skipTLSVerify":  item.SkipTLSVerify,
		"status":         item.Status,
		"lastError":      item.LastError,
		"lastCheckedAt":  item.LastCheckedAt,
		"createdAt":      item.CreatedAt,
		"updatedAt":      item.UpdatedAt,
		"hasCredential":  item.SecretEncrypted != "",
		"dashboardReady": item.Type == "grafana",
	}
}

func normalizeDashboardPath(raw string, kind string) string {
	if strings.TrimSpace(kind) != "grafana" {
		return ""
	}
	return integration.BuildGrafanaEmbedPath(raw)
}

func probeGrafanaWorkspaceAccess(
	ctx context.Context,
	endpoint integration.EndpointConfig,
	sessionCookieHeader string,
	rawPath string,
) error {
	targetURL, err := url.Parse(endpoint.Endpoint)
	if err != nil {
		return fmt.Errorf("invalid grafana endpoint: %w", err)
	}

	requestURL := targetURL.ResolveReference(&url.URL{
		Path: joinProxyPath(targetURL.Path, normalizeDashboardPath(rawPath, "grafana")),
	})
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return err
	}
	applyGrafanaAuthHeaders(request, endpoint, sessionCookieHeader)

	client := &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			Proxy:           http.ProxyFromEnvironment,
			TLSClientConfig: &tls.Config{InsecureSkipVerify: endpoint.SkipTLSVerify}, //nolint:gosec
		},
	}

	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.Request != nil && strings.Contains(response.Request.URL.Path, "/login") {
		return fmt.Errorf("workspace redirected to login")
	}
	if response.StatusCode < 200 || response.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 32*1024))
		return fmt.Errorf("workspace returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil
}

func validateGrafanaProxyRequest(method string, rawPath string) (int, error) {
	normalizedMethod := strings.ToUpper(strings.TrimSpace(method))
	cleanedPath := normalizeGrafanaProxyPath(rawPath)
	if cleanedPath == "" {
		cleanedPath = "/"
	}

	switch normalizedMethod {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return http.StatusOK, nil
	case http.MethodPost:
		if isAllowedGrafanaQueryPostPath(cleanedPath) {
			return http.StatusOK, nil
		}
		return http.StatusForbidden, fmt.Errorf("当前 Grafana 代理只允许只读查询，不允许该写入请求")
	default:
		return http.StatusMethodNotAllowed, fmt.Errorf("当前 Grafana 代理不允许 %s 请求", normalizedMethod)
	}
}

func normalizeGrafanaProxyPath(rawPath string) string {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" || trimmed == "/" {
		return "/"
	}

	cleaned := path.Clean("/" + strings.TrimLeft(trimmed, "/"))
	if cleaned == "." {
		return "/"
	}
	return cleaned
}

func isAllowedGrafanaQueryPostPath(path string) bool {
	return hasGrafanaProxyPrefix(path,
		"/api/ds/query",
		"/api/datasources/proxy/",
		"/api/annotations",
	)
}

func hasGrafanaProxyPrefix(path string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func joinProxyPath(basePath string, proxyPath string) string {
	base := strings.TrimRight(strings.TrimSpace(basePath), "/")
	path := strings.TrimSpace(proxyPath)
	if path == "" {
		return base
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}
