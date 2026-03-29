package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

const (
	DefaultKubesprayImage = "quay.io/kubespray/kubespray:v2.29.0"
)

type Runner struct {
	rootDir  string
	image    string
	platform string
}

type ClusterRequest struct {
	Name                string
	Region              string
	Description         string
	Mode                string
	ProvisionTemplate   string
	APIServerEndpoint   string
	KubernetesVersion   string
	ImageRegistryPreset string
	ImageRegistry       string
	NetworkPlugin       string
	SSHUser             string
	SSHPort             int
	SSHPrivateKey       string
	Nodes               []NodeSpec
}

type NodeSpec struct {
	Name            string
	Address         string
	InternalAddress string
	Role            string
}

type Result struct {
	Kubeconfig string
}

type PrecheckResult struct {
	Ready   bool           `json:"ready"`
	Summary string         `json:"summary"`
	Checks  []PrecheckItem `json:"checks"`
	Nodes   []PrecheckNode `json:"nodes"`
}

type PrecheckItem struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type PrecheckNode struct {
	Name    string         `json:"name"`
	Address string         `json:"address"`
	Role    string         `json:"role"`
	Status  string         `json:"status"`
	Checks  []PrecheckItem `json:"checks"`
}

type JobPaths struct {
	RootDir        string
	InventoryDir   string
	InventoryPath  string
	ExtraVarsPath  string
	ArtifactsDir   string
	ArtifactConfig string
	SSHDir         string
	SSHKeyPath     string
	LogPath        string
}

type extraVars struct {
	KubeVersion                    string                 `json:"kube_version,omitempty"`
	KubeVersionMinRequired         string                 `json:"kube_version_min_required,omitempty"`
	KubeNetworkPlugin              string                 `json:"kube_network_plugin,omitempty"`
	KubeImageRepo                  string                 `json:"kube_image_repo,omitempty"`
	GCRImageRepo                   string                 `json:"gcr_image_repo,omitempty"`
	DockerImageRepo                string                 `json:"docker_image_repo,omitempty"`
	QuayImageRepo                  string                 `json:"quay_image_repo,omitempty"`
	GithubImageRepo                string                 `json:"github_image_repo,omitempty"`
	KubeconfigLocalhost            bool                   `json:"kubeconfig_localhost"`
	KubeconfigLocalhostAnsibleHost bool                   `json:"kubeconfig_localhost_ansible_host"`
	KubectlLocalhost               bool                   `json:"kubectl_localhost"`
	LoadBalancerAPIServer          *loadBalancerAPIServer `json:"loadbalancer_apiserver,omitempty"`
	SupplementarySSLAddresses      []string               `json:"supplementary_addresses_in_ssl_keys,omitempty"`
}

type loadBalancerAPIServer struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
}

func NewRunner(rootDir, image, platform string) *Runner {
	image = strings.TrimSpace(image)
	if image == "" {
		image = DefaultKubesprayImage
	}

	return &Runner{
		rootDir:  rootDir,
		image:    image,
		platform: strings.TrimSpace(platform),
	}
}

func (r *Runner) Validate(req ClusterRequest) error {
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("请输入集群名称")
	}
	if strings.TrimSpace(req.Region) == "" {
		return fmt.Errorf("请输入所属区域")
	}
	if strings.TrimSpace(req.APIServerEndpoint) == "" {
		return fmt.Errorf("请输入 API 入口地址")
	}
	_, host, _, err := normalizeAPIServerEndpoint(req.APIServerEndpoint)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip == nil {
		return fmt.Errorf("API 入口地址目前仅支持 IP，例如 https://10.0.0.10:6443")
	}
	if strings.TrimSpace(req.SSHUser) == "" {
		return fmt.Errorf("请输入 SSH 用户名")
	}
	if strings.TrimSpace(req.SSHPrivateKey) == "" {
		return fmt.Errorf("请粘贴 SSH 私钥")
	}
	if strings.TrimSpace(req.KubernetesVersion) == "" {
		return fmt.Errorf("请输入 Kubernetes 版本")
	}
	if _, _, err := ResolveProvisionTemplate(req.ProvisionTemplate, req.KubernetesVersion); err != nil {
		return err
	}
	if _, _, err := buildImageRepositoryOverrides(req.ImageRegistryPreset, req.ImageRegistry); err != nil {
		return err
	}
	if req.SSHPort <= 0 {
		return fmt.Errorf("SSH 端口无效")
	}
	if len(req.Nodes) == 0 {
		return fmt.Errorf("至少需要配置一个节点")
	}

	controlPlaneCount := 0
	workerCount := 0
	seen := map[string]struct{}{}
	seenAddress := map[string]struct{}{}

	for index, node := range req.Nodes {
		name := strings.TrimSpace(node.Name)
		if name == "" {
			return fmt.Errorf("第 %d 个节点缺少名称", index+1)
		}
		if _, exists := seen[strings.ToLower(name)]; exists {
			return fmt.Errorf("节点名称 %q 重复", name)
		}
		seen[strings.ToLower(name)] = struct{}{}

		address := strings.TrimSpace(node.Address)
		if address == "" {
			return fmt.Errorf("节点 %q 缺少 SSH 地址", name)
		}
		normalizedAddress := strings.ToLower(address)
		if _, exists := seenAddress[normalizedAddress]; exists {
			return fmt.Errorf("节点地址 %q 重复", address)
		}
		seenAddress[normalizedAddress] = struct{}{}

		switch normalizeNodeRole(node.Role) {
		case "control-plane":
			controlPlaneCount++
		case "worker":
			workerCount++
		case "control-plane-worker":
			controlPlaneCount++
			workerCount++
		default:
			return fmt.Errorf("节点 %q 的角色无效", name)
		}
	}

	if controlPlaneCount == 0 {
		return fmt.Errorf("至少需要一个控制平面节点")
	}

	return nil
}

func (r *Runner) Precheck(ctx context.Context, req ClusterRequest) (PrecheckResult, error) {
	if err := r.Validate(req); err != nil {
		return PrecheckResult{}, err
	}

	result := PrecheckResult{
		Checks: make([]PrecheckItem, 0, 4),
		Nodes:  make([]PrecheckNode, 0, len(req.Nodes)),
	}

	template, _, err := ResolveProvisionTemplate(req.ProvisionTemplate, req.KubernetesVersion)
	if err != nil {
		return PrecheckResult{}, err
	}

	endpoint, _, _, _ := normalizeAPIServerEndpoint(req.APIServerEndpoint)
	result.Checks = append(result.Checks, PrecheckItem{
		Key:    "api-endpoint",
		Label:  "API 入口",
		Status: "success",
		Detail: endpoint,
	})
	result.Checks = append(result.Checks, kubernetesVersionCheck(template, req.KubernetesVersion))
	result.Checks = append(result.Checks, PrecheckItem{
		Key:    "provision-template",
		Label:  "Kubespray 模板",
		Status: "success",
		Detail: fmt.Sprintf("%s · %s", template.Label, template.KubesprayVersion),
	})
	result.Checks = append(result.Checks, imageRegistryCheck(req.ImageRegistryPreset, req.ImageRegistry))

	controlPlaneCount, workerCount := countNodeRoles(req.Nodes)
	layoutStatus := "success"
	layoutDetail := fmt.Sprintf("控制平面 %d 台，Worker %d 台", controlPlaneCount, workerCount)
	if workerCount == 0 {
		layoutStatus = "warning"
		layoutDetail = fmt.Sprintf("控制平面 %d 台，未单独配置 Worker，将由控制平面承载工作负载", controlPlaneCount)
	}
	result.Checks = append(result.Checks, PrecheckItem{
		Key:    "cluster-layout",
		Label:  "节点角色",
		Status: layoutStatus,
		Detail: layoutDetail,
	})

	dockerCheck := r.checkDockerEnvironment(ctx)
	result.Checks = append(result.Checks, dockerCheck)

	sshDir, sshKeyPath, cleanup, err := r.prepareSSHWorkspace(req)
	if err != nil {
		return PrecheckResult{}, err
	}
	defer cleanup()
	_ = sshDir

	for index, node := range req.Nodes {
		nodeCheck := r.runNodePrecheck(ctx, req, sshKeyPath, node, index)
		result.Nodes = append(result.Nodes, nodeCheck)
	}

	result.Ready = true
	for _, check := range result.Checks {
		if check.Status == "error" {
			result.Ready = false
			break
		}
	}
	if result.Ready {
		for _, node := range result.Nodes {
			if node.Status == "error" {
				result.Ready = false
				break
			}
		}
	}

	result.Summary = summarizePrecheck(result)

	return result, nil
}

func (r *Runner) Run(ctx context.Context, jobID uint, req ClusterRequest) (Result, error) {
	if err := r.Validate(req); err != nil {
		return Result{}, err
	}

	template, _, err := ResolveProvisionTemplate(req.ProvisionTemplate, req.KubernetesVersion)
	if err != nil {
		return Result{}, err
	}

	paths, err := r.prepareJob(jobID, req, template)
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(paths.SSHKeyPath)

	if err := appendLog(
		paths.LogPath,
		fmt.Sprintf(
			"选用 Kubespray 模板：%s，镜像：%s",
			template.Label,
			template.KubesprayImage,
		),
	); err != nil {
		return Result{}, err
	}

	if preset, overrides, err := buildImageRepositoryOverrides(req.ImageRegistryPreset, req.ImageRegistry); err == nil {
		if overrides.DisplayValue != "" {
			if err := appendLog(
				paths.LogPath,
				fmt.Sprintf("镜像源方案：%s，前缀：%s", preset.Label, overrides.DisplayValue),
			); err != nil {
				return Result{}, err
			}
		} else if preset.Key == ImageRegistryPresetUpstream {
			if err := appendLog(paths.LogPath, "镜像源方案：默认上游"); err != nil {
				return Result{}, err
			}
		}
	}

	if err := r.ensureImage(ctx, paths.LogPath, template.KubesprayImage); err != nil {
		return Result{}, err
	}

	if err := r.verifySSHConnectivity(ctx, req, paths); err != nil {
		return Result{}, err
	}

	if err := appendLog(paths.LogPath, fmt.Sprintf("开始执行 Kubespray，作业目录：%s", paths.RootDir)); err != nil {
		return Result{}, err
	}

	commandArgs := r.buildKubesprayCommandArgs(paths, template, req)

	if err := appendLog(paths.LogPath, fmt.Sprintf("执行命令：docker %s", strings.Join(commandArgs, " "))); err != nil {
		return Result{}, err
	}

	logFile, err := os.OpenFile(paths.LogPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return Result{}, err
	}
	defer logFile.Close()

	cmd := exec.CommandContext(ctx, r.dockerBinary(), commandArgs...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Run(); err != nil {
		return Result{}, fmt.Errorf("Kubespray 执行失败，请检查作业日志")
	}

	content, err := os.ReadFile(paths.ArtifactConfig)
	if err != nil {
		return Result{}, fmt.Errorf("Kubespray 已执行完成，但没有生成 admin.conf")
	}

	patched, err := patchKubeconfig(string(content), req.Name, req.APIServerEndpoint)
	if err != nil {
		return Result{}, err
	}

	if err := os.WriteFile(paths.ArtifactConfig, []byte(patched), 0o600); err != nil {
		return Result{}, err
	}

	return Result{Kubeconfig: patched}, nil
}

func (r *Runner) JobPaths(jobID uint) JobPaths {
	rootDir := filepath.Join(r.rootDir, "jobs", fmt.Sprintf("%d", jobID))
	inventoryDir := filepath.Join(rootDir, "inventory")
	artifactsDir := filepath.Join(inventoryDir, "artifacts")
	sshDir := filepath.Join(rootDir, "ssh")

	return JobPaths{
		RootDir:        rootDir,
		InventoryDir:   inventoryDir,
		InventoryPath:  filepath.Join(inventoryDir, "inventory.ini"),
		ExtraVarsPath:  filepath.Join(inventoryDir, "extra-vars.json"),
		ArtifactsDir:   artifactsDir,
		ArtifactConfig: filepath.Join(artifactsDir, "admin.conf"),
		SSHDir:         sshDir,
		SSHKeyPath:     filepath.Join(sshDir, "id_rsa"),
		LogPath:        filepath.Join(rootDir, "run.log"),
	}
}

func (r *Runner) ReadJobLog(jobID uint, limit int) string {
	if limit <= 0 {
		limit = 24000
	}

	content, err := os.ReadFile(r.JobPaths(jobID).LogPath)
	if err != nil {
		return ""
	}

	if len(content) <= limit {
		return string(content)
	}

	return string(content[len(content)-limit:])
}

func (r *Runner) prepareSSHWorkspace(req ClusterRequest) (string, string, func(), error) {
	rootDir := filepath.Join(r.rootDir, "checks")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return "", "", nil, err
	}

	workDir, err := os.MkdirTemp(rootDir, "precheck-*")
	if err != nil {
		return "", "", nil, err
	}

	cleanup := func() {
		_ = os.RemoveAll(workDir)
	}

	sshDir := filepath.Join(workDir, "ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		cleanup()
		return "", "", nil, err
	}

	keyPath := filepath.Join(sshDir, "id_rsa")
	privateKey := strings.TrimSpace(req.SSHPrivateKey)
	if !strings.HasSuffix(privateKey, "\n") {
		privateKey += "\n"
	}

	if err := os.WriteFile(keyPath, []byte(privateKey), 0o600); err != nil {
		cleanup()
		return "", "", nil, err
	}

	return workDir, keyPath, cleanup, nil
}

func (r *Runner) prepareJob(jobID uint, req ClusterRequest, template ProvisionTemplate) (JobPaths, error) {
	paths := r.JobPaths(jobID)
	if err := os.MkdirAll(paths.InventoryDir, 0o755); err != nil {
		return JobPaths{}, err
	}
	if err := os.MkdirAll(paths.SSHDir, 0o700); err != nil {
		return JobPaths{}, err
	}
	if err := os.WriteFile(paths.LogPath, []byte(""), 0o600); err != nil {
		return JobPaths{}, err
	}

	inventory, err := buildInventory(req)
	if err != nil {
		return JobPaths{}, err
	}
	if err := os.WriteFile(paths.InventoryPath, []byte(inventory), 0o644); err != nil {
		return JobPaths{}, err
	}

	payload, err := buildExtraVars(req, template)
	if err != nil {
		return JobPaths{}, err
	}
	rawVars, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return JobPaths{}, err
	}
	if err := os.WriteFile(paths.ExtraVarsPath, rawVars, 0o644); err != nil {
		return JobPaths{}, err
	}

	privateKey := strings.TrimSpace(req.SSHPrivateKey)
	if !strings.HasSuffix(privateKey, "\n") {
		privateKey += "\n"
	}
	if err := os.WriteFile(paths.SSHKeyPath, []byte(privateKey), 0o600); err != nil {
		return JobPaths{}, err
	}

	return paths, nil
}

func (r *Runner) Catalog() []ProvisionTemplate {
	return BuiltinProvisionTemplates()
}

func (r *Runner) buildKubesprayCommandArgs(
	paths JobPaths,
	template ProvisionTemplate,
	req ClusterRequest,
) []string {
	commandArgs := []string{"run", "--rm"}
	if r.platform != "" {
		commandArgs = append(commandArgs, "--platform", r.platform)
	}

	commandArgs = append(
		commandArgs,
		"-v", fmt.Sprintf("%s:/inventory", paths.InventoryDir),
		"-v", fmt.Sprintf("%s:/workspace/ssh:ro", paths.SSHDir),
		"-w", "/kubespray",
		"-e", "ANSIBLE_HOST_KEY_CHECKING=False",
		"-e", "ANSIBLE_CONFIG=/kubespray/ansible.cfg",
		template.KubesprayImage,
		"ansible-playbook",
		"-i", "/inventory/inventory.ini",
		"cluster.yml",
		"-b",
		"-u", strings.TrimSpace(req.SSHUser),
		"--private-key", "/workspace/ssh/id_rsa",
		"--ssh-common-args", "-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null",
		"-e", "@/inventory/extra-vars.json",
	)

	return commandArgs
}

func (r *Runner) ensureImage(ctx context.Context, logPath string, image string) error {
	if err := appendLog(logPath, fmt.Sprintf("检查 Kubespray 镜像：%s", image)); err != nil {
		return err
	}

	inspect := exec.CommandContext(ctx, r.dockerBinary(), "image", "inspect", image)
	if err := inspect.Run(); err == nil {
		return nil
	}

	if err := appendLog(logPath, "镜像不存在，开始拉取"); err != nil {
		return err
	}

	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()

	pull := exec.CommandContext(ctx, r.dockerBinary(), "pull", image)
	pull.Stdout = logFile
	pull.Stderr = logFile
	if err := pull.Run(); err != nil {
		return fmt.Errorf("拉取 Kubespray 镜像失败")
	}

	return nil
}

func (r *Runner) checkDockerEnvironment(ctx context.Context) PrecheckItem {
	checkCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	cmd := exec.CommandContext(checkCtx, r.dockerBinary(), "version", "--format", "{{.Server.Version}}")
	output, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(output))
	if err != nil {
		return PrecheckItem{
			Key:    "docker",
			Label:  "执行环境",
			Status: "error",
			Detail: classifyDockerFailure(trimmed, err),
		}
	}

	detail := "Docker 服务可用"
	if trimmed != "" {
		detail = fmt.Sprintf("Docker / OrbStack 已就绪，Server %s", trimmed)
	}

	return PrecheckItem{
		Key:    "docker",
		Label:  "执行环境",
		Status: "success",
		Detail: detail,
	}
}

func (r *Runner) verifySSHConnectivity(ctx context.Context, req ClusterRequest, paths JobPaths) error {
	if err := appendLog(paths.LogPath, "开始执行 SSH 预检查"); err != nil {
		return err
	}

	for _, node := range req.Nodes {
		nodeName := normalizeNodeName(node.Name, 0)
		address := strings.TrimSpace(node.Address)
		if nodeName == "" {
			nodeName = address
		}

		if err := appendLog(paths.LogPath, fmt.Sprintf("检查节点 %s 的 SSH 连通性：%s@%s:%d", nodeName, req.SSHUser, address, req.SSHPort)); err != nil {
			return err
		}

		checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := r.runSSHCheck(checkCtx, req, paths, address)
		cancel()
		if err != nil {
			_ = appendLog(paths.LogPath, fmt.Sprintf("SSH 预检查失败：%s", err.Error()))
			return err
		}

		if err := appendLog(paths.LogPath, fmt.Sprintf("节点 %s SSH 预检查通过", nodeName)); err != nil {
			return err
		}
	}

	return nil
}

func (r *Runner) runSSHCheck(ctx context.Context, req ClusterRequest, paths JobPaths, address string) error {
	output, err := r.runSSHScript(ctx, req, paths.SSHKeyPath, address, `
uid="$(id -u 2>/dev/null || echo unknown)"
if [ "$uid" != "0" ]; then
  sudo -n true >/dev/null 2>&1
fi
echo KUBEFEEL_SSH_OK
`)
	if err == nil && strings.Contains(output, "KUBEFEEL_SSH_OK") {
		return nil
	}
	return classifySSHFailure(address, output, err)
}

func (r *Runner) runNodePrecheck(
	ctx context.Context,
	req ClusterRequest,
	sshKeyPath string,
	node NodeSpec,
	index int,
) PrecheckNode {
	name := normalizeNodeName(node.Name, index+1)
	address := strings.TrimSpace(node.Address)
	role := normalizeNodeRole(node.Role)
	result := PrecheckNode{
		Name:    name,
		Address: address,
		Role:    role,
		Status:  "success",
		Checks:  make([]PrecheckItem, 0, 4),
	}

	checkCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	output, err := r.runSSHScript(checkCtx, req, sshKeyPath, address, `
echo 'KUBEFEEL_PRECHECK|ssh|success|SSH 可达'
uid="$(id -u 2>/dev/null || echo unknown)"
if [ "$uid" = "0" ]; then
  echo 'KUBEFEEL_PRECHECK|sudo|success|当前使用 root 登录'
else
  if sudo -n true >/dev/null 2>&1; then
    echo 'KUBEFEEL_PRECHECK|sudo|success|免密 sudo 已就绪'
  else
    echo 'KUBEFEEL_PRECHECK|sudo|error|当前用户缺少免密 sudo'
  fi
fi
pybin="$(command -v python3 || command -v python || true)"
if [ -n "$pybin" ]; then
  echo "KUBEFEEL_PRECHECK|python|success|${pybin}"
else
  echo 'KUBEFEEL_PRECHECK|python|error|未找到 python3 / python'
fi
if [ -r /etc/os-release ]; then
  os_name="$(awk -F= '/^PRETTY_NAME=/{gsub(/"/,"",$2); print $2; exit}' /etc/os-release)"
  if [ -n "$os_name" ]; then
    echo "KUBEFEEL_PRECHECK|os-release|success|${os_name}"
  else
    echo 'KUBEFEEL_PRECHECK|os-release|success|/etc/os-release 可读取'
  fi
else
  echo 'KUBEFEEL_PRECHECK|os-release|error|/etc/os-release 不可读取'
fi
`)
	if err != nil {
		result.Status = "error"
		result.Checks = append(result.Checks, PrecheckItem{
			Key:    "ssh",
			Label:  precheckLabel("ssh"),
			Status: "error",
			Detail: classifySSHFailure(address, output, err).Error(),
		})
		return result
	}

	result.Checks = parseNodePrecheckOutput(output)
	if len(result.Checks) == 0 {
		result.Status = "error"
		result.Checks = append(result.Checks, PrecheckItem{
			Key:    "ssh",
			Label:  precheckLabel("ssh"),
			Status: "error",
			Detail: "节点预检查未返回任何结果",
		})
		return result
	}

	for _, check := range result.Checks {
		if check.Status == "error" {
			result.Status = "error"
			return result
		}
	}

	return result
}

func (r *Runner) runSSHScript(
	ctx context.Context,
	req ClusterRequest,
	sshKeyPath string,
	address string,
	script string,
) (string, error) {
	args := []string{
		"-i", sshKeyPath,
		"-p", fmt.Sprintf("%d", req.SSHPort),
		"-o", "BatchMode=yes",
		"-o", "PreferredAuthentications=publickey",
		"-o", "PasswordAuthentication=no",
		"-o", "KbdInteractiveAuthentication=no",
		"-o", "ConnectTimeout=10",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("%s@%s", strings.TrimSpace(req.SSHUser), strings.TrimSpace(address)),
		"sh", "-s",
	}

	cmd := exec.CommandContext(ctx, r.sshBinary(), args...)
	cmd.Stdin = strings.NewReader(strings.TrimSpace(script) + "\n")
	output, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(output)), err
}

func classifyDockerFailure(output string, err error) string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" && err != nil {
		trimmed = err.Error()
	}

	switch {
	case strings.Contains(trimmed, "Cannot connect to the Docker daemon"):
		return "本机无法连接 Docker / OrbStack，请先确认容器运行时已经启动"
	case strings.Contains(trimmed, "command not found"), strings.Contains(trimmed, "executable file not found"):
		return "本机未检测到 Docker，请先安装并启动 OrbStack / Docker Desktop"
	default:
		if trimmed == "" {
			return "本机 Docker 环境检查失败"
		}
		return fmt.Sprintf("本机 Docker 环境检查失败：%s", trimmed)
	}
}

func classifySSHFailure(address, output string, err error) error {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" && err != nil {
		trimmed = err.Error()
	}

	switch {
	case strings.Contains(trimmed, "Permission denied"):
		return fmt.Errorf("节点 %s SSH 认证失败，请确认 SSH 用户、私钥和目标机上的 authorized_keys 是否匹配", address)
	case strings.Contains(trimmed, "invalid format"):
		return fmt.Errorf("SSH 私钥格式无效，请粘贴完整的 id_ed25519 或 id_rsa 私钥内容")
	case strings.Contains(trimmed, "Connection timed out"), strings.Contains(trimmed, "Operation timed out"):
		return fmt.Errorf("节点 %s SSH 连接超时，请确认网络和安全组已放通", address)
	case strings.Contains(trimmed, "No route to host"):
		return fmt.Errorf("节点 %s 当前不可达，请确认路由和网络配置", address)
	case strings.Contains(trimmed, "Could not resolve hostname"):
		return fmt.Errorf("节点 %s 地址无法解析，请确认节点地址填写正确", address)
	case strings.Contains(trimmed, "Connection refused"):
		return fmt.Errorf("节点 %s SSH 端口拒绝连接，请确认 sshd 已启动并放通端口", address)
	case strings.Contains(trimmed, "sudo:"), strings.Contains(trimmed, "a password is required"):
		return fmt.Errorf("节点 %s SSH 可达，但当前用户缺少免密 sudo 权限", address)
	default:
		if trimmed == "" {
			return fmt.Errorf("节点 %s SSH 预检查失败", address)
		}
		return fmt.Errorf("节点 %s SSH 预检查失败：%s", address, trimmed)
	}
}

func parseNodePrecheckOutput(output string) []PrecheckItem {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	items := make([]PrecheckItem, 0, len(lines))

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "KUBEFEEL_PRECHECK|") {
			continue
		}

		parts := strings.SplitN(trimmed, "|", 4)
		if len(parts) != 4 {
			continue
		}

		key := strings.TrimSpace(parts[1])
		status := strings.TrimSpace(parts[2])
		detail := strings.TrimSpace(parts[3])
		if key == "" || status == "" {
			continue
		}

		items = append(items, PrecheckItem{
			Key:    key,
			Label:  precheckLabel(key),
			Status: normalizePrecheckStatus(status),
			Detail: detail,
		})
	}

	return items
}

func summarizePrecheck(result PrecheckResult) string {
	totalNodes := len(result.Nodes)
	successNodes := 0
	errorNodes := 0
	for _, node := range result.Nodes {
		if node.Status == "error" {
			errorNodes++
			continue
		}
		successNodes++
	}

	if result.Ready {
		return fmt.Sprintf("预检查通过，%d 台节点全部可用，可以提交创建任务。", totalNodes)
	}

	if totalNodes == 0 {
		return "预检查未通过，请先修正必填信息或执行环境。"
	}

	return fmt.Sprintf("预检查未通过，%d 台节点通过，%d 台节点异常，请先处理后再提交。", successNodes, errorNodes)
}

func kubernetesVersionCheck(template ProvisionTemplate, raw string) PrecheckItem {
	value := normalizeKubernetesVersion(raw)
	if value == "" {
		return PrecheckItem{
			Key:    "kubernetes-version",
			Label:  "Kubernetes 版本",
			Status: "warning",
			Detail: fmt.Sprintf(
				"未指定版本，将使用 %s 默认版本，建议填写 %s",
				template.KubesprayVersion,
				template.VersionHint,
			),
		}
	}

	return PrecheckItem{
		Key:    "kubernetes-version",
		Label:  "Kubernetes 版本",
		Status: "success",
		Detail: fmt.Sprintf(
			"%s，已匹配 %s（%s）",
			value,
			template.Label,
			template.KubesprayVersion,
		),
	}
}

func imageRegistryCheck(rawPreset, rawRegistry string) PrecheckItem {
	preset, overrides, err := buildImageRepositoryOverrides(rawPreset, rawRegistry)
	if err != nil {
		return PrecheckItem{
			Key:    "image-registry",
			Label:  "镜像仓库",
			Status: "error",
			Detail: err.Error(),
		}
	}

	if preset.Key == ImageRegistryPresetUpstream {
		return PrecheckItem{
			Key:    "image-registry",
			Label:  "镜像仓库",
			Status: "warning",
			Detail: "未指定镜像源方案，将使用 Kubespray 默认上游仓库",
		}
	}

	return PrecheckItem{
		Key:    "image-registry",
		Label:  "镜像仓库",
		Status: "success",
		Detail: fmt.Sprintf("已切换到 %s：%s", preset.Label, overrides.DisplayValue),
	}
}

func formatTemplateKubernetesVersion(template ProvisionTemplate, raw string) string {
	value := normalizeKubernetesVersion(raw)
	if value == "" {
		return ""
	}
	if template.LegacyVersionPrefix {
		return "v" + value
	}
	return value
}

func countNodeRoles(nodes []NodeSpec) (int, int) {
	controlPlaneCount := 0
	workerCount := 0

	for _, node := range nodes {
		switch normalizeNodeRole(node.Role) {
		case "control-plane":
			controlPlaneCount++
		case "worker":
			workerCount++
		case "control-plane-worker":
			controlPlaneCount++
			workerCount++
		}
	}

	return controlPlaneCount, workerCount
}

func precheckLabel(key string) string {
	switch key {
	case "api-endpoint":
		return "API 入口"
	case "cluster-layout":
		return "节点角色"
	case "kubernetes-version":
		return "Kubernetes 版本"
	case "provision-template":
		return "Kubespray 模板"
	case "image-registry":
		return "镜像仓库"
	case "docker":
		return "执行环境"
	case "ssh":
		return "SSH"
	case "sudo":
		return "sudo"
	case "python":
		return "Python"
	case "os-release":
		return "OS 信息"
	default:
		return key
	}
}

func normalizePrecheckStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "success", "warning", "error":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return "error"
	}
}

func buildInventory(req ClusterRequest) (string, error) {
	controlPlanes := make([]NodeSpec, 0)
	workers := make([]NodeSpec, 0)
	lines := []string{"[all]"}

	for index, node := range req.Nodes {
		name := normalizeNodeName(node.Name, index+1)
		address := strings.TrimSpace(node.Address)
		internalAddress := strings.TrimSpace(node.InternalAddress)
		if internalAddress == "" {
			internalAddress = address
		}

		role := normalizeNodeRole(node.Role)
		hostLine := fmt.Sprintf(
			"%s ansible_host=%s ip=%s access_ip=%s ansible_port=%d",
			name,
			address,
			internalAddress,
			internalAddress,
			req.SSHPort,
		)
		if role == "control-plane" || role == "control-plane-worker" {
			hostLine += fmt.Sprintf(" etcd_member_name=etcd%d", len(controlPlanes)+1)
		}
		lines = append(lines, hostLine)

		switch role {
		case "control-plane":
			controlPlanes = append(controlPlanes, NodeSpec{Name: name})
		case "worker":
			workers = append(workers, NodeSpec{Name: name})
		case "control-plane-worker":
			controlPlanes = append(controlPlanes, NodeSpec{Name: name})
			workers = append(workers, NodeSpec{Name: name})
		default:
			return "", fmt.Errorf("节点 %q 的角色无效", node.Name)
		}
	}

	if len(workers) == 0 {
		workers = append(workers, controlPlanes...)
	}

	lines = append(lines, "", "[kube_control_plane]")
	for _, node := range controlPlanes {
		lines = append(lines, node.Name)
	}

	lines = append(lines, "", "[etcd]")
	for _, node := range controlPlanes {
		lines = append(lines, node.Name)
	}

	lines = append(lines, "", "[kube_node]")
	for _, node := range workers {
		lines = append(lines, node.Name)
	}

	lines = append(lines, "", "[calico_rr]", "", "[k8s_cluster:children]", "kube_control_plane", "kube_node")

	return strings.Join(lines, "\n") + "\n", nil
}

func buildExtraVars(req ClusterRequest, template ProvisionTemplate) (extraVars, error) {
	endpoint, host, port, err := normalizeAPIServerEndpoint(req.APIServerEndpoint)
	if err != nil {
		return extraVars{}, err
	}

	imageRegistry, err := normalizeImageRegistry(req.ImageRegistry)
	if err != nil {
		return extraVars{}, err
	}

	payload := extraVars{
		KubeconfigLocalhost:            true,
		KubeconfigLocalhostAnsibleHost: false,
		KubectlLocalhost:               false,
		KubeNetworkPlugin:              normalizeNetworkPlugin(req.NetworkPlugin),
		KubeVersionMinRequired:         formatTemplateKubernetesVersion(template, template.MinKubernetesVersion),
	}

	if payload.KubeNetworkPlugin == "" {
		payload.KubeNetworkPlugin = "calico"
	}

	if version := normalizeKubernetesVersion(req.KubernetesVersion); version != "" {
		payload.KubeVersion = formatTemplateKubernetesVersion(template, version)
	}

	_, overrides, err := buildImageRepositoryOverrides(req.ImageRegistryPreset, imageRegistry)
	if err != nil {
		return extraVars{}, err
	}

	if overrides.DisplayValue != "" {
		payload.KubeImageRepo = overrides.Kube
		payload.GCRImageRepo = overrides.GCR
		payload.DockerImageRepo = overrides.Docker
		payload.QuayImageRepo = overrides.Quay
		payload.GithubImageRepo = overrides.Github
	}

	if ip := net.ParseIP(host); ip == nil {
		return extraVars{}, fmt.Errorf("API 入口地址目前仅支持 IP，例如 https://10.0.0.10:6443")
	}

	payload.LoadBalancerAPIServer = &loadBalancerAPIServer{
		Address: host,
		Port:    port,
	}
	payload.SupplementarySSLAddresses = []string{host}

	_ = endpoint

	return payload, nil
}

func patchKubeconfig(raw, clusterName, endpoint string) (string, error) {
	config, err := clientcmd.Load([]byte(raw))
	if err != nil {
		return "", fmt.Errorf("解析 Kubespray 生成的 kubeconfig 失败")
	}

	context := config.Contexts[config.CurrentContext]
	if context == nil {
		return "", fmt.Errorf("Kubespray 生成的 kubeconfig 缺少当前上下文")
	}
	clusterInfo := config.Clusters[context.Cluster]
	if clusterInfo == nil {
		return "", fmt.Errorf("Kubespray 生成的 kubeconfig 缺少 cluster 配置")
	}
	authInfo := config.AuthInfos[context.AuthInfo]
	if authInfo == nil {
		return "", fmt.Errorf("Kubespray 生成的 kubeconfig 缺少认证信息")
	}

	endpoint, _, _, err = normalizeAPIServerEndpoint(endpoint)
	if err != nil {
		return "", err
	}

	normalizedName := normalizeClusterIdentifier(clusterName)
	clusterInfo.Server = endpoint

	username := fmt.Sprintf("kubernetes-admin-%s", normalizedName)
	contextName := fmt.Sprintf("%s@%s", username, normalizedName)

	next := clientcmdapi.NewConfig()
	next.Clusters[normalizedName] = clusterInfo.DeepCopy()
	next.AuthInfos[username] = authInfo.DeepCopy()
	next.Contexts[contextName] = &clientcmdapi.Context{
		Cluster:  normalizedName,
		AuthInfo: username,
	}
	next.CurrentContext = contextName

	content, err := clientcmd.Write(*next)
	if err != nil {
		return "", fmt.Errorf("重写 kubeconfig 失败")
	}

	return string(content), nil
}

func normalizeAPIServerEndpoint(raw string) (string, string, int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", "", 0, fmt.Errorf("请输入 API 入口地址")
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}

	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return "", "", 0, fmt.Errorf("API 入口地址格式不正确")
	}
	if parsed.Scheme == "" {
		parsed.Scheme = "https"
	}

	host := parsed.Hostname()
	if host == "" {
		return "", "", 0, fmt.Errorf("API 入口地址缺少主机名")
	}

	port := 6443
	if parsed.Port() != "" {
		if _, err := fmt.Sscanf(parsed.Port(), "%d", &port); err != nil {
			return "", "", 0, fmt.Errorf("API 入口端口格式不正确")
		}
	}

	parsed.Path = ""
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""

	return fmt.Sprintf("%s://%s:%d", parsed.Scheme, host, port), host, port, nil
}

func normalizeNetworkPlugin(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "calico", "cilium", "flannel":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return ""
	}
}

func normalizeKubernetesVersion(raw string) string {
	return strings.TrimPrefix(strings.TrimSpace(raw), "v")
}

func normalizeImageRegistry(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}

	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Host == "" {
			return "", fmt.Errorf("镜像仓库地址格式不正确")
		}
		value = parsed.Host + "/" + strings.Trim(parsed.Path, "/")
	}

	value = strings.Trim(value, "/")
	value = strings.TrimSuffix(value, "/")
	value = strings.Trim(strings.ReplaceAll(value, "//", "/"), "/")
	if value == "" {
		return "", fmt.Errorf("镜像仓库地址格式不正确")
	}
	if strings.ContainsAny(value, " \t\r\n") {
		return "", fmt.Errorf("镜像仓库地址不能包含空格")
	}

	return value, nil
}

func normalizeNodeRole(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "control-plane", "control_plane", "controlplane":
		return "control-plane"
	case "worker", "node":
		return "worker"
	case "control-plane-worker", "control_plane_worker", "controlplaneworker":
		return "control-plane-worker"
	default:
		return ""
	}
}

func normalizeNodeName(raw string, fallback int) string {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		return fmt.Sprintf("node-%d", fallback)
	}

	replacer := regexp.MustCompile(`[^a-z0-9-]+`)
	value = replacer.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return fmt.Sprintf("node-%d", fallback)
	}

	return value
}

func normalizeClusterIdentifier(raw string) string {
	value := normalizeNodeName(raw, 1)
	if value == "" {
		return "cluster"
	}

	return value
}

func appendLog(path, line string) error {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.WriteString(file, strings.TrimRight(line, "\n")+"\n")
	return err
}

func (r *Runner) dockerBinary() string {
	candidates := []string{
		"/usr/local/bin/docker",
		"/opt/homebrew/bin/docker",
		"docker",
	}

	for _, candidate := range candidates {
		if candidate == "docker" {
			if resolved, err := exec.LookPath(candidate); err == nil {
				return resolved
			}
			continue
		}

		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	return "docker"
}

func (r *Runner) sshBinary() string {
	candidates := []string{
		"/usr/bin/ssh",
		"/opt/homebrew/bin/ssh",
		"ssh",
	}

	for _, candidate := range candidates {
		if candidate == "ssh" {
			if resolved, err := exec.LookPath(candidate); err == nil {
				return resolved
			}
			continue
		}

		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	return "ssh"
}
