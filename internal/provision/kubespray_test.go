package provision

import (
	"strings"
	"testing"
)

func TestBuildInventory(t *testing.T) {
	request := ClusterRequest{
		SSHPort: 22,
		Nodes: []NodeSpec{
			{Name: "cp-1", Address: "10.0.0.10", InternalAddress: "192.168.0.10", Role: "control-plane"},
			{Name: "worker-1", Address: "10.0.0.11", Role: "worker"},
		},
	}

	inventory, err := buildInventory(request)
	if err != nil {
		t.Fatalf("build inventory: %v", err)
	}

	expectedFragments := []string{
		"[all]",
		"cp-1 ansible_host=10.0.0.10 ip=192.168.0.10 access_ip=192.168.0.10 ansible_port=22 etcd_member_name=etcd1",
		"[kube_control_plane]",
		"cp-1",
		"[kube_node]",
		"worker-1",
		"[etcd]",
	}
	for _, fragment := range expectedFragments {
		if !strings.Contains(inventory, fragment) {
			t.Fatalf("expected fragment %q in inventory:\n%s", fragment, inventory)
		}
	}
}

func TestPatchKubeconfig(t *testing.T) {
	raw := `
apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: Y2E=
    server: https://127.0.0.1:6443
  name: cluster.local
contexts:
- context:
    cluster: cluster.local
    user: kubernetes-admin-cluster.local
  name: kubernetes-admin-cluster.local@cluster.local
current-context: kubernetes-admin-cluster.local@cluster.local
kind: Config
users:
- name: kubernetes-admin-cluster.local
  user:
    client-certificate-data: Y2VydA==
    client-key-data: a2V5
`

	patched, err := patchKubeconfig(raw, "prod-shanghai-01", "https://10.0.0.20:6443")
	if err != nil {
		t.Fatalf("patch kubeconfig: %v", err)
	}

	if !strings.Contains(patched, "server: https://10.0.0.20:6443") {
		t.Fatalf("expected patched server, got:\n%s", patched)
	}
	if !strings.Contains(patched, "name: prod-shanghai-01") {
		t.Fatalf("expected normalized cluster name in kubeconfig, got:\n%s", patched)
	}
	if !strings.Contains(patched, "current-context: kubernetes-admin-prod-shanghai-01@prod-shanghai-01") {
		t.Fatalf("expected patched current context, got:\n%s", patched)
	}
}

func TestValidateRejectsDuplicateNodeAddress(t *testing.T) {
	runner := NewRunner(t.TempDir(), "", "")
	request := ClusterRequest{
		Name:              "prod",
		Region:            "华东 1",
		APIServerEndpoint: "https://10.0.0.10:6443",
		KubernetesVersion: "1.31.8",
		SSHUser:           "ubuntu",
		SSHPort:           22,
		SSHPrivateKey:     "dummy-private-key",
		Nodes: []NodeSpec{
			{Name: "cp-1", Address: "10.0.0.11", Role: "control-plane"},
			{Name: "worker-1", Address: "10.0.0.11", Role: "worker"},
		},
	}

	err := runner.Validate(request)
	if err == nil || !strings.Contains(err.Error(), "节点地址") {
		t.Fatalf("expected duplicate address validation error, got: %v", err)
	}
}

func TestParseNodePrecheckOutput(t *testing.T) {
	output := strings.Join([]string{
		"KUBEFEEL_PRECHECK|ssh|success|SSH 可达",
		"KUBEFEEL_PRECHECK|sudo|error|当前用户缺少免密 sudo",
		"noise line",
	}, "\n")

	items := parseNodePrecheckOutput(output)
	if len(items) != 2 {
		t.Fatalf("expected 2 parsed items, got %d", len(items))
	}
	if items[0].Key != "ssh" || items[0].Status != "success" {
		t.Fatalf("unexpected first item: %#v", items[0])
	}
	if items[1].Label != "sudo" || items[1].Status != "error" {
		t.Fatalf("unexpected second item: %#v", items[1])
	}
}

func TestValidateAllowsLegacyKubernetesVersionWithMatchingTemplate(t *testing.T) {
	runner := NewRunner(t.TempDir(), "", "")
	request := ClusterRequest{
		Name:              "prod",
		Region:            "华东 1",
		APIServerEndpoint: "https://10.0.0.10:6443",
		ProvisionTemplate: "compat-126",
		KubernetesVersion: "1.26.11",
		SSHUser:           "ubuntu",
		SSHPort:           22,
		SSHPrivateKey:     "dummy-private-key",
		Nodes: []NodeSpec{
			{Name: "cp-1", Address: "10.0.0.11", Role: "control-plane"},
		},
	}

	if err := runner.Validate(request); err != nil {
		t.Fatalf("expected legacy template validation to pass, got: %v", err)
	}
}

func TestValidateRejectsMismatchedProvisionTemplate(t *testing.T) {
	runner := NewRunner(t.TempDir(), "", "")
	request := ClusterRequest{
		Name:              "prod",
		Region:            "华东 1",
		APIServerEndpoint: "https://10.0.0.10:6443",
		ProvisionTemplate: "compat-131-132",
		KubernetesVersion: "1.26.11",
		SSHUser:           "ubuntu",
		SSHPort:           22,
		SSHPrivateKey:     "dummy-private-key",
		Nodes: []NodeSpec{
			{Name: "cp-1", Address: "10.0.0.11", Role: "control-plane"},
		},
	}

	err := runner.Validate(request)
	if err == nil || !strings.Contains(err.Error(), "仅支持 Kubernetes") {
		t.Fatalf("expected mismatched template validation error, got: %v", err)
	}
}

func TestResolveProvisionTemplateByVersion(t *testing.T) {
	template, normalizedVersion, err := ResolveProvisionTemplate("", "1.30.6")
	if err != nil {
		t.Fatalf("resolve provision template: %v", err)
	}
	if template.Key != "compat-129-130" {
		t.Fatalf("expected compat-129-130, got %s", template.Key)
	}
	if normalizedVersion != "1.30.6" {
		t.Fatalf("unexpected normalized version: %s", normalizedVersion)
	}
}

func TestBuildExtraVarsWithTemplateAndImageRegistry(t *testing.T) {
	payload, err := buildExtraVars(
		ClusterRequest{
			APIServerEndpoint:   "https://10.0.0.10:6443",
			KubernetesVersion:   "1.26.11",
			ImageRegistryPreset: ImageRegistryPresetCustom,
			ImageRegistry:       "https://harbor.example.com/k8s",
			NetworkPlugin:       "calico",
		},
		ProvisionTemplate{
			MinKubernetesVersion: "1.26.0",
			LegacyVersionPrefix:  true,
		},
	)
	if err != nil {
		t.Fatalf("build extra vars: %v", err)
	}

	if payload.KubeVersion != "v1.26.11" {
		t.Fatalf("unexpected kube version: %s", payload.KubeVersion)
	}
	if payload.KubeVersionMinRequired != "v1.26.0" {
		t.Fatalf("unexpected min required: %s", payload.KubeVersionMinRequired)
	}
	if payload.KubeImageRepo != "harbor.example.com/k8s" {
		t.Fatalf("unexpected kube image repo: %s", payload.KubeImageRepo)
	}
	if payload.QuayImageRepo != "harbor.example.com/k8s" {
		t.Fatalf("unexpected quay image repo: %s", payload.QuayImageRepo)
	}
}

func TestBuildExtraVarsWithDaocloudPreset(t *testing.T) {
	payload, err := buildExtraVars(
		ClusterRequest{
			APIServerEndpoint:   "https://10.0.0.10:6443",
			KubernetesVersion:   "1.31.8",
			ImageRegistryPreset: ImageRegistryPresetDaocloud,
			NetworkPlugin:       "calico",
		},
		ProvisionTemplate{
			MinKubernetesVersion: "1.31.0",
		},
	)
	if err != nil {
		t.Fatalf("build extra vars: %v", err)
	}

	if payload.KubeImageRepo != defaultDaocloudRegistryPrefix {
		t.Fatalf("unexpected kube image repo: %s", payload.KubeImageRepo)
	}
	if payload.QuayImageRepo != defaultDaocloudRegistryPrefix {
		t.Fatalf("unexpected quay image repo: %s", payload.QuayImageRepo)
	}
}

func TestFormatTemplateKubernetesVersion(t *testing.T) {
	if value := formatTemplateKubernetesVersion(ProvisionTemplate{LegacyVersionPrefix: true}, "1.26.11"); value != "v1.26.11" {
		t.Fatalf("unexpected legacy formatted version: %s", value)
	}
	if value := formatTemplateKubernetesVersion(ProvisionTemplate{}, "1.31.8"); value != "1.31.8" {
		t.Fatalf("unexpected modern formatted version: %s", value)
	}
}

func TestBuildKubesprayCommandArgsUsesJsonExtraVars(t *testing.T) {
	runner := NewRunner(t.TempDir(), "", "linux/amd64")
	args := runner.buildKubesprayCommandArgs(
		JobPaths{
			InventoryDir: "/tmp/inventory",
			SSHDir:       "/tmp/ssh",
		},
		ProvisionTemplate{
			KubesprayImage: "quay.io/kubespray/kubespray:v2.24.0",
		},
		ClusterRequest{
			SSHUser: "root",
		},
	)

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "@/inventory/extra-vars.json") {
		t.Fatalf("expected command args to reference extra-vars.json, got: %s", joined)
	}
	if strings.Contains(joined, "@/inventory/extra-vars.yml") {
		t.Fatalf("unexpected legacy extra-vars.yml reference in command args: %s", joined)
	}
}

func TestBuildImageRepositoryOverrides(t *testing.T) {
	preset, overrides, err := buildImageRepositoryOverrides(ImageRegistryPresetAliyunACR, "registry.cn-hangzhou.aliyuncs.com/team-a")
	if err != nil {
		t.Fatalf("build image overrides: %v", err)
	}

	if preset.Key != ImageRegistryPresetAliyunACR {
		t.Fatalf("unexpected preset: %s", preset.Key)
	}
	if overrides.Kube != "registry.cn-hangzhou.aliyuncs.com/team-a" {
		t.Fatalf("unexpected kube override: %s", overrides.Kube)
	}
}
