package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"multikube-manager/internal/kube"
	"multikube-manager/internal/model"

	"github.com/gin-gonic/gin"
)

type dashboardClusterMetrics struct {
	ID              uint               `json:"id"`
	Name            string             `json:"name"`
	Server          string             `json:"server"`
	CurrentContext  string             `json:"currentContext"`
	Version         string             `json:"version"`
	Status          string             `json:"status"`
	LastError       string             `json:"lastError"`
	LastConnectedAt *time.Time         `json:"lastConnectedAt,omitempty"`
	Nodes           kube.HealthSummary `json:"nodes"`
	Pods            kube.HealthSummary `json:"pods"`
	CPU             kube.ResourceUsage `json:"cpu"`
	Memory          kube.ResourceUsage `json:"memory"`
}

func (s *Server) dashboardSummary(c *gin.Context) {
	var clusterCount int64
	var userCount int64
	var roleCount int64

	if err := s.db.Model(&model.Cluster{}).Count(&clusterCount).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "统计集群数量失败")
		return
	}

	if err := s.db.Model(&model.User{}).Count(&userCount).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "统计用户数量失败")
		return
	}

	if err := s.db.Model(&model.Role{}).Count(&roleCount).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "统计角色数量失败")
		return
	}

	clusterMetrics, err := s.resolveDashboardClusterMetrics(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	respondData(c, http.StatusOK, gin.H{
		"clusters":              clusterCount,
		"users":                 userCount,
		"roles":                 roleCount,
		"defaultKubeconfigPath": kube.DefaultKubeconfigPath(),
		"clusterMetrics":        clusterMetrics,
	})
}

func (s *Server) resolveDashboardClusterMetrics(c *gin.Context) (*dashboardClusterMetrics, error) {
	cluster, err := s.pickDashboardCluster(c.Query("clusterId"))
	if err != nil || cluster == nil {
		return nil, err
	}

	runtime, err := s.kubeFactory.Runtime(cluster)
	if err != nil {
		s.markClusterError(cluster, err)
		return &dashboardClusterMetrics{
			ID:              cluster.ID,
			Name:            cluster.Name,
			Server:          cluster.Server,
			CurrentContext:  cluster.CurrentContext,
			Version:         cluster.Version,
			Status:          "error",
			LastError:       err.Error(),
			LastConnectedAt: cluster.LastConnectedAt,
		}, nil
	}

	overview, err := kube.CollectClusterOverview(c.Request.Context(), runtime)
	if err != nil {
		s.markClusterError(cluster, err)
		return &dashboardClusterMetrics{
			ID:              cluster.ID,
			Name:            cluster.Name,
			Server:          cluster.Server,
			CurrentContext:  cluster.CurrentContext,
			Version:         cluster.Version,
			Status:          "error",
			LastError:       err.Error(),
			LastConnectedAt: cluster.LastConnectedAt,
		}, nil
	}

	cluster.Status = "connected"
	cluster.LastError = ""
	cluster.LastConnectedAt = nowPtr()
	if saveErr := s.db.Save(cluster).Error; saveErr != nil {
		cluster.LastError = saveErr.Error()
	}

	return &dashboardClusterMetrics{
		ID:              cluster.ID,
		Name:            cluster.Name,
		Server:          cluster.Server,
		CurrentContext:  cluster.CurrentContext,
		Version:         cluster.Version,
		Status:          cluster.Status,
		LastError:       cluster.LastError,
		LastConnectedAt: cluster.LastConnectedAt,
		Nodes:           overview.Nodes,
		Pods:            overview.Pods,
		CPU:             overview.CPU,
		Memory:          overview.Memory,
	}, nil
}

func (s *Server) pickDashboardCluster(rawClusterID string) (*model.Cluster, error) {
	if rawClusterID != "" {
		parsed, err := strconv.ParseUint(rawClusterID, 10, 64)
		if err != nil {
			return nil, errors.New("invalid dashboard cluster id")
		}

		var cluster model.Cluster
		if err := s.db.First(&cluster, parsed).Error; err != nil {
			return nil, err
		}

		return &cluster, nil
	}

	var clusters []model.Cluster
	if err := s.db.Order("created_at desc").Find(&clusters).Error; err != nil {
		return nil, err
	}

	for _, cluster := range clusters {
		if cluster.Status == "connected" {
			copyCluster := cluster
			return &copyCluster, nil
		}
	}

	if len(clusters) == 0 {
		return nil, nil
	}

	copyCluster := clusters[0]
	return &copyCluster, nil
}
