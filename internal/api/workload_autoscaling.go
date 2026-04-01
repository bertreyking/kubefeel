package api

import (
	"net/http"
	"strings"

	"multikube-manager/internal/kube"
	"multikube-manager/internal/model"

	"github.com/gin-gonic/gin"
)

func (s *Server) getWorkloadAutoscaling(c *gin.Context) {
	_, runtime, namespace, resourceType, resourceName, ok := s.loadWorkloadAutoscalingContext(c)
	if !ok {
		return
	}

	snapshot, err := kube.ListWorkloadAutoscaling(c.Request.Context(), runtime, resourceType, namespace, resourceName)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	respondData(c, http.StatusOK, snapshot)
}

func (s *Server) upsertWorkloadHPA(c *gin.Context) {
	_, runtime, namespace, resourceType, resourceName, ok := s.loadWorkloadAutoscalingContext(c)
	if !ok {
		return
	}

	var input kube.HPAUpsertPayload
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, http.StatusBadRequest, "invalid hpa payload")
		return
	}

	if err := kube.UpsertWorkloadHPA(c.Request.Context(), runtime, resourceType, namespace, resourceName, input); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	respondNoContent(c)
}

func (s *Server) deleteWorkloadHPA(c *gin.Context) {
	_, runtime, namespace, resourceType, resourceName, ok := s.loadWorkloadAutoscalingContext(c)
	if !ok {
		return
	}

	if err := kube.DeleteWorkloadHPA(c.Request.Context(), runtime, resourceType, namespace, resourceName); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	respondNoContent(c)
}

func (s *Server) upsertWorkloadScaledObject(c *gin.Context) {
	_, runtime, namespace, resourceType, resourceName, ok := s.loadWorkloadAutoscalingContext(c)
	if !ok {
		return
	}

	var input kube.KEDAUpsertPayload
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, http.StatusBadRequest, "invalid event autoscaling payload")
		return
	}

	if err := kube.UpsertWorkloadScaledObject(c.Request.Context(), runtime, resourceType, namespace, resourceName, input); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	respondNoContent(c)
}

func (s *Server) deleteWorkloadScaledObject(c *gin.Context) {
	_, runtime, namespace, resourceType, resourceName, ok := s.loadWorkloadAutoscalingContext(c)
	if !ok {
		return
	}

	if err := kube.DeleteWorkloadScaledObject(c.Request.Context(), runtime, resourceType, namespace, resourceName); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	respondNoContent(c)
}

func (s *Server) upsertWorkloadKnativeAutoscaling(c *gin.Context) {
	_, runtime, namespace, resourceType, resourceName, ok := s.loadWorkloadAutoscalingContext(c)
	if !ok {
		return
	}

	var input kube.KnativeUpsertPayload
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, http.StatusBadRequest, "invalid api autoscaling payload")
		return
	}

	if err := kube.UpsertWorkloadKnativeAutoscaling(c.Request.Context(), runtime, resourceType, namespace, resourceName, input); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	respondNoContent(c)
}

func (s *Server) deleteWorkloadKnativeAutoscaling(c *gin.Context) {
	_, runtime, namespace, resourceType, resourceName, ok := s.loadWorkloadAutoscalingContext(c)
	if !ok {
		return
	}

	if err := kube.DeleteWorkloadKnativeAutoscaling(c.Request.Context(), runtime, resourceType, namespace, resourceName); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	respondNoContent(c)
}

func (s *Server) loadWorkloadAutoscalingContext(
	c *gin.Context,
) (*model.Cluster, *kube.Runtime, string, string, string, bool) {
	cluster, runtime, namespace, ok := s.loadPodContext(c)
	if !ok {
		return nil, nil, "", "", "", false
	}

	resourceType := strings.TrimSpace(c.Param("resourceType"))
	resourceName := strings.TrimSpace(c.Param("name"))
	if !kube.SupportsWorkloadAutoscaling(resourceType) {
		respondError(c, http.StatusBadRequest, "当前资源类型暂不支持弹性伸缩")
		return nil, nil, "", "", "", false
	}
	if resourceName == "" {
		respondError(c, http.StatusBadRequest, "resource name is required")
		return nil, nil, "", "", "", false
	}

	return cluster, runtime, namespace, resourceType, resourceName, true
}
