package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"multikube-manager/internal/kube"
	"multikube-manager/internal/model"
	"multikube-manager/internal/provision"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type clusterProvisionPayload struct {
	Name                string                        `json:"name"`
	Region              string                        `json:"region"`
	Description         string                        `json:"description"`
	Mode                string                        `json:"mode"`
	ProvisionTemplate   string                        `json:"provisionTemplate"`
	APIServerEndpoint   string                        `json:"apiServerEndpoint"`
	KubernetesVersion   string                        `json:"kubernetesVersion"`
	ImageRegistryPreset string                        `json:"imageRegistryPreset"`
	ImageRegistry       string                        `json:"imageRegistry"`
	NetworkPlugin       string                        `json:"networkPlugin"`
	SSHUser             string                        `json:"sshUser"`
	SSHPort             int                           `json:"sshPort"`
	SSHPrivateKey       string                        `json:"sshPrivateKey"`
	Nodes               []clusterProvisionNodePayload `json:"nodes"`
}

type clusterProvisionNodePayload struct {
	Name            string `json:"name"`
	Address         string `json:"address"`
	InternalAddress string `json:"internalAddress"`
	Role            string `json:"role"`
}

func (s *Server) precheckClusterProvision(c *gin.Context) {
	if s.provisioner == nil {
		respondError(c, http.StatusNotImplemented, "当前服务未启用 Kubespray 创建能力")
		return
	}

	var input clusterProvisionPayload
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, http.StatusBadRequest, "invalid provision payload")
		return
	}

	request := toProvisionRequest(input)
	if err := s.provisioner.Validate(request); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	result, err := s.provisioner.Precheck(c.Request.Context(), request)
	if err != nil {
		respondError(c, http.StatusInternalServerError, err.Error())
		return
	}

	nameAvailable, err := s.isProvisionClusterNameAvailable(strings.TrimSpace(input.Name))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "检查集群名称失败")
		return
	}
	if !nameAvailable {
		result.Ready = false
		result.Checks = append([]provision.PrecheckItem{
			{
				Key:    "cluster-name",
				Label:  "集群名称",
				Status: "error",
				Detail: "平台里已存在同名集群，请先更换名称",
			},
		}, result.Checks...)
		result.Summary = "预检查未通过，请先处理重名集群和节点连通性问题。"
	}

	respondData(c, http.StatusOK, result)
}

func (s *Server) createClusterProvisionJob(c *gin.Context) {
	if s.provisioner == nil {
		respondError(c, http.StatusNotImplemented, "当前服务未启用 Kubespray 创建能力")
		return
	}

	var input clusterProvisionPayload
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, http.StatusBadRequest, "invalid provision payload")
		return
	}

	request := toProvisionRequest(input)
	if err := s.provisioner.Validate(request); err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	template, _, err := provision.ResolveProvisionTemplate(request.ProvisionTemplate, request.KubernetesVersion)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	nameAvailable, err := s.isProvisionClusterNameAvailable(strings.TrimSpace(input.Name))
	if err != nil {
		respondError(c, http.StatusInternalServerError, "检查集群名称失败")
		return
	}
	if !nameAvailable {
		respondError(c, http.StatusBadRequest, "集群名称已存在，请更换后重试")
		return
	}

	controlPlaneCount, workerCount := countProvisionNodes(request.Nodes)
	job := model.ClusterProvisionJob{
		Name:              strings.TrimSpace(input.Name),
		Region:            strings.TrimSpace(input.Region),
		Description:       strings.TrimSpace(input.Description),
		Mode:              normalizeClusterMode(input.Mode),
		Provider:          "kubespray",
		ProvisionTemplate: template.Key,
		KubesprayVersion:  template.KubesprayVersion,
		KubesprayImage:    template.KubesprayImage,
		ImageRegistryPreset: provision.NormalizeImageRegistryPresetForJob(
			input.ImageRegistryPreset,
			input.ImageRegistry,
		),
		ImageRegistry:     strings.TrimSpace(input.ImageRegistry),
		Status:            "pending",
		Step:              "等待执行",
		KubernetesVersion: normalizeKubernetesVersionForJob(input.KubernetesVersion),
		NetworkPlugin:     normalizeNetworkPluginForJob(input.NetworkPlugin),
		APIServerEndpoint: strings.TrimSpace(input.APIServerEndpoint),
		SSHUser:           strings.TrimSpace(input.SSHUser),
		ControlPlaneCount: controlPlaneCount,
		WorkerCount:       workerCount,
	}

	if err := s.db.Create(&job).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "创建集群任务失败")
		return
	}

	go s.runClusterProvisionJob(job.ID, request)

	respondData(c, http.StatusAccepted, s.serializeProvisionJob(job))
}

func (s *Server) getClusterProvisionJob(c *gin.Context) {
	job, err := s.loadProvisionJobFromParam(c)
	if err != nil {
		return
	}

	respondData(c, http.StatusOK, s.serializeProvisionJob(*job))
}

func (s *Server) isProvisionClusterNameAvailable(name string) (bool, error) {
	var existing model.Cluster
	if err := s.db.Where("name = ?", name).First(&existing).Error; err == nil {
		return false, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}

	return true, nil
}

func (s *Server) loadProvisionJobFromParam(c *gin.Context) (*model.ClusterProvisionJob, error) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid provision job id")
		return nil, err
	}

	var job model.ClusterProvisionJob
	if err := s.db.First(&job, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respondError(c, http.StatusNotFound, "provision job not found")
		} else {
			respondError(c, http.StatusInternalServerError, "load provision job failed")
		}
		return nil, err
	}

	return &job, nil
}

func (s *Server) serializeProvisionJob(job model.ClusterProvisionJob) gin.H {
	payload := gin.H{
		"id":                  job.ID,
		"name":                job.Name,
		"region":              job.Region,
		"description":         job.Description,
		"mode":                normalizeClusterMode(job.Mode),
		"provider":            job.Provider,
		"provisionTemplate":   job.ProvisionTemplate,
		"kubesprayVersion":    job.KubesprayVersion,
		"kubesprayImage":      job.KubesprayImage,
		"imageRegistryPreset": job.ImageRegistryPreset,
		"imageRegistry":       job.ImageRegistry,
		"status":              job.Status,
		"step":                job.Step,
		"kubernetesVersion":   job.KubernetesVersion,
		"networkPlugin":       job.NetworkPlugin,
		"apiServerEndpoint":   job.APIServerEndpoint,
		"sshUser":             job.SSHUser,
		"controlPlaneCount":   job.ControlPlaneCount,
		"workerCount":         job.WorkerCount,
		"lastError":           job.LastError,
		"resultClusterId":     job.ResultClusterID,
		"startedAt":           job.StartedAt,
		"completedAt":         job.CompletedAt,
		"createdAt":           job.CreatedAt,
		"updatedAt":           job.UpdatedAt,
	}

	if s.provisioner != nil {
		payload["log"] = s.provisioner.ReadJobLog(job.ID, 32000)
	}

	return payload
}

func (s *Server) recoverProvisionJobs() {
	if s.db == nil {
		return
	}

	updates := map[string]any{
		"status":       "failed",
		"step":         "服务已重启",
		"last_error":   "服务重启导致任务中断，请重新提交创建任务",
		"completed_at": time.Now(),
	}
	_ = s.db.Model(&model.ClusterProvisionJob{}).
		Where("status IN ?", []string{"pending", "running"}).
		Updates(updates).Error
}

func (s *Server) runClusterProvisionJob(jobID uint, request provision.ClusterRequest) {
	ctx := context.Background()

	updateJob := func(status, step, lastError string, extras map[string]any) {
		updates := map[string]any{
			"status":     status,
			"step":       step,
			"last_error": lastError,
		}
		for key, value := range extras {
			updates[key] = value
		}
		_ = s.db.Model(&model.ClusterProvisionJob{}).Where("id = ?", jobID).Updates(updates).Error
	}

	now := time.Now()
	updateJob("running", "生成 Kubespray inventory", "", map[string]any{"started_at": &now})

	result, err := s.provisioner.Run(ctx, jobID, request)
	if err != nil {
		completedAt := time.Now()
		updateJob("failed", "创建失败", err.Error(), map[string]any{"completed_at": &completedAt})
		return
	}

	updateJob("running", "校验并导入集群", "", nil)

	probe, err := s.probeCluster(result.Kubeconfig)
	if err != nil {
		completedAt := time.Now()
		updateJob("failed", "集群已创建，但接入失败", err.Error(), map[string]any{"completed_at": &completedAt})
		return
	}

	encrypted, err := s.kubeFactory.Encrypt(result.Kubeconfig)
	if err != nil {
		completedAt := time.Now()
		updateJob("failed", "集群已创建，但加密 kubeconfig 失败", "加密 kubeconfig 失败", map[string]any{"completed_at": &completedAt})
		return
	}

	cluster := model.Cluster{
		Name:                strings.TrimSpace(request.Name),
		Description:         strings.TrimSpace(request.Description),
		Region:              strings.TrimSpace(request.Region),
		Server:              probe.Server,
		CurrentContext:      probe.CurrentContext,
		Version:             probe.Version,
		CRIVersion:          probe.CRIVersion,
		Mode:                normalizeClusterMode(request.Mode),
		Status:              "connected",
		LastConnectedAt:     nowPtr(),
		KubeconfigEncrypted: encrypted,
	}

	if err := s.db.Create(&cluster).Error; err != nil {
		completedAt := time.Now()
		updateJob("failed", "集群已创建，但保存平台记录失败", "保存集群记录失败，名称可能已存在", map[string]any{"completed_at": &completedAt})
		return
	}

	if cluster.Mode == "maintenance" {
		if err := s.applyClusterMode(ctx, &cluster, cluster.Mode); err != nil {
			completedAt := time.Now()
			updateJob(
				"failed",
				"集群已创建，但维护模式设置失败",
				fmt.Sprintf("集群已入库，但执行维护模式失败：%s", err.Error()),
				map[string]any{"completed_at": &completedAt, "result_cluster_id": cluster.ID},
			)
			return
		}

		if err := s.db.Save(&cluster).Error; err != nil {
			completedAt := time.Now()
			updateJob("failed", "集群已创建，但状态保存失败", "保存集群运行状态失败", map[string]any{"completed_at": &completedAt, "result_cluster_id": cluster.ID})
			return
		}
	}

	completedAt := time.Now()
	updateJob("succeeded", "创建完成", "", map[string]any{"completed_at": &completedAt, "result_cluster_id": cluster.ID})
}

func toProvisionRequest(input clusterProvisionPayload) provision.ClusterRequest {
	nodes := make([]provision.NodeSpec, 0, len(input.Nodes))
	for _, node := range input.Nodes {
		nodes = append(nodes, provision.NodeSpec{
			Name:            strings.TrimSpace(node.Name),
			Address:         strings.TrimSpace(node.Address),
			InternalAddress: strings.TrimSpace(node.InternalAddress),
			Role:            strings.TrimSpace(node.Role),
		})
	}

	port := input.SSHPort
	if port == 0 {
		port = 22
	}

	return provision.ClusterRequest{
		Name:                strings.TrimSpace(input.Name),
		Region:              strings.TrimSpace(input.Region),
		Description:         strings.TrimSpace(input.Description),
		Mode:                normalizeClusterMode(input.Mode),
		ProvisionTemplate:   strings.TrimSpace(input.ProvisionTemplate),
		APIServerEndpoint:   strings.TrimSpace(input.APIServerEndpoint),
		KubernetesVersion:   strings.TrimSpace(input.KubernetesVersion),
		ImageRegistryPreset: strings.TrimSpace(input.ImageRegistryPreset),
		ImageRegistry:       strings.TrimSpace(input.ImageRegistry),
		NetworkPlugin:       strings.TrimSpace(input.NetworkPlugin),
		SSHUser:             strings.TrimSpace(input.SSHUser),
		SSHPort:             port,
		SSHPrivateKey:       strings.TrimSpace(input.SSHPrivateKey),
		Nodes:               nodes,
	}
}

func countProvisionNodes(nodes []provision.NodeSpec) (int, int) {
	controlPlaneCount := 0
	workerCount := 0

	for _, node := range nodes {
		switch normalizeProvisionNodeRole(node.Role) {
		case "control-plane":
			controlPlaneCount++
		case "worker":
			workerCount++
		case "control-plane-worker":
			controlPlaneCount++
			workerCount++
		}
	}

	return controlPlaneCount, workerCount
}

func normalizeProvisionNodeRole(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "control-plane", "control_plane", "controlplane":
		return "control-plane"
	case "worker", "node":
		return "worker"
	case "control-plane-worker", "control_plane_worker", "controlplaneworker":
		return "control-plane-worker"
	default:
		return ""
	}
}

func normalizeKubernetesVersionForJob(raw string) string {
	value := strings.TrimSpace(raw)
	return strings.TrimPrefix(value, "v")
}

func normalizeNetworkPluginForJob(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "calico", "cilium", "flannel":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return "calico"
	}
}

func probeProvisionedCluster(kubeconfig string) (kube.ProbeResult, error) {
	return kube.Probe(kubeconfig)
}
