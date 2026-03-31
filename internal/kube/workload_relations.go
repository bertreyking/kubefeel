package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

type WorkloadRelatedResource struct {
	ResourceType string `json:"resourceType"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Namespace    string `json:"namespace,omitempty"`
	Status       string `json:"status,omitempty"`
	MatchReason  string `json:"matchReason"`
	Summary      string `json:"summary,omitempty"`
}

type WorkloadRelations struct {
	Services               []WorkloadRelatedResource `json:"services"`
	Ingresses              []WorkloadRelatedResource `json:"ingresses"`
	NetworkPolicies        []WorkloadRelatedResource `json:"networkPolicies"`
	PersistentVolumeClaims []WorkloadRelatedResource `json:"persistentVolumeClaims"`
	PersistentVolumes      []WorkloadRelatedResource `json:"persistentVolumes"`
}

func SupportsWorkloadRelations(resourceType string) bool {
	return SupportsPodOperations(resourceType)
}

func ListWorkloadRelations(
	ctx context.Context,
	runtime *Runtime,
	resourceType, namespace, resourceName string,
) (WorkloadRelations, error) {
	if runtime == nil || runtime.Clientset == nil || runtime.Dynamic == nil {
		return WorkloadRelations{}, fmt.Errorf("kubernetes client is not ready")
	}
	if strings.TrimSpace(namespace) == "" {
		return WorkloadRelations{}, fmt.Errorf("namespace is required")
	}
	if !SupportsWorkloadRelations(resourceType) {
		return WorkloadRelations{}, fmt.Errorf("resource type %q does not support relation inspection", resourceType)
	}

	definition, ok := LookupResource(resourceType)
	if !ok {
		return WorkloadRelations{}, fmt.Errorf("unsupported resource type %q", resourceType)
	}

	item, err := runtime.Dynamic.Resource(definition.GVR).
		Namespace(namespace).
		Get(ctx, resourceName, metav1.GetOptions{})
	if err != nil {
		return WorkloadRelations{}, err
	}

	templateLabels := workloadTemplateLabels(item.Object)
	serviceHints := workloadServiceHints(item.Object)
	directClaims, templateClaims := workloadPersistentVolumeClaims(item.Object)

	services, matchedServiceNames, err := listRelatedServices(
		ctx,
		runtime,
		namespace,
		templateLabels,
		serviceHints,
	)
	if err != nil {
		return WorkloadRelations{}, err
	}

	ingresses, err := listRelatedIngresses(ctx, runtime, namespace, matchedServiceNames)
	if err != nil {
		return WorkloadRelations{}, err
	}

	networkPolicies, err := listRelatedNetworkPolicies(ctx, runtime, namespace, templateLabels)
	if err != nil {
		return WorkloadRelations{}, err
	}

	pvcs, matchedPVNames, err := listRelatedPVCs(
		ctx,
		runtime,
		namespace,
		resourceName,
		directClaims,
		templateClaims,
	)
	if err != nil {
		return WorkloadRelations{}, err
	}

	pvs, err := listRelatedPVs(ctx, runtime, matchedPVNames)
	if err != nil {
		return WorkloadRelations{}, err
	}

	return WorkloadRelations{
		Services:               services,
		Ingresses:              ingresses,
		NetworkPolicies:        networkPolicies,
		PersistentVolumeClaims: pvcs,
		PersistentVolumes:      pvs,
	}, nil
}

func listRelatedServices(
	ctx context.Context,
	runtime *Runtime,
	namespace string,
	templateLabels map[string]string,
	serviceHints []string,
) ([]WorkloadRelatedResource, map[string]struct{}, error) {
	list, err := runtime.Clientset.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, nil, err
	}

	hintSet := make(map[string]struct{}, len(serviceHints))
	for _, name := range serviceHints {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			hintSet[trimmed] = struct{}{}
		}
	}

	matchedServiceNames := make(map[string]struct{})
	items := make([]WorkloadRelatedResource, 0)

	for _, service := range list.Items {
		matchReason := ""
		if _, ok := hintSet[service.Name]; ok {
			matchReason = "工作负载显式引用该 Service"
		} else if len(service.Spec.Selector) > 0 && selectorMatchesLabels(service.Spec.Selector, templateLabels) {
			matchReason = fmt.Sprintf("Service selector 命中 Pod labels：%s", joinLabelMap(service.Spec.Selector))
		}

		if matchReason == "" {
			continue
		}

		matchedServiceNames[service.Name] = struct{}{}
		items = append(items, WorkloadRelatedResource{
			ResourceType: "service",
			Kind:         "Service",
			Name:         service.Name,
			Namespace:    service.Namespace,
			Status:       string(service.Spec.Type),
			MatchReason:  matchReason,
			Summary:      summarizeService(service),
		})
	}

	sortRelatedResources(items)
	return items, matchedServiceNames, nil
}

func listRelatedIngresses(
	ctx context.Context,
	runtime *Runtime,
	namespace string,
	matchedServiceNames map[string]struct{},
) ([]WorkloadRelatedResource, error) {
	if len(matchedServiceNames) == 0 {
		return []WorkloadRelatedResource{}, nil
	}

	list, err := runtime.Clientset.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	items := make([]WorkloadRelatedResource, 0)
	for _, ingress := range list.Items {
		relatedServices := ingressBackendServiceNames(ingress)
		matchedNames := make([]string, 0)
		for _, name := range relatedServices {
			if _, ok := matchedServiceNames[name]; ok {
				matchedNames = append(matchedNames, name)
			}
		}
		if len(matchedNames) == 0 {
			continue
		}

		sort.Strings(matchedNames)
		items = append(items, WorkloadRelatedResource{
			ResourceType: "ingress",
			Kind:         "Ingress",
			Name:         ingress.Name,
			Namespace:    ingress.Namespace,
			Status:       ingressClassName(ingress),
			MatchReason:  fmt.Sprintf("Ingress 后端指向 Service：%s", strings.Join(matchedNames, ", ")),
			Summary:      summarizeIngress(ingress),
		})
	}

	sortRelatedResources(items)
	return items, nil
}

func listRelatedNetworkPolicies(
	ctx context.Context,
	runtime *Runtime,
	namespace string,
	templateLabels map[string]string,
) ([]WorkloadRelatedResource, error) {
	list, err := runtime.Clientset.NetworkingV1().NetworkPolicies(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	items := make([]WorkloadRelatedResource, 0)
	for _, policy := range list.Items {
		selector, matchReason, ok := networkPolicyMatch(policy.Spec.PodSelector, templateLabels)
		if !ok {
			continue
		}

		if selector != nil && selector.Empty() {
			matchReason = "podSelector 为空，作用于命名空间内全部 Pod"
		}

		items = append(items, WorkloadRelatedResource{
			ResourceType: "networkpolicy",
			Kind:         "NetworkPolicy",
			Name:         policy.Name,
			Namespace:    policy.Namespace,
			Status:       summarizePolicyTypes(policy.Spec.PolicyTypes),
			MatchReason:  matchReason,
			Summary:      summarizeNetworkPolicy(policy),
		})
	}

	sortRelatedResources(items)
	return items, nil
}

func listRelatedPVCs(
	ctx context.Context,
	runtime *Runtime,
	namespace, workloadName string,
	directClaims map[string]string,
	templateClaims map[string]string,
) ([]WorkloadRelatedResource, map[string]struct{}, error) {
	list, err := runtime.Clientset.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, nil, err
	}

	items := make([]WorkloadRelatedResource, 0)
	matchedPVNames := make(map[string]struct{})

	for _, pvc := range list.Items {
		matchReason := ""
		if reason, ok := directClaims[pvc.Name]; ok {
			matchReason = reason
		} else {
			for templateName, reason := range templateClaims {
				prefix := fmt.Sprintf("%s-%s-", templateName, workloadName)
				if strings.HasPrefix(pvc.Name, prefix) {
					matchReason = reason
					break
				}
			}
		}

		if matchReason == "" {
			continue
		}

		if trimmed := strings.TrimSpace(pvc.Spec.VolumeName); trimmed != "" {
			matchedPVNames[trimmed] = struct{}{}
		}

		items = append(items, WorkloadRelatedResource{
			ResourceType: "pvc",
			Kind:         "PersistentVolumeClaim",
			Name:         pvc.Name,
			Namespace:    pvc.Namespace,
			Status:       string(pvc.Status.Phase),
			MatchReason:  matchReason,
			Summary:      summarizePVC(pvc),
		})
	}

	sortRelatedResources(items)
	return items, matchedPVNames, nil
}

func listRelatedPVs(
	ctx context.Context,
	runtime *Runtime,
	matchedPVNames map[string]struct{},
) ([]WorkloadRelatedResource, error) {
	if len(matchedPVNames) == 0 {
		return []WorkloadRelatedResource{}, nil
	}

	list, err := runtime.Clientset.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	items := make([]WorkloadRelatedResource, 0)
	for _, pv := range list.Items {
		if _, ok := matchedPVNames[pv.Name]; !ok {
			continue
		}

		claimRef := ""
		if pv.Spec.ClaimRef != nil {
			claimRef = fmt.Sprintf("%s/%s", pv.Spec.ClaimRef.Namespace, pv.Spec.ClaimRef.Name)
		}

		items = append(items, WorkloadRelatedResource{
			ResourceType: "pv",
			Kind:         "PersistentVolume",
			Name:         pv.Name,
			Status:       string(pv.Status.Phase),
			MatchReason:  fmt.Sprintf("由 PVC 绑定%s", formatOptionalSuffix(claimRef)),
			Summary:      summarizePV(pv),
		})
	}

	sortRelatedResources(items)
	return items, nil
}

func workloadTemplateLabels(object map[string]any) map[string]string {
	paths := [][]string{
		{"spec", "template", "metadata", "labels"},
		{"spec", "jobTemplate", "spec", "template", "metadata", "labels"},
	}

	for _, path := range paths {
		if labelsMap := stringMapAt(object, path...); len(labelsMap) > 0 {
			return labelsMap
		}
	}

	return map[string]string{}
}

func workloadPodSpec(object map[string]any) map[string]any {
	paths := [][]string{
		{"spec", "template", "spec"},
		{"spec", "jobTemplate", "spec", "template", "spec"},
	}

	for _, path := range paths {
		if current, found := nestedObjectValue(object, path...); found && len(current) > 0 {
			return current
		}
	}

	return map[string]any{}
}

func workloadServiceHints(object map[string]any) []string {
	value, found := nestedStringValue(object, "spec", "serviceName")
	if !found || strings.TrimSpace(value) == "" {
		return []string{}
	}
	return []string{value}
}

func workloadPersistentVolumeClaims(object map[string]any) (map[string]string, map[string]string) {
	directClaims := make(map[string]string)
	templateClaims := make(map[string]string)

	spec := workloadPodSpec(object)
	if volumes, ok := spec["volumes"].([]any); ok {
		for _, item := range volumes {
			record, ok := item.(map[string]any)
			if !ok {
				continue
			}
			volumeName, _ := record["name"].(string)
			claimRef, ok := record["persistentVolumeClaim"].(map[string]any)
			if !ok {
				continue
			}
			claimName, _ := claimRef["claimName"].(string)
			if strings.TrimSpace(claimName) == "" {
				continue
			}
			directClaims[claimName] = fmt.Sprintf("Pod volume 挂载 PVC：%s", firstNonEmpty(volumeName, claimName))
		}
	}

	if templates, found := nestedArrayValue(object, "spec", "volumeClaimTemplates"); found {
		for _, item := range templates {
			record, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name, _ := nestedStringValue(record, "metadata", "name")
			if strings.TrimSpace(name) == "" {
				continue
			}
			templateClaims[name] = fmt.Sprintf("StatefulSet volumeClaimTemplate：%s", name)
		}
	}
	return directClaims, templateClaims
}

func selectorMatchesLabels(selector map[string]string, labelsMap map[string]string) bool {
	if len(selector) == 0 || len(labelsMap) == 0 {
		return false
	}

	return labels.SelectorFromSet(selector).Matches(labels.Set(labelsMap))
}

func networkPolicyMatch(
	podSelector metav1.LabelSelector,
	templateLabels map[string]string,
) (labels.Selector, string, bool) {
	selector, err := metav1.LabelSelectorAsSelector(&podSelector)
	if err != nil {
		return nil, "", false
	}

	if selector.Empty() {
		return selector, "", true
	}
	if len(templateLabels) == 0 || !selector.Matches(labels.Set(templateLabels)) {
		return selector, "", false
	}

	return selector, fmt.Sprintf("podSelector 命中 Pod labels：%s", joinLabelMap(selectorToMap(&podSelector))), true
}

func ingressBackendServiceNames(ingress networkingv1.Ingress) []string {
	names := make([]string, 0)

	if ingress.Spec.DefaultBackend != nil && ingress.Spec.DefaultBackend.Service != nil {
		names = append(names, ingress.Spec.DefaultBackend.Service.Name)
	}

	for _, rule := range ingress.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			if path.Backend.Service != nil {
				names = append(names, path.Backend.Service.Name)
			}
		}
	}

	return uniqueSortedStrings(names)
}

func summarizeService(service corev1.Service) string {
	ports := make([]string, 0, len(service.Spec.Ports))
	for _, port := range service.Spec.Ports {
		target := ""
		if port.TargetPort.String() != "" {
			target = fmt.Sprintf(" -> %s", port.TargetPort.String())
		}
		ports = append(ports, fmt.Sprintf("%d/%s%s", port.Port, firstNonEmpty(string(port.Protocol), "TCP"), target))
	}

	networking := firstNonEmpty(service.Spec.ClusterIP, "None")
	if len(ports) == 0 {
		return fmt.Sprintf("%s · 无端口定义", networking)
	}
	return fmt.Sprintf("%s · %s", networking, strings.Join(ports, ", "))
}

func summarizeIngress(ingress networkingv1.Ingress) string {
	hosts := make([]string, 0)
	for _, rule := range ingress.Spec.Rules {
		if strings.TrimSpace(rule.Host) != "" {
			hosts = append(hosts, rule.Host)
		}
	}

	hosts = uniqueSortedStrings(hosts)
	if len(hosts) == 0 {
		return "未配置 Host，依赖默认路由"
	}
	return fmt.Sprintf("Hosts: %s", strings.Join(hosts, ", "))
}

func ingressClassName(ingress networkingv1.Ingress) string {
	if ingress.Spec.IngressClassName != nil && strings.TrimSpace(*ingress.Spec.IngressClassName) != "" {
		return *ingress.Spec.IngressClassName
	}
	return "默认类"
}

func summarizeNetworkPolicy(policy networkingv1.NetworkPolicy) string {
	ingressRules := len(policy.Spec.Ingress)
	egressRules := len(policy.Spec.Egress)
	return fmt.Sprintf("%d ingress rules / %d egress rules", ingressRules, egressRules)
}

func summarizePolicyTypes(types []networkingv1.PolicyType) string {
	if len(types) == 0 {
		return "Ingress"
	}
	values := make([]string, 0, len(types))
	for _, item := range types {
		values = append(values, string(item))
	}
	sort.Strings(values)
	return strings.Join(values, " / ")
}

func summarizePVC(pvc corev1.PersistentVolumeClaim) string {
	size := ""
	if quantity, ok := pvc.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
		size = quantity.String()
	}

	parts := []string{firstNonEmpty(string(pvc.Status.Phase), "Unknown")}
	if pvc.Spec.StorageClassName != nil && strings.TrimSpace(*pvc.Spec.StorageClassName) != "" {
		parts = append(parts, fmt.Sprintf("SC: %s", *pvc.Spec.StorageClassName))
	}
	if size != "" {
		parts = append(parts, size)
	}
	if strings.TrimSpace(pvc.Spec.VolumeName) != "" {
		parts = append(parts, fmt.Sprintf("PV: %s", pvc.Spec.VolumeName))
	}

	return strings.Join(parts, " · ")
}

func summarizePV(pv corev1.PersistentVolume) string {
	size := ""
	if quantity, ok := pv.Spec.Capacity[corev1.ResourceStorage]; ok {
		size = quantity.String()
	}

	parts := []string{firstNonEmpty(string(pv.Status.Phase), "Unknown")}
	if size != "" {
		parts = append(parts, size)
	}
	parts = append(parts, fmt.Sprintf("Reclaim: %s", pv.Spec.PersistentVolumeReclaimPolicy))
	if strings.TrimSpace(pv.Spec.StorageClassName) != "" {
		parts = append(parts, fmt.Sprintf("SC: %s", pv.Spec.StorageClassName))
	}

	return strings.Join(parts, " · ")
}

func stringMapAt(object map[string]any, path ...string) map[string]string {
	current := any(object)
	for _, segment := range path {
		record, ok := current.(map[string]any)
		if !ok {
			return map[string]string{}
		}
		next, exists := record[segment]
		if !exists {
			return map[string]string{}
		}
		current = next
	}

	record, ok := current.(map[string]any)
	if !ok {
		return map[string]string{}
	}

	result := make(map[string]string, len(record))
	for key, value := range record {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			result[key] = text
		}
	}
	return result
}

func nestedObjectValue(object map[string]any, fields ...string) (map[string]any, bool) {
	value := any(object)
	for _, field := range fields {
		current, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		next, exists := current[field]
		if !exists {
			return nil, false
		}
		value = next
	}

	result, ok := value.(map[string]any)
	return result, ok
}

func nestedStringValue(object map[string]any, fields ...string) (string, bool) {
	value := any(object)
	for _, field := range fields {
		current, ok := value.(map[string]any)
		if !ok {
			return "", false
		}
		next, exists := current[field]
		if !exists {
			return "", false
		}
		value = next
	}

	text, ok := value.(string)
	if !ok {
		return "", false
	}
	return text, true
}

func nestedArrayValue(object map[string]any, fields ...string) ([]any, bool) {
	value := any(object)
	for _, field := range fields {
		current, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		next, exists := current[field]
		if !exists {
			return nil, false
		}
		value = next
	}

	result, ok := value.([]any)
	return result, ok
}

func selectorToMap(selector *metav1.LabelSelector) map[string]string {
	if selector == nil || len(selector.MatchLabels) == 0 {
		return map[string]string{}
	}
	return selector.MatchLabels
}

func sortRelatedResources(items []WorkloadRelatedResource) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Namespace != items[j].Namespace {
			return items[i].Namespace < items[j].Namespace
		}
		return items[i].Name < items[j].Name
	})
}

func joinLabelMap(labelMap map[string]string) string {
	if len(labelMap) == 0 {
		return "-"
	}

	keys := make([]string, 0, len(labelMap))
	for key := range labelMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", key, labelMap[key]))
	}
	return strings.Join(parts, ", ")
}

func uniqueSortedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	sort.Strings(result)
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func formatOptionalSuffix(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return "：" + value
}

func statefulSetServiceName(object map[string]any) string {
	serviceName, _ := nestedStringValue(object, "spec", "serviceName")
	return serviceName
}
