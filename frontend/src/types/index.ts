export type Permission = {
  id: number
  key: string
  name: string
  description: string
}

export type Role = {
  id: number
  name: string
  description: string
  builtIn: boolean
  permissions: Permission[]
  createdAt: string
  updatedAt: string
}

export type User = {
  id: number
  username: string
  displayName: string
  active: boolean
  lastLoginAt?: string | null
  roles: Role[]
  permissions: string[]
  createdAt: string
  updatedAt: string
}

export type Cluster = {
  id: number
  name: string
  description: string
  region: string
  nodes?: {
    total: number
    normal: number
    abnormal: number
  } | null
  server: string
  currentContext: string
  version: string
  criVersion: string
  mode: string
  status: string
  lastError: string
  lastConnectedAt?: string | null
  createdAt: string
  updatedAt: string
}

export type ClusterProvisionJob = {
  id: number
  name: string
  region: string
  description: string
  mode: string
  provider: string
  provisionTemplate: string
  kubesprayVersion: string
  kubesprayImage: string
  imageRegistryPreset: string
  imageRegistry: string
  status: string
  step: string
  kubernetesVersion: string
  networkPlugin: string
  apiServerEndpoint: string
  sshUser: string
  controlPlaneCount: number
  workerCount: number
  lastError: string
  resultClusterId?: number | null
  startedAt?: string | null
  completedAt?: string | null
  createdAt: string
  updatedAt: string
  log?: string
}

export type ProvisionTemplate = {
  key: string
  label: string
  description: string
  kubesprayVersion: string
  kubesprayImage: string
  minKubernetesVersion: string
  maxKubernetesVersion: string
  versionHint: string
  recommended: boolean
}

export type ImageRegistryPreset = {
  key: string
  label: string
  description: string
  requiresRegistry: boolean
  placeholder?: string
  defaultRegistry?: string
  recommended: boolean
}

export type RepositoryProvider = {
  key: string
  label: string
  description: string
  endpointHint: string
  namespaceLabel: string
  namespacePlaceholder: string
}

export type RegistryIntegration = {
  id: number
  name: string
  type: string
  description: string
  endpoint: string
  namespace: string
  username: string
  skipTLSVerify: boolean
  status: string
  lastError: string
  lastCheckedAt?: string | null
  createdAt: string
  updatedAt: string
  hasCredential: boolean
  displayAddress: string
}

export type RegistryArtifact = {
  repository: string
  tag: string
  digest?: string
  buildTime?: string | null
}

export type RegistryArtifactVersion = {
  tag: string
  digest?: string
  buildTime?: string | null
}

export type RegistryImage = {
  name: string
  repository: string
  versionCount: number
  latestBuildTime?: string | null
  versions: RegistryArtifactVersion[]
}

export type RegistryImageSpace = {
  name: string
  imageCount: number
  versionCount: number
  images: RegistryImage[]
}

export type RegistryArtifactList = {
  integration: RegistryIntegration
  items: RegistryArtifact[]
  imageSpaces?: RegistryImageSpace[]
  repositoryHint: number
  truncated: boolean
  loadedAt?: string
}

export type ObservabilityKind = {
  key: string
  label: string
  description: string
  endpointHint: string
  dashboardCapable: boolean
  defaultDashboardPath?: string
}

export type ObservabilitySource = {
  id: number
  name: string
  type: string
  description: string
  endpoint: string
  username: string
  dashboardPath: string
  skipTLSVerify: boolean
  status: string
  lastError: string
  lastCheckedAt?: string | null
  createdAt: string
  updatedAt: string
  hasCredential: boolean
  dashboardReady: boolean
}

export type GrafanaDashboardItem = {
  uid: string
  title: string
  url: string
  folderUid: string
  folderTitle: string
  tags: string[]
  isStarred: boolean
}

export type GrafanaDashboardFolder = {
  uid: string
  title: string
  isGeneral: boolean
  dashboardCount: number
  dashboards: GrafanaDashboardItem[]
}

export type GrafanaDashboardCatalog = {
  folders: GrafanaDashboardFolder[]
  folderCount: number
  dashboardCount: number
  loadedAt?: string
}

export type GrafanaDashboardMeta = {
  uid: string
  title: string
  url: string
  folderUid: string
  folderTitle: string
  tags: string[]
  provisioned: boolean
  canSave: boolean
  canDelete: boolean
  definition: string
}

export type ClusterProvisionCheckItem = {
  key: string
  label: string
  status: string
  detail: string
}

export type ClusterProvisionCheckNode = {
  name: string
  address: string
  role: string
  status: string
  checks: ClusterProvisionCheckItem[]
}

export type ClusterProvisionCheckResult = {
  ready: boolean
  summary: string
  checks: ClusterProvisionCheckItem[]
  nodes: ClusterProvisionCheckNode[]
}

export type ClusterInspectionFinding = {
  scope: string
  detail: string
}

export type ClusterInspectionItem = {
  key: string
  label: string
  category: string
  status: string
  summary: string
  detail: string
  findings?: ClusterInspectionFinding[]
}

export type ClusterInspectionReport = {
  inspectedAt: string
  version: string
  overview: {
    nodes: {
      total: number
      normal: number
      abnormal: number
    }
    pods: {
      total: number
      normal: number
      abnormal: number
    }
    cpu: {
      request: string
      total: string
      percentage: number
    }
    memory: {
      request: string
      total: string
      percentage: number
    }
  }
  summary: {
    status: string
    total: number
    passed: number
    warning: number
    failed: number
  }
  items: ClusterInspectionItem[]
}

export type ClusterInspectionSnapshot = {
  cluster: Cluster
  inspection: ClusterInspectionReport
}

export type ResourceDefinition = {
  key: string
  label: string
  kind: string
  apiVersion: string
  namespaced: boolean
}

export type AppTemplate = {
  key: string
  label: string
  description: string
  category: string
  repoURL: string
  chartName: string
  defaultVersion?: string
  releaseNameHint: string
  namespaceHint: string
  values: string
}

export type AppTemplateDeployResult = {
  operation: string
  releaseName: string
  namespace: string
  chart: string
  version: string
  revision: number
  status: string
  notes?: string
}

export type DashboardSummary = {
  clusters: number
  users: number
  roles: number
  defaultKubeconfigPath: string
  clusterMetrics?: {
    id: number
    name: string
    server: string
    currentContext: string
    version: string
    status: string
    lastError: string
    lastConnectedAt?: string | null
    nodes: {
      total: number
      normal: number
      abnormal: number
    }
    pods: {
      total: number
      normal: number
      abnormal: number
    }
    cpu: {
      request: string
      total: string
      percentage: number
    }
    memory: {
      request: string
      total: string
      percentage: number
    }
  } | null
}

export type K8sObject = {
  apiVersion?: string
  kind?: string
  metadata?: {
    uid?: string
    name?: string
    namespace?: string
    creationTimestamp?: string
    generation?: number
    labels?: Record<string, string>
    annotations?: Record<string, string>
    resourceVersion?: string
    managedFields?: Array<{
      manager?: string
      operation?: string
      apiVersion?: string
      time?: string
      subresource?: string
    }>
  }
  [key: string]: unknown
}

export type WorkloadPod = {
  name: string
  namespace: string
  phase: string
  nodeName: string
  podIP: string
  containers: string[]
  readyContainers: number
  totalContainers: number
  restartCount: number
  createdAt: string
}

export type WorkloadRelatedResource = {
  resourceType: string
  kind: string
  name: string
  namespace?: string
  status?: string
  matchReason: string
  summary?: string
}

export type WorkloadRelations = {
  services: WorkloadRelatedResource[]
  ingresses: WorkloadRelatedResource[]
  networkPolicies: WorkloadRelatedResource[]
  persistentVolumeClaims: WorkloadRelatedResource[]
  persistentVolumes: WorkloadRelatedResource[]
}

export type WorkloadHistoryItem = {
  revision: number
  name: string
  createdAt?: string
  current: boolean
  changeCause?: string
  images?: string[]
  summary?: string
}

export type WorkloadHistory = {
  supported: boolean
  rollbackSupported: boolean
  resourceType: string
  items: WorkloadHistoryItem[]
}

export type AutoscalingMetricSnapshot = {
  label: string
  current?: string
  target?: string
}

export type MetricsAutoscalingStatus = {
  supported: boolean
  configured: boolean
  name?: string
  minReplicas: number
  maxReplicas: number
  currentReplicas: number
  desiredReplicas: number
  metrics: AutoscalingMetricSnapshot[]
  lastScaleTime?: string
}

export type AutoscalingTriggerSnapshot = {
  type: string
  metadata?: Record<string, string>
}

export type EventAutoscalingStatus = {
  supported: boolean
  available: boolean
  configured: boolean
  name?: string
  minReplicaCount: number
  maxReplicaCount: number
  pollingInterval: number
  cooldownPeriod: number
  triggers: AutoscalingTriggerSnapshot[]
  lastActiveTime?: string
  originalReplicas: number
}

export type APIAutoscalingStatus = {
  supported: boolean
  available: boolean
  configured: boolean
  name?: string
  class?: string
  metric?: string
  target?: string
  targetUtilizationPercentage: number
  minScale: number
  maxScale: number
  scaleDownDelay?: string
  window?: string
  scaleToZeroRetention?: string
  url?: string
  latestReadyRevision?: string
}

export type WorkloadAutoscaling = {
  supported: boolean
  resourceType: string
  kind: string
  message?: string
  metrics: MetricsAutoscalingStatus
  event: EventAutoscalingStatus
  api: APIAutoscalingStatus
}

export type PodLogsResult = {
  pod: string
  namespace: string
  container: string
  content: string
}

export type PodExecResult = {
  pod: string
  namespace: string
  container: string
  command: string
  stdout: string
  stderr: string
}

export type Session = {
  token: string
  user: User
}
