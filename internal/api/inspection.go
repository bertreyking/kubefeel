package api

import (
	"net/http"

	"multikube-manager/internal/kube"

	"github.com/gin-gonic/gin"
)

func (s *Server) inspectCluster(c *gin.Context) {
	cluster, err := s.loadClusterFromParam(c)
	if err != nil {
		return
	}

	s.refreshClusterRuntimeMetadata(c.Request.Context(), cluster)

	runtime, err := s.kubeFactory.Runtime(cluster)
	if err != nil {
		s.markClusterError(cluster, err)
		respondData(c, http.StatusOK, gin.H{
			"cluster":    serializeCluster(*cluster, nil),
			"inspection": kube.UnavailableInspectionReport(err.Error()),
		})
		return
	}

	report := kube.InspectCluster(c.Request.Context(), runtime, normalizeClusterMode(cluster.Mode))
	if report.Version != "" {
		cluster.Version = report.Version
	}

	if apiItemFailed(report) {
		s.markClusterError(cluster, httpError(report.Items[0].Detail))
		respondData(c, http.StatusOK, gin.H{
			"cluster":    serializeCluster(*cluster, nil),
			"inspection": report,
		})
		return
	}

	cluster.Status = "connected"
	cluster.LastError = ""
	cluster.LastConnectedAt = nowPtr()
	_ = s.db.Save(cluster).Error

	var nodes *kube.HealthSummary
	if report.Overview.Nodes.Total > 0 {
		nodes = &report.Overview.Nodes
	}

	respondData(c, http.StatusOK, gin.H{
		"cluster":    serializeCluster(*cluster, nodes),
		"inspection": report,
	})
}

func apiItemFailed(report kube.ClusterInspectionReport) bool {
	if len(report.Items) == 0 {
		return true
	}

	for _, item := range report.Items {
		if item.Key == "api" {
			return item.Status == "failed"
		}
	}

	return true
}

type httpError string

func (e httpError) Error() string {
	return string(e)
}
