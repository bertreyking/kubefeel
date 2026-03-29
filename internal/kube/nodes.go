package kube

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

func SetWorkerNodesSchedulable(ctx context.Context, runtime *Runtime, schedulable bool) (int, error) {
	nodes, err := runtime.Dynamic.Resource(nodeGVR).List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, err
	}

	updated := 0
	targetUnschedulable := !schedulable
	for index := range nodes.Items {
		node := nodes.Items[index]
		if !isWorkerNode(node.Object) {
			continue
		}

		currentUnschedulable, found, nestedErr := unstructured.NestedBool(
			node.Object,
			"spec",
			"unschedulable",
		)
		if nestedErr == nil && found && currentUnschedulable == targetUnschedulable {
			continue
		}

		if err := unstructured.SetNestedField(node.Object, targetUnschedulable, "spec", "unschedulable"); err != nil {
			return updated, err
		}

		patch := []byte(fmt.Sprintf(`{"spec":{"unschedulable":%t}}`, targetUnschedulable))
		if _, err := runtime.Dynamic.Resource(nodeGVR).Patch(
			ctx,
			node.GetName(),
			types.MergePatchType,
			patch,
			metav1.PatchOptions{},
		); err != nil {
			return updated, err
		}

		updated++
	}

	return updated, nil
}

func isWorkerNode(object map[string]any) bool {
	labels, found, err := unstructured.NestedStringMap(object, "metadata", "labels")
	if err != nil || !found {
		return true
	}

	if _, ok := labels["node-role.kubernetes.io/control-plane"]; ok {
		return false
	}

	if _, ok := labels["node-role.kubernetes.io/master"]; ok {
		return false
	}

	return true
}
