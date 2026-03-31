package kube

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/apimachinery/pkg/labels"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/polymorphichelpers"
	deploymentutil "k8s.io/kubectl/pkg/util/deployment"
)

type WorkloadRevision struct {
	Revision    int64    `json:"revision"`
	Name        string   `json:"name"`
	CreatedAt   string   `json:"createdAt,omitempty"`
	Current     bool     `json:"current"`
	ChangeCause string   `json:"changeCause,omitempty"`
	Images      []string `json:"images,omitempty"`
	Summary     string   `json:"summary,omitempty"`
}

type WorkloadHistory struct {
	Supported         bool               `json:"supported"`
	RollbackSupported bool               `json:"rollbackSupported"`
	ResourceType      string             `json:"resourceType"`
	Items             []WorkloadRevision `json:"items"`
}

func SupportsWorkloadHistory(resourceType string) bool {
	switch resourceType {
	case "deployment", "statefulset", "daemonset":
		return true
	default:
		return false
	}
}

func SupportsWorkloadRollback(resourceType string) bool {
	return SupportsWorkloadHistory(resourceType)
}

func ListWorkloadHistory(
	ctx context.Context,
	runtime *Runtime,
	resourceType, namespace, resourceName string,
) (WorkloadHistory, error) {
	if runtime == nil || runtime.Clientset == nil {
		return WorkloadHistory{}, fmt.Errorf("kubernetes client is not ready")
	}
	if strings.TrimSpace(namespace) == "" {
		return WorkloadHistory{}, fmt.Errorf("namespace is required")
	}

	if !SupportsWorkloadHistory(resourceType) {
		return WorkloadHistory{
			Supported:         false,
			RollbackSupported: false,
			ResourceType:      resourceType,
			Items:             []WorkloadRevision{},
		}, nil
	}

	items := make([]WorkloadRevision, 0)
	switch resourceType {
	case "deployment":
		revisions, err := listDeploymentHistory(ctx, runtime, namespace, resourceName)
		if err != nil {
			return WorkloadHistory{}, err
		}
		items = revisions
	case "statefulset":
		revisions, err := listStatefulSetHistory(ctx, runtime, namespace, resourceName)
		if err != nil {
			return WorkloadHistory{}, err
		}
		items = revisions
	case "daemonset":
		revisions, err := listDaemonSetHistory(ctx, runtime, namespace, resourceName)
		if err != nil {
			return WorkloadHistory{}, err
		}
		items = revisions
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Revision > items[j].Revision
	})
	if len(items) > 6 {
		items = items[:6]
	}

	return WorkloadHistory{
		Supported:         true,
		RollbackSupported: SupportsWorkloadRollback(resourceType),
		ResourceType:      resourceType,
		Items:             items,
	}, nil
}

func RollbackWorkload(
	ctx context.Context,
	runtime *Runtime,
	resourceType, namespace, resourceName string,
	revision int64,
) (string, error) {
	if runtime == nil || runtime.Clientset == nil || runtime.Dynamic == nil {
		return "", fmt.Errorf("kubernetes client is not ready")
	}
	if strings.TrimSpace(namespace) == "" {
		return "", fmt.Errorf("namespace is required")
	}
	if !SupportsWorkloadRollback(resourceType) {
		return "", fmt.Errorf("当前工作负载类型暂不支持 rollout 回滚")
	}
	if revision <= 0 {
		return "", fmt.Errorf("revision is required")
	}

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

	rollbacker, err := polymorphichelpers.RollbackerFor(workloadGroupKind(resourceType), runtime.Clientset)
	if err != nil {
		return "", err
	}

	return rollbacker.Rollback(item, map[string]string{}, revision, cmdutil.DryRunNone)
}

func listDeploymentHistory(
	ctx context.Context,
	runtime *Runtime,
	namespace, resourceName string,
) ([]WorkloadRevision, error) {
	deployment, err := runtime.Clientset.AppsV1().Deployments(namespace).Get(ctx, resourceName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	replicaSets, err := getDeploymentReplicaSets(runtime, namespace, resourceName)
	if err != nil {
		return nil, err
	}

	currentRevision := int64(0)
	if deployment.Annotations != nil {
		if raw := strings.TrimSpace(deployment.Annotations[deploymentutil.RevisionAnnotation]); raw != "" {
			parsed, _ := strconv.ParseInt(raw, 10, 64)
			currentRevision = parsed
		}
	}

	items := make([]WorkloadRevision, 0, len(replicaSets))
	for _, replicaSet := range replicaSets {
		revision, err := deploymentutil.Revision(replicaSet)
		if err != nil {
			continue
		}

		items = append(items, WorkloadRevision{
			Revision:    revision,
			Name:        replicaSet.Name,
			CreatedAt:   replicaSet.CreationTimestamp.Format(timeRFC3339),
			Current:     currentRevision > 0 && revision == currentRevision,
			ChangeCause: firstNonEmpty(replicaSet.Annotations[polymorphichelpers.ChangeCauseAnnotation]),
			Images:      uniqueSortedStrings(imagesFromPodTemplate(replicaSet.Spec.Template)),
			Summary:     fmt.Sprintf("ReplicaSet · %d/%d Ready", replicaSet.Status.ReadyReplicas, replicaSet.Status.Replicas),
		})
	}

	return items, nil
}

func listStatefulSetHistory(
	ctx context.Context,
	runtime *Runtime,
	namespace, resourceName string,
) ([]WorkloadRevision, error) {
	statefulSet, history, err := statefulSetHistoryObjects(ctx, runtime, namespace, resourceName)
	if err != nil {
		return nil, err
	}

	currentRevisionName := firstNonEmpty(statefulSet.Status.UpdateRevision, statefulSet.Status.CurrentRevision)
	items := make([]WorkloadRevision, 0, len(history))
	for _, revision := range history {
		applied, err := applyStatefulSetRevision(statefulSet, revision)
		if err != nil {
			return nil, err
		}

		items = append(items, WorkloadRevision{
			Revision:    revision.Revision,
			Name:        revision.Name,
			CreatedAt:   revision.CreationTimestamp.Format(timeRFC3339),
			Current:     revision.Name == currentRevisionName,
			ChangeCause: firstNonEmpty(revision.Annotations[polymorphichelpers.ChangeCauseAnnotation]),
			Images:      uniqueSortedStrings(imagesFromPodTemplate(applied.Spec.Template)),
			Summary:     fmt.Sprintf("StatefulSet · replicas %d", valueOrDefaultInt32(applied.Spec.Replicas, 1)),
		})
	}

	return items, nil
}

func listDaemonSetHistory(
	ctx context.Context,
	runtime *Runtime,
	namespace, resourceName string,
) ([]WorkloadRevision, error) {
	daemonSet, history, err := daemonSetHistoryObjects(ctx, runtime, namespace, resourceName)
	if err != nil {
		return nil, err
	}

	items := make([]WorkloadRevision, 0, len(history))
	for _, revision := range history {
		applied, err := applyDaemonSetRevision(daemonSet, revision)
		if err != nil {
			return nil, err
		}

		items = append(items, WorkloadRevision{
			Revision:    revision.Revision,
			Name:        revision.Name,
			CreatedAt:   revision.CreationTimestamp.Format(timeRFC3339),
			Current:     daemonSetTemplateEquals(&daemonSet.Spec.Template, &applied.Spec.Template),
			ChangeCause: firstNonEmpty(revision.Annotations[polymorphichelpers.ChangeCauseAnnotation]),
			Images:      uniqueSortedStrings(imagesFromPodTemplate(applied.Spec.Template)),
			Summary:     fmt.Sprintf("DaemonSet · %d/%d Ready", daemonSet.Status.NumberReady, daemonSet.Status.DesiredNumberScheduled),
		})
	}

	return items, nil
}

func getDeploymentReplicaSets(runtime *Runtime, namespace, name string) ([]*appsv1.ReplicaSet, error) {
	deployment, err := runtime.Clientset.AppsV1().Deployments(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve deployment %s: %v", name, err)
	}

	_, oldReplicaSets, newReplicaSet, err := deploymentutil.GetAllReplicaSets(deployment, runtime.Clientset.AppsV1())
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve replica sets from deployment %s: %v", name, err)
	}

	if newReplicaSet == nil {
		return oldReplicaSets, nil
	}
	return append(oldReplicaSets, newReplicaSet), nil
}

func statefulSetHistoryObjects(
	ctx context.Context,
	runtime *Runtime,
	namespace, name string,
) (*appsv1.StatefulSet, []*appsv1.ControllerRevision, error) {
	statefulSet, err := runtime.Clientset.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to retrieve StatefulSet %s: %v", name, err)
	}

	selector, err := metav1.LabelSelectorAsSelector(statefulSet.Spec.Selector)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create selector for StatefulSet %s: %v", name, err)
	}

	history, err := controlledHistory(ctx, runtime, namespace, selector, statefulSet)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to find history controlled by StatefulSet %s: %v", name, err)
	}

	return statefulSet, history, nil
}

func daemonSetHistoryObjects(
	ctx context.Context,
	runtime *Runtime,
	namespace, name string,
) (*appsv1.DaemonSet, []*appsv1.ControllerRevision, error) {
	daemonSet, err := runtime.Clientset.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to retrieve DaemonSet %s: %v", name, err)
	}

	selector, err := metav1.LabelSelectorAsSelector(daemonSet.Spec.Selector)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create selector for DaemonSet %s: %v", name, err)
	}

	history, err := controlledHistory(ctx, runtime, namespace, selector, daemonSet)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to find history controlled by DaemonSet %s: %v", name, err)
	}

	return daemonSet, history, nil
}

func controlledHistory(
	ctx context.Context,
	runtime *Runtime,
	namespace string,
	selector labels.Selector,
	accessor metav1.Object,
) ([]*appsv1.ControllerRevision, error) {
	list, err := runtime.Clientset.AppsV1().ControllerRevisions(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, err
	}

	items := make([]*appsv1.ControllerRevision, 0)
	for index := range list.Items {
		item := list.Items[index]
		if metav1.IsControlledBy(&item, accessor) {
			items = append(items, &item)
		}
	}

	return items, nil
}

func applyStatefulSetRevision(statefulSet *appsv1.StatefulSet, history *appsv1.ControllerRevision) (*appsv1.StatefulSet, error) {
	statefulSetBytes, err := json.Marshal(statefulSet)
	if err != nil {
		return nil, err
	}
	patched, err := strategicpatch.StrategicMergePatch(statefulSetBytes, history.Data.Raw, statefulSet)
	if err != nil {
		return nil, err
	}
	resultSet := &appsv1.StatefulSet{}
	if err := json.Unmarshal(patched, resultSet); err != nil {
		return nil, err
	}
	return resultSet, nil
}

func applyDaemonSetRevision(daemonSet *appsv1.DaemonSet, history *appsv1.ControllerRevision) (*appsv1.DaemonSet, error) {
	daemonSetBytes, err := json.Marshal(daemonSet)
	if err != nil {
		return nil, err
	}
	patched, err := strategicpatch.StrategicMergePatch(daemonSetBytes, history.Data.Raw, daemonSet)
	if err != nil {
		return nil, err
	}
	resultSet := &appsv1.DaemonSet{}
	if err := json.Unmarshal(patched, resultSet); err != nil {
		return nil, err
	}
	return resultSet, nil
}

func workloadGroupKind(resourceType string) schema.GroupKind {
	switch resourceType {
	case "deployment":
		return schema.GroupKind{Group: "apps", Kind: "Deployment"}
	case "statefulset":
		return schema.GroupKind{Group: "apps", Kind: "StatefulSet"}
	case "daemonset":
		return schema.GroupKind{Group: "apps", Kind: "DaemonSet"}
	default:
		return schema.GroupKind{}
	}
}

func daemonSetTemplateEquals(left, right *corev1.PodTemplateSpec) bool {
	leftCopy := left.DeepCopy()
	rightCopy := right.DeepCopy()
	delete(leftCopy.Labels, appsv1.DefaultDaemonSetUniqueLabelKey)
	delete(rightCopy.Labels, appsv1.DefaultDaemonSetUniqueLabelKey)
	return apiequality.Semantic.DeepEqual(leftCopy, rightCopy)
}

func imagesFromPodTemplate(template corev1.PodTemplateSpec) []string {
	images := make([]string, 0, len(template.Spec.InitContainers)+len(template.Spec.Containers))
	for _, container := range template.Spec.InitContainers {
		images = append(images, container.Image)
	}
	for _, container := range template.Spec.Containers {
		images = append(images, container.Image)
	}
	return images
}

func valueOrDefaultInt32(value *int32, fallback int32) int32 {
	if value == nil {
		return fallback
	}
	return *value
}
