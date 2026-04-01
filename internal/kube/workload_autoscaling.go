package kube

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	hpaGVR          = schema.GroupVersionResource{Group: "autoscaling", Version: "v2", Resource: "horizontalpodautoscalers"}
	scaledObjectGVR = schema.GroupVersionResource{Group: "keda.sh", Version: "v1alpha1", Resource: "scaledobjects"}
)

const (
	knativeAutoscalingClassAnnotation                 = "autoscaling.knative.dev/class"
	knativeAutoscalingMetricAnnotation                = "autoscaling.knative.dev/metric"
	knativeAutoscalingTargetAnnotation                = "autoscaling.knative.dev/target"
	knativeAutoscalingTargetUtilizationAnnotation     = "autoscaling.knative.dev/target-utilization-percentage"
	knativeAutoscalingMinScaleAnnotation              = "autoscaling.knative.dev/min-scale"
	knativeAutoscalingMaxScaleAnnotation              = "autoscaling.knative.dev/max-scale"
	knativeAutoscalingWindowAnnotation                = "autoscaling.knative.dev/window"
	knativeAutoscalingScaleDownDelayAnnotation        = "autoscaling.knative.dev/scale-down-delay"
	knativeAutoscalingScaleToZeroRetentionAnnotation  = "autoscaling.knative.dev/scale-to-zero-pod-retention-period"
	defaultKnativeAutoscalingClass                    = "kpa.autoscaling.knative.dev"
	defaultKnativeAutoscalingMetric                   = "concurrency"
)

type WorkloadAutoscaling struct {
	Supported    bool                      `json:"supported"`
	ResourceType string                    `json:"resourceType"`
	Kind         string                    `json:"kind"`
	Message      string                    `json:"message,omitempty"`
	Metrics      MetricsAutoscalingStatus  `json:"metrics"`
	Event        EventAutoscalingStatus    `json:"event"`
	API          APIAutoscalingStatus      `json:"api"`
}

type MetricsAutoscalingStatus struct {
	Supported       bool                        `json:"supported"`
	Configured      bool                        `json:"configured"`
	Name            string                      `json:"name,omitempty"`
	MinReplicas     int32                       `json:"minReplicas"`
	MaxReplicas     int32                       `json:"maxReplicas"`
	CurrentReplicas int32                       `json:"currentReplicas"`
	DesiredReplicas int32                       `json:"desiredReplicas"`
	Metrics         []AutoscalingMetricSnapshot `json:"metrics"`
	LastScaleTime   string                      `json:"lastScaleTime,omitempty"`
}

type EventAutoscalingStatus struct {
	Supported        bool                         `json:"supported"`
	Available        bool                         `json:"available"`
	Configured       bool                         `json:"configured"`
	Name             string                       `json:"name,omitempty"`
	MinReplicaCount  int32                        `json:"minReplicaCount"`
	MaxReplicaCount  int32                        `json:"maxReplicaCount"`
	PollingInterval  int32                        `json:"pollingInterval"`
	CooldownPeriod   int32                        `json:"cooldownPeriod"`
	Triggers         []AutoscalingTriggerSnapshot `json:"triggers"`
	LastActiveTime   string                       `json:"lastActiveTime,omitempty"`
	OriginalReplicas int32                        `json:"originalReplicas"`
}

type APIAutoscalingStatus struct {
	Supported                   bool   `json:"supported"`
	Available                   bool   `json:"available"`
	Configured                  bool   `json:"configured"`
	Name                        string `json:"name,omitempty"`
	Class                       string `json:"class,omitempty"`
	Metric                      string `json:"metric,omitempty"`
	Target                      string `json:"target,omitempty"`
	TargetUtilizationPercentage int32  `json:"targetUtilizationPercentage"`
	MinScale                    int32  `json:"minScale"`
	MaxScale                    int32  `json:"maxScale"`
	ScaleDownDelay              string `json:"scaleDownDelay,omitempty"`
	Window                      string `json:"window,omitempty"`
	ScaleToZeroRetention        string `json:"scaleToZeroRetention,omitempty"`
	URL                         string `json:"url,omitempty"`
	LatestReadyRevision         string `json:"latestReadyRevision,omitempty"`
}

type AutoscalingMetricSnapshot struct {
	Label   string `json:"label"`
	Current string `json:"current,omitempty"`
	Target  string `json:"target,omitempty"`
}

type AutoscalingTriggerSnapshot struct {
	Type     string            `json:"type"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type HPAUpsertPayload struct {
	Name               string `json:"name"`
	MinReplicas        int32  `json:"minReplicas"`
	MaxReplicas        int32  `json:"maxReplicas"`
	CPUUtilization     *int32 `json:"cpuUtilization,omitempty"`
	MemoryUtilization  *int32 `json:"memoryUtilization,omitempty"`
}

type KEDAUpsertPayload struct {
	Name             string                   `json:"name"`
	MinReplicaCount  int32                    `json:"minReplicaCount"`
	MaxReplicaCount  int32                    `json:"maxReplicaCount"`
	PollingInterval  int32                    `json:"pollingInterval"`
	CooldownPeriod   int32                    `json:"cooldownPeriod"`
	Triggers         []AutoscalingTriggerSpec `json:"triggers"`
}

type AutoscalingTriggerSpec struct {
	Type     string            `json:"type"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type KnativeUpsertPayload struct {
	Class                       string `json:"class"`
	Metric                      string `json:"metric"`
	Target                      string `json:"target"`
	TargetUtilizationPercentage *int32 `json:"targetUtilizationPercentage,omitempty"`
	MinScale                    *int32 `json:"minScale,omitempty"`
	MaxScale                    *int32 `json:"maxScale,omitempty"`
	ScaleDownDelay              string `json:"scaleDownDelay,omitempty"`
	Window                      string `json:"window,omitempty"`
	ScaleToZeroRetention        string `json:"scaleToZeroRetention,omitempty"`
}

func SupportsWorkloadAutoscaling(resourceType string) bool {
	switch resourceType {
	case "deployment", "statefulset", "knativeservice":
		return true
	default:
		return false
	}
}

func SupportsMetricsAutoscaling(resourceType string) bool {
	return resourceType == "deployment" || resourceType == "statefulset"
}

func SupportsEventAutoscaling(resourceType string) bool {
	return resourceType == "deployment" || resourceType == "statefulset"
}

func SupportsAPIAutoscaling(resourceType string) bool {
	return resourceType == "knativeservice"
}

func ListWorkloadAutoscaling(
	ctx context.Context,
	runtime *Runtime,
	resourceType, namespace, resourceName string,
) (WorkloadAutoscaling, error) {
	if runtime == nil || runtime.Dynamic == nil {
		return WorkloadAutoscaling{}, fmt.Errorf("kubernetes client is not ready")
	}
	if strings.TrimSpace(namespace) == "" {
		return WorkloadAutoscaling{}, fmt.Errorf("namespace is required")
	}

	definition, ok := LookupResource(resourceType)
	if !ok {
		return WorkloadAutoscaling{}, fmt.Errorf("unsupported resource type %q", resourceType)
	}

	snapshot := WorkloadAutoscaling{
		Supported:    SupportsWorkloadAutoscaling(resourceType),
		ResourceType: resourceType,
		Kind:         definition.Kind,
		Metrics: MetricsAutoscalingStatus{
			Supported: SupportsMetricsAutoscaling(resourceType),
			Metrics:   []AutoscalingMetricSnapshot{},
		},
		Event: EventAutoscalingStatus{
			Supported: SupportsEventAutoscaling(resourceType),
			Triggers:  []AutoscalingTriggerSnapshot{},
		},
		API: APIAutoscalingStatus{
			Supported: SupportsAPIAutoscaling(resourceType),
			Name:      resourceName,
		},
	}
	if !snapshot.Supported {
		snapshot.Message = "当前工作负载类型暂不支持指标 / 事件 / API 弹性伸缩"
		return snapshot, nil
	}
	if err := ensureAutoscalingWorkloadExists(ctx, runtime, definition, namespace, resourceName); err != nil {
		return WorkloadAutoscaling{}, err
	}

	if snapshot.Metrics.Supported {
		hpa, err := getWorkloadHPA(ctx, runtime, definition.Kind, namespace, resourceName)
		if err != nil {
			return WorkloadAutoscaling{}, err
		}
		snapshot.Metrics = hpa
	}

	if snapshot.Event.Supported {
		event, err := getWorkloadScaledObject(ctx, runtime, definition.Kind, namespace, resourceName)
		if err != nil {
			return WorkloadAutoscaling{}, err
		}
		snapshot.Event = event
	}

	if snapshot.API.Supported {
		api, err := getWorkloadKnativeAutoscaling(ctx, runtime, definition, namespace, resourceName)
		if err != nil {
			return WorkloadAutoscaling{}, err
		}
		snapshot.API = api
	}

	return snapshot, nil
}

func UpsertWorkloadHPA(
	ctx context.Context,
	runtime *Runtime,
	resourceType, namespace, resourceName string,
	payload HPAUpsertPayload,
) error {
	if runtime == nil || runtime.Dynamic == nil {
		return fmt.Errorf("kubernetes client is not ready")
	}
	if strings.TrimSpace(namespace) == "" {
		return fmt.Errorf("namespace is required")
	}
	if !SupportsMetricsAutoscaling(resourceType) {
		return fmt.Errorf("当前工作负载类型暂不支持指标弹性伸缩")
	}
	if payload.MinReplicas <= 0 {
		return fmt.Errorf("minReplicas must be greater than 0")
	}
	if payload.MaxReplicas < payload.MinReplicas {
		return fmt.Errorf("maxReplicas must be greater than or equal to minReplicas")
	}
	if payload.CPUUtilization == nil && payload.MemoryUtilization == nil {
		return fmt.Errorf("至少配置一个指标阈值")
	}

	definition, ok := LookupResource(resourceType)
	if !ok {
		return fmt.Errorf("unsupported resource type %q", resourceType)
	}
	if err := ensureAutoscalingWorkloadExists(ctx, runtime, definition, namespace, resourceName); err != nil {
		return err
	}

	existingItem, err := findWorkloadHPA(ctx, runtime, definition.Kind, namespace, resourceName)
	if err != nil {
		return err
	}

	targetName := strings.TrimSpace(payload.Name)
	if existingItem != nil {
		targetName = existingItem.GetName()
	}
	if targetName == "" {
		targetName = defaultHPAName(resourceName)
	}

	metrics := make([]any, 0, 2)
	if payload.CPUUtilization != nil {
		metrics = append(metrics, map[string]any{
			"type": "Resource",
			"resource": map[string]any{
				"name": "cpu",
				"target": map[string]any{
					"type":               "Utilization",
					"averageUtilization": *payload.CPUUtilization,
				},
			},
		})
	}
	if payload.MemoryUtilization != nil {
		metrics = append(metrics, map[string]any{
			"type": "Resource",
			"resource": map[string]any{
				"name": "memory",
				"target": map[string]any{
					"type":               "Utilization",
					"averageUtilization": *payload.MemoryUtilization,
				},
			},
		})
	}

	object := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "autoscaling/v2",
		"kind":       "HorizontalPodAutoscaler",
		"metadata": map[string]any{
			"name":      targetName,
			"namespace": namespace,
		},
		"spec": map[string]any{
			"scaleTargetRef": map[string]any{
				"apiVersion": definition.APIVersion,
				"kind":       definition.Kind,
				"name":       resourceName,
			},
			"minReplicas": payload.MinReplicas,
			"maxReplicas": payload.MaxReplicas,
			"metrics":     metrics,
		},
	}}

	client := runtime.Dynamic.Resource(hpaGVR).Namespace(namespace)
	if existingItem != nil {
		object.SetResourceVersion(existingItem.GetResourceVersion())
		_, err = client.Update(ctx, object, metav1.UpdateOptions{})
		return err
	}

	_, err = client.Create(ctx, object, metav1.CreateOptions{})
	return err
}

func DeleteWorkloadHPA(
	ctx context.Context,
	runtime *Runtime,
	resourceType, namespace, resourceName string,
) error {
	if runtime == nil || runtime.Dynamic == nil {
		return fmt.Errorf("kubernetes client is not ready")
	}
	if !SupportsMetricsAutoscaling(resourceType) {
		return fmt.Errorf("当前工作负载类型暂不支持指标弹性伸缩")
	}
	definition, ok := LookupResource(resourceType)
	if !ok {
		return fmt.Errorf("unsupported resource type %q", resourceType)
	}
	if err := ensureAutoscalingWorkloadExists(ctx, runtime, definition, namespace, resourceName); err != nil {
		return err
	}
	item, err := findWorkloadHPA(ctx, runtime, definition.Kind, namespace, resourceName)
	if err != nil {
		return err
	}
	if item == nil {
		return nil
	}
	return runtime.Dynamic.Resource(hpaGVR).Namespace(namespace).Delete(ctx, item.GetName(), metav1.DeleteOptions{})
}

func UpsertWorkloadScaledObject(
	ctx context.Context,
	runtime *Runtime,
	resourceType, namespace, resourceName string,
	payload KEDAUpsertPayload,
) error {
	if runtime == nil || runtime.Dynamic == nil || runtime.Discovery == nil {
		return fmt.Errorf("kubernetes client is not ready")
	}
	if strings.TrimSpace(namespace) == "" {
		return fmt.Errorf("namespace is required")
	}
	if !SupportsEventAutoscaling(resourceType) {
		return fmt.Errorf("当前工作负载类型暂不支持事件弹性伸缩")
	}
	if payload.MaxReplicaCount <= 0 {
		return fmt.Errorf("maxReplicaCount must be greater than 0")
	}
	if payload.MinReplicaCount < 0 || payload.MinReplicaCount > payload.MaxReplicaCount {
		return fmt.Errorf("minReplicaCount is invalid")
	}
	if len(payload.Triggers) == 0 {
		return fmt.Errorf("至少配置一个事件触发器")
	}
	available, err := scaledObjectAvailable(runtime)
	if err != nil {
		return err
	}
	if !available {
		return fmt.Errorf("当前集群未安装 KEDA")
	}

	definition, ok := LookupResource(resourceType)
	if !ok {
		return fmt.Errorf("unsupported resource type %q", resourceType)
	}
	if err := ensureAutoscalingWorkloadExists(ctx, runtime, definition, namespace, resourceName); err != nil {
		return err
	}
	existingItem, err := findWorkloadScaledObject(ctx, runtime, definition.Kind, namespace, resourceName)
	if err != nil {
		return err
	}

	targetName := strings.TrimSpace(payload.Name)
	if existingItem != nil {
		targetName = existingItem.GetName()
	}
	if targetName == "" {
		targetName = defaultScaledObjectName(resourceName)
	}

	triggers := make([]any, 0, len(payload.Triggers))
	for _, trigger := range payload.Triggers {
		triggerType := strings.TrimSpace(trigger.Type)
		if triggerType == "" {
			return fmt.Errorf("trigger type is required")
		}
		metadata := make(map[string]any, len(trigger.Metadata))
		for key, value := range trigger.Metadata {
			metadata[key] = value
		}
		triggers = append(triggers, map[string]any{
			"type":     triggerType,
			"metadata": metadata,
		})
	}

	object := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "keda.sh/v1alpha1",
		"kind":       "ScaledObject",
		"metadata": map[string]any{
			"name":      targetName,
			"namespace": namespace,
		},
		"spec": map[string]any{
			"scaleTargetRef": map[string]any{
				"name": resourceName,
				"kind": definition.Kind,
			},
			"minReplicaCount": payload.MinReplicaCount,
			"maxReplicaCount": payload.MaxReplicaCount,
			"pollingInterval": payload.PollingInterval,
			"cooldownPeriod":  payload.CooldownPeriod,
			"triggers":        triggers,
		},
	}}

	client := runtime.Dynamic.Resource(scaledObjectGVR).Namespace(namespace)
	if existingItem != nil {
		object.SetResourceVersion(existingItem.GetResourceVersion())
		_, err = client.Update(ctx, object, metav1.UpdateOptions{})
		return err
	}

	_, err = client.Create(ctx, object, metav1.CreateOptions{})
	return err
}

func DeleteWorkloadScaledObject(
	ctx context.Context,
	runtime *Runtime,
	resourceType, namespace, resourceName string,
) error {
	if runtime == nil || runtime.Dynamic == nil || runtime.Discovery == nil {
		return fmt.Errorf("kubernetes client is not ready")
	}
	if !SupportsEventAutoscaling(resourceType) {
		return fmt.Errorf("当前工作负载类型暂不支持事件弹性伸缩")
	}
	definition, ok := LookupResource(resourceType)
	if !ok {
		return fmt.Errorf("unsupported resource type %q", resourceType)
	}
	if err := ensureAutoscalingWorkloadExists(ctx, runtime, definition, namespace, resourceName); err != nil {
		return err
	}
	item, err := findWorkloadScaledObject(ctx, runtime, definition.Kind, namespace, resourceName)
	if err != nil {
		return err
	}
	if item == nil {
		return nil
	}
	return runtime.Dynamic.Resource(scaledObjectGVR).Namespace(namespace).Delete(ctx, item.GetName(), metav1.DeleteOptions{})
}

func UpsertWorkloadKnativeAutoscaling(
	ctx context.Context,
	runtime *Runtime,
	resourceType, namespace, resourceName string,
	payload KnativeUpsertPayload,
) error {
	if runtime == nil || runtime.Dynamic == nil || runtime.Discovery == nil {
		return fmt.Errorf("kubernetes client is not ready")
	}
	if strings.TrimSpace(namespace) == "" {
		return fmt.Errorf("namespace is required")
	}
	if !SupportsAPIAutoscaling(resourceType) {
		return fmt.Errorf("当前工作负载类型暂不支持 API 弹性伸缩")
	}
	available, err := knativeServingAvailable(runtime)
	if err != nil {
		return err
	}
	if !available {
		return fmt.Errorf("当前集群未安装 Knative Serving")
	}

	class := strings.TrimSpace(payload.Class)
	if class == "" {
		class = defaultKnativeAutoscalingClass
	}
	if class != "kpa.autoscaling.knative.dev" && class != "hpa.autoscaling.knative.dev" {
		return fmt.Errorf("当前仅支持 kpa.autoscaling.knative.dev 或 hpa.autoscaling.knative.dev")
	}

	metric := strings.TrimSpace(payload.Metric)
	if metric == "" {
		metric = defaultKnativeAutoscalingMetric
	}
	if metric != "concurrency" && metric != "rps" {
		return fmt.Errorf("Knative metric 仅支持 concurrency 或 rps")
	}

	target := strings.TrimSpace(payload.Target)
	if target == "" {
		return fmt.Errorf("target is required")
	}
	if payload.MinScale != nil && *payload.MinScale < 0 {
		return fmt.Errorf("minScale must be greater than or equal to 0")
	}
	if payload.MaxScale != nil && *payload.MaxScale <= 0 {
		return fmt.Errorf("maxScale must be greater than 0")
	}
	if payload.MinScale != nil && payload.MaxScale != nil && *payload.MaxScale < *payload.MinScale {
		return fmt.Errorf("maxScale must be greater than or equal to minScale")
	}

	definition, ok := LookupResource(resourceType)
	if !ok {
		return fmt.Errorf("unsupported resource type %q", resourceType)
	}
	if err := ensureAutoscalingWorkloadExists(ctx, runtime, definition, namespace, resourceName); err != nil {
		return err
	}

	item, err := runtime.Dynamic.Resource(definition.GVR).Namespace(namespace).Get(ctx, resourceName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	annotations, _, _ := unstructured.NestedStringMap(item.Object, "spec", "template", "metadata", "annotations")
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[knativeAutoscalingClassAnnotation] = class
	annotations[knativeAutoscalingMetricAnnotation] = metric
	annotations[knativeAutoscalingTargetAnnotation] = target

	if payload.TargetUtilizationPercentage != nil && *payload.TargetUtilizationPercentage > 0 {
		annotations[knativeAutoscalingTargetUtilizationAnnotation] = fmt.Sprintf("%d", *payload.TargetUtilizationPercentage)
	} else {
		delete(annotations, knativeAutoscalingTargetUtilizationAnnotation)
	}
	if payload.MinScale != nil {
		annotations[knativeAutoscalingMinScaleAnnotation] = fmt.Sprintf("%d", *payload.MinScale)
	} else {
		delete(annotations, knativeAutoscalingMinScaleAnnotation)
	}
	if payload.MaxScale != nil {
		annotations[knativeAutoscalingMaxScaleAnnotation] = fmt.Sprintf("%d", *payload.MaxScale)
	} else {
		delete(annotations, knativeAutoscalingMaxScaleAnnotation)
	}
	updateOptionalKnativeAnnotation(annotations, knativeAutoscalingScaleDownDelayAnnotation, payload.ScaleDownDelay)
	updateOptionalKnativeAnnotation(annotations, knativeAutoscalingWindowAnnotation, payload.Window)
	updateOptionalKnativeAnnotation(annotations, knativeAutoscalingScaleToZeroRetentionAnnotation, payload.ScaleToZeroRetention)

	if err := unstructured.SetNestedStringMap(item.Object, annotations, "spec", "template", "metadata", "annotations"); err != nil {
		return err
	}

	_, err = runtime.Dynamic.Resource(definition.GVR).Namespace(namespace).Update(ctx, item, metav1.UpdateOptions{})
	return err
}

func DeleteWorkloadKnativeAutoscaling(
	ctx context.Context,
	runtime *Runtime,
	resourceType, namespace, resourceName string,
) error {
	if runtime == nil || runtime.Dynamic == nil || runtime.Discovery == nil {
		return fmt.Errorf("kubernetes client is not ready")
	}
	if !SupportsAPIAutoscaling(resourceType) {
		return fmt.Errorf("当前工作负载类型暂不支持 API 弹性伸缩")
	}
	available, err := knativeServingAvailable(runtime)
	if err != nil {
		return err
	}
	if !available {
		return fmt.Errorf("当前集群未安装 Knative Serving")
	}

	definition, ok := LookupResource(resourceType)
	if !ok {
		return fmt.Errorf("unsupported resource type %q", resourceType)
	}
	if err := ensureAutoscalingWorkloadExists(ctx, runtime, definition, namespace, resourceName); err != nil {
		return err
	}

	item, err := runtime.Dynamic.Resource(definition.GVR).Namespace(namespace).Get(ctx, resourceName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	annotations, _, _ := unstructured.NestedStringMap(item.Object, "spec", "template", "metadata", "annotations")
	if len(annotations) == 0 {
		return nil
	}

	for _, key := range knativeAutoscalingAnnotationKeys() {
		delete(annotations, key)
	}

	if err := unstructured.SetNestedStringMap(item.Object, annotations, "spec", "template", "metadata", "annotations"); err != nil {
		return err
	}

	_, err = runtime.Dynamic.Resource(definition.GVR).Namespace(namespace).Update(ctx, item, metav1.UpdateOptions{})
	return err
}

func getWorkloadHPA(
	ctx context.Context,
	runtime *Runtime,
	workloadKind, namespace, resourceName string,
) (MetricsAutoscalingStatus, error) {
	status := MetricsAutoscalingStatus{Supported: true}
	item, err := findWorkloadHPA(ctx, runtime, workloadKind, namespace, resourceName)
	if err != nil {
		return status, err
	}
	if item == nil {
		status.Name = defaultHPAName(resourceName)
		return status, nil
	}

	status.Configured = true
	status.Name = item.GetName()
	status.MinReplicas = int32Value(item.Object, "spec", "minReplicas")
	if status.MinReplicas == 0 {
		status.MinReplicas = 1
	}
	status.MaxReplicas = int32Value(item.Object, "spec", "maxReplicas")
	status.CurrentReplicas = int32Value(item.Object, "status", "currentReplicas")
	status.DesiredReplicas = int32Value(item.Object, "status", "desiredReplicas")
	status.LastScaleTime = stringValue(item.Object, "status", "lastScaleTime")
	status.Metrics = parseHPAMetrics(item.Object)
	return status, nil
}

func getWorkloadScaledObject(
	ctx context.Context,
	runtime *Runtime,
	workloadKind, namespace, resourceName string,
) (EventAutoscalingStatus, error) {
	status := EventAutoscalingStatus{Supported: true}
	available, err := scaledObjectAvailable(runtime)
	if err != nil {
		return status, err
	}
	status.Available = available
	status.Name = defaultScaledObjectName(resourceName)
	if !available {
		return status, nil
	}

	item, err := findWorkloadScaledObject(ctx, runtime, workloadKind, namespace, resourceName)
	if err != nil {
		return status, err
	}
	if item == nil {
		return status, nil
	}

	status.Configured = true
	status.Name = item.GetName()
	status.MinReplicaCount = int32Value(item.Object, "spec", "minReplicaCount")
	status.MaxReplicaCount = int32Value(item.Object, "spec", "maxReplicaCount")
	status.PollingInterval = int32Value(item.Object, "spec", "pollingInterval")
	status.CooldownPeriod = int32Value(item.Object, "spec", "cooldownPeriod")
	status.LastActiveTime = stringValue(item.Object, "status", "lastActiveTime")
	status.OriginalReplicas = int32Value(item.Object, "status", "originalReplicaCount")
	status.Triggers = parseScaledObjectTriggers(item.Object)
	return status, nil
}

func getWorkloadKnativeAutoscaling(
	ctx context.Context,
	runtime *Runtime,
	definition ResourceDefinition,
	namespace, resourceName string,
) (APIAutoscalingStatus, error) {
	status := APIAutoscalingStatus{
		Supported: true,
		Name:      resourceName,
		Class:     defaultKnativeAutoscalingClass,
		Metric:    defaultKnativeAutoscalingMetric,
	}

	available, err := knativeServingAvailable(runtime)
	if err != nil {
		return status, err
	}
	status.Available = available
	if !available {
		return status, nil
	}

	item, err := runtime.Dynamic.Resource(definition.GVR).Namespace(namespace).Get(ctx, resourceName, metav1.GetOptions{})
	if err != nil {
		return status, err
	}

	status.URL = stringValue(item.Object, "status", "url")
	status.LatestReadyRevision = stringValue(item.Object, "status", "latestReadyRevisionName")
	annotations := knativeAutoscalingAnnotations(item.Object)
	status.Configured = len(annotations) > 0
	if class := strings.TrimSpace(annotations[knativeAutoscalingClassAnnotation]); class != "" {
		status.Class = class
	}
	if metric := strings.TrimSpace(annotations[knativeAutoscalingMetricAnnotation]); metric != "" {
		status.Metric = metric
	}
	status.Target = strings.TrimSpace(annotations[knativeAutoscalingTargetAnnotation])
	status.TargetUtilizationPercentage = parseAnnotationInt32(annotations[knativeAutoscalingTargetUtilizationAnnotation])
	status.MinScale = parseAnnotationInt32(annotations[knativeAutoscalingMinScaleAnnotation])
	status.MaxScale = parseAnnotationInt32(annotations[knativeAutoscalingMaxScaleAnnotation])
	status.ScaleDownDelay = strings.TrimSpace(annotations[knativeAutoscalingScaleDownDelayAnnotation])
	status.Window = strings.TrimSpace(annotations[knativeAutoscalingWindowAnnotation])
	status.ScaleToZeroRetention = strings.TrimSpace(annotations[knativeAutoscalingScaleToZeroRetentionAnnotation])
	return status, nil
}

func findWorkloadHPA(
	ctx context.Context,
	runtime *Runtime,
	workloadKind, namespace, resourceName string,
) (*unstructured.Unstructured, error) {
	list, err := runtime.Dynamic.Resource(hpaGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for index := range list.Items {
		item := &list.Items[index]
		targetName := stringValue(item.Object, "spec", "scaleTargetRef", "name")
		targetKind := stringValue(item.Object, "spec", "scaleTargetRef", "kind")
		if targetName == resourceName && strings.EqualFold(targetKind, workloadKind) {
			return item, nil
		}
	}
	return nil, nil
}

func ensureAutoscalingWorkloadExists(
	ctx context.Context,
	runtime *Runtime,
	definition ResourceDefinition,
	namespace, resourceName string,
) error {
	if runtime == nil || runtime.Dynamic == nil {
		return fmt.Errorf("kubernetes client is not ready")
	}

	if definition.Namespaced {
		if strings.TrimSpace(namespace) == "" {
			return fmt.Errorf("namespace is required")
		}
		if _, err := runtime.Dynamic.Resource(definition.GVR).Namespace(namespace).Get(ctx, resourceName, metav1.GetOptions{}); err != nil {
			if apierrors.IsNotFound(err) {
				return fmt.Errorf("%s/%s 不存在或已被删除", definition.Kind, resourceName)
			}
			return err
		}
		return nil
	}

	if _, err := runtime.Dynamic.Resource(definition.GVR).Get(ctx, resourceName, metav1.GetOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("%s/%s 不存在或已被删除", definition.Kind, resourceName)
		}
		return err
	}

	return nil
}

func findWorkloadScaledObject(
	ctx context.Context,
	runtime *Runtime,
	workloadKind, namespace, resourceName string,
) (*unstructured.Unstructured, error) {
	list, err := runtime.Dynamic.Resource(scaledObjectGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	for index := range list.Items {
		item := &list.Items[index]
		targetName := stringValue(item.Object, "spec", "scaleTargetRef", "name")
		targetKind := stringValue(item.Object, "spec", "scaleTargetRef", "kind")
		if targetName != resourceName {
			continue
		}
		if targetKind == "" || strings.EqualFold(targetKind, workloadKind) {
			return item, nil
		}
	}
	return nil, nil
}

func scaledObjectAvailable(runtime *Runtime) (bool, error) {
	if runtime == nil || runtime.Discovery == nil {
		return false, fmt.Errorf("kubernetes discovery client is not ready")
	}
	_, err := runtime.Discovery.ServerResourcesForGroupVersion("keda.sh/v1alpha1")
	if err != nil {
		if apierrors.IsNotFound(err) || strings.Contains(strings.ToLower(err.Error()), "the server could not find the requested resource") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func knativeServingAvailable(runtime *Runtime) (bool, error) {
	if runtime == nil || runtime.Discovery == nil {
		return false, fmt.Errorf("kubernetes discovery client is not ready")
	}
	_, err := runtime.Discovery.ServerResourcesForGroupVersion("serving.knative.dev/v1")
	if err != nil {
		if apierrors.IsNotFound(err) || strings.Contains(strings.ToLower(err.Error()), "the server could not find the requested resource") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func knativeAutoscalingAnnotations(object map[string]any) map[string]string {
	annotations, _, _ := unstructured.NestedStringMap(object, "spec", "template", "metadata", "annotations")
	if annotations == nil {
		return map[string]string{}
	}

	filtered := make(map[string]string)
	for _, key := range knativeAutoscalingAnnotationKeys() {
		if value := strings.TrimSpace(annotations[key]); value != "" {
			filtered[key] = value
		}
	}
	return filtered
}

func knativeAutoscalingAnnotationKeys() []string {
	return []string{
		knativeAutoscalingClassAnnotation,
		knativeAutoscalingMetricAnnotation,
		knativeAutoscalingTargetAnnotation,
		knativeAutoscalingTargetUtilizationAnnotation,
		knativeAutoscalingMinScaleAnnotation,
		knativeAutoscalingMaxScaleAnnotation,
		knativeAutoscalingWindowAnnotation,
		knativeAutoscalingScaleDownDelayAnnotation,
		knativeAutoscalingScaleToZeroRetentionAnnotation,
	}
}

func parseAnnotationInt32(raw string) int32 {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 32)
	if err != nil {
		return 0
	}
	return int32(value)
}

func updateOptionalKnativeAnnotation(annotations map[string]string, key, value string) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		delete(annotations, key)
		return
	}
	annotations[key] = trimmed
}

func parseHPAMetrics(object map[string]any) []AutoscalingMetricSnapshot {
	specMetrics, _, _ := unstructured.NestedSlice(object, "spec", "metrics")
	currentMetrics, _, _ := unstructured.NestedSlice(object, "status", "currentMetrics")
	result := make([]AutoscalingMetricSnapshot, 0, len(specMetrics))

	for index, item := range specMetrics {
		specMetric, ok := item.(map[string]any)
		if !ok {
			continue
		}
		currentMetric := map[string]any{}
		if index < len(currentMetrics) {
			if current, ok := currentMetrics[index].(map[string]any); ok {
				currentMetric = current
			}
		}
		result = append(result, AutoscalingMetricSnapshot{
			Label:  hpaMetricLabel(specMetric),
			Target: hpaMetricTarget(specMetric),
			Current: hpaMetricCurrent(currentMetric),
		})
	}

	return result
}

func parseScaledObjectTriggers(object map[string]any) []AutoscalingTriggerSnapshot {
	items, _, _ := unstructured.NestedSlice(object, "spec", "triggers")
	result := make([]AutoscalingTriggerSnapshot, 0, len(items))
	for _, item := range items {
		trigger, ok := item.(map[string]any)
		if !ok {
			continue
		}
		metadataRaw, _, _ := unstructured.NestedStringMap(trigger, "metadata")
		result = append(result, AutoscalingTriggerSnapshot{
			Type:     fmt.Sprint(trigger["type"]),
			Metadata: metadataRaw,
		})
	}
	return result
}

func hpaMetricLabel(metric map[string]any) string {
	metricType := strings.ToLower(fmt.Sprint(metric["type"]))
	switch metricType {
	case "resource":
		return fmt.Sprintf("资源指标 · %s", stringValue(metric, "resource", "name"))
	case "pods":
		return fmt.Sprintf("Pods 指标 · %s", stringValue(metric, "pods", "metric", "name"))
	case "object":
		return fmt.Sprintf("对象指标 · %s", stringValue(metric, "object", "metric", "name"))
	case "external":
		return fmt.Sprintf("外部指标 · %s", stringValue(metric, "external", "metric", "name"))
	default:
		return "指标"
	}
}

func hpaMetricTarget(metric map[string]any) string {
	metricType := strings.ToLower(fmt.Sprint(metric["type"]))
	switch metricType {
	case "resource":
		target, _, _ := nestedMap(metric, "resource", "target")
		return formatMetricTarget(target)
	case "pods":
		target, _, _ := nestedMap(metric, "pods", "target")
		return formatMetricTarget(target)
	case "object":
		target, _, _ := nestedMap(metric, "object", "target")
		return formatMetricTarget(target)
	case "external":
		target, _, _ := nestedMap(metric, "external", "target")
		return formatMetricTarget(target)
	default:
		return "-"
	}
}

func hpaMetricCurrent(metric map[string]any) string {
	metricType := strings.ToLower(fmt.Sprint(metric["type"]))
	switch metricType {
	case "resource":
		current, _, _ := nestedMap(metric, "resource", "current")
		return formatMetricCurrent(current)
	case "pods":
		current, _, _ := nestedMap(metric, "pods", "current")
		return formatMetricCurrent(current)
	case "object":
		current, _, _ := nestedMap(metric, "object", "current")
		return formatMetricCurrent(current)
	case "external":
		current, _, _ := nestedMap(metric, "external", "current")
		return formatMetricCurrent(current)
	default:
		return "-"
	}
}

func formatMetricTarget(target map[string]any) string {
	switch strings.ToLower(fmt.Sprint(target["type"])) {
	case "utilization":
		if value, ok := target["averageUtilization"]; ok {
			return fmt.Sprintf("%v%%", value)
		}
	case "averagevalue":
		if value, ok := target["averageValue"]; ok {
			return fmt.Sprint(value)
		}
	case "value":
		if value, ok := target["value"]; ok {
			return fmt.Sprint(value)
		}
	}
	return "-"
}

func formatMetricCurrent(current map[string]any) string {
	if value, ok := current["averageUtilization"]; ok {
		return fmt.Sprintf("%v%%", value)
	}
	if value, ok := current["averageValue"]; ok {
		return fmt.Sprint(value)
	}
	if value, ok := current["value"]; ok {
		return fmt.Sprint(value)
	}
	return "-"
}

func int32Value(source map[string]any, fields ...string) int32 {
	value := nestedValue(source, fields...)
	switch typed := value.(type) {
	case int:
		return int32(typed)
	case int32:
		return typed
	case int64:
		return int32(typed)
	case float64:
		return int32(typed)
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 32)
		if err == nil {
			return int32(parsed)
		}
	}
	return 0
}

func stringValue(source map[string]any, fields ...string) string {
	value := nestedValue(source, fields...)
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func nestedValue(source map[string]any, fields ...string) any {
	current := any(source)
	for _, field := range fields {
		next, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = next[field]
	}
	return current
}

func defaultHPAName(resourceName string) string {
	return fmt.Sprintf("%s-hpa", resourceName)
}

func defaultScaledObjectName(resourceName string) string {
	return fmt.Sprintf("%s-scaledobject", resourceName)
}

func SupportedEventTriggerTypes() []string {
	return []string{"cron", "kafka", "rabbitmq", "prometheus", "cpu", "memory", "aws-sqs-queue"}
}

func IsSupportedEventTriggerType(triggerType string) bool {
	return slices.Contains(SupportedEventTriggerTypes(), strings.TrimSpace(triggerType))
}
