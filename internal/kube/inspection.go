package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	eventGVR        = schema.GroupVersionResource{Version: "v1", Resource: "events"}
	pvcGVR          = schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}
	storageClassGVR = schema.GroupVersionResource{Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses"}
)

const inspectionWarningWindow = 6 * time.Hour

type ClusterInspectionReport struct {
	InspectedAt time.Time                `json:"inspectedAt"`
	Version     string                   `json:"version"`
	Overview    ClusterOverview          `json:"overview"`
	Summary     ClusterInspectionSummary `json:"summary"`
	Items       []ClusterInspectionItem  `json:"items"`
}

type ClusterInspectionSummary struct {
	Status  string `json:"status"`
	Total   int    `json:"total"`
	Passed  int    `json:"passed"`
	Warning int    `json:"warning"`
	Failed  int    `json:"failed"`
}

type ClusterInspectionItem struct {
	Key      string                     `json:"key"`
	Label    string                     `json:"label"`
	Category string                     `json:"category"`
	Status   string                     `json:"status"`
	Summary  string                     `json:"summary"`
	Detail   string                     `json:"detail"`
	Findings []ClusterInspectionFinding `json:"findings"`
}

type ClusterInspectionFinding struct {
	Scope  string `json:"scope"`
	Detail string `json:"detail"`
}

func UnavailableInspectionReport(detail string) ClusterInspectionReport {
	items := []ClusterInspectionItem{
		{
			Key:      "api",
			Label:    "API 连通性",
			Category: "基础连接",
			Status:   "failed",
			Summary:  "无法建立巡检连接",
			Detail:   detail,
		},
	}

	return ClusterInspectionReport{
		InspectedAt: time.Now(),
		Summary:     summarizeInspection(items),
		Items:       items,
	}
}

func InspectCluster(ctx context.Context, runtime *Runtime, clusterMode string) ClusterInspectionReport {
	report := ClusterInspectionReport{InspectedAt: time.Now()}

	version, err := runtime.Discovery.ServerVersion()
	if err != nil {
		return UnavailableInspectionReport(err.Error())
	}
	report.Version = version.String()

	overview, err := CollectClusterOverview(ctx, runtime)
	if err != nil {
		return UnavailableInspectionReport(err.Error())
	}
	report.Overview = overview

	items := make([]ClusterInspectionItem, 0, 7)
	items = append(items, ClusterInspectionItem{
		Key:      "api",
		Label:    "API 连通性",
		Category: "基础连接",
		Status:   "passed",
		Summary:  "API Server 响应正常",
		Detail:   fmt.Sprintf("Kubernetes %s", report.Version),
	})

	nodes, nodeErr := runtime.Dynamic.Resource(nodeGVR).List(ctx, metav1.ListOptions{})
	pods, podErr := runtime.Dynamic.Resource(podGVR).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	pvcs, pvcErr := runtime.Dynamic.Resource(pvcGVR).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	storageClasses, storageClassErr := runtime.Dynamic.Resource(storageClassGVR).List(ctx, metav1.ListOptions{})
	events, eventErr := runtime.Dynamic.Resource(eventGVR).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})

	items = append(items, inspectNodes(clusterMode, nodes, nodeErr))
	items = append(items, inspectSystemPods(pods, podErr))
	items = append(items, inspectWorkloadPods(pods, podErr))
	items = append(items, inspectCapacity(overview))
	items = append(items, inspectStorage(pvcs, pvcErr, storageClasses, storageClassErr))
	items = append(items, inspectEvents(events, eventErr))

	report.Items = items
	report.Summary = summarizeInspection(items)
	return report
}

func inspectNodes(clusterMode string, nodes *unstructured.UnstructuredList, listErr error) ClusterInspectionItem {
	item := ClusterInspectionItem{
		Key:      "nodes",
		Label:    "节点状态",
		Category: "节点",
		Status:   "passed",
		Summary:  "所有节点 Ready",
	}

	if listErr != nil {
		item.Status = "failed"
		item.Summary = "节点信息获取失败"
		item.Detail = listErr.Error()
		return item
	}

	if len(nodes.Items) == 0 {
		item.Status = "failed"
		item.Summary = "未发现任何节点"
		item.Detail = "请确认目标集群已经完成节点注册"
		return item
	}

	readyCount := 0
	controlPlaneCount := 0
	workerCount := 0
	unschedulableWorkers := 0
	pressureCount := 0
	findings := make([]ClusterInspectionFinding, 0, 8)

	for _, node := range nodes.Items {
		if nodeReady(node.Object) {
			readyCount++
		} else {
			findings = append(findings, ClusterInspectionFinding{
				Scope:  node.GetName(),
				Detail: "Ready=False",
			})
		}

		if isWorkerNode(node.Object) {
			workerCount++
			if unschedulable, found, _ := unstructured.NestedBool(node.Object, "spec", "unschedulable"); found && unschedulable {
				unschedulableWorkers++
			}
		} else {
			controlPlaneCount++
		}

		for _, pressure := range activeNodePressures(node.Object) {
			pressureCount++
			findings = append(findings, ClusterInspectionFinding{
				Scope:  node.GetName(),
				Detail: pressure,
			})
		}
	}

	status := "passed"
	switch {
	case readyCount < len(nodes.Items):
		status = "failed"
	case pressureCount > 0:
		status = "warning"
	case strings.EqualFold(clusterMode, "maintenance"):
		status = "warning"
	case unschedulableWorkers > 0:
		status = "warning"
	}

	item.Status = status
	item.Detail = fmt.Sprintf(
		"控制面 %d，工作节点 %d，cordon worker %d，压力告警 %d",
		controlPlaneCount,
		workerCount,
		unschedulableWorkers,
		pressureCount,
	)

	switch status {
	case "failed":
		item.Summary = fmt.Sprintf("%d/%d 节点 Ready", readyCount, len(nodes.Items))
	case "warning":
		if strings.EqualFold(clusterMode, "maintenance") {
			item.Summary = fmt.Sprintf("集群处于维护模式，%d 个 worker 已限制调度", unschedulableWorkers)
		} else if pressureCount > 0 {
			item.Summary = fmt.Sprintf("%d 个节点出现压力告警", pressureCount)
		} else {
			item.Summary = fmt.Sprintf("%d 个 worker 处于 cordon", unschedulableWorkers)
		}
	default:
		item.Summary = fmt.Sprintf("%d/%d 节点 Ready", readyCount, len(nodes.Items))
	}

	item.Findings = limitFindings(findings, 6)
	return item
}

func inspectSystemPods(pods *unstructured.UnstructuredList, listErr error) ClusterInspectionItem {
	item := ClusterInspectionItem{
		Key:      "system-pods",
		Label:    "系统组件",
		Category: "控制面",
		Status:   "passed",
		Summary:  "kube-system 组件正常",
	}

	if listErr != nil {
		item.Status = "failed"
		item.Summary = "系统组件检查失败"
		item.Detail = listErr.Error()
		return item
	}

	abnormal := make([]ClusterInspectionFinding, 0, 6)
	total := 0

	for _, pod := range pods.Items {
		if pod.GetNamespace() != "kube-system" {
			continue
		}

		total++
		if podHealthy(pod.Object) || podTerminal(pod.Object) {
			continue
		}

		abnormal = append(abnormal, ClusterInspectionFinding{
			Scope:  fmt.Sprintf("%s/%s", pod.GetNamespace(), pod.GetName()),
			Detail: describePodIssue(pod.Object),
		})
	}

	switch count := len(abnormal); {
	case count == 0:
		item.Detail = fmt.Sprintf("已检查 %d 个 kube-system Pod", total)
	case count <= 2:
		item.Status = "warning"
		item.Summary = fmt.Sprintf("%d 个系统 Pod 异常", count)
		item.Detail = fmt.Sprintf("已检查 %d 个 kube-system Pod", total)
	default:
		item.Status = "failed"
		item.Summary = fmt.Sprintf("%d 个系统 Pod 异常", count)
		item.Detail = fmt.Sprintf("已检查 %d 个 kube-system Pod", total)
	}

	item.Findings = limitFindings(abnormal, 6)
	return item
}

func inspectWorkloadPods(pods *unstructured.UnstructuredList, listErr error) ClusterInspectionItem {
	item := ClusterInspectionItem{
		Key:      "workloads",
		Label:    "业务工作负载",
		Category: "应用",
		Status:   "passed",
		Summary:  "业务 Pod 运行正常",
	}

	if listErr != nil {
		item.Status = "failed"
		item.Summary = "业务 Pod 检查失败"
		item.Detail = listErr.Error()
		return item
	}

	abnormal := make([]ClusterInspectionFinding, 0, 10)
	runningPods := 0
	pendingPods := 0
	imagePullIssues := 0
	restartIssues := 0

	for _, pod := range pods.Items {
		if pod.GetNamespace() == "kube-system" {
			continue
		}

		if podTerminal(pod.Object) {
			continue
		}

		runningPods++
		if podHealthy(pod.Object) {
			continue
		}

		issue := describePodIssue(pod.Object)
		switch {
		case strings.Contains(issue, "ImagePull"):
			imagePullIssues++
		case strings.Contains(issue, "CrashLoopBackOff"), strings.Contains(issue, "RunContainerError"):
			restartIssues++
		case strings.Contains(issue, "Pending"):
			pendingPods++
		}

		abnormal = append(abnormal, ClusterInspectionFinding{
			Scope:  fmt.Sprintf("%s/%s", pod.GetNamespace(), pod.GetName()),
			Detail: issue,
		})
	}

	switch count := len(abnormal); {
	case count == 0:
		item.Detail = fmt.Sprintf("已检查 %d 个非终态业务 Pod", runningPods)
	case count <= 5:
		item.Status = "warning"
		item.Summary = fmt.Sprintf("%d 个业务 Pod 异常", count)
		item.Detail = fmt.Sprintf("Pending %d，镜像拉取问题 %d，重启异常 %d", pendingPods, imagePullIssues, restartIssues)
	default:
		item.Status = "failed"
		item.Summary = fmt.Sprintf("%d 个业务 Pod 异常", count)
		item.Detail = fmt.Sprintf("Pending %d，镜像拉取问题 %d，重启异常 %d", pendingPods, imagePullIssues, restartIssues)
	}

	item.Findings = limitFindings(abnormal, 8)
	return item
}

func inspectCapacity(overview ClusterOverview) ClusterInspectionItem {
	item := ClusterInspectionItem{
		Key:      "capacity",
		Label:    "资源容量",
		Category: "容量",
		Status:   "passed",
	}

	maxUsage := overview.CPU.Percentage
	if overview.Memory.Percentage > maxUsage {
		maxUsage = overview.Memory.Percentage
	}

	switch {
	case maxUsage >= 90:
		item.Status = "failed"
	case maxUsage >= 75:
		item.Status = "warning"
	}

	item.Summary = fmt.Sprintf(
		"CPU %s，内存 %s",
		formatInspectionPercent(overview.CPU.Percentage),
		formatInspectionPercent(overview.Memory.Percentage),
	)
	item.Detail = fmt.Sprintf(
		"CPU %s / %s，内存 %s / %s",
		overview.CPU.Request,
		overview.CPU.Total,
		overview.Memory.Request,
		overview.Memory.Total,
	)

	return item
}

func inspectStorage(
	pvcs *unstructured.UnstructuredList,
	pvcErr error,
	storageClasses *unstructured.UnstructuredList,
	storageClassErr error,
) ClusterInspectionItem {
	item := ClusterInspectionItem{
		Key:      "storage",
		Label:    "存储供应",
		Category: "存储",
		Status:   "passed",
		Summary:  "存储类与 PVC 正常",
	}

	if pvcErr != nil {
		item.Status = "failed"
		item.Summary = "PVC 检查失败"
		item.Detail = pvcErr.Error()
		return item
	}
	if storageClassErr != nil {
		item.Status = "failed"
		item.Summary = "StorageClass 检查失败"
		item.Detail = storageClassErr.Error()
		return item
	}

	defaultStorageClasses := 0
	for _, storageClass := range storageClasses.Items {
		if isDefaultStorageClass(storageClass.Object) {
			defaultStorageClasses++
		}
	}

	pendingPVCs := make([]ClusterInspectionFinding, 0, 6)
	for _, pvc := range pvcs.Items {
		phase, _, _ := unstructured.NestedString(pvc.Object, "status", "phase")
		if phase == "Bound" {
			continue
		}

		pendingPVCs = append(pendingPVCs, ClusterInspectionFinding{
			Scope:  fmt.Sprintf("%s/%s", pvc.GetNamespace(), pvc.GetName()),
			Detail: fmt.Sprintf("状态 %s", valueOrDefault(phase, "Unknown")),
		})
	}

	switch {
	case len(pendingPVCs) > 3:
		item.Status = "failed"
	case len(pendingPVCs) > 0 || defaultStorageClasses == 0:
		item.Status = "warning"
	}

	switch {
	case len(pendingPVCs) > 0:
		item.Summary = fmt.Sprintf("%d 个 PVC 未就绪", len(pendingPVCs))
	case defaultStorageClasses == 0:
		item.Summary = "缺少默认 StorageClass"
	}

	item.Detail = fmt.Sprintf("默认 StorageClass %d 个，未就绪 PVC %d 个", defaultStorageClasses, len(pendingPVCs))
	item.Findings = limitFindings(pendingPVCs, 6)
	if defaultStorageClasses == 0 {
		item.Findings = append([]ClusterInspectionFinding{{
			Scope:  "cluster",
			Detail: "未发现默认 StorageClass",
		}}, item.Findings...)
		item.Findings = limitFindings(item.Findings, 6)
	}

	return item
}

func inspectEvents(events *unstructured.UnstructuredList, listErr error) ClusterInspectionItem {
	item := ClusterInspectionItem{
		Key:      "events",
		Label:    "告警事件",
		Category: "告警",
		Status:   "passed",
		Summary:  "近 6 小时无 Warning 事件",
	}

	if listErr != nil {
		item.Status = "failed"
		item.Summary = "事件检查失败"
		item.Detail = listErr.Error()
		return item
	}

	cutoff := time.Now().Add(-inspectionWarningWindow)
	type warningEvent struct {
		scope  string
		detail string
		time   time.Time
	}
	warnings := make([]warningEvent, 0, 12)

	for _, event := range events.Items {
		eventType, _, _ := unstructured.NestedString(event.Object, "type")
		if !strings.EqualFold(eventType, "Warning") {
			continue
		}

		occurredAt := readEventTime(event.Object)
		if occurredAt.IsZero() || occurredAt.Before(cutoff) {
			continue
		}

		reason, _, _ := unstructured.NestedString(event.Object, "reason")
		message, _, _ := unstructured.NestedString(event.Object, "message")
		involvedKind, _, _ := unstructured.NestedString(event.Object, "involvedObject", "kind")
		involvedName, _, _ := unstructured.NestedString(event.Object, "involvedObject", "name")

		warnings = append(warnings, warningEvent{
			scope:  fmt.Sprintf("%s/%s %s", event.GetNamespace(), valueOrDefault(involvedKind, "Object"), valueOrDefault(involvedName, event.GetName())),
			detail: strings.TrimSpace(fmt.Sprintf("%s %s", reason, message)),
			time:   occurredAt,
		})
	}

	sort.Slice(warnings, func(i, j int) bool {
		return warnings[i].time.After(warnings[j].time)
	})

	switch count := len(warnings); {
	case count == 0:
		item.Detail = "重点关注调度失败、拉镜像失败和探针告警"
	case count <= 10:
		item.Status = "warning"
		item.Summary = fmt.Sprintf("近 6 小时 %d 条 Warning 事件", count)
		item.Detail = "建议优先处理最近的 Warning"
	default:
		item.Status = "failed"
		item.Summary = fmt.Sprintf("近 6 小时 %d 条 Warning 事件", count)
		item.Detail = "Warning 数量偏高，建议立即排查"
	}

	findings := make([]ClusterInspectionFinding, 0, 6)
	for _, warning := range warnings {
		findings = append(findings, ClusterInspectionFinding{
			Scope:  warning.scope,
			Detail: warning.detail,
		})
		if len(findings) >= 6 {
			break
		}
	}
	item.Findings = findings

	return item
}

func summarizeInspection(items []ClusterInspectionItem) ClusterInspectionSummary {
	summary := ClusterInspectionSummary{Total: len(items), Status: "passed"}

	for _, item := range items {
		switch item.Status {
		case "failed":
			summary.Failed++
		case "warning":
			summary.Warning++
		default:
			summary.Passed++
		}
	}

	switch {
	case summary.Failed > 0:
		summary.Status = "failed"
	case summary.Warning > 0:
		summary.Status = "warning"
	default:
		summary.Status = "passed"
	}

	return summary
}

func activeNodePressures(object map[string]any) []string {
	conditions, found, err := unstructured.NestedSlice(object, "status", "conditions")
	if err != nil || !found {
		return nil
	}

	pressures := make([]string, 0, 4)
	for _, condition := range conditions {
		record, ok := condition.(map[string]any)
		if !ok {
			continue
		}

		conditionType := fmt.Sprint(record["type"])
		switch conditionType {
		case "MemoryPressure", "DiskPressure", "PIDPressure", "NetworkUnavailable":
			if fmt.Sprint(record["status"]) == "True" {
				pressures = append(pressures, conditionType)
			}
		}
	}

	sort.Strings(pressures)
	return pressures
}

func describePodIssue(object map[string]any) string {
	phase, _, _ := unstructured.NestedString(object, "status", "phase")
	if phase == "Pending" {
		return "Pending"
	}

	statuses, found, _ := unstructured.NestedSlice(object, "status", "containerStatuses")
	if found {
		for _, status := range statuses {
			record, ok := status.(map[string]any)
			if !ok {
				continue
			}

			state, _ := record["state"].(map[string]any)
			if waiting, ok := state["waiting"].(map[string]any); ok {
				reason := fmt.Sprint(waiting["reason"])
				message := strings.TrimSpace(fmt.Sprint(waiting["message"]))
				if message != "" && message != "<nil>" {
					return strings.TrimSpace(reason + " " + message)
				}
				if reason != "" && reason != "<nil>" {
					return reason
				}
			}

			if terminated, ok := state["terminated"].(map[string]any); ok {
				reason := fmt.Sprint(terminated["reason"])
				message := strings.TrimSpace(fmt.Sprint(terminated["message"]))
				if message != "" && message != "<nil>" {
					return strings.TrimSpace(reason + " " + message)
				}
				if reason != "" && reason != "<nil>" {
					return reason
				}
			}
		}
	}

	if phase == "Running" && !podContainersReady(object) {
		return "ContainersNotReady"
	}

	if phase != "" {
		return phase
	}

	return "Unknown"
}

func isDefaultStorageClass(object map[string]any) bool {
	annotations, found, err := unstructured.NestedStringMap(object, "metadata", "annotations")
	if err != nil || !found {
		return false
	}

	return strings.EqualFold(annotations["storageclass.kubernetes.io/is-default-class"], "true") ||
		strings.EqualFold(annotations["storageclass.beta.kubernetes.io/is-default-class"], "true")
}

func readEventTime(object map[string]any) time.Time {
	candidates := [][]string{
		{"eventTime"},
		{"series", "lastObservedTime"},
		{"lastTimestamp"},
		{"firstTimestamp"},
		{"metadata", "creationTimestamp"},
	}

	for _, fields := range candidates {
		value, found, err := unstructured.NestedFieldNoCopy(object, fields...)
		if err != nil || !found || value == nil {
			continue
		}

		parsed, parseErr := time.Parse(time.RFC3339, fmt.Sprint(value))
		if parseErr == nil {
			return parsed
		}
	}

	return time.Time{}
}

func formatInspectionPercent(value float64) string {
	if value <= 0 {
		return "0%"
	}

	return fmt.Sprintf("%.1f%%", value)
}

func valueOrDefault(value, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "<nil>" {
		return fallback
	}

	return trimmed
}

func limitFindings(findings []ClusterInspectionFinding, limit int) []ClusterInspectionFinding {
	if len(findings) <= limit {
		return findings
	}

	return findings[:limit]
}
