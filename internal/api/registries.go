package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"multikube-manager/internal/integration"
	"multikube-manager/internal/model"

	"github.com/gin-gonic/gin"
)

type registryIntegrationPayload struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	Description   string `json:"description"`
	Endpoint      string `json:"endpoint"`
	Namespace     string `json:"namespace"`
	Username      string `json:"username"`
	Secret        string `json:"secret"`
	SkipTLSVerify bool   `json:"skipTLSVerify"`
}

type registryArtifactCacheEntry struct {
	result   integration.RegistryArtifactList
	loadedAt time.Time
}

const registryArtifactCacheTTL = 45 * time.Second

func (s *Server) registryArtifactCacheKey(identifier uint, namespace string, search string, limit int) string {
	return strings.Join([]string{
		strconv.FormatUint(uint64(identifier), 10),
		strings.TrimSpace(namespace),
		strings.ToLower(strings.TrimSpace(search)),
		strconv.Itoa(limit),
	}, "|")
}

func (s *Server) loadRegistryArtifactCache(key string) (integration.RegistryArtifactList, time.Time, bool) {
	s.cacheMu.RLock()
	entry, ok := s.registryArtifactCache[key]
	s.cacheMu.RUnlock()
	if !ok {
		return integration.RegistryArtifactList{}, time.Time{}, false
	}

	if time.Since(entry.loadedAt) > registryArtifactCacheTTL {
		s.cacheMu.Lock()
		delete(s.registryArtifactCache, key)
		s.cacheMu.Unlock()
		return integration.RegistryArtifactList{}, time.Time{}, false
	}

	return entry.result, entry.loadedAt, true
}

func (s *Server) storeRegistryArtifactCache(key string, result integration.RegistryArtifactList, loadedAt time.Time) {
	s.cacheMu.Lock()
	s.registryArtifactCache[key] = registryArtifactCacheEntry{
		result:   result,
		loadedAt: loadedAt,
	}
	s.cacheMu.Unlock()
}

func (s *Server) clearRegistryArtifactCache() {
	s.cacheMu.Lock()
	s.registryArtifactCache = make(map[string]registryArtifactCacheEntry)
	s.cacheMu.Unlock()
}

func (s *Server) repositoryProviderCatalog(c *gin.Context) {
	respondData(c, http.StatusOK, integration.RepositoryProviders())
}

func (s *Server) listRegistryIntegrations(c *gin.Context) {
	var items []model.RegistryIntegration
	if err := s.db.Order("created_at desc").Find(&items).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "查询仓库集成失败")
		return
	}

	result := make([]gin.H, 0, len(items))
	for _, item := range items {
		result = append(result, serializeRegistryIntegration(item))
	}

	respondData(c, http.StatusOK, result)
}

func (s *Server) testRegistryIntegration(c *gin.Context) {
	var input registryIntegrationPayload
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, http.StatusBadRequest, "invalid registry payload")
		return
	}

	probe, err := s.probeRegistryIntegration(c.Request.Context(), input)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	respondData(c, http.StatusOK, probe)
}

func (s *Server) testStoredRegistryIntegration(c *gin.Context) {
	item, endpoint, err := s.loadRegistryEndpoint(c)
	if err != nil {
		return
	}

	client, err := integration.NewRegistryClient(endpoint)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	probe, err := client.Test(c.Request.Context())
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
		"integration": serializeRegistryIntegration(*item),
		"probe":       probe,
	})
}

func (s *Server) createRegistryIntegration(c *gin.Context) {
	var input registryIntegrationPayload
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, http.StatusBadRequest, "invalid registry payload")
		return
	}

	probe, err := s.probeRegistryIntegration(c.Request.Context(), input)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	secretEncrypted := ""
	if strings.TrimSpace(input.Secret) != "" {
		secretEncrypted, err = s.kubeFactory.Encrypt(strings.TrimSpace(input.Secret))
		if err != nil {
			respondError(c, http.StatusInternalServerError, "加密仓库凭据失败")
			return
		}
	}

	item := model.RegistryIntegration{
		Name:            strings.TrimSpace(input.Name),
		Type:            strings.TrimSpace(input.Type),
		Description:     strings.TrimSpace(input.Description),
		Endpoint:        strings.TrimSpace(input.Endpoint),
		Namespace:       strings.Trim(strings.TrimSpace(input.Namespace), "/"),
		Username:        strings.TrimSpace(input.Username),
		SecretEncrypted: secretEncrypted,
		SkipTLSVerify:   input.SkipTLSVerify,
		Status:          "connected",
		LastError:       "",
		LastCheckedAt:   nowPtr(),
	}

	if err := s.db.Create(&item).Error; err != nil {
		respondError(c, http.StatusBadRequest, "保存仓库集成失败，名称可能已存在")
		return
	}

	s.clearRegistryArtifactCache()

	respondData(c, http.StatusCreated, gin.H{
		"integration": serializeRegistryIntegration(item),
		"probe":       probe,
	})
}

func (s *Server) updateRegistryIntegration(c *gin.Context) {
	identifier, err := parseUintParam(c, "id")
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid registry id")
		return
	}

	var item model.RegistryIntegration
	if err := s.db.First(&item, identifier).Error; err != nil {
		respondError(c, http.StatusNotFound, "registry integration not found")
		return
	}

	var input registryIntegrationPayload
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, http.StatusBadRequest, "invalid registry payload")
		return
	}

	secretValue := strings.TrimSpace(input.Secret)
	if secretValue == "" && item.SecretEncrypted != "" {
		if decrypted, decryptErr := s.kubeFactory.Decrypt(item.SecretEncrypted); decryptErr == nil {
			secretValue = decrypted
		}
	}

	payload := registryIntegrationPayload{
		Name:          firstNonEmpty(input.Name, item.Name),
		Type:          firstNonEmpty(input.Type, item.Type),
		Description:   firstNonEmpty(input.Description, item.Description),
		Endpoint:      firstNonEmpty(input.Endpoint, item.Endpoint),
		Namespace:     firstNonEmpty(input.Namespace, item.Namespace),
		Username:      firstNonEmpty(input.Username, item.Username),
		Secret:        secretValue,
		SkipTLSVerify: input.SkipTLSVerify,
	}

	probe, err := s.probeRegistryIntegration(c.Request.Context(), payload)
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
	item.Namespace = strings.Trim(strings.TrimSpace(payload.Namespace), "/")
	item.Username = strings.TrimSpace(payload.Username)
	item.SkipTLSVerify = payload.SkipTLSVerify
	item.Status = "connected"
	item.LastError = ""
	item.LastCheckedAt = nowPtr()

	secretEncrypted := ""
	if strings.TrimSpace(secretValue) != "" {
		secretEncrypted, err = s.kubeFactory.Encrypt(secretValue)
		if err != nil {
			respondError(c, http.StatusInternalServerError, "加密仓库凭据失败")
			return
		}
	}
	item.SecretEncrypted = secretEncrypted

	if err := s.db.Save(&item).Error; err != nil {
		respondError(c, http.StatusBadRequest, "更新仓库集成失败")
		return
	}

	s.clearRegistryArtifactCache()

	respondData(c, http.StatusOK, gin.H{
		"integration": serializeRegistryIntegration(item),
		"probe":       probe,
	})
}

func (s *Server) deleteRegistryIntegration(c *gin.Context) {
	identifier, err := parseUintParam(c, "id")
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid registry id")
		return
	}

	if err := s.db.Delete(&model.RegistryIntegration{}, identifier).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "删除仓库集成失败")
		return
	}

	s.clearRegistryArtifactCache()

	respondNoContent(c)
}

func (s *Server) listRegistryArtifacts(c *gin.Context) {
	item, endpoint, err := s.loadRegistryEndpoint(c)
	if err != nil {
		return
	}

	client, err := integration.NewRegistryClient(endpoint)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	limit := defaultInt(c.Query("limit"), 160)
	search := strings.TrimSpace(c.Query("search"))
	cacheKey := s.registryArtifactCacheKey(item.ID, item.Namespace, search, limit)
	if cached, loadedAt, ok := s.loadRegistryArtifactCache(cacheKey); ok {
		respondData(c, http.StatusOK, gin.H{
			"integration":    serializeRegistryIntegration(*item),
			"items":          cached.Items,
			"imageSpaces":    cached.ImageSpaces,
			"repositoryHint": cached.RepositoryHint,
			"truncated":      cached.Truncated,
			"loadedAt":       loadedAt,
		})
		return
	}

	result, err := client.ListArtifacts(c.Request.Context(), item.Namespace, search, limit)
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

	loadedAt := time.Now()
	s.storeRegistryArtifactCache(cacheKey, result, loadedAt)

	respondData(c, http.StatusOK, gin.H{
		"integration":    serializeRegistryIntegration(*item),
		"items":          result.Items,
		"imageSpaces":    result.ImageSpaces,
		"repositoryHint": result.RepositoryHint,
		"truncated":      result.Truncated,
		"loadedAt":       loadedAt,
	})
}

func (s *Server) loadRegistryEndpoint(c *gin.Context) (*model.RegistryIntegration, integration.EndpointConfig, error) {
	identifier, err := parseUintParam(c, "id")
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid registry id")
		return nil, integration.EndpointConfig{}, err
	}

	var item model.RegistryIntegration
	if err := s.db.First(&item, identifier).Error; err != nil {
		respondError(c, http.StatusNotFound, "registry integration not found")
		return nil, integration.EndpointConfig{}, err
	}

	secret, err := s.kubeFactory.Decrypt(item.SecretEncrypted)
	if err != nil && item.SecretEncrypted != "" {
		respondError(c, http.StatusInternalServerError, "解密仓库凭据失败")
		return nil, integration.EndpointConfig{}, err
	}

	return &item, integration.EndpointConfig{
		Endpoint:      item.Endpoint,
		Username:      item.Username,
		Secret:        secret,
		SkipTLSVerify: item.SkipTLSVerify,
	}, nil
}

func (s *Server) probeRegistryIntegration(ctx context.Context, input registryIntegrationPayload) (integration.RegistryProbe, error) {
	if strings.TrimSpace(input.Name) == "" {
		return integration.RegistryProbe{}, httpError("name is required")
	}
	if !integration.ValidRepositoryProvider(strings.TrimSpace(input.Type)) {
		return integration.RegistryProbe{}, httpError("unsupported registry provider")
	}
	if strings.TrimSpace(input.Endpoint) == "" {
		return integration.RegistryProbe{}, httpError("endpoint is required")
	}

	client, err := integration.NewRegistryClient(integration.EndpointConfig{
		Endpoint:      strings.TrimSpace(input.Endpoint),
		Username:      strings.TrimSpace(input.Username),
		Secret:        strings.TrimSpace(input.Secret),
		SkipTLSVerify: input.SkipTLSVerify,
	})
	if err != nil {
		return integration.RegistryProbe{}, err
	}

	return client.Test(ctx)
}

func serializeRegistryIntegration(item model.RegistryIntegration) gin.H {
	return gin.H{
		"id":             item.ID,
		"name":           item.Name,
		"type":           item.Type,
		"description":    item.Description,
		"endpoint":       item.Endpoint,
		"namespace":      item.Namespace,
		"username":       item.Username,
		"skipTLSVerify":  item.SkipTLSVerify,
		"status":         item.Status,
		"lastError":      item.LastError,
		"lastCheckedAt":  item.LastCheckedAt,
		"createdAt":      item.CreatedAt,
		"updatedAt":      item.UpdatedAt,
		"hasCredential":  item.SecretEncrypted != "",
		"displayAddress": item.Endpoint,
	}
}

func firstNonEmpty(current string, fallback string) string {
	if strings.TrimSpace(current) != "" {
		return strings.TrimSpace(current)
	}

	return strings.TrimSpace(fallback)
}

func defaultInt(raw string, fallback int) int {
	if parsed, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && parsed > 0 {
		return parsed
	}

	return fallback
}
