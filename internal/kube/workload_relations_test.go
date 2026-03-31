package kube

import (
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSelectorMatchesLabels(t *testing.T) {
	if !selectorMatchesLabels(
		map[string]string{"app": "demo"},
		map[string]string{"app": "demo", "tier": "web"},
	) {
		t.Fatalf("expected service selector to match workload labels")
	}

	if selectorMatchesLabels(
		map[string]string{"app": "demo", "tier": "api"},
		map[string]string{"app": "demo", "tier": "web"},
	) {
		t.Fatalf("expected selector mismatch to return false")
	}
}

func TestWorkloadPersistentVolumeClaimsIncludesVolumeTemplates(t *testing.T) {
	directClaims, templateClaims := workloadPersistentVolumeClaims(map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"volumes": []any{
						map[string]any{
							"name": "data",
							"persistentVolumeClaim": map[string]any{
								"claimName": "demo-pvc",
							},
						},
					},
				},
			},
			"volumeClaimTemplates": []any{
				map[string]any{
					"metadata": map[string]any{
						"name": "cache",
					},
				},
			},
		},
	})

	if directClaims["demo-pvc"] == "" {
		t.Fatalf("expected direct pvc claim to be collected")
	}
	if templateClaims["cache"] == "" {
		t.Fatalf("expected volume claim template to be collected")
	}
}

func TestIngressBackendServiceNames(t *testing.T) {
	names := ingressBackendServiceNames(networkingv1.Ingress{
		Spec: networkingv1.IngressSpec{
			DefaultBackend: &networkingv1.IngressBackend{
				Service: &networkingv1.IngressServiceBackend{Name: "gateway"},
			},
			Rules: []networkingv1.IngressRule{
				{
					Host: "demo.example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{Name: "web"},
									},
								},
							},
						},
					},
				},
			},
		},
	})

	if len(names) != 2 || names[0] != "gateway" || names[1] != "web" {
		t.Fatalf("unexpected ingress service names: %#v", names)
	}
}

func TestNetworkPolicyMatchEmptySelectorAppliesToAllPods(t *testing.T) {
	selector, reason, ok := networkPolicyMatch(metav1.LabelSelector{}, map[string]string{"app": "demo"})
	if !ok {
		t.Fatalf("expected empty selector network policy to match")
	}
	if selector == nil || !selector.Empty() {
		t.Fatalf("expected empty selector to match all pods")
	}
	if reason != "" {
		t.Fatalf("expected empty selector to defer reason formatting")
	}
}
