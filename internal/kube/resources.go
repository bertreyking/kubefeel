package kube

import "k8s.io/apimachinery/pkg/runtime/schema"

type ResourceDefinition struct {
	Key        string                      `json:"key"`
	Label      string                      `json:"label"`
	Kind       string                      `json:"kind"`
	APIVersion string                      `json:"apiVersion"`
	Namespaced bool                        `json:"namespaced"`
	GVR        schema.GroupVersionResource `json:"-"`
}

var resourceCatalog = []ResourceDefinition{
	{Key: "deployment", Label: "Deployments", Kind: "Deployment", APIVersion: "apps/v1", Namespaced: true, GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}},
	{Key: "statefulset", Label: "StatefulSets", Kind: "StatefulSet", APIVersion: "apps/v1", Namespaced: true, GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}},
	{Key: "daemonset", Label: "DaemonSets", Kind: "DaemonSet", APIVersion: "apps/v1", Namespaced: true, GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}},
	{Key: "cronjob", Label: "CronJobs", Kind: "CronJob", APIVersion: "batch/v1", Namespaced: true, GVR: schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}},
	{Key: "job", Label: "Jobs", Kind: "Job", APIVersion: "batch/v1", Namespaced: true, GVR: schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}},
	{Key: "knativeservice", Label: "Knative Services", Kind: "Service", APIVersion: "serving.knative.dev/v1", Namespaced: true, GVR: schema.GroupVersionResource{Group: "serving.knative.dev", Version: "v1", Resource: "services"}},
	{Key: "configmap", Label: "ConfigMaps", Kind: "ConfigMap", APIVersion: "v1", Namespaced: true, GVR: schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}},
	{Key: "secret", Label: "Secrets", Kind: "Secret", APIVersion: "v1", Namespaced: true, GVR: schema.GroupVersionResource{Version: "v1", Resource: "secrets"}},
	{Key: "service", Label: "Services", Kind: "Service", APIVersion: "v1", Namespaced: true, GVR: schema.GroupVersionResource{Version: "v1", Resource: "services"}},
	{Key: "ingress", Label: "Ingresses", Kind: "Ingress", APIVersion: "networking.k8s.io/v1", Namespaced: true, GVR: schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}},
	{Key: "networkpolicy", Label: "NetworkPolicies", Kind: "NetworkPolicy", APIVersion: "networking.k8s.io/v1", Namespaced: true, GVR: schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"}},
	{Key: "ingressclass", Label: "IngressClasses", Kind: "IngressClass", APIVersion: "networking.k8s.io/v1", Namespaced: false, GVR: schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingressclasses"}},
	{Key: "pvc", Label: "PersistentVolumeClaims", Kind: "PersistentVolumeClaim", APIVersion: "v1", Namespaced: true, GVR: schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}},
	{Key: "pv", Label: "PersistentVolumes", Kind: "PersistentVolume", APIVersion: "v1", Namespaced: false, GVR: schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumes"}},
	{Key: "storageclass", Label: "StorageClasses", Kind: "StorageClass", APIVersion: "storage.k8s.io/v1", Namespaced: false, GVR: schema.GroupVersionResource{Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses"}},
}

func Catalog() []ResourceDefinition {
	result := make([]ResourceDefinition, len(resourceCatalog))
	copy(result, resourceCatalog)
	return result
}

func LookupResource(key string) (ResourceDefinition, bool) {
	for _, resource := range resourceCatalog {
		if resource.Key == key {
			return resource, true
		}
	}

	return ResourceDefinition{}, false
}

func LookupResourceByAPIVersionKind(apiVersion, kind string) (ResourceDefinition, bool) {
	for _, resource := range resourceCatalog {
		if resource.APIVersion == apiVersion && resource.Kind == kind {
			return resource, true
		}
	}

	return ResourceDefinition{}, false
}
