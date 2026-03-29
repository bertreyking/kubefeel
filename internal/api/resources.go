package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"multikube-manager/internal/kube"
	"multikube-manager/internal/model"

	"github.com/gin-gonic/gin"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
)

var namespaceGVR = schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}

type resourcePayload struct {
	Manifest string `json:"manifest"`
}

func (s *Server) listResources(c *gin.Context) {
	cluster, runtime, definition, namespace, ok := s.loadResourceContext(c)
	if !ok {
		return
	}

	target := runtime.Dynamic.Resource(definition.GVR)
	if definition.Namespaced {
		if namespace == "" {
			namespace = metav1.NamespaceAll
		}
		list, err := target.Namespace(namespace).List(c.Request.Context(), metav1.ListOptions{})
		if err != nil {
			s.markClusterError(cluster, err)
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		respondData(c, http.StatusOK, serializeUnstructuredList(list.Items, c.Query("search")))
		return
	}

	list, err := target.List(c.Request.Context(), metav1.ListOptions{})
	if err != nil {
		s.markClusterError(cluster, err)
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	respondData(c, http.StatusOK, serializeUnstructuredList(list.Items, c.Query("search")))
}

func (s *Server) getResource(c *gin.Context) {
	cluster, runtime, definition, namespace, ok := s.loadResourceContext(c)
	if !ok {
		return
	}

	resource, resourceInterface, ok := s.resourceTarget(c, runtime, definition, namespace)
	if !ok {
		return
	}

	item, err := resourceInterface.Get(c.Request.Context(), resource, metav1.GetOptions{})
	if err != nil {
		s.markClusterError(cluster, err)
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	respondData(c, http.StatusOK, item.Object)
}

func (s *Server) createResource(c *gin.Context) {
	cluster, runtime, definition, namespace, ok := s.loadResourceContext(c)
	if !ok {
		return
	}

	var input resourcePayload
	if err := c.ShouldBindJSON(&input); err != nil || strings.TrimSpace(input.Manifest) == "" {
		respondError(c, http.StatusBadRequest, "manifest is required")
		return
	}

	documents, err := buildResourceObjects(definition, namespace, input.Manifest)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	created := make([]map[string]any, 0, len(documents))
	createdRefs := make([]createdResourceRef, 0, len(documents))

	for _, document := range documents {
		document.Object.SetResourceVersion("")

		var (
			item      *unstructured.Unstructured
			createErr error
		)
		if document.Definition.Namespaced {
			item, createErr = runtime.Dynamic.Resource(document.Definition.GVR).
				Namespace(document.Object.GetNamespace()).
				Create(c.Request.Context(), document.Object, metav1.CreateOptions{})
		} else {
			item, createErr = runtime.Dynamic.Resource(document.Definition.GVR).
				Create(c.Request.Context(), document.Object, metav1.CreateOptions{})
		}
		if createErr != nil {
			s.markClusterError(cluster, createErr)
			rollbackCreatedResources(c.Request.Context(), runtime, createdRefs)
			respondError(c, http.StatusBadRequest, createErr.Error())
			return
		}

		created = append(created, item.Object)
		createdRefs = append(createdRefs, createdResourceRef{
			Definition: document.Definition,
			Name:       item.GetName(),
			Namespace:  item.GetNamespace(),
		})
	}

	if len(created) == 1 {
		respondData(c, http.StatusCreated, created[0])
		return
	}

	respondData(c, http.StatusCreated, gin.H{
		"count": len(created),
		"items": created,
	})
}

func (s *Server) updateResource(c *gin.Context) {
	cluster, runtime, definition, namespace, ok := s.loadResourceContext(c)
	if !ok {
		return
	}

	var input resourcePayload
	if err := c.ShouldBindJSON(&input); err != nil || strings.TrimSpace(input.Manifest) == "" {
		respondError(c, http.StatusBadRequest, "manifest is required")
		return
	}

	name := c.Param("name")
	documents, err := buildResourceObjects(definition, namespace, input.Manifest)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}
	if len(documents) != 1 {
		respondError(c, http.StatusBadRequest, "更新现有资源暂不支持多段 YAML，请仅保留当前对象")
		return
	}

	object := documents[0].Object
	object.SetName(name)

	resourceName, resourceInterface, ok := s.resourceTarget(c, runtime, definition, object.GetNamespace())
	if !ok {
		return
	}

	if object.GetResourceVersion() == "" {
		existing, getErr := resourceInterface.Get(c.Request.Context(), resourceName, metav1.GetOptions{})
		if getErr != nil {
			s.markClusterError(cluster, getErr)
			respondError(c, http.StatusBadRequest, getErr.Error())
			return
		}
		object.SetResourceVersion(existing.GetResourceVersion())
	}

	updated, err := resourceInterface.Update(c.Request.Context(), object, metav1.UpdateOptions{})
	if err != nil {
		s.markClusterError(cluster, err)
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	respondData(c, http.StatusOK, updated.Object)
}

func (s *Server) deleteResource(c *gin.Context) {
	cluster, runtime, definition, namespace, ok := s.loadResourceContext(c)
	if !ok {
		return
	}

	name, resourceInterface, ok := s.resourceTarget(c, runtime, definition, namespace)
	if !ok {
		return
	}

	propagationPolicy := metav1.DeletePropagationBackground
	if err := resourceInterface.Delete(c.Request.Context(), name, metav1.DeleteOptions{
		PropagationPolicy: &propagationPolicy,
	}); err != nil {
		s.markClusterError(cluster, err)
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	respondNoContent(c)
}

func (s *Server) loadResourceContext(c *gin.Context) (*model.Cluster, *kube.Runtime, kube.ResourceDefinition, string, bool) {
	cluster, err := s.loadClusterFromParam(c)
	if err != nil {
		return nil, nil, kube.ResourceDefinition{}, "", false
	}

	definition, ok := kube.LookupResource(c.Param("resourceType"))
	if !ok {
		respondError(c, http.StatusBadRequest, "unsupported resource type")
		return nil, nil, kube.ResourceDefinition{}, "", false
	}

	runtime, err := s.kubeFactory.Runtime(cluster)
	if err != nil {
		s.markClusterError(cluster, err)
		respondError(c, http.StatusBadRequest, err.Error())
		return nil, nil, kube.ResourceDefinition{}, "", false
	}

	namespace := strings.TrimSpace(c.Query("namespace"))
	return cluster, runtime, definition, namespace, true
}

func (s *Server) resourceTarget(c *gin.Context, runtime *kube.Runtime, definition kube.ResourceDefinition, namespace string) (string, dynamic.ResourceInterface, bool) {
	name := c.Param("name")
	if definition.Namespaced {
		if strings.TrimSpace(namespace) == "" {
			respondError(c, http.StatusBadRequest, "namespace is required for this resource type")
			return "", nil, false
		}

		return name, runtime.Dynamic.Resource(definition.GVR).Namespace(namespace), true
	}

	return name, runtime.Dynamic.Resource(definition.GVR), true
}

func buildResourceObject(definition kube.ResourceDefinition, namespace, manifest string) (*unstructured.Unstructured, error) {
	documents, err := buildResourceObjects(definition, namespace, manifest)
	if err != nil {
		return nil, err
	}
	if len(documents) != 1 {
		return nil, fmt.Errorf("当前操作仅支持单个资源对象")
	}

	return documents[0].Object, nil
}

type resourceDocument struct {
	Definition kube.ResourceDefinition
	Object     *unstructured.Unstructured
}

type createdResourceRef struct {
	Definition kube.ResourceDefinition
	Name       string
	Namespace  string
}

func buildResourceObjects(definition kube.ResourceDefinition, namespace, manifest string) ([]resourceDocument, error) {
	decoder := utilyaml.NewYAMLOrJSONDecoder(strings.NewReader(manifest), 4096)
	result := make([]resourceDocument, 0, 1)

	for {
		raw := map[string]any{}
		if err := decoder.Decode(&raw); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if len(raw) == 0 {
			continue
		}

		documentDefinition, object, err := buildResourceObjectDocument(definition, namespace, raw)
		if err != nil {
			return nil, err
		}

		result = append(result, resourceDocument{
			Definition: documentDefinition,
			Object:     object,
		})
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("manifest 中没有可创建的资源对象")
	}

	return result, nil
}

func buildResourceObjectDocument(
	defaultDefinition kube.ResourceDefinition,
	namespace string,
	raw map[string]any,
) (kube.ResourceDefinition, *unstructured.Unstructured, error) {
	object := &unstructured.Unstructured{Object: raw}
	if object.GetAPIVersion() == "" {
		object.SetAPIVersion(defaultDefinition.APIVersion)
	}
	if object.GetKind() == "" {
		object.SetKind(defaultDefinition.Kind)
	}

	documentDefinition, ok := kube.LookupResourceByAPIVersionKind(
		object.GetAPIVersion(),
		object.GetKind(),
	)
	if !ok {
		return kube.ResourceDefinition{}, nil, fmt.Errorf(
			"暂不支持的资源类型：%s / %s",
			object.GetAPIVersion(),
			object.GetKind(),
		)
	}
	if strings.TrimSpace(object.GetName()) == "" {
		return kube.ResourceDefinition{}, nil, fmt.Errorf("metadata.name is required")
	}
	if documentDefinition.Namespaced {
		if strings.TrimSpace(namespace) != "" {
			object.SetNamespace(namespace)
		}
		if strings.TrimSpace(object.GetNamespace()) == "" {
			return kube.ResourceDefinition{}, nil, fmt.Errorf("metadata.namespace or query namespace is required")
		}
	} else {
		object.SetNamespace("")
	}

	unstructured.RemoveNestedField(object.Object, "status")
	return documentDefinition, object, nil
}

func rollbackCreatedResources(ctx context.Context, runtime *kube.Runtime, refs []createdResourceRef) {
	if len(refs) == 0 {
		return
	}

	propagationPolicy := metav1.DeletePropagationBackground
	for index := len(refs) - 1; index >= 0; index -= 1 {
		ref := refs[index]
		if ref.Definition.Namespaced {
			_ = runtime.Dynamic.Resource(ref.Definition.GVR).
				Namespace(ref.Namespace).
				Delete(ctx, ref.Name, metav1.DeleteOptions{
					PropagationPolicy: &propagationPolicy,
				})
			continue
		}
		_ = runtime.Dynamic.Resource(ref.Definition.GVR).Delete(ctx, ref.Name, metav1.DeleteOptions{
			PropagationPolicy: &propagationPolicy,
		})
	}
}

func serializeUnstructuredList(items []unstructured.Unstructured, search string) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	search = strings.ToLower(strings.TrimSpace(search))

	for _, item := range items {
		if search != "" && !strings.Contains(strings.ToLower(item.GetName()), search) {
			continue
		}
		result = append(result, item.Object)
	}

	return result
}
