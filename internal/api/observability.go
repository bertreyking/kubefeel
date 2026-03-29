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
	"regexp"
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

func (s *Server) proxyGrafana(c *gin.Context) {
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
		proxyPath := strings.TrimSpace(c.Param("proxyPath"))
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
		if location := strings.TrimSpace(resp.Header.Get("Location")); strings.HasPrefix(location, "/") {
			resp.Header.Set("Location", proxyPrefix+location)
		}
		resp.Header.Del("Set-Cookie")
		resp.Header.Del("X-Frame-Options")
		resp.Header.Del("Frame-Options")
		resp.Header.Del("Content-Security-Policy")
		resp.Header.Del("Content-Security-Policy-Report-Only")

		if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
			body, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				return readErr
			}
			_ = resp.Body.Close()

			rewritten := rewriteGrafanaHTML(body, proxyPrefix)
			resp.Body = io.NopCloser(bytes.NewReader(rewritten))
			resp.ContentLength = int64(len(rewritten))
			resp.Header.Set("Content-Length", strconv.Itoa(len(rewritten)))
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
		if _, _, err := loginGrafanaSession(ctx, integration.EndpointConfig{
			Endpoint:      strings.TrimSpace(input.Endpoint),
			Username:      strings.TrimSpace(input.Username),
			Secret:        strings.TrimSpace(input.Secret),
			SkipTLSVerify: input.SkipTLSVerify,
		}); err != nil {
			return integration.ObservabilityProbe{}, fmt.Errorf("grafana web login failed: %w", err)
		}
		probe.Message = "Grafana 已连通，且 Web 登录凭据已校验。"
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
