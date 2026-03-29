package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"multikube-manager/internal/kube"
	"multikube-manager/internal/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type clusterPayload struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Region      string `json:"region"`
	Mode        string `json:"mode"`
	Kubeconfig  string `json:"kubeconfig"`
}

func (s *Server) previewCluster(c *gin.Context) {
	var input struct {
		Kubeconfig string `json:"kubeconfig"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, http.StatusBadRequest, "invalid cluster payload")
		return
	}

	if strings.TrimSpace(input.Kubeconfig) == "" {
		respondError(c, http.StatusBadRequest, "kubeconfig is required")
		return
	}

	probe, err := s.probeCluster(input.Kubeconfig)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	respondData(c, http.StatusOK, gin.H{
		"server":         probe.Server,
		"currentContext": probe.CurrentContext,
		"version":        probe.Version,
		"criVersion":     probe.CRIVersion,
	})
}

func (s *Server) listClusters(c *gin.Context) {
	var clusters []model.Cluster
	if err := s.db.Order("created_at desc").Find(&clusters).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "查询集群失败")
		return
	}

	result := make([]gin.H, 0, len(clusters))
	for index := range clusters {
		s.refreshClusterRuntimeMetadata(c.Request.Context(), &clusters[index])
		cluster := clusters[index]
		result = append(result, serializeCluster(cluster, s.resolveClusterNodeHealth(c.Request.Context(), &cluster)))
	}

	respondData(c, http.StatusOK, result)
}

func (s *Server) getCluster(c *gin.Context) {
	cluster, err := s.loadClusterFromParam(c)
	if err != nil {
		return
	}

	s.refreshClusterRuntimeMetadata(c.Request.Context(), cluster)
	respondData(c, http.StatusOK, serializeCluster(*cluster, s.resolveClusterNodeHealth(c.Request.Context(), cluster)))
}

func (s *Server) createCluster(c *gin.Context) {
	var input clusterPayload
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, http.StatusBadRequest, "invalid cluster payload")
		return
	}

	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Region) == "" || strings.TrimSpace(input.Kubeconfig) == "" {
		respondError(c, http.StatusBadRequest, "name, region and kubeconfig are required")
		return
	}

	probe, err := s.probeCluster(input.Kubeconfig)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	encrypted, err := s.kubeFactory.Encrypt(input.Kubeconfig)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "加密 kubeconfig 失败")
		return
	}

	cluster := model.Cluster{
		Name:                strings.TrimSpace(input.Name),
		Description:         strings.TrimSpace(input.Description),
		Region:              strings.TrimSpace(input.Region),
		Server:              probe.Server,
		CurrentContext:      probe.CurrentContext,
		Version:             probe.Version,
		CRIVersion:          probe.CRIVersion,
		Mode:                normalizeClusterMode(input.Mode),
		Status:              "connected",
		LastConnectedAt:     nowPtr(),
		KubeconfigEncrypted: encrypted,
	}

	if err := s.db.Create(&cluster).Error; err != nil {
		respondError(c, http.StatusBadRequest, "保存集群失败，名称可能已存在")
		return
	}

	if cluster.Mode == "maintenance" {
		if err := s.applyClusterMode(c.Request.Context(), &cluster, cluster.Mode); err != nil {
			s.kubeFactory.Invalidate(cluster.ID)
			_ = s.db.Delete(&cluster).Error
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}

		if err := s.db.Save(&cluster).Error; err != nil {
			respondError(c, http.StatusBadRequest, "保存集群失败")
			return
		}
	}

	respondData(c, http.StatusCreated, serializeCluster(cluster, s.resolveClusterNodeHealth(c.Request.Context(), &cluster)))
}

func (s *Server) updateCluster(c *gin.Context) {
	cluster, err := s.loadClusterFromParam(c)
	if err != nil {
		return
	}

	var input clusterPayload
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, http.StatusBadRequest, "invalid cluster payload")
		return
	}

	if strings.TrimSpace(input.Region) == "" {
		respondError(c, http.StatusBadRequest, "region is required")
		return
	}

	previousMode := normalizeClusterMode(cluster.Mode)
	nextMode := previousMode
	if strings.TrimSpace(input.Mode) != "" {
		nextMode = normalizeClusterMode(input.Mode)
	}

	if trimmed := strings.TrimSpace(input.Name); trimmed != "" {
		cluster.Name = trimmed
	}
	cluster.Description = strings.TrimSpace(input.Description)
	cluster.Region = strings.TrimSpace(input.Region)
	modeNeedsSync := false

	if strings.TrimSpace(input.Kubeconfig) != "" {
		probe, probeErr := s.probeCluster(input.Kubeconfig)
		if probeErr != nil {
			respondError(c, http.StatusBadRequest, probeErr.Error())
			return
		}

		encrypted, encryptErr := s.kubeFactory.Encrypt(input.Kubeconfig)
		if encryptErr != nil {
			respondError(c, http.StatusInternalServerError, "加密 kubeconfig 失败")
			return
		}

		cluster.Server = probe.Server
		cluster.CurrentContext = probe.CurrentContext
		cluster.Version = probe.Version
		cluster.CRIVersion = probe.CRIVersion
		cluster.Status = "connected"
		cluster.LastError = ""
		cluster.LastConnectedAt = nowPtr()
		cluster.KubeconfigEncrypted = encrypted
		s.kubeFactory.Invalidate(cluster.ID)
		modeNeedsSync = true
	}

	if nextMode != previousMode || modeNeedsSync {
		if err := s.applyClusterMode(c.Request.Context(), cluster, nextMode); err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
	}
	cluster.Mode = nextMode

	if err := s.db.Save(cluster).Error; err != nil {
		respondError(c, http.StatusBadRequest, "更新集群失败")
		return
	}

	respondData(c, http.StatusOK, serializeCluster(*cluster, s.resolveClusterNodeHealth(c.Request.Context(), cluster)))
}

func (s *Server) deleteCluster(c *gin.Context) {
	cluster, err := s.loadClusterFromParam(c)
	if err != nil {
		return
	}

	s.kubeFactory.Invalidate(cluster.ID)
	if err := s.db.Delete(cluster).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "删除集群失败")
		return
	}

	respondNoContent(c)
}

func (s *Server) testCluster(c *gin.Context) {
	cluster, err := s.loadClusterFromParam(c)
	if err != nil {
		return
	}

	s.kubeFactory.Invalidate(cluster.ID)
	runtime, err := s.kubeFactory.Runtime(cluster)
	if err != nil {
		cluster.Status = "error"
		cluster.LastError = err.Error()
		s.db.Save(cluster)
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	version, err := runtime.Discovery.ServerVersion()
	if err != nil {
		cluster.Status = "error"
		cluster.LastError = err.Error()
		s.db.Save(cluster)
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	cluster.Status = "connected"
	cluster.Version = version.String()
	if criVersion, criErr := kube.DetectContainerRuntimeVersion(c.Request.Context(), runtime); criErr == nil && criVersion != "" {
		cluster.CRIVersion = criVersion
	}
	cluster.Mode = normalizeClusterMode(cluster.Mode)
	cluster.LastError = ""
	cluster.LastConnectedAt = nowPtr()
	if err := s.db.Save(cluster).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "更新集群状态失败")
		return
	}

	respondData(c, http.StatusOK, serializeCluster(*cluster, s.resolveClusterNodeHealth(c.Request.Context(), cluster)))
}

func (s *Server) listNamespaces(c *gin.Context) {
	cluster, err := s.loadClusterFromParam(c)
	if err != nil {
		return
	}

	runtime, err := s.kubeFactory.Runtime(cluster)
	if err != nil {
		s.markClusterError(cluster, err)
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	list, err := runtime.Dynamic.Resource(namespaceGVR).List(c.Request.Context(), metav1.ListOptions{})
	if err != nil {
		s.markClusterError(cluster, err)
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	items := make([]gin.H, 0, len(list.Items))
	for _, item := range list.Items {
		items = append(items, gin.H{
			"name":              item.GetName(),
			"creationTimestamp": item.GetCreationTimestamp(),
			"labels":            item.GetLabels(),
		})
	}

	respondData(c, http.StatusOK, items)
}

func (s *Server) loadClusterFromParam(c *gin.Context) (*model.Cluster, error) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid cluster id")
		return nil, err
	}

	var cluster model.Cluster
	if err := s.db.First(&cluster, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondError(c, http.StatusNotFound, "cluster not found")
		} else {
			respondError(c, http.StatusInternalServerError, "load cluster failed")
		}
		return nil, err
	}

	return &cluster, nil
}

func (s *Server) probeCluster(kubeconfig string) (kube.ProbeResult, error) {
	probe, err := kube.Probe(kubeconfig)
	if err != nil {
		return kube.ProbeResult{}, err
	}

	return probe, nil
}

func (s *Server) markClusterError(cluster *model.Cluster, err error) {
	cluster.Status = "error"
	cluster.LastError = err.Error()
	cluster.Mode = normalizeClusterMode(cluster.Mode)
	s.db.Save(cluster)
}

func (s *Server) applyClusterMode(ctx context.Context, cluster *model.Cluster, mode string) error {
	mode = normalizeClusterMode(mode)

	runtime, err := s.kubeFactory.Runtime(cluster)
	if err != nil {
		s.markClusterError(cluster, err)
		return err
	}

	switch mode {
	case "maintenance":
		if _, err := kube.SetWorkerNodesSchedulable(ctx, runtime, false); err != nil {
			s.markClusterError(cluster, err)
			return err
		}
	case "ready":
		if _, err := kube.SetWorkerNodesSchedulable(ctx, runtime, true); err != nil {
			s.markClusterError(cluster, err)
			return err
		}
	}

	cluster.Status = "connected"
	cluster.Mode = mode
	cluster.LastError = ""
	cluster.LastConnectedAt = nowPtr()

	return nil
}

func serializeCluster(cluster model.Cluster, nodes *kube.HealthSummary) gin.H {
	payload := gin.H{
		"id":              cluster.ID,
		"name":            cluster.Name,
		"description":     cluster.Description,
		"region":          cluster.Region,
		"server":          cluster.Server,
		"currentContext":  cluster.CurrentContext,
		"version":         cluster.Version,
		"criVersion":      cluster.CRIVersion,
		"mode":            normalizeClusterMode(cluster.Mode),
		"status":          cluster.Status,
		"lastError":       cluster.LastError,
		"lastConnectedAt": cluster.LastConnectedAt,
		"createdAt":       cluster.CreatedAt,
		"updatedAt":       cluster.UpdatedAt,
	}

	if nodes != nil {
		payload["nodes"] = nodes
	}

	return payload
}

func normalizeClusterMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "maintenance":
		return "maintenance"
	default:
		return "ready"
	}
}

func (s *Server) refreshClusterRuntimeMetadata(ctx context.Context, cluster *model.Cluster) {
	if cluster == nil {
		return
	}

	cluster.Mode = normalizeClusterMode(cluster.Mode)
	if cluster.CRIVersion != "" && cluster.Version != "" {
		return
	}

	runtime, err := s.kubeFactory.Runtime(cluster)
	if err != nil {
		return
	}

	serverVersion, err := runtime.Discovery.ServerVersion()
	if err == nil && serverVersion != nil && cluster.Version == "" {
		cluster.Version = serverVersion.String()
	}

	if cluster.CRIVersion == "" {
		if criVersion, criErr := kube.DetectContainerRuntimeVersion(ctx, runtime); criErr == nil {
			cluster.CRIVersion = criVersion
		}
	}

	_ = s.db.Save(cluster).Error
}

func (s *Server) resolveClusterNodeHealth(ctx context.Context, cluster *model.Cluster) *kube.HealthSummary {
	if cluster == nil || cluster.Status != "connected" {
		return nil
	}

	runtime, err := s.kubeFactory.Runtime(cluster)
	if err != nil {
		s.markClusterError(cluster, err)
		return nil
	}

	summary, err := kube.CollectNodeHealth(ctx, runtime)
	if err != nil {
		s.markClusterError(cluster, err)
		return nil
	}

	cluster.LastError = ""
	return &summary
}
