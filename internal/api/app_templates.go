package api

import (
	"errors"
	"net/http"
	"strings"

	helmapp "multikube-manager/internal/helm"
	"multikube-manager/internal/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type appTemplateDeployPayload struct {
	ClusterID       uint   `json:"clusterId"`
	Namespace       string `json:"namespace"`
	CreateNamespace bool   `json:"createNamespace"`
	ReleaseName     string `json:"releaseName"`
	TemplateKey     string `json:"templateKey"`
	RepoURL         string `json:"repoURL"`
	ChartName       string `json:"chartName"`
	Version         string `json:"version"`
	Values          string `json:"values"`
}

func (s *Server) appTemplateCatalog(c *gin.Context) {
	respondData(c, http.StatusOK, helmapp.BuiltinTemplates())
}

func (s *Server) deployAppTemplate(c *gin.Context) {
	var input appTemplateDeployPayload
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, http.StatusBadRequest, "invalid app template payload")
		return
	}

	if input.ClusterID == 0 || strings.TrimSpace(input.Namespace) == "" || strings.TrimSpace(input.ReleaseName) == "" {
		respondError(c, http.StatusBadRequest, "clusterId, namespace and releaseName are required")
		return
	}

	var cluster model.Cluster
	if err := s.db.First(&cluster, input.ClusterID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondError(c, http.StatusNotFound, "cluster not found")
			return
		}
		respondError(c, http.StatusInternalServerError, "load cluster failed")
		return
	}

	runtime, err := s.kubeFactory.Runtime(&cluster)
	if err != nil {
		s.markClusterError(&cluster, err)
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	request := helmapp.DeployRequest{
		Namespace:       strings.TrimSpace(input.Namespace),
		CreateNamespace: input.CreateNamespace,
		ReleaseName:     strings.TrimSpace(input.ReleaseName),
		RepoURL:         strings.TrimSpace(input.RepoURL),
		ChartName:       strings.TrimSpace(input.ChartName),
		Version:         strings.TrimSpace(input.Version),
		Values:          input.Values,
	}

	if strings.TrimSpace(input.TemplateKey) != "" {
		template, ok := helmapp.LookupTemplate(strings.TrimSpace(input.TemplateKey))
		if !ok {
			respondError(c, http.StatusBadRequest, "app template not found")
			return
		}
		if strings.TrimSpace(template.LocalChartPath) != "" {
			request.RepoURL = ""
			request.ChartName = template.LocalChartPath
		}
		if request.RepoURL == "" {
			request.RepoURL = template.RepoURL
		}
		if request.ChartName == "" {
			request.ChartName = template.ChartName
		}
		if request.Version == "" {
			request.Version = template.DefaultVersion
		}
		if strings.TrimSpace(request.Values) == "" {
			request.Values = template.Values
		}
	}

	if request.ChartName == "" {
		respondError(c, http.StatusBadRequest, "chartName is required")
		return
	}

	kubeconfig, err := s.kubeFactory.Decrypt(cluster.KubeconfigEncrypted)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "解密集群凭据失败")
		return
	}

	result, err := helmapp.Deploy(c.Request.Context(), runtime, kubeconfig, request)
	if err != nil {
		s.markClusterError(&cluster, err)
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	cluster.Status = "connected"
	cluster.LastError = ""
	cluster.LastConnectedAt = nowPtr()
	_ = s.db.Save(&cluster).Error

	respondData(c, http.StatusOK, result)
}
