package kube

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

type WorkloadPod struct {
	Name            string   `json:"name"`
	Namespace       string   `json:"namespace"`
	Phase           string   `json:"phase"`
	NodeName        string   `json:"nodeName"`
	PodIP           string   `json:"podIP"`
	Containers      []string `json:"containers"`
	ReadyContainers int      `json:"readyContainers"`
	TotalContainers int      `json:"totalContainers"`
	RestartCount    int      `json:"restartCount"`
	CreatedAt       string   `json:"createdAt"`
}

type PodExecResult struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

func SupportsPodOperations(resourceType string) bool {
	switch resourceType {
	case "deployment", "statefulset", "daemonset", "job", "cronjob", "knativeservice":
		return true
	default:
		return false
	}
}

func ListWorkloadPods(
	ctx context.Context,
	runtime *Runtime,
	resourceType, namespace, resourceName string,
) ([]WorkloadPod, error) {
	if runtime == nil || runtime.Clientset == nil {
		return nil, fmt.Errorf("kubernetes client is not ready")
	}
	if strings.TrimSpace(namespace) == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	if !SupportsPodOperations(resourceType) {
		return nil, fmt.Errorf("resource type %q does not support pod operations", resourceType)
	}

	switch resourceType {
	case "cronjob":
		return listPodsForCronJob(ctx, runtime, namespace, resourceName)
	case "job":
		selector, err := workloadSelector(ctx, runtime, resourceType, namespace, resourceName)
		if err != nil {
			return nil, err
		}
		if selector != "" {
			return listPodsBySelector(ctx, runtime, namespace, selector)
		}
		return listPodsForJob(ctx, runtime, namespace, resourceName)
	case "knativeservice":
		return listPodsForKnativeService(ctx, runtime, namespace, resourceName)
	default:
		selector, err := workloadSelector(ctx, runtime, resourceType, namespace, resourceName)
		if err != nil {
			return nil, err
		}
		if selector == "" {
			return nil, fmt.Errorf("当前工作负载没有可用的 selector")
		}
		return listPodsBySelector(ctx, runtime, namespace, selector)
	}
}

func GetPodLogs(
	ctx context.Context,
	runtime *Runtime,
	namespace, podName, container string,
	tailLines int64,
) (string, error) {
	if runtime == nil || runtime.Clientset == nil {
		return "", fmt.Errorf("kubernetes client is not ready")
	}
	if strings.TrimSpace(namespace) == "" {
		return "", fmt.Errorf("namespace is required")
	}
	if strings.TrimSpace(podName) == "" {
		return "", fmt.Errorf("pod name is required")
	}
	if tailLines <= 0 {
		tailLines = 300
	}

	options := &corev1.PodLogOptions{TailLines: &tailLines}
	if trimmed := strings.TrimSpace(container); trimmed != "" {
		options.Container = trimmed
	}

	stream, err := runtime.Clientset.CoreV1().Pods(namespace).GetLogs(podName, options).Stream(ctx)
	if err != nil {
		return "", err
	}
	defer stream.Close()

	content, err := io.ReadAll(stream)
	if err != nil {
		return "", err
	}

	return string(content), nil
}

func ExecPodCommand(
	ctx context.Context,
	runtime *Runtime,
	namespace, podName, container, command string,
) (PodExecResult, error) {
	if runtime == nil || runtime.Clientset == nil {
		return PodExecResult{}, fmt.Errorf("kubernetes client is not ready")
	}
	if strings.TrimSpace(namespace) == "" {
		return PodExecResult{}, fmt.Errorf("namespace is required")
	}
	if strings.TrimSpace(podName) == "" {
		return PodExecResult{}, fmt.Errorf("pod name is required")
	}
	if strings.TrimSpace(command) == "" {
		return PodExecResult{}, fmt.Errorf("command is required")
	}

	request := runtime.Clientset.CoreV1().
		RESTClient().
		Post().
		Resource("pods").
		Namespace(namespace).
		Name(podName).
		SubResource("exec")

	request.VersionedParams(&corev1.PodExecOptions{
		Container: strings.TrimSpace(container),
		Command:   []string{"sh", "-lc", command},
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(runtime.Config, "POST", request.URL())
	if err != nil {
		return PodExecResult{}, err
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	if err := executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
		Tty:    false,
	}); err != nil {
		return PodExecResult{}, err
	}

	return PodExecResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}, nil
}

func workloadSelector(
	ctx context.Context,
	runtime *Runtime,
	resourceType, namespace, resourceName string,
) (string, error) {
	definition, ok := LookupResource(resourceType)
	if !ok {
		return "", fmt.Errorf("unsupported resource type %q", resourceType)
	}

	item, err := runtime.Dynamic.Resource(definition.GVR).
		Namespace(namespace).
		Get(ctx, resourceName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	return selectorStringFromObject(item.Object)
}

func selectorStringFromObject(object map[string]any) (string, error) {
	rawSelector, found, err := nestedMap(object, "spec", "selector")
	if err != nil || !found || len(rawSelector) == 0 {
		return "", err
	}

	selector := &metav1.LabelSelector{}
	if err := k8sruntime.DefaultUnstructuredConverter.FromUnstructured(rawSelector, selector); err != nil {
		return "", err
	}

	compiled, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return "", err
	}

	return compiled.String(), nil
}

func listPodsBySelector(
	ctx context.Context,
	runtime *Runtime,
	namespace, selector string,
) ([]WorkloadPod, error) {
	list, err := runtime.Clientset.CoreV1().
		Pods(namespace).
		List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, err
	}

	return serializePods(list.Items), nil
}

func listPodsForJob(
	ctx context.Context,
	runtime *Runtime,
	namespace, jobName string,
) ([]WorkloadPod, error) {
	list, err := runtime.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	filtered := make([]corev1.Pod, 0)
	for _, pod := range list.Items {
		if hasOwnerReference(pod.OwnerReferences, "Job", jobName) {
			filtered = append(filtered, pod)
		}
	}

	return serializePods(filtered), nil
}

func listPodsForCronJob(
	ctx context.Context,
	runtime *Runtime,
	namespace, cronJobName string,
) ([]WorkloadPod, error) {
	jobs, err := runtime.Clientset.BatchV1().Jobs(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	jobNames := make(map[string]struct{})
	for _, job := range jobs.Items {
		if hasOwnerReference(job.OwnerReferences, "CronJob", cronJobName) {
			jobNames[job.Name] = struct{}{}
		}
	}

	if len(jobNames) == 0 {
		return []WorkloadPod{}, nil
	}

	pods, err := runtime.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	filtered := make([]corev1.Pod, 0)
	for _, pod := range pods.Items {
		for _, ownerReference := range pod.OwnerReferences {
			if ownerReference.Kind != "Job" {
				continue
			}
			if _, ok := jobNames[ownerReference.Name]; ok {
				filtered = append(filtered, pod)
				break
			}
		}
	}

	return serializePods(filtered), nil
}

func listPodsForKnativeService(
	ctx context.Context,
	runtime *Runtime,
	namespace, serviceName string,
) ([]WorkloadPod, error) {
	definition, ok := LookupResource("knativeservice")
	if !ok {
		return nil, fmt.Errorf("knative service resource definition is not available")
	}

	item, err := runtime.Dynamic.Resource(definition.GVR).
		Namespace(namespace).
		Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	selector := fmt.Sprintf("serving.knative.dev/service=%s", serviceName)
	pods, err := listPodsBySelector(ctx, runtime, namespace, selector)
	if err == nil && len(pods) > 0 {
		return pods, nil
	}

	revisionNames := make([]string, 0, 3)
	if latestReady, found, _ := unstructured.NestedString(item.Object, "status", "latestReadyRevisionName"); found && strings.TrimSpace(latestReady) != "" {
		revisionNames = append(revisionNames, latestReady)
	}
	if latestCreated, found, _ := unstructured.NestedString(item.Object, "status", "latestCreatedRevisionName"); found && strings.TrimSpace(latestCreated) != "" {
		revisionNames = append(revisionNames, latestCreated)
	}

	for _, revisionName := range uniqueSortedStrings(revisionNames) {
		matched, listErr := listPodsBySelector(ctx, runtime, namespace, fmt.Sprintf("serving.knative.dev/revision=%s", revisionName))
		if listErr != nil {
			return nil, listErr
		}
		if len(matched) > 0 {
			return matched, nil
		}
	}

	return []WorkloadPod{}, nil
}

func serializePods(items []corev1.Pod) []WorkloadPod {
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreationTimestamp.Time.After(items[j].CreationTimestamp.Time)
	})

	result := make([]WorkloadPod, 0, len(items))
	for _, item := range items {
		result = append(result, serializePod(item))
	}

	return result
}

func serializePod(item corev1.Pod) WorkloadPod {
	containerNames := make([]string, 0, len(item.Spec.Containers))
	for _, container := range item.Spec.Containers {
		containerNames = append(containerNames, container.Name)
	}

	readyContainers := 0
	restartCount := 0
	for _, status := range item.Status.ContainerStatuses {
		if status.Ready {
			readyContainers += 1
		}
		restartCount += int(status.RestartCount)
	}

	phase := string(item.Status.Phase)
	if item.DeletionTimestamp != nil {
		phase = "Terminating"
	}

	return WorkloadPod{
		Name:            item.Name,
		Namespace:       item.Namespace,
		Phase:           phase,
		NodeName:        item.Spec.NodeName,
		PodIP:           item.Status.PodIP,
		Containers:      containerNames,
		ReadyContainers: readyContainers,
		TotalContainers: len(item.Spec.Containers),
		RestartCount:    restartCount,
		CreatedAt:       item.CreationTimestamp.Format(timeRFC3339),
	}
}

func hasOwnerReference(references []metav1.OwnerReference, kind, name string) bool {
	for _, reference := range references {
		if reference.Kind == kind && reference.Name == name {
			return true
		}
	}

	return false
}

func nestedMap(object map[string]any, fields ...string) (map[string]any, bool, error) {
	value := any(object)
	for _, field := range fields {
		current, ok := value.(map[string]any)
		if !ok {
			return nil, false, fmt.Errorf("field %s is not an object", field)
		}
		next, exists := current[field]
		if !exists {
			return nil, false, nil
		}
		value = next
	}

	result, ok := value.(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("target is not an object")
	}

	return result, true, nil
}

const timeRFC3339 = "2006-01-02T15:04:05Z07:00"
