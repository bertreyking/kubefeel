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

export type Session = {
  token: string
  user: User
}
