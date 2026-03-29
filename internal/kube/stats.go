package kube

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	nodeGVR = schema.GroupVersionResource{Version: "v1", Resource: "nodes"}
	podGVR  = schema.GroupVersionResource{Version: "v1", Resource: "pods"}
)

type HealthSummary struct {
	Total    int `json:"total"`
	Normal   int `json:"normal"`
	Abnormal int `json:"abnormal"`
}

type ResourceUsage struct {
	Request    string  `json:"request"`
	Total      string  `json:"total"`
	Percentage float64 `json:"percentage"`
}

type ClusterOverview struct {
	Nodes  HealthSummary `json:"nodes"`
	Pods   HealthSummary `json:"pods"`
	CPU    ResourceUsage `json:"cpu"`
	Memory ResourceUsage `json:"memory"`
}

func CollectNodeHealth(ctx context.Context, runtime *Runtime) (HealthSummary, error) {
	nodes, err := runtime.Dynamic.Resource(nodeGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return HealthSummary{}, err
	}

	summary := HealthSummary{Total: len(nodes.Items)}
	for _, node := range nodes.Items {
		if nodeReady(node.Object) {
			summary.Normal += 1
		} else {
			summary.Abnormal += 1
		}
	}

	return summary, nil
}

func CollectClusterOverview(ctx context.Context, runtime *Runtime) (ClusterOverview, error) {
	nodes, err := runtime.Dynamic.Resource(nodeGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return ClusterOverview{}, err
	}

	pods, err := runtime.Dynamic.Resource(podGVR).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return ClusterOverview{}, err
	}

	var totalCPUMilli int64
	var totalMemoryBytes int64
	nodeSummary := HealthSummary{Total: len(nodes.Items)}
	for _, node := range nodes.Items {
		if nodeReady(node.Object) {
			nodeSummary.Normal += 1
		} else {
			nodeSummary.Abnormal += 1
		}

		totalCPUMilli += quantityMilliValue(readQuantity(node.Object, "status", "allocatable", "cpu"))
		totalMemoryBytes += quantityByteValue(readQuantity(node.Object, "status", "allocatable", "memory"))
	}

	var requestCPUMilli int64
	var requestMemoryBytes int64
	podSummary := HealthSummary{Total: len(pods.Items)}
	for _, pod := range pods.Items {
		if podHealthy(pod.Object) {
			podSummary.Normal += 1
		} else {
			podSummary.Abnormal += 1
		}

		if podTerminal(pod.Object) {
			continue
		}

		cpuMilli, memoryBytes := podRequestTotals(pod.Object)
		requestCPUMilli += cpuMilli
		requestMemoryBytes += memoryBytes
	}

	return ClusterOverview{
		Nodes: nodeSummary,
		Pods:  podSummary,
		CPU: ResourceUsage{
			Request:    resource.NewMilliQuantity(requestCPUMilli, resource.DecimalSI).String(),
			Total:      resource.NewMilliQuantity(totalCPUMilli, resource.DecimalSI).String(),
			Percentage: percentage(requestCPUMilli, totalCPUMilli),
		},
		Memory: ResourceUsage{
			Request:    resource.NewQuantity(requestMemoryBytes, resource.BinarySI).String(),
			Total:      resource.NewQuantity(totalMemoryBytes, resource.BinarySI).String(),
			Percentage: percentage(requestMemoryBytes, totalMemoryBytes),
		},
	}, nil
}

func nodeReady(object map[string]any) bool {
	conditions, found, err := unstructured.NestedSlice(object, "status", "conditions")
	if err != nil || !found {
		return false
	}

	for _, condition := range conditions {
		record, ok := condition.(map[string]any)
		if !ok {
			continue
		}

		if fmt.Sprint(record["type"]) == "Ready" {
			return fmt.Sprint(record["status"]) == "True"
		}
	}

	return false
}

func podHealthy(object map[string]any) bool {
	phase, _, err := unstructured.NestedString(object, "status", "phase")
	if err != nil {
		return false
	}

	switch phase {
	case "Succeeded":
		return true
	case "Running":
		return podContainersReady(object)
	default:
		return false
	}
}

func podContainersReady(object map[string]any) bool {
	statuses, found, err := unstructured.NestedSlice(object, "status", "containerStatuses")
	if err != nil || !found || len(statuses) == 0 {
		return false
	}

	for _, status := range statuses {
		record, ok := status.(map[string]any)
		if !ok {
			return false
		}

		ready, ok := record["ready"].(bool)
		if !ok || !ready {
			return false
		}
	}

	return true
}

func podTerminal(object map[string]any) bool {
	phase, _, err := unstructured.NestedString(object, "status", "phase")
	if err != nil {
		return false
	}

	return phase == "Succeeded" || phase == "Failed"
}

func podRequestTotals(object map[string]any) (cpuMilli int64, memoryBytes int64) {
	containerSlice, _, _ := unstructured.NestedSlice(object, "spec", "containers")
	initContainerSlice, _, _ := unstructured.NestedSlice(object, "spec", "initContainers")

	var appCPUMilli int64
	var appMemoryBytes int64
	for _, item := range containerSlice {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}

		appCPUMilli += quantityMilliValue(readQuantity(record, "resources", "requests", "cpu"))
		appMemoryBytes += quantityByteValue(readQuantity(record, "resources", "requests", "memory"))
	}

	var initCPUMilli int64
	var initMemoryBytes int64
	for _, item := range initContainerSlice {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}

		cpu := quantityMilliValue(readQuantity(record, "resources", "requests", "cpu"))
		memory := quantityByteValue(readQuantity(record, "resources", "requests", "memory"))
		if cpu > initCPUMilli {
			initCPUMilli = cpu
		}
		if memory > initMemoryBytes {
			initMemoryBytes = memory
		}
	}

	if initCPUMilli > appCPUMilli {
		cpuMilli = initCPUMilli
	} else {
		cpuMilli = appCPUMilli
	}

	if initMemoryBytes > appMemoryBytes {
		memoryBytes = initMemoryBytes
	} else {
		memoryBytes = appMemoryBytes
	}

	return cpuMilli, memoryBytes
}

func readQuantity(object map[string]any, fields ...string) resource.Quantity {
	value, found, err := unstructured.NestedFieldNoCopy(object, fields...)
	if err != nil || !found || value == nil {
		return resource.Quantity{}
	}

	parsed, err := resource.ParseQuantity(fmt.Sprint(value))
	if err != nil {
		return resource.Quantity{}
	}

	return parsed
}

func quantityMilliValue(value resource.Quantity) int64 {
	return value.MilliValue()
}

func quantityByteValue(value resource.Quantity) int64 {
	return value.Value()
}

func percentage(request, total int64) float64 {
	if total <= 0 {
		return 0
	}

	return float64(request) / float64(total) * 100
}

func DetectContainerRuntimeVersion(ctx context.Context, runtime *Runtime) (string, error) {
	nodes, err := runtime.Dynamic.Resource(nodeGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", err
	}

	seen := make(map[string]struct{})
	versions := make([]string, 0, len(nodes.Items))
	for _, node := range nodes.Items {
		version, _, nestedErr := unstructured.NestedString(
			node.Object,
			"status",
			"nodeInfo",
			"containerRuntimeVersion",
		)
		if nestedErr != nil {
			continue
		}

		version = strings.TrimSpace(version)
		if version == "" {
			continue
		}

		if _, ok := seen[version]; ok {
			continue
		}

		seen[version] = struct{}{}
		versions = append(versions, version)
	}

	if len(versions) == 0 {
		return "", nil
	}

	if len(versions) == 1 {
		return versions[0], nil
	}

	return fmt.Sprintf("%s +%d", versions[0], len(versions)-1), nil
}
