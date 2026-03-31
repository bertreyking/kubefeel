package api

import (
	"errors"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"multikube-manager/internal/config"
	"multikube-manager/internal/kube"
	"multikube-manager/internal/model"
	"multikube-manager/internal/provision"
	"multikube-manager/internal/rbac"
	"multikube-manager/internal/security"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type Server struct {
	cfg                   config.Config
	db                    *gorm.DB
	jwtManager            *security.JWTManager
	kubeFactory           *kube.Factory
	provisioner           *provision.Runner
	cacheMu               sync.RWMutex
	registryArtifactCache map[string]registryArtifactCacheEntry
	grafanaSessionCache   map[uint]grafanaSessionCacheEntry
}

func NewServer(
	cfg config.Config,
	db *gorm.DB,
	jwtManager *security.JWTManager,
	kubeFactory *kube.Factory,
	provisioner *provision.Runner,
) *Server {
	server := &Server{
		cfg:                   cfg,
		db:                    db,
		jwtManager:            jwtManager,
		kubeFactory:           kubeFactory,
		provisioner:           provisioner,
		registryArtifactCache: make(map[string]registryArtifactCacheEntry),
		grafanaSessionCache:   make(map[uint]grafanaSessionCacheEntry),
	}

	server.recoverProvisionJobs()

	return server
}

func (s *Server) Run() error {
	router := gin.Default()
	router.Use(corsMiddleware())

	router.GET("/api/healthz", func(c *gin.Context) {
		respondData(c, http.StatusOK, gin.H{"status": "ok"})
	})

	router.POST("/api/auth/login", s.login)
	router.POST("/api/auth/logout", s.logout)

	api := router.Group("/api")
	api.Use(s.authMiddleware())
	{
		api.GET("/auth/me", s.currentUser)

		api.GET("/catalog/permissions", s.requirePermission(rbac.PermissionRolesRead), s.permissionCatalog)
		api.GET("/catalog/resource-types", s.requirePermission(rbac.PermissionResourcesRead), s.resourceCatalog)
		api.GET("/catalog/app-templates", s.requirePermission(rbac.PermissionResourcesRead), s.appTemplateCatalog)
		api.GET("/catalog/provision-templates", s.requirePermission(rbac.PermissionClustersWrite), s.provisionTemplateCatalog)
		api.GET("/catalog/image-registry-presets", s.requirePermission(rbac.PermissionClustersWrite), s.imageRegistryPresetCatalog)
		api.GET("/catalog/repository-providers", s.requirePermission(rbac.PermissionRegistriesRead), s.repositoryProviderCatalog)
		api.GET("/catalog/observability-kinds", s.requirePermission(rbac.PermissionObservabilityRead), s.observabilityKindCatalog)

		api.GET("/dashboard", s.requirePermission(rbac.PermissionDashboardRead), s.dashboardSummary)

		api.GET("/clusters", s.requirePermission(rbac.PermissionClustersRead), s.listClusters)
		api.POST("/clusters/preview", s.requirePermission(rbac.PermissionClustersWrite), s.previewCluster)
		api.POST("/clusters", s.requirePermission(rbac.PermissionClustersWrite), s.createCluster)
		api.POST("/clusters/provision-checks", s.requirePermission(rbac.PermissionClustersWrite), s.precheckClusterProvision)
		api.POST("/clusters/provision-jobs", s.requirePermission(rbac.PermissionClustersWrite), s.createClusterProvisionJob)
		api.GET("/clusters/provision-jobs/:id", s.requirePermission(rbac.PermissionClustersRead), s.getClusterProvisionJob)
		api.GET("/clusters/:id", s.requirePermission(rbac.PermissionClustersRead), s.getCluster)
		api.PUT("/clusters/:id", s.requirePermission(rbac.PermissionClustersWrite), s.updateCluster)
		api.DELETE("/clusters/:id", s.requirePermission(rbac.PermissionClustersWrite), s.deleteCluster)
		api.POST("/clusters/:id/test", s.requirePermission(rbac.PermissionClustersWrite), s.testCluster)
		api.GET("/clusters/:id/namespaces", s.requirePermission(rbac.PermissionResourcesRead), s.listNamespaces)
		api.GET("/clusters/:id/inspection", s.requirePermission(rbac.PermissionClustersRead), s.inspectCluster)

		api.GET("/clusters/:id/resources/:resourceType", s.requirePermission(rbac.PermissionResourcesRead), s.listResources)
		api.GET("/clusters/:id/resources/:resourceType/:name", s.requirePermission(rbac.PermissionResourcesRead), s.getResource)
		api.POST("/clusters/:id/resources/:resourceType", s.requirePermission(rbac.PermissionResourcesWrite), s.createResource)
		api.PUT("/clusters/:id/resources/:resourceType/:name", s.requirePermission(rbac.PermissionResourcesWrite), s.updateResource)
		api.DELETE("/clusters/:id/resources/:resourceType/:name", s.requirePermission(rbac.PermissionResourcesWrite), s.deleteResource)
		api.GET("/clusters/:id/workloads/:resourceType/:name/pods", s.requirePermission(rbac.PermissionResourcesRead), s.listWorkloadPods)
		api.GET("/clusters/:id/workloads/:resourceType/:name/relations", s.requirePermission(rbac.PermissionResourcesRead), s.listWorkloadRelations)
		api.GET("/clusters/:id/workloads/:resourceType/:name/history", s.requirePermission(rbac.PermissionResourcesRead), s.getWorkloadHistory)
		api.POST("/clusters/:id/workloads/:resourceType/:name/rollback", s.requirePermission(rbac.PermissionResourcesWrite), s.rollbackWorkload)
		api.GET("/clusters/:id/pods/:name/logs", s.requirePermission(rbac.PermissionResourcesRead), s.getPodLogs)
		api.GET("/clusters/:id/pods/:name/logs/stream", s.requirePermission(rbac.PermissionResourcesRead), s.streamPodLogs)
		api.POST("/clusters/:id/pods/:name/exec", s.requirePermission(rbac.PermissionResourcesWrite), s.execPodCommand)
		api.GET("/clusters/:id/pods/:name/terminal", s.requirePermission(rbac.PermissionResourcesWrite), s.openPodTerminal)
		api.POST("/clusters/:id/pods/:name/files/upload", s.requirePermission(rbac.PermissionResourcesWrite), s.uploadPodFile)
		api.GET("/clusters/:id/pods/:name/files/download", s.requirePermission(rbac.PermissionResourcesRead), s.downloadPodFile)
		api.POST("/app-templates/deploy", s.requirePermission(rbac.PermissionResourcesWrite), s.deployAppTemplate)

		api.GET("/users", s.requirePermission(rbac.PermissionUsersRead), s.listUsers)
		api.POST("/users", s.requirePermission(rbac.PermissionUsersWrite), s.createUser)
		api.PUT("/users/:id", s.requirePermission(rbac.PermissionUsersWrite), s.updateUser)
		api.DELETE("/users/:id", s.requirePermission(rbac.PermissionUsersWrite), s.deleteUser)

		api.GET("/roles", s.requirePermission(rbac.PermissionRolesRead), s.listRoles)
		api.POST("/roles", s.requirePermission(rbac.PermissionRolesWrite), s.createRole)
		api.GET("/roles/:id", s.requirePermission(rbac.PermissionRolesRead), s.getRole)
		api.PUT("/roles/:id", s.requirePermission(rbac.PermissionRolesWrite), s.updateRole)
		api.DELETE("/roles/:id", s.requirePermission(rbac.PermissionRolesWrite), s.deleteRole)

		api.GET("/registries", s.requirePermission(rbac.PermissionRegistriesRead), s.listRegistryIntegrations)
		api.POST("/registries/test", s.requirePermission(rbac.PermissionRegistriesWrite), s.testRegistryIntegration)
		api.POST("/registries/:id/test", s.requirePermission(rbac.PermissionRegistriesWrite), s.testStoredRegistryIntegration)
		api.POST("/registries", s.requirePermission(rbac.PermissionRegistriesWrite), s.createRegistryIntegration)
		api.PUT("/registries/:id", s.requirePermission(rbac.PermissionRegistriesWrite), s.updateRegistryIntegration)
		api.DELETE("/registries/:id", s.requirePermission(rbac.PermissionRegistriesWrite), s.deleteRegistryIntegration)
		api.GET("/registries/:id/artifacts", s.requirePermission(rbac.PermissionRegistriesRead), s.listRegistryArtifacts)

		api.GET("/observability/sources", s.requirePermission(rbac.PermissionObservabilityRead), s.listObservabilitySources)
		api.POST("/observability/sources/test", s.requirePermission(rbac.PermissionObservabilityWrite), s.testObservabilitySource)
		api.POST("/observability/sources/:id/test", s.requirePermission(rbac.PermissionObservabilityWrite), s.testStoredObservabilitySource)
		api.POST("/observability/sources", s.requirePermission(rbac.PermissionObservabilityWrite), s.createObservabilitySource)
		api.PUT("/observability/sources/:id", s.requirePermission(rbac.PermissionObservabilityWrite), s.updateObservabilitySource)
		api.DELETE("/observability/sources/:id", s.requirePermission(rbac.PermissionObservabilityWrite), s.deleteObservabilitySource)
		api.GET("/observability/grafana-catalog/:id", s.requirePermission(rbac.PermissionObservabilityRead), s.listGrafanaCatalog)
		api.POST("/observability/grafana-catalog/:id/folders", s.requirePermission(rbac.PermissionObservabilityWrite), s.createGrafanaFolder)
		api.PUT("/observability/grafana-catalog/:id/folders/:folderUid", s.requirePermission(rbac.PermissionObservabilityWrite), s.updateGrafanaFolder)
		api.DELETE("/observability/grafana-catalog/:id/folders/:folderUid", s.requirePermission(rbac.PermissionObservabilityWrite), s.deleteGrafanaFolder)
		api.POST("/observability/grafana-catalog/:id/dashboards", s.requirePermission(rbac.PermissionObservabilityWrite), s.createGrafanaDashboard)
		api.GET("/observability/grafana-catalog/:id/dashboards/:dashboardUid/meta", s.requirePermission(rbac.PermissionObservabilityRead), s.getGrafanaDashboardMeta)
		api.PUT("/observability/grafana-catalog/:id/dashboards/:dashboardUid", s.requirePermission(rbac.PermissionObservabilityWrite), s.updateGrafanaDashboard)
		api.DELETE("/observability/grafana-catalog/:id/dashboards/:dashboardUid", s.requirePermission(rbac.PermissionObservabilityWrite), s.deleteGrafanaDashboard)
		api.Any("/observability/grafana/:id/*proxyPath", s.requirePermission(rbac.PermissionObservabilityRead), s.proxyGrafana)
	}

	s.attachFrontend(router)

	return router.Run(s.cfg.Addr)
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Expose-Headers", "Content-Length, Content-Type")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

func (s *Server) attachFrontend(router *gin.Engine) {
	indexFile := filepath.Join(s.cfg.FrontendDir, "index.html")
	if _, err := os.Stat(indexFile); err != nil {
		return
	}

	router.Static("/assets", filepath.Join(s.cfg.FrontendDir, "assets"))

	router.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			respondError(c, http.StatusNotFound, "route not found")
			return
		}

		if assetFile, ok := frontendAssetFile(s.cfg.FrontendDir, c.Request.URL.Path); ok {
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
			c.File(assetFile)
			return
		}

		c.Header("Cache-Control", "no-store")
		c.File(indexFile)
	})
}

func frontendAssetFile(frontendDir, requestPath string) (string, bool) {
	cleanPath := path.Clean("/" + requestPath)
	if cleanPath == "/" {
		return "", false
	}

	assetFile := filepath.Join(frontendDir, filepath.FromSlash(strings.TrimPrefix(cleanPath, "/")))
	info, err := os.Stat(assetFile)
	if err != nil || info.IsDir() {
		return "", false
	}

	return assetFile, true
}

func (s *Server) login(c *gin.Context) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, http.StatusBadRequest, "invalid login payload")
		return
	}

	var user model.User
	if err := s.db.Preload("Roles.Permissions").Where("username = ?", input.Username).First(&user).Error; err != nil {
		respondError(c, http.StatusUnauthorized, "用户名或密码错误")
		return
	}

	if !user.Active {
		respondError(c, http.StatusForbidden, "用户已停用")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(input.Password)); err != nil {
		respondError(c, http.StatusUnauthorized, "用户名或密码错误")
		return
	}

	token, err := s.jwtManager.Issue(user.ID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "签发令牌失败")
		return
	}

	now := time.Now()
	if err := s.db.Model(&user).Update("last_login_at", now).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "更新登录时间失败")
		return
	}
	user.LastLoginAt = &now
	setSessionCookie(c, token, now.Add(24*time.Hour))

	respondData(c, http.StatusOK, gin.H{
		"token": token,
		"user":  serializeUser(user),
	})
}

func (s *Server) logout(c *gin.Context) {
	clearSessionCookie(c)
	respondNoContent(c)
}

func (s *Server) currentUser(c *gin.Context) {
	user := currentUserFromContext(c)
	respondData(c, http.StatusOK, serializeUser(*user))
}

func (s *Server) permissionCatalog(c *gin.Context) {
	respondData(c, http.StatusOK, rbac.Catalog())
}

func (s *Server) resourceCatalog(c *gin.Context) {
	respondData(c, http.StatusOK, kube.Catalog())
}

func (s *Server) provisionTemplateCatalog(c *gin.Context) {
	if s.provisioner == nil {
		respondData(c, http.StatusOK, []provision.ProvisionTemplate{})
		return
	}

	respondData(c, http.StatusOK, s.provisioner.Catalog())
}

func (s *Server) imageRegistryPresetCatalog(c *gin.Context) {
	if s.provisioner == nil {
		respondData(c, http.StatusOK, []provision.ImageRegistryPreset{})
		return
	}

	respondData(c, http.StatusOK, provision.BuiltinImageRegistryPresets())
}

func setSessionCookie(c *gin.Context, token string, expiresAt time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isHTTPSRequest(c.Request),
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
	})
}

func clearSessionCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isHTTPSRequest(c.Request),
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
	})
}

func isHTTPSRequest(request *http.Request) bool {
	if request == nil {
		return false
	}
	if request.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(request.Header.Get("X-Forwarded-Proto")), "https")
}

func (s *Server) loadUserByID(id uint) (*model.User, error) {
	var user model.User
	if err := s.db.Preload("Roles.Permissions").First(&user, id).Error; err != nil {
		return nil, err
	}

	return &user, nil
}

func serializeUser(user model.User) gin.H {
	return gin.H{
		"id":          user.ID,
		"username":    user.Username,
		"displayName": user.DisplayName,
		"active":      user.Active,
		"lastLoginAt": user.LastLoginAt,
		"roles":       user.Roles,
		"permissions": user.PermissionKeys(),
		"createdAt":   user.CreatedAt,
		"updatedAt":   user.UpdatedAt,
	}
}

func parseUintParam(c *gin.Context, key string) (uint, error) {
	value := c.Param(key)
	parsed, err := strconv.ParseUint(value, 10, 64)
	return uint(parsed), err
}

func isRecordNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

func nowPtr() *time.Time {
	now := time.Now()
	return &now
}
