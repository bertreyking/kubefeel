package api

import (
	"strings"
	"testing"

	"multikube-manager/internal/kube"
)

func TestBuildResourceObjectsWithMultiDocumentManifest(t *testing.T) {
	definition, ok := kube.LookupResource("deployment")
	if !ok {
		t.Fatalf("deployment definition not found")
	}

	manifest := strings.TrimSpace(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: demo
spec:
  selector:
    matchLabels:
      app: demo
  template:
    metadata:
      labels:
        app: demo
    spec:
      containers:
        - name: app
          image: nginx:1.27
---
apiVersion: v1
kind: Service
metadata:
  name: demo
spec:
  selector:
    app: demo
  ports:
    - port: 80
      targetPort: 80
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: demo-data
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 10Gi
`)

	documents, err := buildResourceObjects(definition, "default", manifest)
	if err != nil {
		t.Fatalf("build resource objects: %v", err)
	}
	if len(documents) != 3 {
		t.Fatalf("expected 3 documents, got %d", len(documents))
	}
	if documents[1].Definition.Kind != "Service" {
		t.Fatalf("expected second document to be Service, got %s", documents[1].Definition.Kind)
	}
	if documents[2].Object.GetNamespace() != "default" {
		t.Fatalf("expected namespace to be default, got %s", documents[2].Object.GetNamespace())
	}
}
