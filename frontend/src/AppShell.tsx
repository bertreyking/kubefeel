import {
  startTransition,
  useDeferredValue,
  useEffect,
  useEffectEvent,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import {
  Alert,
  App as AntApp,
  Badge,
  Button,
  Checkbox,
  ConfigProvider,
  Descriptions,
  Divider,
  Drawer,
  Empty,
  Form,
  Input,
  Layout,
  Menu,
  Modal,
  Progress,
  Segmented,
  Select,
  Space,
  Spin,
  Steps,
  Switch,
  Table,
  Tag,
  Typography,
  Upload,
} from 'antd'
import type { MenuProps, TableColumnsType } from 'antd'
import {
  ApiOutlined,
  CheckCircleOutlined,
  CloudUploadOutlined,
  ClusterOutlined,
  ControlOutlined,
  CopyOutlined,
  DeleteOutlined,
  DeploymentUnitOutlined,
  EyeOutlined,
  LogoutOutlined,
  PlusOutlined,
  ReloadOutlined,
  SafetyCertificateOutlined,
  SyncOutlined,
  TeamOutlined,
} from '@ant-design/icons'
import dayjs from 'dayjs'
import YAML from 'yaml'

import { HttpError, createApi, postPublic } from './api/client'
import './AppShell.css'
import type {
  Cluster,
  ClusterInspectionSnapshot,
  ClusterProvisionCheckResult,
  ClusterProvisionJob,
  DashboardSummary,
  ImageRegistryPreset,
  K8sObject,
  ObservabilityKind,
  ObservabilitySource,
  Permission,
  RegistryArtifactVersion,
  RegistryImage,
  RegistryArtifactList,
  RegistryImageSpace,
  RegistryIntegration,
  RepositoryProvider,
  ProvisionTemplate,
  ResourceDefinition,
  Role,
  Session,
  User,
} from './types'

const sessionStorageKey = 'kubefeel-session'
const legacySessionStorageKey = 'kubefleet-session'
const defaultProvisionTemplateKey = 'compat-131-132'
const defaultImageRegistryPresetKey = 'upstream'
const platformMarkSrc = `${import.meta.env.BASE_URL}kubefeel-mark.svg`

type ViewKey =
  | 'dashboard'
  | 'clusters'
  | 'inspection'
  | 'resources'
  | 'registryIntegrations'
  | 'registryArtifacts'
  | 'observabilityDashboards'
  | 'observabilitySources'
  | 'users'
  | 'roles'

type NamespaceItem = {
  name: string
  creationTimestamp?: string
}

type ClusterFormValues = {
  name: string
  region: string
  mode: string
  description?: string
  kubeconfig?: string
}

type ClusterProvisionNodeFormValue = {
  name: string
  address: string
  internalAddress?: string
  role: string
}

type ClusterProvisionFormValues = {
  name: string
  region: string
  mode: string
  description?: string
  provisionTemplate: string
  apiServerEndpoint: string
  kubernetesVersion?: string
  imageRegistryPreset: string
  imageRegistry?: string
  networkPlugin: string
  sshUser: string
  sshPort: number
  sshPrivateKey: string
  nodes: ClusterProvisionNodeFormValue[]
}

type UserFormValues = {
  username: string
  displayName: string
  password?: string
  active: boolean
  roleIds: number[]
}

type RoleFormValues = {
  name: string
  description: string
  permissionKeys: string[]
}

type RegistryFormValues = {
  name: string
  type: string
  description?: string
  endpoint: string
  namespace?: string
  username?: string
  secret?: string
  skipTLSVerify: boolean
}

type ObservabilityFormValues = {
  name: string
  type: string
  description?: string
  endpoint: string
  username?: string
  secret?: string
  dashboardPath?: string
  skipTLSVerify: boolean
}

type MenuItemDefinition = {
  key: ViewKey
  label: string
  icon: ReactNode
  permission: string
}

type ClusterConnectionPreview = {
  server: string
  currentContext: string
  version: string
  criVersion: string
}

type KubeconfigPreviewContext = {
  name: string
  cluster: string
  user: string
}

type KubeconfigPreview = {
  valid: boolean
  suggestedName: string
  currentContext: string
  primaryServer: string
  clusterNames: string[]
  users: string[]
  contexts: KubeconfigPreviewContext[]
  clusterCount: number
  userCount: number
  contextCount: number
  error?: string
}

type ClusterDraftSource = 'paste' | 'upload'

type GrafanaWorkspaceSection = 'dashboard' | 'explore'

type ResourceDrawerMode = 'create' | 'edit' | 'clone' | 'inspect'

type ResourceDrawerPanel = 'yaml' | 'diff' | 'audit'

type PermissionGroup = {
  key: string
  label: string
  permissions: Permission[]
}

type PermissionMapping = {
  permission: string
  label: string
  effect: string
  kubernetesScope: string
  note: string
}

type YamlDiffLine = {
  type: 'added' | 'removed' | 'unchanged'
  content: string
  leftNumber?: number
  rightNumber?: number
}

type AuditEntry = {
  title: string
  detail: string
  time?: string
  accent: 'neutral' | 'info' | 'success'
}

type ResourceRiskItem = {
  level: 'low' | 'medium' | 'high'
  title: string
  detail: string
}

type DeploymentConditionInsight = {
  type: string
  status: string
  reason: string
  message: string
  lastUpdateTime?: string
}

type DeploymentInsight = {
  desired: number
  updated: number
  ready: number
  available: number
  unavailable: number
  revision: string
  strategyType: string
  maxSurge: string
  maxUnavailable: string
  rolloutPercent: number
  rolloutLabel: string
  rolloutSummary: string
  rolloutTone: 'success' | 'warning' | 'processing'
  selectorLabels: Record<string, string>
  templateLabels: Record<string, string>
  containers: Array<{
    name: string
    image: string
    imagePullPolicy: string
  }>
  conditions: DeploymentConditionInsight[]
}

const containerMenuKey = 'container-management'
const registryMenuKey = 'registry-management'
const observabilityMenuKey = 'observability-management'

const menuDefinitions: MenuItemDefinition[] = [
  {
    key: 'dashboard',
    label: '工作台',
    icon: <ControlOutlined />,
    permission: 'dashboard:read',
  },
  {
    key: 'clusters',
    label: '集群列表',
    icon: <ClusterOutlined />,
    permission: 'clusters:read',
  },
  {
    key: 'inspection',
    label: '一键巡检',
    icon: <CheckCircleOutlined />,
    permission: 'clusters:read',
  },
  {
    key: 'resources',
    label: '资源中心',
    icon: <DeploymentUnitOutlined />,
    permission: 'resources:read',
  },
  {
    key: 'registryIntegrations',
    label: '仓库集成',
    icon: <CloudUploadOutlined />,
    permission: 'registries:read',
  },
  {
    key: 'registryArtifacts',
    label: '镜像列表',
    icon: <DeploymentUnitOutlined />,
    permission: 'registries:read',
  },
  {
    key: 'observabilityDashboards',
    label: '仪表盘',
    icon: <ControlOutlined />,
    permission: 'observability:read',
  },
  {
    key: 'observabilitySources',
    label: '数据源',
    icon: <ApiOutlined />,
    permission: 'observability:read',
  },
  {
    key: 'users',
    label: '用户管理',
    icon: <TeamOutlined />,
    permission: 'users:read',
  },
  {
    key: 'roles',
    label: '角色权限',
    icon: <SafetyCertificateOutlined />,
    permission: 'roles:read',
  },
]

function RootApp() {
  return (
    <ConfigProvider
      theme={{
        token: {
          colorPrimary: '#0f6c7b',
          colorInfo: '#0f6c7b',
          borderRadius: 18,
          fontFamily: '"IBM Plex Sans", "Segoe UI", sans-serif',
          colorBgBase: '#f3f6f8',
          colorTextBase: '#10202b',
        },
        components: {
          Layout: {
            siderBg: '#0c1720',
            headerBg: '#f3f6f8',
            bodyBg: '#f3f6f8',
          },
          Menu: {
            darkItemBg: '#0c1720',
            darkItemSelectedBg: 'rgba(17, 172, 194, 0.16)',
            darkItemSelectedColor: '#effcff',
            darkItemHoverBg: 'rgba(255, 255, 255, 0.08)',
            darkItemColor: '#93a8b8',
          },
          Table: {
            headerBg: '#edf3f6',
            headerColor: '#10202b',
          },
        },
      }}
    >
      <AntApp>
        <Workspace />
      </AntApp>
    </ConfigProvider>
  )
}

function Workspace() {
  const { message, modal } = AntApp.useApp()

  const [session, setSession] = useState<Session | null>(() => loadSession())
  const [activeView, setActiveView] = useState<ViewKey>('dashboard')
  const [bootstrapping, setBootstrapping] = useState(false)
  const [submittingLogin, setSubmittingLogin] = useState(false)

  const [dashboard, setDashboard] = useState<DashboardSummary | null>(null)
  const [clusters, setClusters] = useState<Cluster[]>([])
  const [roles, setRoles] = useState<Role[]>([])
  const [permissions, setPermissions] = useState<Permission[]>([])
  const [users, setUsers] = useState<User[]>([])
  const [resourceTypes, setResourceTypes] = useState<ResourceDefinition[]>([])
  const [provisionTemplates, setProvisionTemplates] = useState<ProvisionTemplate[]>([])
  const [imageRegistryPresets, setImageRegistryPresets] = useState<ImageRegistryPreset[]>([])
  const [repositoryProviders, setRepositoryProviders] = useState<RepositoryProvider[]>([])
  const [registries, setRegistries] = useState<RegistryIntegration[]>([])
  const [observabilityKinds, setObservabilityKinds] = useState<ObservabilityKind[]>([])
  const [observabilitySources, setObservabilitySources] = useState<ObservabilitySource[]>([])
  const [namespaces, setNamespaces] = useState<NamespaceItem[]>([])
  const [resources, setResources] = useState<K8sObject[]>([])
  const [resourceLoading, setResourceLoading] = useState(false)
  const [clusterInspection, setClusterInspection] = useState<ClusterInspectionSnapshot | null>(null)
  const [clusterInspectionLoading, setClusterInspectionLoading] = useState(false)
  const [registryArtifactCache, setRegistryArtifactCache] = useState<Record<number, RegistryArtifactList>>({})
  const [registryArtifactsSnapshot, setRegistryArtifactsSnapshot] = useState<RegistryArtifactList | null>(
    null,
  )
  const [registryArtifactsLoading, setRegistryArtifactsLoading] = useState(false)

  const [selectedClusterId, setSelectedClusterId] = useState<number>()
  const [selectedResourceType, setSelectedResourceType] = useState('deployment')
  const [selectedNamespace, setSelectedNamespace] = useState('')
  const [resourceSearch, setResourceSearch] = useState('')
  const deferredSearch = useDeferredValue(resourceSearch)

  const [clusterSearch, setClusterSearch] = useState('')
  const [clusterSearchDraft, setClusterSearchDraft] = useState('')
  const [clusterStatusFilter, setClusterStatusFilter] = useState<'all' | 'connected' | 'error'>(
    'all',
  )
  const [clusterModeSavingId, setClusterModeSavingId] = useState<number>()
  const [registrySearch, setRegistrySearch] = useState('')
  const [registryArtifactSearch, setRegistryArtifactSearch] = useState('')
  const deferredRegistryArtifactSearch = useDeferredValue(registryArtifactSearch)
  const [selectedRegistryId, setSelectedRegistryId] = useState<number>()
  const [expandedRegistrySpaceKeys, setExpandedRegistrySpaceKeys] = useState<string[]>([])
  const [expandedRegistryImageKeys, setExpandedRegistryImageKeys] = useState<Record<string, string[]>>({})
  const [observabilitySourceSearch, setObservabilitySourceSearch] = useState('')
  const [selectedGrafanaSourceId, setSelectedGrafanaSourceId] = useState<number>()
  const [grafanaWorkspaceSection, setGrafanaWorkspaceSection] =
    useState<GrafanaWorkspaceSection>('dashboard')
  const [grafanaEmbedError, setGrafanaEmbedError] = useState('')
  const [grafanaFrameNonce, setGrafanaFrameNonce] = useState(0)
  const grafanaFrameRef = useRef<HTMLIFrameElement | null>(null)

  const [userSearch, setUserSearch] = useState('')
  const [userStatusFilter, setUserStatusFilter] = useState<'all' | 'active' | 'disabled'>(
    'all',
  )
  const [selectedUserId, setSelectedUserId] = useState<number>()

  const [roleSearch, setRoleSearch] = useState('')
  const [roleScopeFilter, setRoleScopeFilter] = useState<'all' | 'builtin' | 'custom'>(
    'all',
  )
  const [selectedRoleId, setSelectedRoleId] = useState<number>()

  const [selectedResourceKey, setSelectedResourceKey] = useState('')

  const [clusterDrawerOpen, setClusterDrawerOpen] = useState(false)
  const [editingCluster, setEditingCluster] = useState<Cluster | null>(null)
  const [clusterWizardStep, setClusterWizardStep] = useState(0)
  const [clusterDraftSource, setClusterDraftSource] = useState<ClusterDraftSource>('paste')
  const [clusterKubePreview, setClusterKubePreview] = useState<KubeconfigPreview | null>(null)
  const [clusterConnectionPreview, setClusterConnectionPreview] =
    useState<ClusterConnectionPreview | null>(null)
  const [clusterPreviewLoading, setClusterPreviewLoading] = useState(false)
  const [clusterProvisionDrawerOpen, setClusterProvisionDrawerOpen] = useState(false)
  const [clusterProvisionSubmitting, setClusterProvisionSubmitting] = useState(false)
  const [clusterProvisionCheckLoading, setClusterProvisionCheckLoading] = useState(false)
  const [clusterProvisionJob, setClusterProvisionJob] = useState<ClusterProvisionJob | null>(null)
  const [clusterProvisionDraft, setClusterProvisionDraft] = useState<ClusterProvisionFormValues>(
    () => defaultClusterProvisionFormValues(),
  )
  const [clusterProvisionCheckResult, setClusterProvisionCheckResult] =
    useState<ClusterProvisionCheckResult | null>(null)
  const [clusterProvisionCheckFingerprint, setClusterProvisionCheckFingerprint] = useState('')

  const [userModalOpen, setUserModalOpen] = useState(false)
  const [editingUser, setEditingUser] = useState<User | null>(null)

  const [roleModalOpen, setRoleModalOpen] = useState(false)
  const [editingRole, setEditingRole] = useState<Role | null>(null)

  const [resourceDrawerOpen, setResourceDrawerOpen] = useState(false)
  const [resourceDrawerMode, setResourceDrawerMode] = useState<ResourceDrawerMode>('create')
  const [resourceDrawerPanel, setResourceDrawerPanel] =
    useState<ResourceDrawerPanel>('yaml')
  const [resourceDraft, setResourceDraft] = useState('')
  const [resourceBaselineDraft, setResourceBaselineDraft] = useState('')
  const [editingResource, setEditingResource] = useState<K8sObject | null>(null)
  const [resourceDetailOpen, setResourceDetailOpen] = useState(false)
  const [resourceDetailLoading, setResourceDetailLoading] = useState(false)
  const [resourceDetailResource, setResourceDetailResource] = useState<K8sObject | null>(null)
  const [resourceReviewOpen, setResourceReviewOpen] = useState(false)
  const [resourceSubmitting, setResourceSubmitting] = useState(false)
  const [registryDrawerOpen, setRegistryDrawerOpen] = useState(false)
  const [editingRegistry, setEditingRegistry] = useState<RegistryIntegration | null>(null)
  const [registrySubmitting, setRegistrySubmitting] = useState(false)
  const [observabilityDrawerOpen, setObservabilityDrawerOpen] = useState(false)
  const [editingObservabilitySource, setEditingObservabilitySource] =
    useState<ObservabilitySource | null>(null)
  const [observabilitySubmitting, setObservabilitySubmitting] = useState(false)

  const [clusterForm] = Form.useForm<ClusterFormValues>()
  const [clusterProvisionForm] = Form.useForm<ClusterProvisionFormValues>()
  const [userForm] = Form.useForm<UserFormValues>()
  const [roleForm] = Form.useForm<RoleFormValues>()
  const [registryForm] = Form.useForm<RegistryFormValues>()
  const [observabilityForm] = Form.useForm<ObservabilityFormValues>()

  const watchedKubeconfig = Form.useWatch('kubeconfig', clusterForm) ?? ''
  const watchedRoleIds = Form.useWatch('roleIds', userForm) ?? []
  const watchedPermissionKeys = Form.useWatch('permissionKeys', roleForm) ?? []
  const watchedRegistryType = Form.useWatch('type', registryForm) ?? repositoryProviders[0]?.key ?? 'registry'
  const watchedObservabilityType =
    Form.useWatch('type', observabilityForm) ?? observabilityKinds[0]?.key ?? 'prometheus'
  const clusterProvisionDraftFingerprint = provisionPayloadFingerprint(
    buildClusterProvisionPayload(clusterProvisionDraft),
  )
  const clusterProvisionCheckReady =
    clusterProvisionCheckResult?.ready === true &&
    clusterProvisionCheckFingerprint === clusterProvisionDraftFingerprint
  const clusterProvisionStep = clusterProvisionJob ? 2 : clusterProvisionCheckReady ? 1 : 0

  const currentPermissions = new Set(session?.user.permissions ?? [])
  const canReadDashboard = currentPermissions.has('dashboard:read')
  const canReadClusters = currentPermissions.has('clusters:read')
  const canWriteClusters = currentPermissions.has('clusters:write')
  const canReadResources = currentPermissions.has('resources:read')
  const canWriteResources = currentPermissions.has('resources:write')
  const canReadRegistries = currentPermissions.has('registries:read')
  const canWriteRegistries = currentPermissions.has('registries:write')
  const canReadObservability = currentPermissions.has('observability:read')
  const canWriteObservability = currentPermissions.has('observability:write')
  const canReadUsers = currentPermissions.has('users:read')
  const canWriteUsers = currentPermissions.has('users:write')
  const canReadRoles = currentPermissions.has('roles:read')
  const canWriteRoles = currentPermissions.has('roles:write')
  const sessionToken = session?.token ?? ''
  const accessibleViews = menuDefinitions.filter((item) => currentPermissions.has(item.permission))
  const accessibleViewKeys = accessibleViews.map((item) => item.key)
  const containerChildren = accessibleViews
    .filter(
      (item) =>
        item.key === 'clusters' || item.key === 'inspection' || item.key === 'resources',
    )
    .map((item) => ({
      key: item.key,
      label: item.label,
      icon: item.icon,
    }))
  const registryChildren = accessibleViews
    .filter((item) => item.key === 'registryIntegrations' || item.key === 'registryArtifacts')
    .map((item) => ({
      key: item.key,
      label: item.label,
      icon: item.icon,
    }))
  const observabilityChildren = accessibleViews
    .filter(
      (item) =>
        item.key === 'observabilityDashboards' || item.key === 'observabilitySources',
    )
    .map((item) => ({
      key: item.key,
      label: item.label,
      icon: item.icon,
    }))
  const menuItems: MenuProps['items'] = [
    accessibleViews.find((item) => item.key === 'dashboard')
      ? {
          key: 'dashboard',
          icon: <ControlOutlined />,
          label: '工作台',
        }
      : null,
    containerChildren.length > 0
      ? {
          key: containerMenuKey,
          icon: <ClusterOutlined />,
          label: '容器管理',
          children: containerChildren,
        }
      : null,
    registryChildren.length > 0
      ? {
          key: registryMenuKey,
          icon: <CloudUploadOutlined />,
          label: '镜像仓库',
          children: registryChildren,
        }
      : null,
    observabilityChildren.length > 0
      ? {
          key: observabilityMenuKey,
          icon: <EyeOutlined />,
          label: '可观测性',
          children: observabilityChildren,
        }
      : null,
    accessibleViews.find((item) => item.key === 'users')
      ? {
          key: 'users',
          icon: <TeamOutlined />,
          label: '用户管理',
        }
      : null,
    accessibleViews.find((item) => item.key === 'roles')
      ? {
          key: 'roles',
          icon: <SafetyCertificateOutlined />,
          label: '角色权限',
        }
      : null,
  ].filter(Boolean)

  const api = useMemo(() => {
    if (!sessionToken) {
      return null
    }

    return createApi(sessionToken, () => {
      persistSession(null)
      setSession(null)
      setDashboard(null)
      setClusters([])
      setRoles([])
      setPermissions([])
      setUsers([])
      setResourceTypes([])
      setRepositoryProviders([])
      setRegistries([])
      setObservabilityKinds([])
      setObservabilitySources([])
      setRegistryArtifactCache({})
      setNamespaces([])
      setResources([])
      setClusterInspection(null)
      setRegistryArtifactsSnapshot(null)
      setSelectedClusterId(undefined)
      setSelectedRegistryId(undefined)
      setSelectedGrafanaSourceId(undefined)
      setSelectedNamespace('')
      setSelectedUserId(undefined)
      setSelectedRoleId(undefined)
      setSelectedResourceKey('')
      setResourceDetailOpen(false)
      setResourceDetailLoading(false)
      setResourceDetailResource(null)
    })
  }, [sessionToken])

  const permissionGroups = groupPermissions(permissions)

  const filteredClusters = clusters.filter((cluster) => {
    const query = clusterSearch.trim().toLowerCase()
    const matchesQuery =
      query === '' ||
      [cluster.name, cluster.region, cluster.description, cluster.server, cluster.currentContext]
        .filter(Boolean)
        .some((value) => value.toLowerCase().includes(query))
    const matchesStatus =
      clusterStatusFilter === 'all' ? true : cluster.status === clusterStatusFilter
    return matchesQuery && matchesStatus
  })

  const filteredUsers = users.filter((user) => {
    const query = userSearch.trim().toLowerCase()
    const matchesQuery =
      query === '' ||
      [user.username, user.displayName, ...user.roles.map((role) => role.name)]
        .filter(Boolean)
        .some((value) => value.toLowerCase().includes(query))
    const matchesStatus =
      userStatusFilter === 'all'
        ? true
        : userStatusFilter === 'active'
          ? user.active
          : !user.active
    return matchesQuery && matchesStatus
  })

  const filteredRoles = roles.filter((role) => {
    const query = roleSearch.trim().toLowerCase()
    const matchesQuery =
      query === '' ||
      [role.name, role.description, ...role.permissions.map((permission) => permission.key)]
        .filter(Boolean)
        .some((value) => value.toLowerCase().includes(query))
    const matchesScope =
      roleScopeFilter === 'all'
        ? true
        : roleScopeFilter === 'builtin'
          ? role.builtIn
          : !role.builtIn
    return matchesQuery && matchesScope
  })

  const filteredRegistries = registries.filter((item) => {
    const query = registrySearch.trim().toLowerCase()
    return (
      query === '' ||
      [item.name, item.type, item.endpoint, item.namespace, item.description]
        .filter(Boolean)
        .some((value) => value.toLowerCase().includes(query))
    )
  })

  const filteredObservabilitySources = observabilitySources.filter((item) => {
    const query = observabilitySourceSearch.trim().toLowerCase()
    return (
      query === '' ||
      [item.name, item.type, item.endpoint, item.description]
        .filter(Boolean)
        .some((value) => value.toLowerCase().includes(query))
    )
  })

  const selectedCluster = clusters.find((item) => item.id === selectedClusterId)
  const selectedInspectionSnapshot =
    clusterInspection && clusterInspection.cluster.id === selectedClusterId
      ? clusterInspection
      : null
  const selectedInspectionReport = selectedInspectionSnapshot?.inspection ?? null
  const dashboardClusterMetrics = dashboard?.clusterMetrics ?? null
  const selectedResourceDefinition = resourceTypes.find(
    (item) => item.key === selectedResourceType,
  )
  const selectedProvisionTemplate = provisionTemplates.find(
    (item) => item.key === clusterProvisionDraft.provisionTemplate,
  )
  const selectedImageRegistryPreset =
    imageRegistryPresets.find((item) => item.key === clusterProvisionDraft.imageRegistryPreset) ??
    imageRegistryPresets.find((item) => item.key === defaultImageRegistryPresetKey)
  const selectedRepositoryProvider =
    repositoryProviders.find((item) => item.key === watchedRegistryType) ?? repositoryProviders[0] ?? null
  const selectedObservabilityKind =
    observabilityKinds.find((item) => item.key === watchedObservabilityType) ?? observabilityKinds[0] ?? null
  const selectedRegistry =
    registries.find((item) => item.id === selectedRegistryId) ?? filteredRegistries[0] ?? null
  const grafanaSources = observabilitySources.filter((item) => item.type === 'grafana')
  const selectedGrafanaSource =
    grafanaSources.find((item) => item.id === selectedGrafanaSourceId) ?? grafanaSources[0] ?? null
  const grafanaWorkspacePath = selectedGrafanaSource
    ? buildGrafanaWorkspacePath(selectedGrafanaSource.dashboardPath || '/dashboards', grafanaWorkspaceSection)
    : '/dashboards'
  const grafanaImmersivePath = selectedGrafanaSource
    ? buildGrafanaImmersivePath(grafanaWorkspacePath)
    : '/dashboards'
  const grafanaEmbedSrc = selectedGrafanaSource
    ? buildGrafanaEmbedSrc(selectedGrafanaSource.id, grafanaImmersivePath)
    : ''
  const grafanaExternalHref = selectedGrafanaSource
    ? buildGrafanaExternalHref(selectedGrafanaSource.endpoint, grafanaWorkspacePath)
    : ''
  const registryImageSpaces = useMemo(
    () =>
      registryArtifactsSnapshot?.imageSpaces && registryArtifactsSnapshot.imageSpaces.length > 0
        ? registryArtifactsSnapshot.imageSpaces
        : groupRegistryArtifactsBySpace(registryArtifactsSnapshot?.items ?? []),
    [registryArtifactsSnapshot],
  )
  const filteredRegistryImageSpaces = useMemo(
    () => filterRegistryImageSpaces(registryImageSpaces, deferredRegistryArtifactSearch),
    [deferredRegistryArtifactSearch, registryImageSpaces],
  )
  const registryImageSpaceCount = registryImageSpaces.length
  const registryImageCount = registryImageSpaces.reduce((total, item) => total + item.imageCount, 0)
  const registryVersionCount = registryImageSpaces.reduce((total, item) => total + item.versionCount, 0)
  const filteredRegistryImageSpaceCount = filteredRegistryImageSpaces.length
  const filteredRegistryImageCount = filteredRegistryImageSpaces.reduce(
    (total, item) => total + item.imageCount,
    0,
  )
  const filteredRegistryVersionCount = filteredRegistryImageSpaces.reduce(
    (total, item) => total + item.versionCount,
    0,
  )
  const isImmersiveDashboardView = activeView === 'observabilityDashboards'
  const hideShellHeader =
    activeView === 'observabilityDashboards' || activeView === 'observabilitySources'
  const hideShellActions =
    activeView === 'registryIntegrations' || activeView === 'registryArtifacts'
  const selectedUser = users.find((user) => user.id === selectedUserId)
  const selectedRole = roles.find((role) => role.id === selectedRoleId)
  const selectedRoleUsers = selectedRole
    ? users.filter((user) => user.roles.some((role) => role.id === selectedRole.id))
    : []
  const connectedClusterCount = clusters.filter((cluster) => cluster.status === 'connected').length
  const activeUserCount = users.filter((user) => user.active).length
  const disabledUserCount = users.length - activeUserCount
  const builtInRoleCount = roles.filter((role) => role.builtIn).length
  const customRoleCount = roles.length - builtInRoleCount

  const selectedUserRoles = roles.filter((role) => watchedRoleIds.includes(role.id))
  const selectedFormPermissions = permissions.filter((permission) =>
    watchedPermissionKeys.includes(permission.key),
  )
  const parsedResourceDraft = useMemo(() => safeParseResourceDraft(resourceDraft), [resourceDraft])
  const parsedResourceBaseline = useMemo(
    () => safeParseResourceDraft(resourceBaselineDraft),
    [resourceBaselineDraft],
  )
  const resourceBaselineTarget = parsedResourceBaseline.resource
  const resourceDraftTarget = parsedResourceDraft.resource
  const resourceDraftDocuments = parsedResourceDraft.resources
  const resourceAuditTarget = editingResource ?? resourceDraftTarget
  const resourceDiffLines = useMemo(
    () => buildYamlDiffLines(resourceBaselineDraft, resourceDraft),
    [resourceBaselineDraft, resourceDraft],
  )
  const resourceDiffSummary = useMemo(
    () => summarizeYamlDiff(resourceDiffLines),
    [resourceDiffLines],
  )
  const resourceAuditEntries = useMemo(
    () => buildResourceAuditEntries(editingResource, resourceDraftTarget),
    [editingResource, resourceDraftTarget],
  )
  const resourceRiskItems = useMemo(
    () =>
      buildResourceRiskItems(
        resourceDrawerMode,
        selectedResourceDefinition,
        resourceBaselineTarget,
        resourceDraftTarget,
        resourceDraftDocuments,
      ),
    [
      resourceDrawerMode,
      selectedResourceDefinition,
      resourceBaselineTarget,
      resourceDraftTarget,
      resourceDraftDocuments,
    ],
  )
  const resourceDetailDeploymentInsight = useMemo(
    () => buildDeploymentInsight(resourceDetailResource),
    [resourceDetailResource],
  )

  const hydrateWorkspaceEvent = useEffectEvent(
    async (client: ReturnType<typeof createApi>, token: string) => {
      await hydrateWorkspace(client, token)
    },
  )

  const refreshNamespacesEvent = useEffectEvent(
    async (client: ReturnType<typeof createApi>, clusterId: number) => {
      await refreshNamespaces(client, clusterId)
    },
  )

  const refreshResourcesEvent = useEffectEvent(
    async (
      client: ReturnType<typeof createApi>,
      clusterId: number,
      definition: ResourceDefinition,
      namespace: string,
      search: string,
    ) => {
      await refreshResources(client, clusterId, definition, namespace, search)
    },
  )

  const refreshDashboardEvent = useEffectEvent(
    async (client: ReturnType<typeof createApi>, clusterId?: number) => {
      await refreshDashboard(client, clusterId)
    },
  )

  const refreshInspectionEvent = useEffectEvent(
    async (client: ReturnType<typeof createApi>, clusterId: number) => {
      await refreshInspection(client, clusterId)
    },
  )

  const refreshRegistryArtifactsEvent = useEffectEvent(
    async (client: ReturnType<typeof createApi>, registryId: number) => {
      await refreshRegistryArtifacts(client, registryId)
    },
  )

  const refreshClusterProvisionJobEvent = useEffectEvent(
    async (client: ReturnType<typeof createApi>, currentJob: ClusterProvisionJob) => {
      try {
        const nextJob = await client.get<ClusterProvisionJob>(
          `/clusters/provision-jobs/${currentJob.id}`,
        )

        setClusterProvisionJob(nextJob)

        if (nextJob.status === 'succeeded' && currentJob.status !== 'succeeded') {
          await Promise.all([refreshClusters(client), refreshDashboard(client)])
          if (nextJob.resultClusterId) {
            setSelectedClusterId(nextJob.resultClusterId)
          }
          message.success('集群创建完成')
        }

        if (nextJob.status === 'failed' && currentJob.status !== 'failed') {
          message.error(nextJob.lastError || 'Kubespray 创建失败')
        }
      } catch (error) {
        handleError(error, '获取创建任务状态失败')
      }
    },
  )

  useEffect(() => {
    if (!sessionToken || !api) {
      return
    }

    void hydrateWorkspaceEvent(api, sessionToken)
  }, [api, sessionToken])

  useEffect(() => {
    if (!api || !canReadDashboard || activeView !== 'dashboard') {
      return
    }

    void refreshDashboardEvent(api, selectedClusterId)
  }, [activeView, api, canReadDashboard, selectedClusterId])

  useEffect(() => {
    if (!canReadResources || !selectedClusterId || !api) {
      return
    }

    void refreshNamespacesEvent(api, selectedClusterId)
  }, [api, canReadResources, selectedClusterId])

  useEffect(() => {
    if (!canReadResources || !selectedClusterId || !api) {
      return
    }

    if (!selectedResourceDefinition) {
      return
    }

    void refreshResourcesEvent(
      api,
      selectedClusterId,
      selectedResourceDefinition,
      selectedNamespace,
      deferredSearch,
    )
  }, [
    api,
    canReadResources,
    selectedClusterId,
    selectedResourceDefinition,
    selectedNamespace,
    deferredSearch,
  ])

  useEffect(() => {
    if (!canReadClusters || !selectedClusterId || !api || activeView !== 'inspection') {
      return
    }

    void refreshInspectionEvent(api, selectedClusterId)
  }, [activeView, api, canReadClusters, selectedClusterId])

  useEffect(() => {
    if (
      !api ||
      !canReadRegistries ||
      (activeView !== 'registryArtifacts' && activeView !== 'registryIntegrations') ||
      !selectedRegistryId
    ) {
      return
    }

    if (registryArtifactCache[selectedRegistryId]) {
      setRegistryArtifactsSnapshot(registryArtifactCache[selectedRegistryId] ?? null)
      return
    }

    void refreshRegistryArtifactsEvent(api, selectedRegistryId)
  }, [activeView, api, canReadRegistries, registryArtifactCache, selectedRegistryId])

  useEffect(() => {
    if (!selectedResourceDefinition?.namespaced) {
      setSelectedNamespace('')
    }
  }, [selectedResourceDefinition?.namespaced])

  useEffect(() => {
    if (!selectedResourceDefinition?.namespaced) {
      return
    }

    if (selectedNamespace && namespaces.some((item) => item.name === selectedNamespace)) {
      return
    }

    const preferred = namespaces.find((item) => item.name === 'default') ?? namespaces[0]
    setSelectedNamespace(preferred?.name ?? '')
  }, [selectedResourceDefinition?.namespaced, namespaces, selectedNamespace])

  useEffect(() => {
    if (accessibleViewKeys.length === 0) {
      return
    }

    if (!accessibleViewKeys.includes(activeView)) {
      startTransition(() => {
        setActiveView(accessibleViewKeys[0] as ViewKey)
      })
    }
  }, [activeView, accessibleViewKeys])

  useEffect(() => {
    setSelectedRegistryId((current) =>
      pickCurrentOrFirstId(registries.map((item) => item.id), current),
    )
  }, [registries])

  useEffect(() => {
    if (!selectedRegistryId) {
      setRegistryArtifactsSnapshot(null)
      setExpandedRegistrySpaceKeys([])
      setExpandedRegistryImageKeys({})
      return
    }

    setRegistryArtifactsSnapshot(registryArtifactCache[selectedRegistryId] ?? null)
  }, [registryArtifactCache, selectedRegistryId])

  useEffect(() => {
    setSelectedGrafanaSourceId((current) =>
      pickCurrentOrFirstId(grafanaSources.map((item) => item.id), current),
    )
  }, [grafanaSources])

  useEffect(() => {
    setExpandedRegistrySpaceKeys([])
    setExpandedRegistryImageKeys({})
  }, [registryArtifactsSnapshot?.integration.id, deferredRegistryArtifactSearch])

  useEffect(() => {
    if (!selectedGrafanaSource) {
      setGrafanaEmbedError('')
      return
    }
  }, [selectedGrafanaSource])

  useEffect(() => {
    setGrafanaEmbedError('')
  }, [grafanaEmbedSrc, selectedGrafanaSourceId, grafanaWorkspaceSection])

  useEffect(() => {
    setGrafanaFrameNonce(0)
  }, [selectedGrafanaSourceId, grafanaWorkspaceSection])

  useEffect(() => {
    if (editingCluster) {
      return
    }

    const preview = parseKubeconfigPreview(watchedKubeconfig)
    setClusterKubePreview(preview)
    setClusterConnectionPreview(null)

    if (
      preview?.valid &&
      preview.suggestedName &&
      !clusterForm.isFieldTouched('name') &&
      !clusterForm.getFieldValue('name')
    ) {
      clusterForm.setFieldValue('name', preview.suggestedName)
    }
  }, [watchedKubeconfig, editingCluster, clusterForm])

  useEffect(() => {
    if (
      !api ||
      !clusterProvisionDrawerOpen ||
      !clusterProvisionJob ||
      isFinalProvisionStatus(clusterProvisionJob.status)
    ) {
      return
    }

    const timer = window.setTimeout(() => {
      void refreshClusterProvisionJobEvent(api, clusterProvisionJob)
    }, 2500)

    return () => window.clearTimeout(timer)
  }, [
    api,
    clusterProvisionDrawerOpen,
    clusterProvisionJob,
  ])

  useEffect(() => {
    if (provisionTemplates.length === 0) {
      return
    }

    if (provisionTemplates.some((item) => item.key === clusterProvisionDraft.provisionTemplate)) {
      return
    }

    const fallbackTemplate =
      provisionTemplates.find((item) => item.recommended)?.key ||
      provisionTemplates[0]?.key ||
      defaultProvisionTemplateKey
    const nextDraft = {
      ...clusterProvisionDraft,
      provisionTemplate: fallbackTemplate,
    }

    setClusterProvisionDraft(nextDraft)
    clusterProvisionForm.setFieldValue('provisionTemplate', fallbackTemplate)
  }, [clusterProvisionDraft, clusterProvisionForm, provisionTemplates])

  useEffect(() => {
    if (imageRegistryPresets.length === 0) {
      return
    }

    if (imageRegistryPresets.some((item) => item.key === clusterProvisionDraft.imageRegistryPreset)) {
      return
    }

    const fallbackPreset =
      imageRegistryPresets.find((item) => item.recommended)?.key ||
      imageRegistryPresets.find((item) => item.key === defaultImageRegistryPresetKey)?.key ||
      imageRegistryPresets[0]?.key ||
      defaultImageRegistryPresetKey
    const nextDraft = {
      ...clusterProvisionDraft,
      imageRegistryPreset: fallbackPreset,
    }

    setClusterProvisionDraft(nextDraft)
    clusterProvisionForm.setFieldValue('imageRegistryPreset', fallbackPreset)
  }, [clusterProvisionDraft, clusterProvisionForm, imageRegistryPresets])

  useEffect(() => {
    if (!filteredUsers.some((user) => user.id === selectedUserId)) {
      setSelectedUserId(filteredUsers[0]?.id)
    }
  }, [filteredUsers, selectedUserId])

  useEffect(() => {
    if (!filteredRoles.some((role) => role.id === selectedRoleId)) {
      setSelectedRoleId(filteredRoles[0]?.id)
    }
  }, [filteredRoles, selectedRoleId])

  useEffect(() => {
    if (
      selectedResourceKey &&
      !resources.some((resource) => buildResourceSelectionKey(resource) === selectedResourceKey)
    ) {
      setSelectedResourceKey('')
      setResourceDetailOpen(false)
      setResourceDetailLoading(false)
      setResourceDetailResource(null)
    }
  }, [resources, selectedResourceKey])

  useEffect(() => {
    setResourceDetailOpen(false)
    setResourceDetailLoading(false)
    setResourceDetailResource(null)
    setSelectedResourceKey('')
  }, [selectedClusterId, selectedResourceType, selectedNamespace])

  function logout(notify = true) {
    void postPublic<void>('/auth/logout', {})
    persistSession(null)
    setSession(null)
    setDashboard(null)
    setClusters([])
    setRoles([])
    setPermissions([])
    setUsers([])
    setResourceTypes([])
    setImageRegistryPresets([])
    setNamespaces([])
    setResources([])
    setSelectedClusterId(undefined)
    setSelectedNamespace('')
    setSelectedUserId(undefined)
    setSelectedRoleId(undefined)
    setSelectedResourceKey('')
    setResourceDetailOpen(false)
    setResourceDetailLoading(false)
    setResourceDetailResource(null)
    if (notify) {
      message.info('已退出当前会话')
    }
  }

  async function hydrateWorkspace(client: ReturnType<typeof createApi>, token: string) {
    setBootstrapping(true)
    try {
      const me = await client.get<User>('/auth/me')

      const nextSession = { token, user: me }
      persistSession(nextSession)
      setSession(nextSession)

      const [
        clusterData,
        roleData,
        userData,
        permissionData,
        resourceDefinitionData,
        provisionTemplateData,
        imageRegistryPresetData,
        repositoryProviderData,
        registryData,
        observabilityKindData,
        observabilitySourceData,
      ] =
        await Promise.all([
        me.permissions.includes('clusters:read')
          ? client.get<Cluster[]>('/clusters')
          : Promise.resolve([]),
        me.permissions.includes('roles:read')
          ? client.get<Role[]>('/roles')
          : Promise.resolve([]),
        me.permissions.includes('users:read')
          ? client.get<User[]>('/users')
          : Promise.resolve([]),
        me.permissions.includes('roles:read')
          ? client.get<Permission[]>('/catalog/permissions')
          : Promise.resolve([]),
        me.permissions.includes('resources:read')
          ? client.get<ResourceDefinition[]>('/catalog/resource-types')
          : Promise.resolve([]),
        me.permissions.includes('clusters:write')
          ? client.get<ProvisionTemplate[]>('/catalog/provision-templates')
          : Promise.resolve([]),
        me.permissions.includes('clusters:write')
          ? client.get<ImageRegistryPreset[]>('/catalog/image-registry-presets')
          : Promise.resolve([]),
        me.permissions.includes('registries:read')
          ? client.get<RepositoryProvider[]>('/catalog/repository-providers')
          : Promise.resolve([]),
        me.permissions.includes('registries:read')
          ? client.get<RegistryIntegration[]>('/registries')
          : Promise.resolve([]),
        me.permissions.includes('observability:read')
          ? client.get<ObservabilityKind[]>('/catalog/observability-kinds')
          : Promise.resolve([]),
        me.permissions.includes('observability:read')
          ? client.get<ObservabilitySource[]>('/observability/sources')
          : Promise.resolve([]),
        ])

      const preferredDashboardClusterId = me.permissions.includes('clusters:read')
        ? pickPreferredClusterId(clusterData)
        : undefined

      const dashboardData = me.permissions.includes('dashboard:read')
        ? await client.get<DashboardSummary>(buildDashboardPath(preferredDashboardClusterId))
        : null

      startTransition(() => {
        setDashboard(dashboardData)
        setClusters(clusterData)
        setClusterInspection(null)
        setRoles(roleData)
        setUsers(userData)
        setPermissions(permissionData)
        setResourceTypes(resourceDefinitionData)
        setProvisionTemplates(provisionTemplateData)
        setImageRegistryPresets(imageRegistryPresetData)
        setRepositoryProviders(repositoryProviderData)
        setRegistries(registryData)
        setObservabilityKinds(observabilityKindData)
        setObservabilitySources(observabilitySourceData)
        setSelectedClusterId((current) => pickPreferredClusterId(clusterData, current))
        setSelectedRegistryId((current) =>
          pickCurrentOrFirstId(registryData.map((item) => item.id), current),
        )
        setSelectedGrafanaSourceId((current) =>
          pickCurrentOrFirstId(
            observabilitySourceData
              .filter((item) => item.type === 'grafana')
              .map((item) => item.id),
            current,
          ),
        )
        setSelectedResourceType((current) =>
          resourceDefinitionData.some((item) => item.key === current)
            ? current
            : resourceDefinitionData[0]?.key ?? 'deployment',
        )
      })
    } catch (error) {
      handleError(error, '加载平台数据失败')
    } finally {
      setBootstrapping(false)
    }
  }

  async function refreshClusters(client = api) {
    if (!client || !canReadClusters) {
      return
    }

    const data = await client.get<Cluster[]>('/clusters')
    startTransition(() => {
      setClusters(data)
      setSelectedClusterId((current) => pickPreferredClusterId(data, current))
    })
  }

  async function refreshUsers(client = api) {
    if (!client || !canReadUsers) {
      return
    }

    const data = await client.get<User[]>('/users')
    setUsers(data)
  }

  async function refreshRoles(client = api) {
    if (!client || !canReadRoles) {
      return
    }

    const [roleData, permissionData] = await Promise.all([
      client.get<Role[]>('/roles'),
      client.get<Permission[]>('/catalog/permissions'),
    ])
    setRoles(roleData)
    setPermissions(permissionData)
  }

  async function refreshRegistries(client = api) {
    if (!client || !canReadRegistries) {
      return
    }

    const [providerData, registryData] = await Promise.all([
      client.get<RepositoryProvider[]>('/catalog/repository-providers'),
      client.get<RegistryIntegration[]>('/registries'),
    ])
    setRepositoryProviders(providerData)
    setRegistries(registryData)
    setSelectedRegistryId((current) =>
      pickCurrentOrFirstId(registryData.map((item) => item.id), current),
    )
  }

  async function refreshRegistryArtifacts(
    client = api,
    registryId = selectedRegistryId,
  ) {
    if (!client || !canReadRegistries || !registryId) {
      return
    }

    setRegistryArtifactsLoading(true)
    try {
      const params = new URLSearchParams()
      params.set('limit', '240')
      const suffix = params.toString() ? `?${params.toString()}` : ''
      const data = await client.get<RegistryArtifactList>(`/registries/${registryId}/artifacts${suffix}`)
      setRegistryArtifactCache((current) => ({
        ...current,
        [registryId]: data,
      }))
      setRegistryArtifactsSnapshot(data)
    } catch (error) {
      handleError(error, '加载镜像列表失败')
    } finally {
      setRegistryArtifactsLoading(false)
    }
  }

  async function refreshObservabilitySources(client = api) {
    if (!client || !canReadObservability) {
      return
    }

    const [kindData, sourceData] = await Promise.all([
      client.get<ObservabilityKind[]>('/catalog/observability-kinds'),
      client.get<ObservabilitySource[]>('/observability/sources'),
    ])
    setObservabilityKinds(kindData)
    setObservabilitySources(sourceData)
    setSelectedGrafanaSourceId((current) =>
      pickCurrentOrFirstId(
        sourceData.filter((item) => item.type === 'grafana').map((item) => item.id),
        current,
      ),
    )
  }

  async function refreshDashboard(client = api, clusterId = selectedClusterId) {
    if (!client || !canReadDashboard) {
      return
    }

    const data = await client.get<DashboardSummary>(buildDashboardPath(clusterId))
    setDashboard(data)
  }

  async function refreshInspection(client = api, clusterId = selectedClusterId) {
    if (!client || !canReadClusters || !clusterId) {
      return
    }

    setClusterInspectionLoading(true)
    try {
      const data = await client.get<ClusterInspectionSnapshot>(`/clusters/${clusterId}/inspection`)
      startTransition(() => {
        setClusterInspection(data)
        setClusters((current) =>
          current.map((cluster) => (cluster.id === data.cluster.id ? data.cluster : cluster)),
        )
      })
    } catch (error) {
      handleError(error, '执行巡检失败')
    } finally {
      setClusterInspectionLoading(false)
    }
  }

  async function refreshNamespaces(client: ReturnType<typeof createApi>, clusterId: number) {
    try {
      const data = await client.get<NamespaceItem[]>(`/clusters/${clusterId}/namespaces`)
      setNamespaces(data)
    } catch (error) {
      handleError(error, '加载命名空间失败')
    }
  }

  async function refreshResources(
    client: ReturnType<typeof createApi>,
    clusterId: number,
    definition: ResourceDefinition,
    namespace: string,
    search: string,
  ) {
    setResourceLoading(true)
    try {
      const params = new URLSearchParams()
      if (definition.namespaced && namespace) {
        params.set('namespace', namespace)
      }
      if (search.trim()) {
        params.set('search', search.trim())
      }

      const suffix = params.toString() ? `?${params.toString()}` : ''
      const data = await client.get<K8sObject[]>(
        `/clusters/${clusterId}/resources/${definition.key}${suffix}`,
      )
      startTransition(() => {
        setResources(data)
      })
    } catch (error) {
      handleError(error, '加载资源列表失败')
    } finally {
      setResourceLoading(false)
    }
  }

  async function handleLogin(values: { username: string; password: string }) {
    setSubmittingLogin(true)
    try {
      const response = await postPublic<{ token: string; user: User }>('/auth/login', values)
      const nextSession: Session = {
        token: response.token,
        user: response.user,
      }
      persistSession(nextSession)
      setSession(nextSession)
      message.success('登录成功')
    } catch (error) {
      handleError(error, '登录失败')
    } finally {
      setSubmittingLogin(false)
    }
  }

  async function previewClusterConnection() {
    if (!api) {
      return null
    }

    const values = await clusterForm.validateFields(['kubeconfig'])
    setClusterPreviewLoading(true)
    try {
      const preview = await api.post<ClusterConnectionPreview>('/clusters/preview', {
        kubeconfig: values.kubeconfig,
      })
      setClusterConnectionPreview(preview)
      message.success('预检查通过，目标集群可访问')
      return preview
    } finally {
      setClusterPreviewLoading(false)
    }
  }

  async function submitCluster() {
    if (!api) {
      return
    }

    try {
      if (editingCluster) {
        await clusterForm.validateFields(['name', 'region', 'mode'])
      }

      const values = clusterForm.getFieldsValue(true) as ClusterFormValues
      const payload: ClusterFormValues = {
        name: values.name?.trim() ?? '',
        region: values.region?.trim() ?? '',
        mode: values.mode ?? 'ready',
        description: values.description?.trim() ?? '',
        kubeconfig: values.kubeconfig?.trim() ?? '',
      }

      if (!editingCluster) {
        if (!payload.name) {
          setClusterWizardStep(1)
          clusterForm.setFields([{ name: 'name', errors: ['请输入集群名称'] }])
          return
        }

        if (!payload.region) {
          setClusterWizardStep(1)
          clusterForm.setFields([{ name: 'region', errors: ['请输入所属区域'] }])
          return
        }

        if (!payload.kubeconfig) {
          setClusterWizardStep(0)
          clusterForm.setFields([{ name: 'kubeconfig', errors: ['请粘贴 kubeconfig 内容'] }])
          return
        }
      }

      if (editingCluster) {
        await api.put<Cluster>(`/clusters/${editingCluster.id}`, payload)
        message.success('集群更新成功')
      } else {
        await api.post<Cluster>('/clusters', payload)
        message.success('集群接入成功')
      }

      closeClusterDrawer()
      await Promise.all([refreshClusters(api), refreshDashboard(api)])
    } catch (error) {
      if (isFormValidationError(error)) {
        return
      }
      handleError(error, '保存集群失败')
    }
  }

  async function submitClusterProvisionJob() {
    if (!api) {
      return
    }

    try {
      const values = await clusterProvisionForm.validateFields()
      const payload = buildClusterProvisionPayload(values)
      const fingerprint = provisionPayloadFingerprint(payload)
      setClusterProvisionDraft(normalizeClusterProvisionDraft(values))

      if (!clusterProvisionCheckReady || clusterProvisionCheckFingerprint !== fingerprint) {
        message.warning('请先执行预检查，并确认当前参数已经通过检查')
        return
      }

      setClusterProvisionSubmitting(true)
      const job = await api.post<ClusterProvisionJob>('/clusters/provision-jobs', payload)
      setClusterProvisionJob(job)
      message.success('创建任务已提交，Kubespray 开始执行')
    } catch (error) {
      if (isFormValidationError(error)) {
        return
      }
      handleError(error, '提交创建任务失败')
    } finally {
      setClusterProvisionSubmitting(false)
    }
  }

  async function runClusterProvisionPrecheck() {
    if (!api) {
      return
    }

    try {
      setClusterProvisionCheckLoading(true)
      const values = await clusterProvisionForm.validateFields()
      const payload = buildClusterProvisionPayload(values)
      const result = await api.post<ClusterProvisionCheckResult>('/clusters/provision-checks', payload)

      setClusterProvisionDraft(normalizeClusterProvisionDraft(values))
      setClusterProvisionCheckResult(result)
      setClusterProvisionCheckFingerprint(provisionPayloadFingerprint(payload))

      if (result.ready) {
        message.success('预检查通过，可以提交创建任务')
      } else {
        message.warning(result.summary || '预检查未通过，请先修正问题')
      }
    } catch (error) {
      if (isFormValidationError(error)) {
        return
      }
      handleError(error, '执行预检查失败')
    } finally {
      setClusterProvisionCheckLoading(false)
    }
  }

  function handleClusterProvisionFormChange(
    _: Partial<ClusterProvisionFormValues>,
    allValues: ClusterProvisionFormValues,
  ) {
    const nextDraft = normalizeClusterProvisionDraft(allValues)
    const nextFingerprint = provisionPayloadFingerprint(buildClusterProvisionPayload(nextDraft))

    setClusterProvisionDraft(nextDraft)

    if (clusterProvisionCheckFingerprint && nextFingerprint !== clusterProvisionCheckFingerprint) {
      setClusterProvisionCheckResult(null)
      setClusterProvisionCheckFingerprint('')
    }
  }

  async function continueClusterWizard() {
    try {
      if (clusterWizardStep === 0) {
        const values = await clusterForm.validateFields(['kubeconfig'])
        const preview = parseKubeconfigPreview(values.kubeconfig ?? '')
        if (!preview?.valid) {
          message.error(preview?.error || '无法解析 kubeconfig，请检查内容后重试')
          return
        }
        if (!clusterForm.getFieldValue('name') && preview.suggestedName) {
          clusterForm.setFieldValue('name', preview.suggestedName)
        }
        setClusterWizardStep(1)
        return
      }

      if (clusterWizardStep === 1) {
        await clusterForm.validateFields(['name', 'region', 'mode'])
        await previewClusterConnection()
        setClusterWizardStep(2)
        return
      }

      await submitCluster()
    } catch (error) {
      if (isFormValidationError(error)) {
        return
      }
      handleError(error, '集群导入预检查失败')
    }
  }

  async function handleClusterFileUpload(file: File) {
    try {
      const content = await readTextFile(file)
      clusterForm.setFieldValue('kubeconfig', content)
      setClusterDraftSource('upload')
      message.success(`已载入 ${file.name}`)
    } catch (error) {
      handleError(error, '读取 kubeconfig 文件失败')
    }
  }

  async function submitUser() {
    if (!api) {
      return
    }

    try {
      const values = await userForm.validateFields()
      if (editingUser) {
        await api.put<User>(`/users/${editingUser.id}`, values)
        message.success('用户更新成功')
      } else {
        await api.post<User>('/users', values)
        message.success('用户创建成功')
      }

      closeUserModal()
      await Promise.all([refreshUsers(api), refreshDashboard(api)])
    } catch (error) {
      if (isFormValidationError(error)) {
        return
      }
      handleError(error, '保存用户失败')
    }
  }

  async function submitRole() {
    if (!api) {
      return
    }

    try {
      const values = await roleForm.validateFields()
      if (editingRole) {
        await api.put<Role>(`/roles/${editingRole.id}`, values)
        message.success('角色更新成功')
      } else {
        await api.post<Role>('/roles', values)
        message.success('角色创建成功')
      }

      closeRoleModal()
      await Promise.all([refreshRoles(api), refreshDashboard(api)])
    } catch (error) {
      if (isFormValidationError(error)) {
        return
      }
      handleError(error, '保存角色失败')
    }
  }

  function openResourceReview() {
    if (!selectedResourceDefinition) {
      return
    }

    if (parsedResourceDraft.error || parsedResourceDraft.resources.length === 0) {
      handleError(
        new Error(parsedResourceDraft.error || '当前 YAML 无法解析'),
        '请先修复 YAML 后再提交',
      )
      return
    }

    setResourceReviewOpen(true)
  }

  async function submitResource() {
    if (!api || !selectedClusterId || !selectedResourceDefinition) {
      return
    }

    try {
      setResourceSubmitting(true)
      const namespace = resolveResourceNamespace(
        selectedResourceDefinition,
        selectedNamespace,
        editingResource,
      )
      const query = buildNamespaceQuery(selectedResourceDefinition, namespace)
      const payload = { manifest: resourceDraft }

      if (resourceDrawerMode === 'create' || resourceDrawerMode === 'clone') {
        await api.post<K8sObject>(
          `/clusters/${selectedClusterId}/resources/${selectedResourceDefinition.key}${query}`,
          payload,
        )
        message.success(resourceDrawerMode === 'clone' ? '资源克隆成功' : '资源创建成功')
      } else if (resourceDrawerMode === 'edit' && editingResource) {
        await api.put<K8sObject>(
          `/clusters/${selectedClusterId}/resources/${selectedResourceDefinition.key}/${resourceName(editingResource)}${query}`,
          payload,
        )
        message.success('资源更新成功')
      }

      setResourceReviewOpen(false)
      closeResourceDrawer()
      await refreshResources(
        api,
        selectedClusterId,
        selectedResourceDefinition,
        selectedNamespace,
        deferredSearch,
      )
    } catch (error) {
      handleError(error, '保存资源失败')
    } finally {
      setResourceSubmitting(false)
    }
  }

  async function openResourceEditor(
    resource?: K8sObject,
    panel: ResourceDrawerPanel = 'yaml',
  ) {
    if (!api || !selectedClusterId || !selectedResourceDefinition) {
      return
    }

    if (!resource) {
      setEditingResource(null)
      setResourceDrawerMode('create')
      setResourceDrawerPanel(panel)
      setResourceBaselineDraft('')
      setResourceDraft(buildStandardResourceManifest(selectedResourceDefinition, selectedNamespace))
      setResourceDrawerOpen(true)
      return
    }

    try {
      const query = buildNamespaceQuery(
        selectedResourceDefinition,
        resourceNamespace(resource),
      )
      const detail = await api.get<K8sObject>(
        `/clusters/${selectedClusterId}/resources/${selectedResourceDefinition.key}/${resourceName(resource)}${query}`,
      )
      const detailYaml = YAML.stringify(detail)
      setEditingResource(detail)
      setResourceDrawerMode(canWriteResources ? 'edit' : 'inspect')
      setResourceDrawerPanel(panel)
      setResourceBaselineDraft(detailYaml)
      setResourceDraft(detailYaml)
      setResourceDrawerOpen(true)
    } catch (error) {
      handleError(error, '加载资源详情失败')
    }
  }

  function openStandardResourceTemplate() {
    if (!selectedResourceDefinition) {
      return
    }

    setEditingResource(null)
    setResourceDrawerMode('create')
    setResourceDrawerPanel('yaml')
    setResourceBaselineDraft('')
    setResourceDraft(buildStandardResourceManifest(selectedResourceDefinition, selectedNamespace))
    setResourceDrawerOpen(true)
  }

  async function openResourceDetail(resource: K8sObject) {
    if (!api || !selectedClusterId || !selectedResourceDefinition) {
      return
    }

    setSelectedResourceKey(buildResourceSelectionKey(resource))
    setResourceDetailOpen(true)
    setResourceDetailLoading(true)

    try {
      const query = buildNamespaceQuery(
        selectedResourceDefinition,
        resourceNamespace(resource),
      )
      const detail = await api.get<K8sObject>(
        `/clusters/${selectedClusterId}/resources/${selectedResourceDefinition.key}/${resourceName(resource)}${query}`,
      )
      setResourceDetailResource(detail)
    } catch (error) {
      setResourceDetailOpen(false)
      setResourceDetailResource(null)
      handleError(error, '加载资源详情失败')
    } finally {
      setResourceDetailLoading(false)
    }
  }

  function openResourceEditorFromDetail(panel: ResourceDrawerPanel) {
    if (!resourceDetailResource) {
      return
    }

    closeResourceDetailDrawer()
    void openResourceEditor(resourceDetailResource, panel)
  }

  function openResourceCloneFromDetail() {
    if (!resourceDetailResource) {
      return
    }

    closeResourceDetailDrawer()
    openResourceClone(resourceDetailResource)
  }

  async function removeResourceFromDetail() {
    if (!resourceDetailResource) {
      return
    }

    closeResourceDetailDrawer()
    await removeResource(resourceDetailResource)
  }

  function openResourceClone(resource: K8sObject) {
    if (!selectedResourceDefinition) {
      return
    }

    const cloned = sanitizeResourceForCreate(resource)
    setEditingResource(resource)
    setResourceDrawerMode('clone')
    setResourceDrawerPanel('yaml')
    setResourceBaselineDraft(YAML.stringify(resource))
    setResourceDraft(YAML.stringify(cloned))
    setResourceDrawerOpen(true)
  }

  function formatResourceYaml() {
    try {
      const parsed = parseYamlResourceDocuments(resourceDraft)
      setResourceDraft(stringifyYamlDocuments(parsed))
      message.success('YAML 已格式化')
    } catch (error) {
      handleError(error, 'YAML 格式不正确，无法格式化')
    }
  }

  async function copyResourceYaml() {
    try {
      await copyText(resourceDraft)
      message.success('YAML 已复制到剪贴板')
    } catch (error) {
      handleError(error, '复制失败')
    }
  }

  async function testCluster(cluster: Cluster) {
    if (!api) {
      return
    }

    try {
      await api.post<Cluster>(`/clusters/${cluster.id}/test`, {})
      message.success(`集群 ${cluster.name} 连接正常`)
      await Promise.all([refreshClusters(api), refreshDashboard(api)])
    } catch (error) {
      handleError(error, '集群联通性校验失败')
    }
  }

  async function updateClusterMode(cluster: Cluster, mode: string) {
    if (!api || !canWriteClusters) {
      return
    }

    if ((cluster.mode || 'ready') === mode) {
      return
    }

    if (!cluster.region?.trim()) {
      message.warning('请先补充区域，再切换运行状态')
      openClusterDrawer(cluster)
      return
    }

    modal.confirm({
      title: mode === 'maintenance' ? `将 ${cluster.name} 置为维护` : `恢复 ${cluster.name} 为就绪`,
      content:
        mode === 'maintenance'
          ? '会将所有 worker 节点执行 cordon，暂停新 Pod 调度。'
          : '会将所有 worker 节点执行 uncordon，恢复调度。',
      okText: mode === 'maintenance' ? '确认维护' : '确认恢复',
      cancelText: '取消',
      onOk: async () => {
        setClusterModeSavingId(cluster.id)
        try {
          await api.put<Cluster>(`/clusters/${cluster.id}`, {
            name: cluster.name,
            description: cluster.description,
            region: cluster.region,
            mode,
          })
          message.success(`集群 ${cluster.name} 已切换为${clusterModeLabel(mode)}`)
          await Promise.all([refreshClusters(api), refreshDashboard(api)])
        } catch (error) {
          handleError(error, '更新集群运行状态失败')
        } finally {
          setClusterModeSavingId(undefined)
        }
      },
    })
  }

  async function removeCluster(cluster: Cluster) {
    if (!api) {
      return
    }

    modal.confirm({
      title: `删除集群 ${cluster.name}`,
      content: '删除后平台将不再持有该集群的接入信息。',
      okText: '删除',
      okButtonProps: { danger: true },
      cancelText: '取消',
      onOk: async () => {
        try {
          await api.delete(`/clusters/${cluster.id}`)
          message.success('集群已删除')
          await Promise.all([refreshClusters(api), refreshDashboard(api)])
        } catch (error) {
          handleError(error, '删除集群失败')
        }
      },
    })
  }

  async function removeUser(user: User) {
    if (!api) {
      return
    }

    modal.confirm({
      title: `删除用户 ${user.username}`,
      content: '删除后该用户将无法再次登录平台。',
      okText: '删除',
      okButtonProps: { danger: true },
      cancelText: '取消',
      onOk: async () => {
        try {
          await api.delete(`/users/${user.id}`)
          message.success('用户已删除')
          await Promise.all([refreshUsers(api), refreshDashboard(api)])
        } catch (error) {
          handleError(error, '删除用户失败')
        }
      },
    })
  }

  async function removeRole(role: Role) {
    if (!api) {
      return
    }

    modal.confirm({
      title: `删除角色 ${role.name}`,
      content: '删除后不会影响现有 Kubernetes 集群，但会移除平台侧权限映射。',
      okText: '删除',
      okButtonProps: { danger: true },
      cancelText: '取消',
      onOk: async () => {
        try {
          await api.delete(`/roles/${role.id}`)
          message.success('角色已删除')
          await Promise.all([refreshRoles(api), refreshDashboard(api), refreshUsers(api)])
        } catch (error) {
          handleError(error, '删除角色失败')
        }
      },
    })
  }

  async function removeResource(resource: K8sObject) {
    if (!api || !selectedClusterId || !selectedResourceDefinition) {
      return
    }

    modal.confirm({
      title: `删除资源 ${resourceName(resource)}`,
      content: '删除后资源会立即从目标 Kubernetes 集群中移除。',
      okText: '删除',
      okButtonProps: { danger: true },
      cancelText: '取消',
      onOk: async () => {
        try {
          const query = buildNamespaceQuery(
            selectedResourceDefinition,
            resourceNamespace(resource),
          )
          await api.delete(
            `/clusters/${selectedClusterId}/resources/${selectedResourceDefinition.key}/${resourceName(resource)}${query}`,
          )
          message.success('资源已删除')
          await refreshResources(
            api,
            selectedClusterId,
            selectedResourceDefinition,
            selectedNamespace,
            deferredSearch,
          )
        } catch (error) {
          handleError(error, '删除资源失败')
        }
      },
    })
  }

  function openClusterDrawer(cluster?: Cluster) {
    setEditingCluster(cluster ?? null)
    setClusterDrawerOpen(true)
    setClusterWizardStep(0)
    setClusterDraftSource('paste')
    setClusterKubePreview(null)
    setClusterConnectionPreview(null)
    clusterForm.setFieldsValue({
      name: cluster?.name ?? '',
      region: cluster?.region ?? '',
      mode: cluster?.mode || 'ready',
      description: cluster?.description ?? '',
      kubeconfig: '',
    })
  }

  function openClusterProvisionDrawer() {
    setClusterProvisionDrawerOpen(true)
    setClusterProvisionSubmitting(false)
    setClusterProvisionCheckLoading(false)
    if (clusterProvisionJob?.status === 'failed') {
      setClusterProvisionJob(null)
    }
    clusterProvisionForm.setFieldsValue(clusterProvisionDraft)
  }

  function closeClusterDrawer() {
    setClusterDrawerOpen(false)
    setEditingCluster(null)
    setClusterWizardStep(0)
    setClusterDraftSource('paste')
    setClusterKubePreview(null)
    setClusterConnectionPreview(null)
    clusterForm.resetFields()
  }

  function resetClusterProvisionDrawer() {
    setClusterProvisionSubmitting(false)
    setClusterProvisionCheckLoading(false)
    setClusterProvisionJob(null)
    setClusterProvisionCheckResult(null)
    setClusterProvisionCheckFingerprint('')
    const nextDraft = defaultClusterProvisionFormValues()
    setClusterProvisionDraft(nextDraft)
    clusterProvisionForm.setFieldsValue(nextDraft)
  }

  function closeClusterProvisionDrawer() {
    setClusterProvisionDrawerOpen(false)
    setClusterProvisionSubmitting(false)
    setClusterProvisionCheckLoading(false)
    if (clusterProvisionJob?.status === 'failed') {
      setClusterProvisionJob(null)
    }
  }

  function resumeClusterProvisionEditing() {
    setClusterProvisionJob(null)
    clusterProvisionForm.setFieldsValue(clusterProvisionDraft)
  }

  function openUserModal(user?: User) {
    setEditingUser(user ?? null)
    setUserModalOpen(true)
    userForm.setFieldsValue({
      username: user?.username ?? '',
      displayName: user?.displayName ?? '',
      password: '',
      active: user?.active ?? true,
      roleIds: user?.roles.map((role) => role.id) ?? [],
    })
  }

  function closeUserModal() {
    setUserModalOpen(false)
    setEditingUser(null)
    userForm.resetFields()
  }

  function openRoleModal(role?: Role) {
    setEditingRole(role ?? null)
    setRoleModalOpen(true)
    roleForm.setFieldsValue({
      name: role?.name ?? '',
      description: role?.description ?? '',
      permissionKeys: role?.permissions.map((permission) => permission.key) ?? [],
    })
  }

  function closeRoleModal() {
    setRoleModalOpen(false)
    setEditingRole(null)
    roleForm.resetFields()
  }

  function openRegistryDrawer(item?: RegistryIntegration) {
    setEditingRegistry(item ?? null)
    setRegistryDrawerOpen(true)
    registryForm.setFieldsValue({
      name: item?.name ?? '',
      type: item?.type ?? repositoryProviders[0]?.key ?? 'registry',
      description: item?.description ?? '',
      endpoint: item?.endpoint ?? '',
      namespace: item?.namespace ?? '',
      username: item?.username ?? '',
      secret: '',
      skipTLSVerify: item?.skipTLSVerify ?? false,
    })
  }

  function closeRegistryDrawer() {
    setRegistryDrawerOpen(false)
    setEditingRegistry(null)
    setRegistrySubmitting(false)
    registryForm.resetFields()
  }

  function openObservabilityDrawer(item?: ObservabilitySource) {
    setEditingObservabilitySource(item ?? null)
    setObservabilityDrawerOpen(true)
    observabilityForm.setFieldsValue({
      name: item?.name ?? '',
      type: item?.type ?? observabilityKinds[0]?.key ?? 'prometheus',
      description: item?.description ?? '',
      endpoint: item?.endpoint ?? '',
      username: item?.username ?? '',
      secret: '',
      dashboardPath: item?.dashboardPath ?? '/dashboards',
      skipTLSVerify: item?.skipTLSVerify ?? false,
    })
  }

  function closeObservabilityDrawer() {
    setObservabilityDrawerOpen(false)
    setEditingObservabilitySource(null)
    setObservabilitySubmitting(false)
    observabilityForm.resetFields()
  }

  function jumpToRegistryArtifacts(item?: RegistryIntegration) {
    if (item) {
      setSelectedRegistryId(item.id)
    }
    startTransition(() => {
      setActiveView('registryArtifacts')
    })
  }

  async function testRegistryConnection(values?: RegistryFormValues) {
    if (!api) {
      return
    }

    const payload = normalizeRegistryPayload(
      values ?? ((await registryForm.validateFields()) as RegistryFormValues),
    )
    const result = await api.post<{ message: string }>('/registries/test', payload)
    message.success(result.message || '仓库连接测试通过')
  }

  async function submitRegistryIntegration() {
    if (!api) {
      return
    }

    setRegistrySubmitting(true)
    try {
      const payload = normalizeRegistryPayload(
        (await registryForm.validateFields()) as RegistryFormValues,
      )
      if (editingRegistry) {
        await api.put(`/registries/${editingRegistry.id}`, payload)
        message.success('仓库集成已更新')
      } else {
        await api.post('/registries', payload)
        message.success('仓库集成成功')
      }
      await refreshRegistries(api)
      closeRegistryDrawer()
    } catch (error) {
      handleError(error, '保存仓库集成失败')
    } finally {
      setRegistrySubmitting(false)
    }
  }

  async function testSavedRegistryIntegration(item: RegistryIntegration) {
    if (!api) {
      return
    }

    try {
      const result = await api.post<{ probe?: { message?: string } }>(`/registries/${item.id}/test`)
      message.success(result.probe?.message || '仓库连接测试通过')
      await refreshRegistries(api)
    } catch (error) {
      handleError(error, '测试仓库连接失败')
      await refreshRegistries(api)
    }
  }

  async function testObservabilityConnection(values?: ObservabilityFormValues) {
    if (!api) {
      return
    }

    const payload = normalizeObservabilityPayload(
      values ?? ((await observabilityForm.validateFields()) as ObservabilityFormValues),
    )
    const result = await api.post<{ message: string }>('/observability/sources/test', payload)
    message.success(result.message || '数据源测试通过')
  }

  async function submitObservabilitySource() {
    if (!api) {
      return
    }

    setObservabilitySubmitting(true)
    try {
      const payload = normalizeObservabilityPayload(
        (await observabilityForm.validateFields()) as ObservabilityFormValues,
      )
      if (editingObservabilitySource) {
        await api.put(`/observability/sources/${editingObservabilitySource.id}`, payload)
        message.success('数据源已更新')
      } else {
        await api.post('/observability/sources', payload)
        message.success('数据源接入成功')
      }
      await refreshObservabilitySources(api)
      closeObservabilityDrawer()
    } catch (error) {
      handleError(error, '保存数据源失败')
    } finally {
      setObservabilitySubmitting(false)
    }
  }

  async function testSavedObservabilitySource(item: ObservabilitySource) {
    if (!api) {
      return
    }

    try {
      const result = await api.post<{ probe?: { message?: string } }>(
        `/observability/sources/${item.id}/test`,
      )
      message.success(result.probe?.message || '数据源测试通过')
      await refreshObservabilitySources(api)
    } catch (error) {
      handleError(error, '测试数据源失败')
      await refreshObservabilitySources(api)
    }
  }

  async function removeRegistryIntegration(item: RegistryIntegration) {
    if (!api) {
      return
    }

    modal.confirm({
      title: `删除仓库集成 ${item.name}?`,
      content: '删除后不会保留连接信息，需要重新录入凭据。',
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          await api.delete(`/registries/${item.id}`)
          message.success('仓库集成已删除')
          await refreshRegistries(api)
          setRegistryArtifactCache((current) => {
            const next = { ...current }
            delete next[item.id]
            return next
          })
        } catch (error) {
          handleError(error, '删除仓库集成失败')
        }
      },
    })
  }

  async function removeObservabilitySource(item: ObservabilitySource) {
    if (!api) {
      return
    }

    modal.confirm({
      title: `删除数据源 ${item.name}?`,
      content: item.type === 'grafana' ? '删除后仪表盘页面将不再有可嵌入入口。' : '删除后需重新接入才能继续使用。',
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          await api.delete(`/observability/sources/${item.id}`)
          message.success('数据源已删除')
          await refreshObservabilitySources(api)
        } catch (error) {
          handleError(error, '删除数据源失败')
        }
      },
    })
  }

  function closeResourceDrawer() {
    setResourceDrawerOpen(false)
    setResourceReviewOpen(false)
    setEditingResource(null)
    setResourceDraft('')
    setResourceBaselineDraft('')
    setResourceDrawerMode('create')
    setResourceDrawerPanel('yaml')
  }

  function closeResourceDetailDrawer() {
    setResourceDetailOpen(false)
    setResourceDetailLoading(false)
    setResourceDetailResource(null)
  }

  function handleError(error: unknown, fallback: string) {
    if (error instanceof HttpError) {
      message.error(error.message || fallback)
      return
    }

    if (error instanceof Error) {
      message.error(error.message || fallback)
      return
    }

    message.error(fallback)
  }

  const clusterColumns: TableColumnsType<Cluster> = [
    {
      title: '集群名称',
      dataIndex: 'name',
      render: (_, cluster) => (
        <div className="entity-stack">
          <span className="entity-primary">{cluster.name}</span>
          <span className="entity-secondary">{cluster.description || cluster.currentContext}</span>
        </div>
      ),
    },
    {
      title: '区域',
      dataIndex: 'region',
      render: (value) => value || '未设置',
    },
    {
      title: '节点数',
      key: 'nodes',
      render: (_, cluster) =>
        cluster.status === 'connected' && cluster.nodes ? (
          <Space direction="vertical" size={4}>
            <Typography.Text className="table-emphasis table-slash-metric">
              {`${cluster.nodes.normal}/${cluster.nodes.abnormal}/${cluster.nodes.total}`}
            </Typography.Text>
            <Typography.Text type="secondary" className="table-note">
              正常/异常/总
            </Typography.Text>
          </Space>
        ) : (
          <Typography.Text type="secondary" className="table-note">
            -
          </Typography.Text>
        ),
    },
    {
      title: 'API状态',
      dataIndex: 'status',
      render: (value, cluster) => (
        <Space direction="vertical" size={4}>
          <Tag color={clusterStatusColor(value)}>{clusterStatusLabel(value)}</Tag>
          <Typography.Text type="secondary" className="table-note">
            {cluster.lastError || `最近校验：${formatDate(cluster.lastConnectedAt)}`}
          </Typography.Text>
        </Space>
      ),
    },
    {
      title: 'K8s 版本',
      dataIndex: 'version',
      render: (value) => value || '-',
    },
    {
      title: 'CRI 版本',
      dataIndex: 'criVersion',
      render: (value) => value || '-',
    },
    {
      title: '接入时间',
      dataIndex: 'createdAt',
      render: (value) => formatDate(value),
    },
    {
      title: '运行状态',
      dataIndex: 'mode',
      render: (_, cluster) => {
        const runtimeState = resolveClusterRuntimeState(cluster)
        return (
          <Space direction="vertical" size={4}>
            <Tag color={runtimeState.color}>{runtimeState.label}</Tag>
            <Typography.Text type="secondary" className="table-note">
              {runtimeState.detail}
            </Typography.Text>
          </Space>
        )
      },
    },
    {
      title: '操作',
      key: 'actions',
      render: (_, cluster) =>
        canWriteClusters ? (
          <Space size="small">
            <Button size="small" onClick={() => void testCluster(cluster)}>
              测试连接
            </Button>
            <Button
              size="small"
              loading={clusterModeSavingId === cluster.id}
              disabled={cluster.status !== 'connected'}
              onClick={() =>
                void updateClusterMode(
                  cluster,
                  cluster.mode === 'maintenance' ? 'ready' : 'maintenance',
                )
              }
            >
              {cluster.mode === 'maintenance' ? '恢复就绪' : '进入维护'}
            </Button>
            <Button size="small" onClick={() => openClusterDrawer(cluster)}>
              编辑
            </Button>
            <Button danger size="small" onClick={() => void removeCluster(cluster)}>
              删除
            </Button>
          </Space>
        ) : null,
    },
  ]

  const resourceColumns: TableColumnsType<K8sObject> = (() => {
    const actionColumn: TableColumnsType<K8sObject>[number] = {
      title: '操作',
      key: 'actions',
      render: (_, resource) => (
        <Space size="small">
          <Button
            size="small"
            onClick={(event) => {
              event.stopPropagation()
              void openResourceEditor(resource)
            }}
          >
            YAML
          </Button>
          {canWriteResources ? (
            <Button
              size="small"
              onClick={(event) => {
                event.stopPropagation()
                openResourceClone(resource)
              }}
            >
              克隆
            </Button>
          ) : null}
          {canWriteResources ? (
            <Button
              danger
              size="small"
              onClick={(event) => {
                event.stopPropagation()
                void removeResource(resource)
              }}
            >
              删除
            </Button>
          ) : null}
        </Space>
      ),
    }

    if (selectedResourceDefinition?.key === 'deployment') {
      return [
        {
          title: '名称',
          key: 'name',
          render: (_, resource) => (
            <div className="entity-stack">
              <span className="entity-primary">{resourceName(resource)}</span>
              <span className="entity-secondary">{resource.kind}</span>
            </div>
          ),
        },
        {
          title: '命名空间',
          key: 'namespace',
          render: (_, resource) => resourceNamespace(resource) || 'Cluster Scope',
        },
        {
          title: 'Ready',
          key: 'ready',
          render: (_, resource) => renderDeploymentReadySummary(resource),
        },
        {
          title: '镜像',
          key: 'images',
          render: (_, resource) => (
            <div className="entity-stack">
              <span className="entity-primary">{primaryContainerImage(resource) || '-'}</span>
              <span className="entity-secondary">
                {renderExtraContainerSummary(resource)}
              </span>
            </div>
          ),
        },
        {
          title: '策略',
          key: 'strategy',
          render: (_, resource) =>
            valueAsString(readNestedValue(resource, ['spec', 'strategy', 'type'])) || 'RollingUpdate',
        },
        {
          title: '创建时间',
          key: 'creationTimestamp',
          render: (_, resource) => formatDate(resource.metadata?.creationTimestamp),
        },
        actionColumn,
      ]
    }

    return [
      {
        title: '名称',
        key: 'name',
        render: (_, resource) => (
          <div className="entity-stack">
            <span className="entity-primary">{resourceName(resource)}</span>
            <span className="entity-secondary">{resource.kind}</span>
          </div>
        ),
      },
      {
        title: '命名空间',
        key: 'namespace',
        render: (_, resource) => resourceNamespace(resource) || 'Cluster Scope',
      },
      {
        title: '标签',
        key: 'labels',
        render: (_, resource) => (
          <div className="label-preview-row">
            {renderLabelPreview(resource)}
          </div>
        ),
      },
      {
        title: '创建时间',
        key: 'creationTimestamp',
        render: (_, resource) => formatDate(resource.metadata?.creationTimestamp),
      },
      actionColumn,
    ]
  })()

  const userColumns: TableColumnsType<User> = [
    {
      title: '用户',
      key: 'user',
      render: (_, user) => (
        <div className="entity-stack">
          <span className="entity-primary">{user.displayName}</span>
          <span className="entity-secondary">{user.username}</span>
        </div>
      ),
    },
    {
      title: '角色',
      key: 'roles',
      render: (_, user) => (
        <Space wrap>
          {user.roles.map((role) => (
            <Tag key={role.id}>{role.name}</Tag>
          ))}
        </Space>
      ),
    },
    {
      title: '状态',
      dataIndex: 'active',
      render: (value) => (
        <Tag color={value ? 'green' : 'default'}>{value ? 'ACTIVE' : 'DISABLED'}</Tag>
      ),
    },
    {
      title: '最近登录',
      dataIndex: 'lastLoginAt',
      render: (value) => formatDate(value),
    },
    {
      title: '操作',
      key: 'actions',
      render: (_, user) =>
        canWriteUsers ? (
          <Space size="small">
            <Button size="small" onClick={() => openUserModal(user)}>
              编辑
            </Button>
            <Button danger size="small" onClick={() => void removeUser(user)}>
              删除
            </Button>
          </Space>
        ) : null,
    },
  ]

  const roleColumns: TableColumnsType<Role> = [
    {
      title: '角色',
      key: 'role',
      render: (_, role) => (
        <div className="entity-stack">
          <span className="entity-primary">{role.name}</span>
          <span className="entity-secondary">{role.description || '未填写说明'}</span>
        </div>
      ),
    },
    {
      title: '类型',
      dataIndex: 'builtIn',
      render: (value) => (
        <Tag color={value ? 'geekblue' : 'default'}>{value ? 'BUILT-IN' : 'CUSTOM'}</Tag>
      ),
    },
    {
      title: '权限数量',
      key: 'permissions',
      render: (_, role) => role.permissions.length,
    },
    {
      title: '操作',
      key: 'actions',
      render: (_, role) =>
        canWriteRoles ? (
          <Space size="small">
            <Button size="small" onClick={() => openRoleModal(role)}>
              编辑
            </Button>
            {!role.builtIn ? (
              <Button danger size="small" onClick={() => void removeRole(role)}>
                删除
              </Button>
            ) : null}
          </Space>
        ) : null,
    },
  ]

  const registryColumns: TableColumnsType<RegistryIntegration> = [
    {
      title: '仓库',
      key: 'registry',
      render: (_, item) => (
        <div className="entity-stack">
          <span className="entity-primary">{item.name}</span>
          <span className="entity-secondary">{item.description || item.endpoint}</span>
        </div>
      ),
    },
    {
      title: '类型',
      dataIndex: 'type',
      render: (value) => <Tag>{String(value).toUpperCase()}</Tag>,
    },
    {
      title: 'Endpoint',
      dataIndex: 'endpoint',
      render: (value, item) => (
        <Space direction="vertical" size={4}>
          <Typography.Text>{value}</Typography.Text>
          <Typography.Text type="secondary" className="table-note">
            {item.namespace ? `前缀 ${item.namespace}` : '全仓库'}
          </Typography.Text>
        </Space>
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      render: (value, item) => (
        <Space direction="vertical" size={4}>
          <Tag color={clusterStatusColor(value)}>{clusterStatusLabel(value)}</Tag>
          <Typography.Text type="secondary" className="table-note">
            {item.lastError || `最近校验：${formatDate(item.lastCheckedAt)}`}
          </Typography.Text>
        </Space>
      ),
    },
    {
      title: '接入时间',
      dataIndex: 'createdAt',
      render: (value) => formatDate(value),
    },
    {
      title: '操作',
      key: 'actions',
      render: (_, item) => (
        <Space size="small">
          <Button size="small" onClick={() => jumpToRegistryArtifacts(item)}>
            浏览镜像
          </Button>
          {canWriteRegistries ? (
            <Button size="small" onClick={() => void testSavedRegistryIntegration(item)}>
              测试
            </Button>
          ) : null}
          {canWriteRegistries ? (
            <Button size="small" onClick={() => openRegistryDrawer(item)}>
              编辑
            </Button>
          ) : null}
          {canWriteRegistries ? (
            <Button danger size="small" onClick={() => void removeRegistryIntegration(item)}>
              删除
            </Button>
          ) : null}
        </Space>
      ),
    },
  ]

  const observabilityColumns: TableColumnsType<ObservabilitySource> = [
    {
      title: '数据源',
      key: 'source',
      render: (_, item) => (
        <div className="entity-stack">
          <span className="entity-primary">{item.name}</span>
          <span className="entity-secondary">{item.description || item.endpoint}</span>
        </div>
      ),
    },
    {
      title: '类型',
      dataIndex: 'type',
      render: (value) => (
        <Tag color={value === 'grafana' ? 'geekblue' : 'default'}>{String(value)}</Tag>
      ),
    },
    {
      title: 'Endpoint',
      dataIndex: 'endpoint',
      render: (value, item) => (
        <Space direction="vertical" size={4}>
          <Typography.Text>{value}</Typography.Text>
          <Typography.Text type="secondary" className="table-note">
            {item.type === 'grafana' ? item.dashboardPath || '/dashboards' : '健康检测可用'}
          </Typography.Text>
        </Space>
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      render: (value, item) => (
        <Space direction="vertical" size={4}>
          <Tag color={clusterStatusColor(value)}>{clusterStatusLabel(value)}</Tag>
          <Typography.Text type="secondary" className="table-note">
            {item.lastError || `最近校验：${formatDate(item.lastCheckedAt)}`}
          </Typography.Text>
        </Space>
      ),
    },
    {
      title: '操作',
      key: 'actions',
      render: (_, item) => (
        <Space size="small">
          {item.type === 'grafana' ? (
            <Button
              size="small"
              onClick={() => {
                setSelectedGrafanaSourceId(item.id)
                startTransition(() => setActiveView('observabilityDashboards'))
              }}
            >
              打开仪表盘
            </Button>
          ) : null}
          {canWriteObservability ? (
            <Button size="small" onClick={() => void testSavedObservabilitySource(item)}>
              测试
            </Button>
          ) : null}
          {canWriteObservability ? (
            <Button size="small" onClick={() => openObservabilityDrawer(item)}>
              编辑
            </Button>
          ) : null}
          {canWriteObservability ? (
            <Button danger size="small" onClick={() => void removeObservabilitySource(item)}>
              删除
            </Button>
          ) : null}
        </Space>
      ),
    },
  ]

  if (!session) {
    return <LoginScreen loading={submittingLogin} onSubmit={handleLogin} />
  }

  const renderDashboardView = () => (
    <div className="view-grid">
      <section className="surface-hero">
        <div>
          <span className="surface-kicker">Workbench</span>
          <Typography.Title level={2}>集群仪表盘</Typography.Title>
          <Typography.Paragraph type="secondary">
            聚合当前工作集群的健康状态与资源请求。
          </Typography.Paragraph>
        </div>
        <div className="hero-action-grid">
          {canReadResources ? (
            <Button onClick={() => startTransition(() => setActiveView('resources'))}>
              进入资源中心
            </Button>
          ) : null}
          {canWriteClusters ? (
            <Button type="primary" icon={<PlusOutlined />} onClick={() => openClusterDrawer()}>
              接入新集群
            </Button>
          ) : null}
        </div>
      </section>

      <section className="overview-grid">
        <article className="overview-unit">
          <span className="metric-label">集群</span>
          <strong className="metric-value">{dashboard?.clusters ?? clusters.length}</strong>
          <span className="metric-foot">已连通 {connectedClusterCount} 个</span>
        </article>
        <article className="overview-unit">
          <span className="metric-label">节点</span>
          {dashboardClusterMetrics?.status === 'connected' ? (
            <>
              <div className="status-pair">
                <div className="status-item">
                  <span className="status-caption">正常</span>
                  <strong className="status-value">{dashboardClusterMetrics.nodes.normal}</strong>
                </div>
                <div className="status-item status-item-danger">
                  <span className="status-caption">异常</span>
                  <strong className="status-value status-value-danger">
                    {dashboardClusterMetrics.nodes.abnormal}
                  </strong>
                </div>
              </div>
              <span className="metric-foot">总数 {dashboardClusterMetrics.nodes.total}</span>
            </>
          ) : (
            <span className="metric-foot">等待集群指标</span>
          )}
        </article>
        <article className="overview-unit">
          <span className="metric-label">Pods</span>
          {dashboardClusterMetrics?.status === 'connected' ? (
            <>
              <div className="status-pair">
                <div className="status-item">
                  <span className="status-caption">正常</span>
                  <strong className="status-value">{dashboardClusterMetrics.pods.normal}</strong>
                </div>
                <div className="status-item status-item-danger">
                  <span className="status-caption">异常</span>
                  <strong className="status-value status-value-danger">
                    {dashboardClusterMetrics.pods.abnormal}
                  </strong>
                </div>
              </div>
              <span className="metric-foot">总数 {dashboardClusterMetrics.pods.total}</span>
            </>
          ) : (
            <span className="metric-foot">等待集群指标</span>
          )}
        </article>
      </section>

      <section className="dashboard-capacity-grid">
        <article className="soft-panel section-block">
          <div className="section-headline compact-headline">
            <div>
              <Typography.Title level={4}>CPU / 内存请求</Typography.Title>
              <Typography.Text type="secondary">
                当前集群 Pod Request 与总资源对比。
              </Typography.Text>
            </div>
          </div>

          {dashboardClusterMetrics?.status === 'connected' ? (
            <div className="capacity-stack">
              <div className="capacity-panel">
                <div className="capacity-head">
                  <strong>CPU</strong>
                  <span>{formatPercent(dashboardClusterMetrics.cpu.percentage)}</span>
                </div>
                <div className="capacity-metric-grid">
                  <div className="capacity-metric-unit">
                    <span>Request</span>
                    <strong>{dashboardClusterMetrics.cpu.request}</strong>
                  </div>
                  <div className="capacity-metric-unit">
                    <span>Total</span>
                    <strong>{dashboardClusterMetrics.cpu.total}</strong>
                  </div>
                </div>
                <Progress
                  percent={Math.min(100, Math.round(dashboardClusterMetrics.cpu.percentage))}
                  showInfo={false}
                />
                <div className="capacity-foot">
                  <span>基于 Pod requests 汇总</span>
                </div>
              </div>

              <div className="capacity-panel">
                <div className="capacity-head">
                  <strong>Memory</strong>
                  <span>{formatPercent(dashboardClusterMetrics.memory.percentage)}</span>
                </div>
                <div className="capacity-metric-grid">
                  <div className="capacity-metric-unit">
                    <span>Request</span>
                    <strong>{dashboardClusterMetrics.memory.request}</strong>
                  </div>
                  <div className="capacity-metric-unit">
                    <span>Total</span>
                    <strong>{dashboardClusterMetrics.memory.total}</strong>
                  </div>
                </div>
                <Progress
                  percent={Math.min(100, Math.round(dashboardClusterMetrics.memory.percentage))}
                  showInfo={false}
                />
                <div className="capacity-foot">
                  <span>包含所有运行中工作负载</span>
                </div>
              </div>
            </div>
          ) : (
            <Empty description="暂无集群容量数据" />
          )}
        </article>

        <aside className="soft-panel section-block">
          <div className="section-headline compact-headline">
            <div>
              <Typography.Title level={4}>当前集群</Typography.Title>
              <Typography.Text type="secondary">
                仪表盘当前展示的集群入口。
              </Typography.Text>
            </div>
          </div>

          {dashboardClusterMetrics ? (
            <>
              {dashboardClusterMetrics.status === 'error' ? (
                <Alert
                  type="warning"
                  showIcon
                  message="集群实时指标拉取失败"
                  description={dashboardClusterMetrics.lastError || '请稍后重试'}
                />
              ) : null}
              <div className="hint-block">
                <div className="hint-row">
                  <span className="hint-label">Cluster</span>
                  <span>{dashboardClusterMetrics.name}</span>
                </div>
                <div className="hint-row">
                  <span className="hint-label">Version</span>
                  <span>{dashboardClusterMetrics.version || '-'}</span>
                </div>
                <div className="hint-row">
                  <span className="hint-label">Context</span>
                  <span>{dashboardClusterMetrics.currentContext || '-'}</span>
                </div>
                <div className="hint-row">
                  <span className="hint-label">Server</span>
                  <span>{dashboardClusterMetrics.server || '-'}</span>
                </div>
                <div className="hint-row">
                  <span className="hint-label">Node Total</span>
                  <span>{dashboardClusterMetrics.nodes.total}</span>
                </div>
                <div className="hint-row">
                  <span className="hint-label">Pod Total</span>
                  <span>{dashboardClusterMetrics.pods.total}</span>
                </div>
                <div className="hint-row">
                  <span className="hint-label">CPU Total</span>
                  <span>{dashboardClusterMetrics.cpu.total}</span>
                </div>
                <div className="hint-row">
                  <span className="hint-label">Memory Total</span>
                  <span>{dashboardClusterMetrics.memory.total}</span>
                </div>
                <div className="hint-row">
                  <span className="hint-label">Last Sync</span>
                  <span>{formatDate(dashboardClusterMetrics.lastConnectedAt)}</span>
                </div>
              </div>
            </>
          ) : (
            <Empty description="没有可用的集群指标" />
          )}
        </aside>
      </section>
    </div>
  )

  const renderClustersView = () => (
    <div className="view-grid clusters-view">
      <section className="soft-panel section-block">
        <div className="cluster-toolbar">
          <Input
            allowClear
            className="cluster-toolbar-search"
            placeholder="输入集群名称 / 区域搜索"
            value={clusterSearchDraft}
            onChange={(event) => {
              const nextValue = event.target.value
              setClusterSearchDraft(nextValue)
              if (!nextValue.trim()) {
                setClusterSearch('')
              }
            }}
            onPressEnter={() => setClusterSearch(clusterSearchDraft.trim())}
          />
          <div className="cluster-toolbar-controls">
            <Segmented
              className="cluster-toolbar-filter"
              value={clusterStatusFilter}
              onChange={(value) =>
                setClusterStatusFilter(value as 'all' | 'connected' | 'error')
              }
              options={[
                { label: '全部', value: 'all' },
                { label: '已连通', value: 'connected' },
                { label: '异常', value: 'error' },
              ]}
            />
            <Button
              className="cluster-refresh-button"
              shape="circle"
              icon={<SyncOutlined />}
              onClick={() => void refreshClusters(api)}
            />
            {canWriteClusters ? (
              <Button icon={<DeploymentUnitOutlined />} onClick={() => openClusterProvisionDrawer()}>
                创建集群
              </Button>
            ) : null}
            {canWriteClusters ? (
              <Button type="primary" icon={<PlusOutlined />} onClick={() => openClusterDrawer()}>
                接入集群
              </Button>
            ) : null}
          </div>
        </div>

        <div className="cluster-summary-strip">
          <div className="cluster-summary-chip">
            <span>全部集群</span>
            <strong>{clusters.length}</strong>
          </div>
          <div className="cluster-summary-chip">
            <span>已连通</span>
            <strong>{connectedClusterCount}</strong>
          </div>
          <div className="cluster-summary-chip cluster-summary-chip-danger">
            <span>异常</span>
            <strong>{clusters.length - connectedClusterCount}</strong>
          </div>
        </div>
      </section>

      <section className="soft-panel section-block">
        <Table
          rowKey="id"
          columns={clusterColumns}
          dataSource={filteredClusters}
          scroll={{ x: 1400 }}
          pagination={{ pageSize: 8 }}
          rowClassName={(record) => (record.id === selectedClusterId ? 'is-selected-row' : '')}
          onRow={(record) => ({
            onClick: () => {
              setSelectedClusterId(record.id)
            },
          })}
          locale={{ emptyText: <Empty description="没有符合条件的集群" /> }}
        />
      </section>
    </div>
  )

  const renderInspectionView = () => {
    const runtimeState = selectedCluster ? resolveClusterRuntimeState(selectedCluster) : null

    return (
      <div className="view-grid">
        <section className="soft-panel section-block">
          <div className="section-headline">
            <div>
              <Typography.Title level={4}>一键巡检</Typography.Title>
              <Typography.Text type="secondary">
                节点、Pod、容量、存储和告警一次看清。
              </Typography.Text>
            </div>
            <Button
              type="primary"
              icon={<CheckCircleOutlined />}
              loading={clusterInspectionLoading}
              disabled={!selectedClusterId}
              onClick={() => void refreshInspection()}
            >
              {selectedInspectionReport ? '重新巡检' : '开始巡检'}
            </Button>
          </div>

          <div className="inspection-toolbar">
            <Select
              placeholder="选择要巡检的集群"
              value={selectedClusterId}
              onChange={setSelectedClusterId}
              options={clusters.map((cluster) => ({
                label: cluster.name,
                value: cluster.id,
              }))}
            />
            <div className="inspection-context-line">
              {selectedCluster ? (
                <>
                  <Tag color={clusterStatusColor(selectedCluster.status)}>
                    API {clusterStatusLabel(selectedCluster.status)}
                  </Tag>
                  {runtimeState ? <Tag color={runtimeState.color}>{runtimeState.label}</Tag> : null}
                  <span className="inspection-context-copy">
                    区域 {selectedCluster.region || '未设置'}
                  </span>
                  <span className="inspection-context-copy">
                    版本 {selectedInspectionReport?.version || selectedCluster.version || '-'}
                  </span>
                  <span className="inspection-context-copy">
                    巡检 {formatDate(selectedInspectionReport?.inspectedAt)}
                  </span>
                </>
              ) : (
                <Typography.Text type="secondary">先选择一个已接入集群</Typography.Text>
              )}
            </div>
          </div>
        </section>

        {!selectedClusterId ? (
          <section className="soft-panel section-block">
            <Empty description="先选择一个已接入集群" />
          </section>
        ) : clusterInspectionLoading && !selectedInspectionReport ? (
          <section className="soft-panel section-block inspection-loading-panel">
            <Spin tip="正在执行集群巡检" />
          </section>
        ) : !selectedInspectionReport ? (
          <section className="soft-panel section-block">
            <Empty description="点击开始巡检，生成当前集群健康报告" />
          </section>
        ) : (
          <>
            <section className="inspection-summary-grid">
              <article
                className={`inspection-summary-card inspection-summary-card-${selectedInspectionReport.summary.status}`}
              >
                <span className="metric-label">巡检结论</span>
                <strong className="inspection-summary-value">
                  {inspectionStatusLabel(selectedInspectionReport.summary.status)}
                </strong>
                <span className="metric-foot">
                  通过 {selectedInspectionReport.summary.passed} / 告警{' '}
                  {selectedInspectionReport.summary.warning} / 失败{' '}
                  {selectedInspectionReport.summary.failed}
                </span>
              </article>

              <article className="inspection-summary-card">
                <span className="metric-label">节点</span>
                <strong className="inspection-summary-value table-slash-metric">
                  {`${selectedInspectionReport.overview.nodes.normal}/${selectedInspectionReport.overview.nodes.abnormal}/${selectedInspectionReport.overview.nodes.total}`}
                </strong>
                <span className="metric-foot">正常 / 异常 / 总</span>
              </article>

              <article className="inspection-summary-card">
                <span className="metric-label">Pods</span>
                <strong className="inspection-summary-value table-slash-metric">
                  {`${selectedInspectionReport.overview.pods.normal}/${selectedInspectionReport.overview.pods.abnormal}/${selectedInspectionReport.overview.pods.total}`}
                </strong>
                <span className="metric-foot">正常 / 异常 / 总</span>
              </article>

              <article className="inspection-summary-card inspection-summary-card-capacity">
                <span className="metric-label">容量水位</span>
                <div className="inspection-capacity-pair">
                  <div className="inspection-capacity-row">
                    <span>CPU</span>
                    <strong>{formatPercent(selectedInspectionReport.overview.cpu.percentage)}</strong>
                  </div>
                  <div className="inspection-capacity-row">
                    <span>MEM</span>
                    <strong>
                      {formatPercent(selectedInspectionReport.overview.memory.percentage)}
                    </strong>
                  </div>
                </div>
                <span className="metric-foot">
                  {selectedInspectionReport.overview.cpu.request} /{' '}
                  {selectedInspectionReport.overview.cpu.total}
                </span>
              </article>
            </section>

            <section className="inspection-item-grid">
              {selectedInspectionReport.items.map((item) => (
                <article
                  key={item.key}
                  className={`soft-panel inspection-item-card inspection-item-card-${item.status}`}
                >
                  <div className="inspection-item-head">
                    <div className="inspection-item-copy">
                      <span className="surface-kicker">{item.category}</span>
                      <strong>{item.label}</strong>
                    </div>
                    <Tag color={inspectionStatusColor(item.status)}>
                      {inspectionStatusLabel(item.status)}
                    </Tag>
                  </div>

                  <div className="inspection-item-body">
                    <strong>{item.summary}</strong>
                    <span>{item.detail}</span>
                  </div>

                  {(item.findings?.length ?? 0) > 0 ? (
                    <div className="inspection-findings">
                      {item.findings!.map((finding, index) => (
                        <div
                          key={`${item.key}-${finding.scope}-${index}`}
                          className="inspection-finding"
                        >
                          <strong>{finding.scope}</strong>
                          <span>{finding.detail}</span>
                        </div>
                      ))}
                    </div>
                  ) : null}
                </article>
              ))}
            </section>
          </>
        )}
      </div>
    )
  }

  const renderResourcesView = () => (
    <div className="view-grid">
      <section className="soft-panel section-block">
        <div className="section-headline">
          <div>
            <Typography.Title level={4}>资源操作面</Typography.Title>
            <Typography.Text type="secondary">
              先锁定集群和范围，再看 YAML、Diff 与审计。
            </Typography.Text>
          </div>
          <Space>
            <Button icon={<ReloadOutlined />} onClick={() => {
              if (!api || !selectedClusterId || !selectedResourceDefinition) {
                return
              }
              void refreshResources(
                api,
                selectedClusterId,
                selectedResourceDefinition,
                selectedNamespace,
                deferredSearch,
              )
            }}>
              刷新
            </Button>
            {canWriteResources ? (
              <Button onClick={() => openStandardResourceTemplate()}>
                标准模板
              </Button>
            ) : null}
            {canWriteResources ? (
              <Button type="primary" icon={<PlusOutlined />} onClick={() => void openResourceEditor()}>
                新建资源
              </Button>
            ) : null}
          </Space>
        </div>

        <div className="toolbar-grid toolbar-grid-pair">
          <Select
            placeholder="选择集群"
            value={selectedClusterId}
            onChange={(value) => {
              setSelectedClusterId(value)
              setSelectedResourceKey('')
            }}
            options={clusters.map((cluster) => ({
              label: cluster.name,
              value: cluster.id,
            }))}
          />
          <Select
            value={selectedResourceType}
            onChange={(value) => {
              setSelectedResourceType(value)
              setSelectedResourceKey('')
            }}
            options={resourceTypes.map((item) => ({
              label: item.label,
              value: item.key,
            }))}
          />
          <Select
            value={selectedNamespace}
            onChange={setSelectedNamespace}
            disabled={!selectedResourceDefinition?.namespaced}
            options={[
              { label: '全部命名空间', value: '' },
              ...namespaces.map((namespace) => ({
                label: namespace.name,
                value: namespace.name,
              })),
            ]}
          />
          <Input.Search
            allowClear
            placeholder="按资源名称搜索"
            value={resourceSearch}
            onChange={(event) => setResourceSearch(event.target.value)}
          />
        </div>

      </section>

      <section className="soft-panel section-block">
        {selectedClusterId ? (
          <Table
            rowKey={(resource) => buildResourceSelectionKey(resource)}
            columns={resourceColumns}
            dataSource={resources}
            loading={resourceLoading}
            pagination={{ pageSize: 10 }}
            locale={{ emptyText: <Empty description="当前条件下没有资源" /> }}
            rowClassName={(record) =>
              buildResourceSelectionKey(record) === selectedResourceKey
                ? 'is-selected-row is-clickable-row'
                : 'is-clickable-row'
            }
            onRow={(record) => ({
              onClick: () => {
                void openResourceDetail(record)
              },
            })}
          />
        ) : (
          <Empty description="先选择一个已接入集群" />
        )}
      </section>
    </div>
  )

  const renderRegistryVersionTable = (versions: RegistryArtifactVersion[], repositoryKey: string) => (
    <Table
      className="registry-version-table"
      rowKey={(version) => `${repositoryKey}:${version.tag}`}
      size="small"
      pagination={false}
      dataSource={versions}
      columns={[
        {
          title: '版本',
          dataIndex: 'tag',
          render: (value) => <Tag>{value}</Tag>,
        },
        {
          title: 'Digest',
          dataIndex: 'digest',
          render: (value) => value || '-',
        },
        {
          title: '构建时间',
          dataIndex: 'buildTime',
          render: (value) => formatDate(value),
        },
      ]}
    />
  )

  const renderRegistryImageTable = (space: RegistryImageSpace) => (
    <Table
      className="registry-hierarchy-table"
      rowKey={(image) => image.repository}
      size="middle"
      pagination={false}
      dataSource={space.images}
      expandable={{
        expandedRowKeys: expandedRegistryImageKeys[space.name] ?? [],
        onExpand: (expanded, record) => {
          setExpandedRegistryImageKeys((current) => ({
            ...current,
            [space.name]: expanded ? [record.repository] : [],
          }))
        },
        expandedRowRender: (image) => renderRegistryVersionTable(image.versions, image.repository),
        rowExpandable: (image) => image.versions.length > 0,
      }}
      columns={[
        {
          title: '镜像',
          key: 'name',
          render: (_, image: RegistryImage) => (
            <div className="entity-stack">
              <span className="entity-primary">{image.name}</span>
              <span className="entity-secondary">{image.repository}</span>
            </div>
          ),
        },
        {
          title: '版本数',
          dataIndex: 'versionCount',
          render: (value) => <Typography.Text className="table-emphasis">{value}</Typography.Text>,
        },
        {
          title: '最近构建',
          key: 'latestBuildTime',
          render: (_, image: RegistryImage) => formatDate(image.latestBuildTime),
        },
      ]}
    />
  )

  const renderRegistryHierarchyTable = (
    spaces: RegistryImageSpace[],
    emptyDescription: string,
  ) => {
    if (spaces.length === 0) {
      return <Empty description={emptyDescription} />
    }

    return (
      <Table
        className="registry-hierarchy-table"
        rowKey={(space) => space.name}
        loading={registryArtifactsLoading}
        pagination={false}
        dataSource={spaces}
        expandable={{
          expandedRowKeys: expandedRegistrySpaceKeys,
          onExpand: (expanded, record) => {
            setExpandedRegistrySpaceKeys(expanded ? [record.name] : [])
            setExpandedRegistryImageKeys((current) => ({
              ...current,
              [record.name]: [],
            }))
          },
          expandedRowRender: (space) => renderRegistryImageTable(space),
          rowExpandable: (space) => space.images.length > 0,
        }}
        columns={[
          {
            title: '镜像空间',
            key: 'name',
            render: (_, space: RegistryImageSpace) => (
              <div className="entity-stack">
                <span className="entity-primary">{space.name}</span>
                <span className="entity-secondary">单击展开镜像与版本列表</span>
              </div>
            ),
          },
          {
            title: '镜像数',
            dataIndex: 'imageCount',
            render: (value) => <Typography.Text className="table-emphasis">{value}</Typography.Text>,
          },
          {
            title: '版本数',
            dataIndex: 'versionCount',
            render: (value) => <Typography.Text className="table-emphasis">{value}</Typography.Text>,
          },
        ]}
      />
    )
  }

  const renderRegistryIntegrationsView = () => (
    <div className="view-grid">
      <section className="soft-panel section-block">
        <div className="section-headline">
          <div>
            <Typography.Title level={4}>仓库集成</Typography.Title>
            <Typography.Text type="secondary">
              接入 Registry、Harbor、Nexus、JFrog，并统一浏览镜像版本。
            </Typography.Text>
          </div>
          <Space>
            <Button icon={<ReloadOutlined />} onClick={() => void refreshRegistries(api)}>
              刷新
            </Button>
            {canWriteRegistries ? (
              <Button type="primary" icon={<PlusOutlined />} onClick={() => openRegistryDrawer()}>
                新建接入
              </Button>
            ) : null}
          </Space>
        </div>

        <div className="filter-bar">
          <Input.Search
            allowClear
            placeholder="按仓库名、类型、地址搜索"
            value={registrySearch}
            onChange={(event) => setRegistrySearch(event.target.value)}
          />
        </div>
      </section>

      <section className="soft-panel section-block">
        <Table
          rowKey="id"
          columns={registryColumns}
          dataSource={filteredRegistries}
          pagination={{ pageSize: 8 }}
          rowClassName={(record) => (record.id === selectedRegistryId ? 'is-selected-row' : '')}
          onRow={(record) => ({
            onClick: () => setSelectedRegistryId(record.id),
          })}
          locale={{
            emptyText: (
              <Empty
                description="还没有仓库接入"
                image={Empty.PRESENTED_IMAGE_SIMPLE}
              />
            ),
          }}
        />
      </section>

      <section className="soft-panel section-block">
        <div className="section-headline compact-headline">
          <div>
            <Typography.Title level={5}>镜像空间概览</Typography.Title>
            <Typography.Text type="secondary">
              选中一个仓库后，按镜像空间查看镜像数量和版本分布。
            </Typography.Text>
          </div>
        </div>

        {selectedRegistry ? (
          <>
            <div className="context-inline-strip">
              <Tag color={clusterStatusColor(selectedRegistry.status)}>
                {clusterStatusLabel(selectedRegistry.status)}
              </Tag>
              <span>{selectedRegistry.name}</span>
              <span>{`${registryImageSpaceCount} 个镜像空间`}</span>
              <span>{`${registryImageCount} 个镜像`}</span>
              <span>{`${registryVersionCount} 个版本`}</span>
            </div>
            <div className="registry-hierarchy-hint">单击镜像空间，再单击镜像，即可查看版本表格。</div>
            {renderRegistryHierarchyTable(registryImageSpaces, '当前仓库下还没有可展示的镜像空间')}
          </>
        ) : (
          <Empty description="先选择一个仓库接入" />
        )}
      </section>
    </div>
  )

  const renderRegistryArtifactsView = () => (
    <div className="view-grid">
      <section className="soft-panel section-block">
        <div className="section-headline">
          <div>
            <Typography.Title level={4}>镜像列表</Typography.Title>
            <Typography.Text type="secondary">
              按仓库和镜像空间查看全部镜像、版本和构建时间。
            </Typography.Text>
          </div>
          <Space>
            <Button
              icon={<ReloadOutlined />}
              onClick={() => void refreshRegistryArtifacts(api, selectedRegistryId)}
            >
              刷新
            </Button>
            {canWriteRegistries ? (
              <Button onClick={() => startTransition(() => setActiveView('registryIntegrations'))}>
                管理接入
              </Button>
            ) : null}
          </Space>
        </div>

        <div className="toolbar-grid toolbar-grid-triple">
          <Select
            placeholder="选择仓库"
            value={selectedRegistryId}
            onChange={setSelectedRegistryId}
            options={registries.map((item) => ({
              label: item.name,
              value: item.id,
            }))}
          />
          <Input.Search
            allowClear
            placeholder="按镜像名或版本搜索"
            value={registryArtifactSearch}
            onChange={(event) => setRegistryArtifactSearch(event.target.value)}
          />
        </div>

        {selectedRegistry ? (
          <div className="context-inline-strip">
            <Tag color={clusterStatusColor(selectedRegistry.status)}>
              {clusterStatusLabel(selectedRegistry.status)}
            </Tag>
            <span>{selectedRegistry.endpoint}</span>
            <span>{selectedRegistry.namespace ? `镜像空间前缀 ${selectedRegistry.namespace}` : '全仓库'}</span>
            {registryArtifactsSnapshot ? (
              <span>{`${filteredRegistryImageSpaceCount} 个镜像空间 / ${filteredRegistryImageCount} 个镜像 / ${filteredRegistryVersionCount} 个版本`}</span>
            ) : null}
          </div>
        ) : null}
      </section>

      <section className="soft-panel section-block">
        {selectedRegistry ? (
          <>
            <div className="registry-hierarchy-hint">
              单击镜像空间展开镜像，再展开具体镜像查看 `ver1 / ver2 / ver3` 版本表格。
            </div>
            {renderRegistryHierarchyTable(filteredRegistryImageSpaces, '当前条件下没有匹配的镜像版本')}
          </>
        ) : (
          <Empty description="先接入一个镜像仓库" />
        )}

        {registryArtifactsSnapshot?.truncated ? (
          <Alert
            type="info"
            showIcon
            message="当前只展示了部分结果"
            description="为保证响应速度，平台会优先返回前一批仓库和版本记录。"
          />
        ) : null}
      </section>
    </div>
  )

  const renderObservabilityDashboardView = () => (
    <div className="grafana-immersive-shell">
      {selectedGrafanaSource ? (
        <div className="grafana-immersive-toolbar">
          <div className="grafana-immersive-toolbar-main">
            <span className="grafana-immersive-kicker">Grafana</span>
            <Segmented<GrafanaWorkspaceSection>
              className="grafana-immersive-nav"
              options={[
                { label: 'Dashboard', value: 'dashboard' },
                { label: 'Explore', value: 'explore' },
              ]}
              value={grafanaWorkspaceSection}
              onChange={(value) => setGrafanaWorkspaceSection(value)}
            />
            <Select
              className="grafana-immersive-select"
              placeholder="选择 Grafana"
              value={selectedGrafanaSourceId}
              onChange={setSelectedGrafanaSourceId}
              options={grafanaSources.map((item) => ({
                label: item.name,
                value: item.id,
              }))}
            />
          </div>
          <Space className="grafana-immersive-actions">
            <Button
              onClick={() => {
                setGrafanaEmbedError('')
                setGrafanaFrameNonce((current) => current + 1)
              }}
            >
              刷新画布
            </Button>
            {grafanaExternalHref ? (
              <Button type="primary" href={grafanaExternalHref} target="_blank" rel="noreferrer">
                打开原版 Grafana
              </Button>
            ) : null}
          </Space>
        </div>
      ) : null}

      <section className="grafana-immersive-stage">
        {selectedGrafanaSource ? (
          <>
            {grafanaEmbedError ? (
              <Alert
                type="warning"
                showIcon
                message="Grafana 页面暂时无法内嵌"
                description={grafanaEmbedError}
                className="grafana-immersive-alert"
              />
            ) : null}
            <iframe
              key={`${grafanaEmbedSrc}-${grafanaFrameNonce}`}
              ref={grafanaFrameRef}
              title={`grafana-${selectedGrafanaSource.name}`}
              src={grafanaEmbedSrc}
              className="grafana-frame"
              style={grafanaEmbedError ? { display: 'none' } : undefined}
              onLoad={() => {
                const frame = grafanaFrameRef.current
                if (!frame) {
                  return
                }

                try {
                  const text = frame.contentDocument?.body?.innerText?.trim() ?? ''
                  const locationPath = frame.contentWindow?.location?.pathname ?? ''
                  if (text.startsWith('{') && text.includes('"message"')) {
                    const parsed = JSON.parse(text) as { message?: string }
                    setGrafanaEmbedError(
                      normalizeGrafanaEmbedError(parsed.message || 'Grafana 代理返回异常响应'),
                    )
                    return
                  }
                  if (locationPath.includes('/login')) {
                    setGrafanaEmbedError('Grafana 返回了登录页，请检查数据源账号密码，或开启匿名访问后再接入。')
                    return
                  }
                  setGrafanaEmbedError('')
                } catch {
                  setGrafanaEmbedError('Grafana 页面加载异常，请检查数据源配置和访问路径。')
                }
              }}
            />
          </>
        ) : (
          <div className="soft-panel section-block grafana-empty-state">
            <Empty description="先在数据源中接入一个 Grafana" />
            {canWriteObservability ? (
              <Button type="primary" onClick={() => startTransition(() => setActiveView('observabilitySources'))}>
                去接入数据源
              </Button>
            ) : null}
          </div>
        )}
      </section>
    </div>
  )

  const renderObservabilitySourcesView = () => (
    <div className="view-grid">
      <section className="soft-panel section-block">
        <div className="section-headline">
          <div>
            <Typography.Title level={4}>数据源</Typography.Title>
            <Typography.Text type="secondary">
              接入 Prometheus、VictoriaMetrics、Grafana，并统一校验健康状态。
            </Typography.Text>
          </div>
          <Space>
            <Button icon={<ReloadOutlined />} onClick={() => void refreshObservabilitySources(api)}>
              刷新
            </Button>
            {canWriteObservability ? (
              <Button type="primary" icon={<PlusOutlined />} onClick={() => openObservabilityDrawer()}>
                新建数据源
              </Button>
            ) : null}
          </Space>
        </div>

        <div className="filter-bar">
          <Input.Search
            allowClear
            placeholder="按数据源名、类型、地址搜索"
            value={observabilitySourceSearch}
            onChange={(event) => setObservabilitySourceSearch(event.target.value)}
          />
        </div>
      </section>

      <section className="soft-panel section-block">
        <Table
          rowKey="id"
          columns={observabilityColumns}
          dataSource={filteredObservabilitySources}
          pagination={{ pageSize: 8 }}
          locale={{
            emptyText: (
              <Empty
                description="还没有数据源接入"
                image={Empty.PRESENTED_IMAGE_SIMPLE}
              />
            ),
          }}
        />
      </section>
    </div>
  )

  const renderUsersView = () => (
    <div className="view-grid">
      <section className="soft-panel section-block">
        <div className="section-headline">
          <div>
            <Typography.Title level={4}>用户目录</Typography.Title>
            <Typography.Text type="secondary">
              搜索、筛选并查看权限覆盖。
            </Typography.Text>
          </div>
          <Space>
            <Button icon={<ReloadOutlined />} onClick={() => void refreshUsers(api)}>
              刷新
            </Button>
            {canWriteUsers ? (
              <Button type="primary" icon={<PlusOutlined />} onClick={() => openUserModal()}>
                新建用户
              </Button>
            ) : null}
          </Space>
        </div>

        <div className="overview-grid compact-overview-grid">
          <article className="overview-unit muted-unit">
            <span className="metric-label">Active Users</span>
            <strong className="metric-value">{activeUserCount}</strong>
            <span className="metric-foot">当前可登录的平台用户</span>
          </article>
          <article className="overview-unit muted-unit">
            <span className="metric-label">Disabled</span>
            <strong className="metric-value">{disabledUserCount}</strong>
            <span className="metric-foot">暂时停用的账号</span>
          </article>
        </div>

        <div className="filter-bar">
          <Input.Search
            allowClear
            placeholder="按用户名、显示名称、角色搜索"
            value={userSearch}
            onChange={(event) => setUserSearch(event.target.value)}
          />
          <Segmented
            value={userStatusFilter}
            onChange={(value) =>
              setUserStatusFilter(value as 'all' | 'active' | 'disabled')
            }
            options={[
              { label: '全部', value: 'all' },
              { label: '启用', value: 'active' },
              { label: '停用', value: 'disabled' },
            ]}
          />
        </div>
      </section>

      <section className="workspace-split">
        <div className="soft-panel section-block">
          <Table
            rowKey="id"
            columns={userColumns}
            dataSource={filteredUsers}
            pagination={{ pageSize: 8 }}
            rowClassName={(record) => (record.id === selectedUserId ? 'is-selected-row' : '')}
            onRow={(record) => ({
              onClick: () => setSelectedUserId(record.id),
            })}
            locale={{ emptyText: <Empty description="没有符合条件的用户" /> }}
          />
        </div>

        <aside className="soft-panel inspector-panel">
          <div className="section-headline compact-headline">
            <div>
              <Typography.Title level={5}>用户透视</Typography.Title>
              <Typography.Text type="secondary">
                角色、权限和最近登录。
              </Typography.Text>
            </div>
          </div>

          {selectedUser ? (
            <div className="inspector-stack">
              <div className="identity-block">
                <strong>{selectedUser.displayName}</strong>
                <span>{selectedUser.username}</span>
              </div>

              <Descriptions column={1} size="small">
                <Descriptions.Item label="状态">
                  <Tag color={selectedUser.active ? 'green' : 'default'}>
                    {selectedUser.active ? 'ACTIVE' : 'DISABLED'}
                  </Tag>
                </Descriptions.Item>
                <Descriptions.Item label="最近登录">
                  {formatDate(selectedUser.lastLoginAt)}
                </Descriptions.Item>
                <Descriptions.Item label="角色数">
                  {selectedUser.roles.length}
                </Descriptions.Item>
                <Descriptions.Item label="权限覆盖">
                  {selectedUser.permissions.length}
                </Descriptions.Item>
              </Descriptions>

              <div className="tag-cloud">
                {selectedUser.roles.map((role) => (
                  <Tag key={role.id}>{role.name}</Tag>
                ))}
              </div>

              <div className="permission-pill-list">
                {selectedUser.permissions.length > 0 ? (
                  selectedUser.permissions.map((permission) => (
                    <span key={permission} className="permission-pill">
                      {permission}
                    </span>
                  ))
                ) : (
                  <Typography.Text type="secondary">当前用户尚未分配权限</Typography.Text>
                )}
              </div>

              <Alert
                type="info"
                showIcon
                message="平台权限与集群凭据分层生效"
                description="能不能发起操作看平台角色，能不能执行成功看集群凭据。"
              />

              <div className="mapping-card-stack">
                {buildPermissionMappings(selectedUser.permissions).map((mapping) => (
                  <section key={mapping.permission} className="mapping-card">
                    <strong>{mapping.label}</strong>
                    <span>{mapping.effect}</span>
                    <small>{mapping.kubernetesScope}</small>
                    <em>{mapping.note}</em>
                  </section>
                ))}
              </div>
            </div>
          ) : (
            <Empty description="选择一个用户查看详情" />
          )}
        </aside>
      </section>
    </div>
  )

  const renderRolesView = () => (
    <div className="view-grid">
      <section className="soft-panel section-block">
        <div className="section-headline">
          <div>
            <Typography.Title level={4}>角色与权限</Typography.Title>
            <Typography.Text type="secondary">
              筛选角色，并查看权限分组与影响范围。
            </Typography.Text>
          </div>
          <Space>
            <Button icon={<ReloadOutlined />} onClick={() => void refreshRoles(api)}>
              刷新
            </Button>
            {canWriteRoles ? (
              <Button type="primary" icon={<PlusOutlined />} onClick={() => openRoleModal()}>
                新建角色
              </Button>
            ) : null}
          </Space>
        </div>

        <div className="overview-grid compact-overview-grid">
          <article className="overview-unit muted-unit">
            <span className="metric-label">Built-in</span>
            <strong className="metric-value">{builtInRoleCount}</strong>
            <span className="metric-foot">系统预置角色</span>
          </article>
          <article className="overview-unit muted-unit">
            <span className="metric-label">Custom</span>
            <strong className="metric-value">{customRoleCount}</strong>
            <span className="metric-foot">业务自定义角色</span>
          </article>
        </div>

        <div className="filter-bar">
          <Input.Search
            allowClear
            placeholder="按角色名、说明、权限键搜索"
            value={roleSearch}
            onChange={(event) => setRoleSearch(event.target.value)}
          />
          <Segmented
            value={roleScopeFilter}
            onChange={(value) =>
              setRoleScopeFilter(value as 'all' | 'builtin' | 'custom')
            }
            options={[
              { label: '全部', value: 'all' },
              { label: '内建', value: 'builtin' },
              { label: '自定义', value: 'custom' },
            ]}
          />
        </div>
      </section>

      <section className="workspace-split">
        <div className="soft-panel section-block">
          <Table
            rowKey="id"
            columns={roleColumns}
            dataSource={filteredRoles}
            pagination={{ pageSize: 8 }}
            rowClassName={(record) => (record.id === selectedRoleId ? 'is-selected-row' : '')}
            onRow={(record) => ({
              onClick: () => setSelectedRoleId(record.id),
            })}
            locale={{ emptyText: <Empty description="没有符合条件的角色" /> }}
          />
        </div>

        <aside className="soft-panel inspector-panel">
          <div className="section-headline compact-headline">
            <div>
              <Typography.Title level={5}>角色透视</Typography.Title>
              <Typography.Text type="secondary">
                权限分组和关联用户一眼看清。
              </Typography.Text>
            </div>
          </div>

          {selectedRole ? (
            <div className="inspector-stack">
              <div className="identity-block">
                <strong>{selectedRole.name}</strong>
                <span>{selectedRole.description || '未填写角色说明'}</span>
              </div>

              <Descriptions column={1} size="small">
                <Descriptions.Item label="类型">
                  <Tag color={selectedRole.builtIn ? 'geekblue' : 'default'}>
                    {selectedRole.builtIn ? 'BUILT-IN' : 'CUSTOM'}
                  </Tag>
                </Descriptions.Item>
                <Descriptions.Item label="权限数">
                  {selectedRole.permissions.length}
                </Descriptions.Item>
                <Descriptions.Item label="关联用户">
                  {selectedRoleUsers.length}
                </Descriptions.Item>
              </Descriptions>

              <div className="permission-group-stack">
                {groupPermissions(selectedRole.permissions).map((group) => (
                  <section key={group.key} className="permission-group-panel">
                    <strong>{group.label}</strong>
                    <div className="permission-pill-list">
                      {group.permissions.map((permission) => (
                        <span key={permission.key} className="permission-pill">
                          {permission.key}
                        </span>
                      ))}
                    </div>
                  </section>
                ))}
              </div>

              <div className="tag-cloud">
                {selectedRoleUsers.length > 0 ? (
                  selectedRoleUsers.map((user) => <Tag key={user.id}>{user.displayName}</Tag>)
                ) : (
                  <Typography.Text type="secondary">当前还没有用户绑定这个角色</Typography.Text>
                )}
              </div>

              <Alert
                type="info"
                showIcon
                message="平台角色不替代 Kubernetes RBAC"
                description="角色只决定平台入口，真正执行仍取决于集群凭据。"
              />

              <div className="mapping-card-stack">
                {buildPermissionMappings(selectedRole.permissions.map((permission) => permission.key)).map(
                  (mapping) => (
                    <section key={mapping.permission} className="mapping-card">
                      <strong>{mapping.label}</strong>
                      <span>{mapping.effect}</span>
                      <small>{mapping.kubernetesScope}</small>
                      <em>{mapping.note}</em>
                    </section>
                  ),
                )}
              </div>
            </div>
          ) : (
            <Empty description="选择一个角色查看详情" />
          )}
        </aside>
      </section>
    </div>
  )

  const renderActiveView = () => {
    switch (activeView) {
      case 'dashboard':
        return renderDashboardView()
      case 'clusters':
        return renderClustersView()
      case 'inspection':
        return renderInspectionView()
      case 'resources':
        return renderResourcesView()
      case 'registryIntegrations':
        return renderRegistryIntegrationsView()
      case 'registryArtifacts':
        return renderRegistryArtifactsView()
      case 'observabilityDashboards':
        return renderObservabilityDashboardView()
      case 'observabilitySources':
        return renderObservabilitySourcesView()
      case 'users':
        return renderUsersView()
      case 'roles':
        return renderRolesView()
      default:
        return <Empty description="未找到视图" />
    }
  }

  return (
    <Layout className="shell">
      <Layout.Sider width={264} theme="dark" className="shell-sider">
        <div className="brand-block">
          <div className="brand-mark">
            <img src={platformMarkSrc} alt="平台图标" className="brand-mark-image" />
          </div>
          <div>
            <div className="brand-title">KubeFeel</div>
          </div>
        </div>

        <div className="sider-section">
          <div className="sider-label">Workspace</div>
          <Menu
            theme="dark"
            mode="inline"
            defaultOpenKeys={[
              ...(containerChildren.length > 0 ? [containerMenuKey] : []),
              ...(registryChildren.length > 0 ? [registryMenuKey] : []),
              ...(observabilityChildren.length > 0 ? [observabilityMenuKey] : []),
            ]}
            selectedKeys={[activeView]}
            items={menuItems}
            onClick={(event) => {
              startTransition(() => {
                setActiveView(event.key as ViewKey)
              })
            }}
          />
        </div>

        <div className="sider-footer">
          <div className="user-chip">
            <div className="user-chip-meta">
              <span className="user-chip-name">{session.user.displayName}</span>
              <span className="user-chip-sub">{session.user.username}</span>
            </div>
            <Badge status="processing" text="RBAC Online" />
          </div>
        </div>
      </Layout.Sider>

      <Layout>
        {hideShellHeader ? null : (
          <Layout.Header className="shell-header">
            <div>
              <Typography.Title level={3} className="page-title">
                {viewTitle(activeView)}
              </Typography.Title>
              <Typography.Text type="secondary">
                统一接入集群、仓库与可观测入口。
              </Typography.Text>
            </div>
            {hideShellActions ? null : (
              <Space>
                <Button
                  icon={<ReloadOutlined />}
                  onClick={() => {
                    if (!api || !session) {
                      return
                    }
                    void hydrateWorkspace(api, session.token)
                  }}
                >
                  刷新
                </Button>
                <Button icon={<LogoutOutlined />} onClick={() => logout()}>
                  退出
                </Button>
              </Space>
            )}
          </Layout.Header>
        )}

        <Layout.Content className={`shell-content${isImmersiveDashboardView ? ' is-immersive-content' : ''}`}>
          <Spin spinning={bootstrapping}>
            <div className={`workspace-frame${isImmersiveDashboardView ? ' is-immersive-frame' : ''}`}>
              {renderActiveView()}
            </div>
          </Spin>
        </Layout.Content>
      </Layout>

      <Drawer
        title={editingCluster ? `编辑集群 · ${editingCluster.name}` : '集群导入向导'}
        open={clusterDrawerOpen}
        width={760}
        onClose={closeClusterDrawer}
        extra={
          editingCluster ? (
            <Space>
              <Button onClick={closeClusterDrawer}>取消</Button>
              <Button type="primary" onClick={() => void submitCluster()}>
                保存
              </Button>
            </Space>
          ) : (
            <Space>
              <Button onClick={closeClusterDrawer}>取消</Button>
              {clusterWizardStep > 0 ? (
                <Button onClick={() => setClusterWizardStep((step) => Math.max(step - 1, 0))}>
                  上一步
                </Button>
              ) : null}
              <Button
                type="primary"
                loading={clusterPreviewLoading}
                onClick={() => void continueClusterWizard()}
              >
                {clusterWizardStep === 2 ? '接入集群' : clusterWizardStep === 1 ? '执行预检查' : '继续'}
              </Button>
            </Space>
          )
        }
      >
        <Form
          layout="vertical"
          form={clusterForm}
          initialValues={{ region: '', mode: 'ready', description: '', kubeconfig: '' }}
        >
          {editingCluster ? (
            <div className="section-block">
              <Alert
                type="info"
                showIcon
                message="更新已有接入"
                description="可修改名称、区域、运行状态；需要时再替换 kubeconfig。"
              />
              <Form.Item
                name="name"
                label="集群名称"
                rules={[{ required: true, message: '请输入集群名称' }]}
              >
                <Input placeholder="例如：prod-shanghai-01" />
              </Form.Item>
              <Form.Item
                name="region"
                label="区域"
                rules={[{ required: true, message: '请输入所属区域' }]}
              >
                <Input placeholder="例如：华东 1 / 上海 / 新加坡" />
              </Form.Item>
              <Form.Item name="mode" label="运行状态" rules={[{ required: true }]}>
                <Segmented
                  block
                  options={[
                    { label: '就绪', value: 'ready' },
                    { label: '维护', value: 'maintenance' },
                  ]}
                />
              </Form.Item>
              <Form.Item name="description" label="集群说明">
                <Input placeholder="业务环境、归属团队、用途等" />
              </Form.Item>
              <Form.Item name="kubeconfig" label="新的 kubeconfig（留空则保持不变）">
                <Input.TextArea
                  autoSize={{ minRows: 18, maxRows: 18 }}
                  className="code-editor"
                  placeholder="仅在需要替换接入凭据时粘贴新的 kubeconfig"
                />
              </Form.Item>
            </div>
          ) : (
            <Spin spinning={clusterPreviewLoading}>
              <div className="section-block">
                <Steps
                  current={clusterWizardStep}
                  items={[
                    { title: '载入 kubeconfig' },
                    { title: '校对元信息' },
                    { title: '预检查并接入' },
                  ]}
                />

                {clusterWizardStep === 0 ? (
                  <div className="wizard-layout">
                    <div className="wizard-main">
                      <div className="wizard-source-switch">
                        <span className="wizard-label">输入方式</span>
                        <Segmented
                          value={clusterDraftSource}
                          onChange={(value) =>
                            setClusterDraftSource(value as ClusterDraftSource)
                          }
                          options={[
                            { label: '粘贴内容', value: 'paste' },
                            { label: '上传文件', value: 'upload' },
                          ]}
                        />
                      </div>

                      <Alert
                        type="info"
                        showIcon
                        message="优先使用现有 kubeconfig"
                        description={`常见路径：${dashboard?.defaultKubeconfigPath || '~/.kube/config'}。保存时会在服务端加密。`}
                      />

                      {clusterDraftSource === 'upload' ? (
                        <div className="upload-shell">
                          <Upload
                            showUploadList={false}
                            beforeUpload={(file) => {
                              void handleClusterFileUpload(file as File)
                              return false
                            }}
                          >
                            <Button icon={<CloudUploadOutlined />}>选择 kubeconfig 文件</Button>
                          </Upload>
                          <Typography.Text type="secondary">
                            上传后自动解析 context、cluster 和 server。
                          </Typography.Text>
                        </div>
                      ) : null}

                      <Form.Item
                        name="kubeconfig"
                        label="kubeconfig"
                        rules={[{ required: true, message: '请粘贴 kubeconfig 内容' }]}
                      >
                        <Input.TextArea
                          autoSize={{ minRows: 18, maxRows: 18 }}
                          className="code-editor"
                          placeholder="粘贴当前可用的 kubeconfig"
                        />
                      </Form.Item>
                    </div>

                    <aside className="wizard-side">
                      <Typography.Title level={5}>结构预览</Typography.Title>
                      {clusterKubePreview ? (
                        clusterKubePreview.valid ? (
                          <div className="preview-stack">
                            <div className="preview-metric-grid">
                              <div className="preview-metric">
                                <span>Contexts</span>
                                <strong>{clusterKubePreview.contextCount}</strong>
                              </div>
                              <div className="preview-metric">
                                <span>Clusters</span>
                                <strong>{clusterKubePreview.clusterCount}</strong>
                              </div>
                              <div className="preview-metric">
                                <span>Users</span>
                                <strong>{clusterKubePreview.userCount}</strong>
                              </div>
                            </div>
                            <Descriptions column={1} size="small">
                              <Descriptions.Item label="当前上下文">
                                {clusterKubePreview.currentContext}
                              </Descriptions.Item>
                              <Descriptions.Item label="主入口地址">
                                {clusterKubePreview.primaryServer}
                              </Descriptions.Item>
                              <Descriptions.Item label="建议名称">
                                {clusterKubePreview.suggestedName}
                              </Descriptions.Item>
                            </Descriptions>
                          </div>
                        ) : (
                          <Alert
                            type="warning"
                            showIcon
                            message="当前 kubeconfig 无法解析"
                            description={clusterKubePreview.error}
                          />
                        )
                      ) : (
                        <Empty description="粘贴或上传 kubeconfig 后显示预览" />
                      )}
                    </aside>
                  </div>
                ) : null}

                {clusterWizardStep === 1 ? (
                  <div className="wizard-layout">
                    <div className="wizard-main">
                      <Form.Item
                        name="name"
                        label="集群名称"
                        rules={[{ required: true, message: '请输入集群名称' }]}
                      >
                        <Input placeholder="例如：prod-shanghai-01" />
                      </Form.Item>
                      <Form.Item
                        name="region"
                        label="区域"
                        rules={[{ required: true, message: '请输入所属区域' }]}
                      >
                        <Input placeholder="例如：华东 1 / 上海 / 新加坡" />
                      </Form.Item>
                      <Form.Item name="mode" label="运行状态" rules={[{ required: true }]}>
                        <Segmented
                          block
                          options={[
                            { label: '就绪', value: 'ready' },
                            { label: '维护', value: 'maintenance' },
                          ]}
                        />
                      </Form.Item>
                      <Form.Item name="description" label="集群说明">
                        <Input placeholder="业务环境、归属团队、用途等" />
                      </Form.Item>
                      <Alert
                        type="info"
                        showIcon
                        message="下一步将执行 API Server 预检查"
                        description="只探测连通性，不先写库。"
                      />
                    </div>

                    <aside className="wizard-side">
                      <Typography.Title level={5}>导入摘要</Typography.Title>
                      {clusterKubePreview?.valid ? (
                        <div className="preview-stack">
                          <Descriptions column={1} size="small">
                            <Descriptions.Item label="当前上下文">
                              {clusterKubePreview.currentContext}
                            </Descriptions.Item>
                            <Descriptions.Item label="入口地址">
                              {clusterKubePreview.primaryServer}
                            </Descriptions.Item>
                            <Descriptions.Item label="clusters">
                              {clusterKubePreview.clusterNames.join(', ')}
                            </Descriptions.Item>
                          </Descriptions>
                          <div className="tag-cloud">
                            {clusterKubePreview.contexts.map((context) => (
                              <Tag key={context.name}>{context.name}</Tag>
                            ))}
                          </div>
                        </div>
                      ) : (
                        <Empty description="缺少 kubeconfig 预览" />
                      )}
                    </aside>
                  </div>
                ) : null}

                {clusterWizardStep === 2 ? (
                  <div className="wizard-layout">
                    <div className="wizard-main">
                      {clusterConnectionPreview ? (
                        <>
                          <Alert
                            type="success"
                            showIcon
                            message="预检查成功"
                            description="目标集群可访问，现在可以保存。"
                          />
                          <div className="verification-rail">
                            <div className="verification-badge">
                              <CheckCircleOutlined />
                              <div>
                                <strong>{clusterConnectionPreview.version}</strong>
                                <span>Kubernetes Version</span>
                              </div>
                            </div>
                            <div className="verification-badge">
                              <CheckCircleOutlined />
                              <div>
                                <strong>{clusterConnectionPreview.criVersion || '-'}</strong>
                                <span>CRI Version</span>
                              </div>
                            </div>
                            <Progress percent={100} status="active" showInfo={false} />
                          </div>
                          <Descriptions column={1} size="small">
                            <Descriptions.Item label="Server">
                              {clusterConnectionPreview.server}
                            </Descriptions.Item>
                            <Descriptions.Item label="Current Context">
                              {clusterConnectionPreview.currentContext}
                            </Descriptions.Item>
                            <Descriptions.Item label="建议名称">
                              {clusterForm.getFieldValue('name') || clusterKubePreview?.suggestedName}
                            </Descriptions.Item>
                            <Descriptions.Item label="区域">
                              {clusterForm.getFieldValue('region') || '-'}
                            </Descriptions.Item>
                            <Descriptions.Item label="运行状态">
                              {clusterModeLabel(clusterForm.getFieldValue('mode'))}
                            </Descriptions.Item>
                          </Descriptions>
                        </>
                      ) : (
                        <Alert
                          type="warning"
                          showIcon
                          message="尚未拿到预检查结果"
                          description="请返回上一步重新预检查。"
                        />
                      )}
                    </div>

                    <aside className="wizard-side">
                      <Typography.Title level={5}>保存前提醒</Typography.Title>
                      <div className="hint-block">
                        <div className="hint-row">
                          <span className="hint-label">凭据处理</span>
                          <span>kubeconfig 会在服务端加密保存。</span>
                        </div>
                        <div className="hint-row">
                          <span className="hint-label">后续操作</span>
                          <span>保存后即可在资源中心直接操作该集群。</span>
                        </div>
                      </div>
                    </aside>
                  </div>
                ) : null}
              </div>
            </Spin>
          )}
        </Form>
      </Drawer>

      <Drawer
        title="创建集群 · Kubespray"
        open={clusterProvisionDrawerOpen}
        width={880}
        onClose={closeClusterProvisionDrawer}
        extra={
          clusterProvisionJob ? (
            <Space>
              <Button onClick={closeClusterProvisionDrawer}>关闭</Button>
              {clusterProvisionJob.status === 'failed' ? (
                <Button onClick={resumeClusterProvisionEditing}>返回修改</Button>
              ) : null}
              {isFinalProvisionStatus(clusterProvisionJob.status) ? (
                <Button onClick={resetClusterProvisionDrawer}>新建任务</Button>
              ) : null}
            </Space>
          ) : (
            <Space>
              <Button onClick={closeClusterProvisionDrawer}>取消</Button>
              <Button loading={clusterProvisionCheckLoading} onClick={() => void runClusterProvisionPrecheck()}>
                执行预检查
              </Button>
              <Button
                type="primary"
                loading={clusterProvisionSubmitting}
                disabled={!clusterProvisionCheckReady}
                onClick={() => void submitClusterProvisionJob()}
              >
                提交创建
              </Button>
            </Space>
          )
        }
      >
        <div className="section-block provision-step-shell">
          <Steps
            size="small"
            current={clusterProvisionStep}
            items={[
              { title: '填写参数' },
              { title: '执行预检查' },
              { title: '提交创建' },
            ]}
          />
        </div>

        {clusterProvisionJob ? (
          <div className="section-block provision-job-shell">
            <Alert
              type={provisionJobAlertType(clusterProvisionJob.status)}
              showIcon
              message={provisionJobHeadline(clusterProvisionJob.status)}
              description={
                clusterProvisionJob.lastError ||
                (clusterProvisionJob.status === 'succeeded'
                  ? 'Kubespray 已完成集群创建并自动接入平台。'
                  : '任务仍在执行，请稍候查看最新日志。')
              }
            />

            <div className="provision-job-grid">
              <div className="preview-metric">
                <span>状态</span>
                <strong>{provisionJobStatusLabel(clusterProvisionJob.status)}</strong>
              </div>
              <div className="preview-metric">
                <span>阶段</span>
                <strong>{clusterProvisionJob.step || '-'}</strong>
              </div>
              <div className="preview-metric">
                <span>结果集群</span>
                <strong>{clusterProvisionJob.resultClusterId ? `#${clusterProvisionJob.resultClusterId}` : '-'}</strong>
              </div>
            </div>

            <Descriptions column={2} size="small">
              <Descriptions.Item label="集群名称">{clusterProvisionJob.name}</Descriptions.Item>
              <Descriptions.Item label="区域">{clusterProvisionJob.region}</Descriptions.Item>
              <Descriptions.Item label="模板">
                {resolveProvisionTemplateLabel(provisionTemplates, clusterProvisionJob.provisionTemplate)}
              </Descriptions.Item>
              <Descriptions.Item label="Kubespray">{clusterProvisionJob.kubesprayVersion || '-'}</Descriptions.Item>
              <Descriptions.Item label="镜像源方案">
                {resolveImageRegistryPresetLabel(
                  imageRegistryPresets,
                  clusterProvisionJob.imageRegistryPreset,
                )}
              </Descriptions.Item>
              <Descriptions.Item label="镜像仓库">
                {clusterProvisionJob.imageRegistry || '默认'}
              </Descriptions.Item>
              <Descriptions.Item label="API 入口">{clusterProvisionJob.apiServerEndpoint}</Descriptions.Item>
              <Descriptions.Item label="SSH 用户">{clusterProvisionJob.sshUser}</Descriptions.Item>
              <Descriptions.Item label="Kubernetes">{clusterProvisionJob.kubernetesVersion || '默认'}</Descriptions.Item>
              <Descriptions.Item label="网络插件">{clusterProvisionJob.networkPlugin || 'calico'}</Descriptions.Item>
              <Descriptions.Item label="控制平面">{clusterProvisionJob.controlPlaneCount}</Descriptions.Item>
              <Descriptions.Item label="Worker">{clusterProvisionJob.workerCount}</Descriptions.Item>
              <Descriptions.Item label="开始时间">{formatDate(clusterProvisionJob.startedAt)}</Descriptions.Item>
              <Descriptions.Item label="完成时间">{formatDate(clusterProvisionJob.completedAt)}</Descriptions.Item>
            </Descriptions>

            <div className="review-code-panel provision-log-panel">
              <div className="review-code-head">
                <strong>任务日志</strong>
                <span>仅展示最近输出</span>
              </div>
              <pre className="review-code-block provision-log-block">
                {clusterProvisionJob.log || '暂无日志输出'}
              </pre>
            </div>
          </div>
        ) : (
          <Form
            layout="vertical"
            form={clusterProvisionForm}
            onValuesChange={handleClusterProvisionFormChange}
            initialValues={defaultClusterProvisionFormValues()}
          >
            <div className="section-block provision-form-shell">
              <Alert
                type="info"
                showIcon
                message="使用社区 Kubespray 创建集群"
                description="当前流程会先做预检查。通过后才能提交创建，失败后可直接回到原表单继续修改。"
              />

              <div className="provision-form-grid">
                <Form.Item
                  name="name"
                  label="集群名称"
                  rules={[{ required: true, message: '请输入集群名称' }]}
                >
                  <Input placeholder="例如：prod-hangzhou-01" />
                </Form.Item>
                <Form.Item
                  name="region"
                  label="区域"
                  rules={[{ required: true, message: '请输入所属区域' }]}
                >
                  <Input placeholder="例如：华东 1" />
                </Form.Item>
                <Form.Item
                  name="provisionTemplate"
                  label="Kubespray 模板"
                  rules={[{ required: true, message: '请选择 Kubespray 模板' }]}
                >
                  <Select
                    options={provisionTemplates.map((template) => ({
                      label: `${template.label} · ${template.kubesprayVersion}`,
                      value: template.key,
                    }))}
                    placeholder="选择兼容模板"
                  />
                </Form.Item>
                <Form.Item name="mode" label="运行状态" rules={[{ required: true }]}>
                  <Segmented
                    block
                    options={[
                      { label: '就绪', value: 'ready' },
                      { label: '维护', value: 'maintenance' },
                    ]}
                  />
                </Form.Item>
                <Form.Item name="description" label="集群说明">
                  <Input placeholder="用途、归属团队、环境说明" />
                </Form.Item>
                <Form.Item
                  name="apiServerEndpoint"
                  label="API 入口"
                  rules={[{ required: true, message: '请输入 API 入口地址' }]}
                >
                  <Input placeholder="例如：https://10.10.0.10:6443" />
                </Form.Item>
                <Form.Item
                  name="kubernetesVersion"
                  label="Kubernetes 版本"
                  rules={[{ required: true, message: '请输入 Kubernetes 版本' }]}
                  extra={
                    selectedProvisionTemplate
                      ? `当前模板：${selectedProvisionTemplate.label}，支持 ${selectedProvisionTemplate.minKubernetesVersion} - ${selectedProvisionTemplate.maxKubernetesVersion}`
                      : '请输入目标 Kubernetes 版本，例如 1.31.8'
                  }
                >
                  <Input placeholder={selectedProvisionTemplate?.versionHint || '例如：1.31.8'} />
                </Form.Item>
                <Form.Item
                  name="imageRegistryPreset"
                  label="镜像源方案"
                  rules={[{ required: true, message: '请选择镜像源方案' }]}
                >
                  <Select
                    options={imageRegistryPresets.map((preset) => ({
                      label: `${preset.label}${preset.recommended ? ' · 推荐' : ''}`,
                      value: preset.key,
                    }))}
                    placeholder="选择镜像源方案"
                  />
                </Form.Item>
                <Form.Item
                  name="imageRegistry"
                  label="镜像仓库"
                  rules={[
                    {
                      validator: async (_, value) => {
                        if (selectedImageRegistryPreset?.requiresRegistry && !String(value || '').trim()) {
                          throw new Error('请填写镜像仓库地址')
                        }
                      },
                    },
                  ]}
                  extra={
                    selectedImageRegistryPreset?.requiresRegistry
                      ? selectedImageRegistryPreset.description
                      : selectedImageRegistryPreset?.description ||
                        '当前方案会自动使用预置镜像前缀'
                  }
                >
                  <Input
                    disabled={!selectedImageRegistryPreset?.requiresRegistry}
                    placeholder={
                      selectedImageRegistryPreset?.placeholder ||
                      '例如：docker.m.daocloud.io 或 harbor.example.com/k8s'
                    }
                  />
                </Form.Item>
                <Form.Item
                  name="networkPlugin"
                  label="网络插件"
                  rules={[{ required: true, message: '请选择网络插件' }]}
                >
                  <Select
                    options={[
                      { label: 'Calico', value: 'calico' },
                      { label: 'Cilium', value: 'cilium' },
                      { label: 'Flannel', value: 'flannel' },
                    ]}
                  />
                </Form.Item>
                <Form.Item
                  name="sshUser"
                  label="SSH 用户"
                  rules={[{ required: true, message: '请输入 SSH 用户名' }]}
                >
                  <Input placeholder="例如：ubuntu / rocky / ec2-user / root" />
                </Form.Item>
                <Form.Item
                  name="sshPort"
                  label="SSH 端口"
                  rules={[{ required: true, message: '请输入 SSH 端口' }]}
                >
                  <Input type="number" min={1} max={65535} placeholder="22" />
                </Form.Item>
              </div>

              {selectedProvisionTemplate ? (
                <div className="provision-template-brief">
                  <span>{selectedProvisionTemplate.description}</span>
                  <strong>{selectedProvisionTemplate.kubesprayImage}</strong>
                </div>
              ) : null}

              <Form.Item
                name="sshPrivateKey"
                label="SSH 私钥"
                rules={[{ required: true, message: '请粘贴 SSH 私钥' }]}
              >
                <Input.TextArea
                  autoSize={{ minRows: 10, maxRows: 14 }}
                  className="code-editor"
                  placeholder="粘贴具备 sudo 权限的 SSH 私钥，例如 ~/.ssh/id_ed25519 或 ~/.ssh/id_rsa"
                />
              </Form.Item>

              <div className="inspector-subtitle-row">
                <strong>节点清单</strong>
                <span>至少 1 个控制平面节点</span>
              </div>

              <Form.List name="nodes">
                {(fields, { add, remove }) => (
                  <div className="provision-node-list">
                    {fields.map((field, index) => (
                      <div key={field.key} className="provision-node-row">
                        <Form.Item
                          {...field}
                          name={[field.name, 'name']}
                          label={index === 0 ? '节点名称' : ''}
                          rules={[{ required: true, message: '请输入节点名称' }]}
                        >
                          <Input placeholder={`例如：node-${index + 1}`} />
                        </Form.Item>
                        <Form.Item
                          {...field}
                          name={[field.name, 'address']}
                          label={index === 0 ? 'SSH 地址' : ''}
                          rules={[{ required: true, message: '请输入 SSH 地址' }]}
                        >
                          <Input placeholder="公网 IP / 管理 IP" />
                        </Form.Item>
                        <Form.Item
                          {...field}
                          name={[field.name, 'internalAddress']}
                          label={index === 0 ? '内网 IP' : ''}
                        >
                          <Input placeholder="留空则与 SSH 地址一致" />
                        </Form.Item>
                        <Form.Item
                          {...field}
                          name={[field.name, 'role']}
                          label={index === 0 ? '角色' : ''}
                          rules={[{ required: true, message: '请选择角色' }]}
                        >
                          <Select
                            options={[
                              { label: '控制平面', value: 'control-plane' },
                              { label: 'Worker', value: 'worker' },
                              { label: '控制平面 + Worker', value: 'control-plane-worker' },
                            ]}
                          />
                        </Form.Item>
                        <Button
                          danger
                          type="text"
                          className="provision-node-remove"
                          icon={<DeleteOutlined />}
                          onClick={() => remove(field.name)}
                          disabled={fields.length <= 1}
                        />
                      </div>
                    ))}
                    <Button
                      icon={<PlusOutlined />}
                      onClick={() => add({ name: '', address: '', internalAddress: '', role: 'worker' })}
                    >
                      添加节点
                    </Button>
                  </div>
                )}
              </Form.List>

              <div className="provision-check-shell">
                {clusterProvisionCheckResult ? (
                  <>
                    <Alert
                      type={clusterProvisionCheckResult.ready ? 'success' : 'warning'}
                      showIcon
                      message={clusterProvisionCheckResult.ready ? '预检查已通过' : '预检查未通过'}
                      description={
                        clusterProvisionCheckResult.summary ||
                        (clusterProvisionCheckResult.ready
                          ? '当前参数可以提交创建任务。'
                          : '请先处理检查项里的异常。')
                      }
                    />

                    {clusterProvisionCheckResult.checks.length > 0 ? (
                      <div className="provision-check-grid">
                        {clusterProvisionCheckResult.checks.map((check) => (
                          <div key={check.key} className="provision-check-item">
                            <div className="provision-check-item-head">
                              <strong>{check.label}</strong>
                              <Tag color={provisionCheckTagColor(check.status)}>
                                {provisionCheckStatusLabel(check.status)}
                              </Tag>
                            </div>
                            <p>{check.detail || '-'}</p>
                          </div>
                        ))}
                      </div>
                    ) : null}

                    {clusterProvisionCheckResult.nodes.length > 0 ? (
                      <div className="provision-check-node-list">
                        {clusterProvisionCheckResult.nodes.map((node) => (
                          <div key={`${node.name}-${node.address}`} className="provision-check-node">
                            <div className="provision-check-node-head">
                              <div className="provision-check-node-meta">
                                <strong>{node.name}</strong>
                                <span>{node.address}</span>
                                <em>{provisionNodeRoleLabel(node.role)}</em>
                              </div>
                              <Tag color={provisionCheckTagColor(node.status)}>
                                {provisionCheckStatusLabel(node.status)}
                              </Tag>
                            </div>
                            <div className="provision-check-grid">
                              {node.checks.map((check) => (
                                <div
                                  key={`${node.name}-${check.key}`}
                                  className="provision-check-item provision-check-item-compact"
                                >
                                  <div className="provision-check-item-head">
                                    <strong>{check.label}</strong>
                                    <Tag color={provisionCheckTagColor(check.status)}>
                                      {provisionCheckStatusLabel(check.status)}
                                    </Tag>
                                  </div>
                                  <p>{check.detail || '-'}</p>
                                </div>
                              ))}
                            </div>
                          </div>
                        ))}
                      </div>
                    ) : null}
                  </>
                ) : (
                  <Alert
                    type="info"
                    showIcon
                    message="先执行预检查"
                    description="会验证本机执行环境、集群名称冲突、节点 SSH、免密 sudo、Python 和系统信息。"
                  />
                )}
              </div>
            </div>
          </Form>
        )}
      </Drawer>

      <Modal
        title={editingUser ? `编辑用户 · ${editingUser.username}` : '创建用户'}
        open={userModalOpen}
        onCancel={closeUserModal}
        onOk={() => void submitUser()}
        width={820}
      >
        <div className="modal-split">
          <Form layout="vertical" form={userForm} initialValues={{ active: true, roleIds: [] }}>
            <Form.Item
              name="username"
              label="用户名"
              rules={[{ required: true, message: '请输入用户名' }]}
            >
              <Input placeholder="例如：alice.ops" />
            </Form.Item>
            <Form.Item
              name="displayName"
              label="显示名称"
              rules={[{ required: true, message: '请输入显示名称' }]}
            >
              <Input placeholder="例如：Alice Zhang" />
            </Form.Item>
            <Form.Item
              name="password"
              label={editingUser ? '新密码（留空则不修改）' : '登录密码'}
              rules={editingUser ? [] : [{ required: true, message: '请输入登录密码' }]}
            >
              <Input.Password placeholder="输入平台登录密码" />
            </Form.Item>
            <Form.Item
              name="roleIds"
              label="角色"
              rules={[{ required: true, message: '至少选择一个角色' }]}
            >
              <Select
                mode="multiple"
                placeholder="选择用户角色"
                options={roles.map((role) => ({
                  label: `${role.name}${role.builtIn ? ' · Built-in' : ''}`,
                  value: role.id,
                }))}
              />
            </Form.Item>
            <Form.Item name="active" label="启用状态" valuePropName="checked">
              <Switch checkedChildren="启用" unCheckedChildren="停用" />
            </Form.Item>
          </Form>

          <aside className="modal-aside">
            <Typography.Title level={5}>角色覆盖</Typography.Title>
            <Typography.Text type="secondary">
              所选角色决定平台权限。
            </Typography.Text>
            <Divider />
            <div className="tag-cloud">
              {selectedUserRoles.length > 0 ? (
                selectedUserRoles.map((role) => <Tag key={role.id}>{role.name}</Tag>)
              ) : (
                <Typography.Text type="secondary">还没有选择角色</Typography.Text>
              )}
            </div>
            <Divider />
            <Typography.Text type="secondary">
              合计权限：
              {' '}
              {new Set(
                selectedUserRoles.flatMap((role) => role.permissions.map((permission) => permission.key)),
              ).size}
            </Typography.Text>
          </aside>
        </div>
      </Modal>

      <Modal
        title={editingRole ? `编辑角色 · ${editingRole.name}` : '创建角色'}
        open={roleModalOpen}
        onCancel={closeRoleModal}
        onOk={() => void submitRole()}
        width={920}
      >
        <div className="modal-split">
          <Form layout="vertical" form={roleForm} initialValues={{ permissionKeys: [] }}>
            <Form.Item
              name="name"
              label="角色名称"
              rules={[{ required: true, message: '请输入角色名称' }]}
            >
              <Input placeholder="例如：release-manager" />
            </Form.Item>
            <Form.Item name="description" label="角色说明">
              <Input placeholder="说明该角色负责的操作边界" />
            </Form.Item>
            <Form.Item
              name="permissionKeys"
              label="权限集合"
              rules={[{ required: true, message: '至少选择一个权限' }]}
            >
              <Checkbox.Group className="permission-checklist">
                <div className="permission-group-stack">
                  {permissionGroups.map((group) => (
                    <section key={group.key} className="permission-group-panel">
                      <strong>{group.label}</strong>
                      <div className="permission-option-list">
                        {group.permissions.map((permission) => (
                          <label key={permission.key} className="permission-option">
                            <Checkbox value={permission.key}>
                              <div className="permission-option-copy">
                                <span>{permission.name}</span>
                                <small>{permission.description}</small>
                              </div>
                            </Checkbox>
                          </label>
                        ))}
                      </div>
                    </section>
                  ))}
                </div>
              </Checkbox.Group>
            </Form.Item>
          </Form>

          <aside className="modal-aside">
            <Typography.Title level={5}>权限预览</Typography.Title>
            <Typography.Text type="secondary">
              勾选权限，组合角色。
            </Typography.Text>
            <Divider />
            <div className="permission-pill-list">
              {selectedFormPermissions.length > 0 ? (
                selectedFormPermissions.map((permission) => (
                  <span key={permission.key} className="permission-pill">
                    {permission.key}
                  </span>
                ))
              ) : (
                <Typography.Text type="secondary">尚未选择权限</Typography.Text>
              )}
            </div>
          </aside>
        </div>
      </Modal>

      <Modal
        title={editingRegistry ? `编辑仓库 · ${editingRegistry.name}` : '接入仓库'}
        open={registryDrawerOpen}
        onCancel={closeRegistryDrawer}
        onOk={() => void submitRegistryIntegration()}
        okButtonProps={{ loading: registrySubmitting }}
        width={860}
      >
        <div className="modal-split">
          <Form
            layout="vertical"
            form={registryForm}
            initialValues={{ type: repositoryProviders[0]?.key ?? 'registry', skipTLSVerify: false }}
          >
            <Form.Item
              name="name"
              label="接入名称"
              rules={[{ required: true, message: '请输入接入名称' }]}
            >
              <Input placeholder="例如：harbor-prod" />
            </Form.Item>
            <Form.Item
              name="type"
              label="仓库类型"
              rules={[{ required: true, message: '请选择仓库类型' }]}
            >
              <Select
                options={repositoryProviders.map((item) => ({
                  label: item.label,
                  value: item.key,
                }))}
              />
            </Form.Item>
            <Form.Item name="description" label="说明">
              <Input placeholder="可写环境、业务线或用途" />
            </Form.Item>
            <Form.Item
              name="endpoint"
              label="访问地址"
              rules={[{ required: true, message: '请输入访问地址' }]}
            >
              <Input placeholder={selectedRepositoryProvider?.endpointHint || 'https://registry.example.com'} />
            </Form.Item>
            <Form.Item name="namespace" label={selectedRepositoryProvider?.namespaceLabel || '命名空间前缀'}>
              <Input placeholder={selectedRepositoryProvider?.namespacePlaceholder || '可留空'} />
            </Form.Item>
            <Form.Item name="username" label="用户名">
              <Input placeholder="匿名访问可留空" />
            </Form.Item>
            <Form.Item
              name="secret"
              label={editingRegistry ? '密钥 / 密码（留空则沿用）' : '密钥 / 密码'}
            >
              <Input.Password placeholder="支持密码或访问令牌" />
            </Form.Item>
            <Form.Item name="skipTLSVerify" label="TLS 校验" valuePropName="checked">
              <Switch checkedChildren="跳过校验" unCheckedChildren="严格校验" />
            </Form.Item>
          </Form>

          <aside className="modal-aside">
            <Typography.Title level={5}>接入说明</Typography.Title>
            <Typography.Text type="secondary">
              {selectedRepositoryProvider?.description || '选择一种兼容的 Docker / OCI 仓库类型。'}
            </Typography.Text>
            <Divider />
            <div className="inspector-stack">
              <div className="identity-block compact-identity-block">
                <strong>建议地址</strong>
                <span>{selectedRepositoryProvider?.endpointHint || '-'}</span>
              </div>
              <div className="identity-block compact-identity-block">
                <strong>浏览结果</strong>
                <span>镜像 / 版本 / 构建时间</span>
              </div>
            </div>
            <Divider />
            <Button block icon={<CheckCircleOutlined />} onClick={() => void testRegistryConnection()}>
              测试连接
            </Button>
          </aside>
        </div>
      </Modal>

      <Modal
        title={editingObservabilitySource ? `编辑数据源 · ${editingObservabilitySource.name}` : '接入数据源'}
        open={observabilityDrawerOpen}
        onCancel={closeObservabilityDrawer}
        onOk={() => void submitObservabilitySource()}
        okButtonProps={{ loading: observabilitySubmitting }}
        width={860}
      >
        <div className="modal-split">
          <Form
            layout="vertical"
            form={observabilityForm}
            initialValues={{ type: observabilityKinds[0]?.key ?? 'prometheus', dashboardPath: '/dashboards', skipTLSVerify: false }}
          >
            <Form.Item
              name="name"
              label="数据源名称"
              rules={[{ required: true, message: '请输入数据源名称' }]}
            >
              <Input placeholder="例如：grafana-prod" />
            </Form.Item>
            <Form.Item
              name="type"
              label="数据源类型"
              rules={[{ required: true, message: '请选择数据源类型' }]}
            >
              <Select
                options={observabilityKinds.map((item) => ({
                  label: item.label,
                  value: item.key,
                }))}
              />
            </Form.Item>
            <Form.Item name="description" label="说明">
              <Input placeholder="可写用途、环境或所属平台" />
            </Form.Item>
            <Form.Item
              name="endpoint"
              label="访问地址"
              rules={[{ required: true, message: '请输入访问地址' }]}
            >
              <Input placeholder={selectedObservabilityKind?.endpointHint || 'https://example.com'} />
            </Form.Item>
            <Form.Item name="username" label="用户名">
              <Input placeholder="若用 Token 可留空" />
            </Form.Item>
            <Form.Item
              name="secret"
              label={editingObservabilitySource ? '密码 / Token（留空则沿用）' : '密码 / Token'}
            >
              <Input.Password placeholder="Grafana / Prometheus 访问凭据" />
            </Form.Item>
            {watchedObservabilityType === 'grafana' ? (
              <Form.Item name="dashboardPath" label="默认仪表盘路径">
                <Input placeholder={selectedObservabilityKind?.defaultDashboardPath || '/dashboards'} />
              </Form.Item>
            ) : null}
            <Form.Item name="skipTLSVerify" label="TLS 校验" valuePropName="checked">
              <Switch checkedChildren="跳过校验" unCheckedChildren="严格校验" />
            </Form.Item>
          </Form>

          <aside className="modal-aside">
            <Typography.Title level={5}>接入说明</Typography.Title>
            <Typography.Text type="secondary">
              {selectedObservabilityKind?.description || '接入查询或展示入口后，平台会先执行健康检查。'}
            </Typography.Text>
            <Divider />
            <div className="inspector-stack">
              <div className="identity-block compact-identity-block">
                <strong>建议地址</strong>
                <span>{selectedObservabilityKind?.endpointHint || '-'}</span>
              </div>
              <div className="identity-block compact-identity-block">
                <strong>Grafana 内嵌</strong>
                <span>{selectedObservabilityKind?.dashboardCapable ? '支持同源嵌入' : '当前类型不支持'}</span>
              </div>
            </div>
            <Divider />
            <Button block icon={<CheckCircleOutlined />} onClick={() => void testObservabilityConnection()}>
              测试连接
            </Button>
          </aside>
        </div>
      </Modal>

      <Drawer
        title={resourceDetailResource ? `资源详情 · ${resourceName(resourceDetailResource)}` : '资源详情'}
        width={640}
        open={resourceDetailOpen}
        onClose={closeResourceDetailDrawer}
      >
        {resourceDetailLoading ? (
          <Spin />
        ) : resourceDetailResource ? (
          <div className="resource-detail-shell inspector-stack">
            <div className="resource-detail-hero">
              <div className="identity-block">
                <strong>{resourceName(resourceDetailResource)}</strong>
                <span>{resourceDetailResource.kind}</span>
              </div>
              <div className="resource-detail-actions">
                <Button icon={<EyeOutlined />} onClick={() => openResourceEditorFromDetail('yaml')}>
                  YAML
                </Button>
                <Button onClick={() => openResourceEditorFromDetail('diff')}>Diff</Button>
                <Button onClick={() => openResourceEditorFromDetail('audit')}>审计</Button>
                {canWriteResources ? (
                  <Button icon={<CopyOutlined />} onClick={openResourceCloneFromDetail}>
                    克隆
                  </Button>
                ) : null}
                {canWriteResources ? (
                  <Button danger icon={<DeleteOutlined />} onClick={() => void removeResourceFromDetail()}>
                    删除
                  </Button>
                ) : null}
              </div>
            </div>

            {resourceDetailDeploymentInsight ? (
              <>
                <div className={`deployment-health-card deployment-health-card-${resourceDetailDeploymentInsight.rolloutTone}`}>
                  <div className="deployment-health-copy">
                    <span className="context-chip-label">Rollout</span>
                    <strong>{resourceDetailDeploymentInsight.rolloutLabel}</strong>
                    <span>{resourceDetailDeploymentInsight.rolloutSummary}</span>
                  </div>
                  <div className="deployment-health-progress">
                    <Progress
                      percent={resourceDetailDeploymentInsight.rolloutPercent}
                      status={deploymentProgressStatus(resourceDetailDeploymentInsight.rolloutTone)}
                      showInfo={false}
                    />
                    <div className="deployment-health-meta">
                      <Tag color={deploymentToneTagColor(resourceDetailDeploymentInsight.rolloutTone)}>
                        {`${resourceDetailDeploymentInsight.ready}/${resourceDetailDeploymentInsight.desired} Ready`}
                      </Tag>
                      <span>{resourceDetailDeploymentInsight.strategyType}</span>
                    </div>
                  </div>
                </div>

                <div className="audit-mini-grid">
                  <div className="audit-mini-card">
                    <span>Desired</span>
                    <strong>{resourceDetailDeploymentInsight.desired}</strong>
                  </div>
                  <div className="audit-mini-card">
                    <span>Updated</span>
                    <strong>{resourceDetailDeploymentInsight.updated}</strong>
                  </div>
                  <div className="audit-mini-card">
                    <span>Available</span>
                    <strong>{resourceDetailDeploymentInsight.available}</strong>
                  </div>
                </div>

                <div className="audit-mini-grid">
                  <div className="audit-mini-card">
                    <span>Ready</span>
                    <strong>{resourceDetailDeploymentInsight.ready}</strong>
                  </div>
                  <div className="audit-mini-card">
                    <span>Unavailable</span>
                    <strong>{resourceDetailDeploymentInsight.unavailable}</strong>
                  </div>
                  <div className="audit-mini-card">
                    <span>Revision</span>
                    <strong>{resourceDetailDeploymentInsight.revision || '-'}</strong>
                  </div>
                </div>
              </>
            ) : null}

            <Descriptions column={1} size="small">
              <Descriptions.Item label="命名空间">
                {resourceNamespace(resourceDetailResource) || 'Cluster Scope'}
              </Descriptions.Item>
              <Descriptions.Item label="创建时间">
                {formatDate(resourceDetailResource.metadata?.creationTimestamp)}
              </Descriptions.Item>
              <Descriptions.Item label="资源版本">
                {resourceDetailResource.metadata?.resourceVersion || '-'}
              </Descriptions.Item>
              <Descriptions.Item label="Generation">
                {resourceDetailResource.metadata?.generation ?? '-'}
              </Descriptions.Item>
            </Descriptions>

            <div className="audit-mini-grid">
              <div className="audit-mini-card">
                <span>Labels</span>
                <strong>{Object.keys(resourceDetailResource.metadata?.labels ?? {}).length}</strong>
              </div>
              <div className="audit-mini-card">
                <span>Annotations</span>
                <strong>{Object.keys(resourceDetailResource.metadata?.annotations ?? {}).length}</strong>
              </div>
              <div className="audit-mini-card">
                <span>Managed Fields</span>
                <strong>{resourceDetailResource.metadata?.managedFields?.length ?? 0}</strong>
              </div>
            </div>

            {resourceDetailDeploymentInsight ? (
              <>
                <section className="inspector-subsection">
                  <div className="inspector-subtitle-row">
                    <strong>容器镜像</strong>
                    <span>{resourceDetailDeploymentInsight.containers.length} containers</span>
                  </div>
                  <div className="deployment-chip-grid">
                    {resourceDetailDeploymentInsight.containers.map((container) => (
                      <article
                        key={`${container.name}-${container.image}`}
                        className="deployment-chip-card"
                      >
                        <strong>{container.name}</strong>
                        <span>{container.image || '-'}</span>
                        <em>{container.imagePullPolicy || 'imagePullPolicy 未显式设置'}</em>
                      </article>
                    ))}
                  </div>
                </section>

                <section className="inspector-subsection">
                  <div className="inspector-subtitle-row">
                    <strong>发布策略</strong>
                    <span>{resourceDetailDeploymentInsight.strategyType}</span>
                  </div>
                  <div className="tag-cloud">
                    <Tag>{`maxSurge: ${resourceDetailDeploymentInsight.maxSurge}`}</Tag>
                    <Tag>{`maxUnavailable: ${resourceDetailDeploymentInsight.maxUnavailable}`}</Tag>
                  </div>
                </section>

                <section className="inspector-subsection">
                  <div className="inspector-subtitle-row">
                    <strong>Selector</strong>
                    <span>{Object.keys(resourceDetailDeploymentInsight.selectorLabels).length} labels</span>
                  </div>
                  <div className="tag-cloud">
                    {Object.entries(resourceDetailDeploymentInsight.selectorLabels).length > 0 ? (
                      Object.entries(resourceDetailDeploymentInsight.selectorLabels).map(([key, value]) => (
                        <Tag key={`${key}-${value}`}>{`${key}: ${value}`}</Tag>
                      ))
                    ) : (
                      <Typography.Text type="secondary">当前 Deployment 未显式设置 selector labels</Typography.Text>
                    )}
                  </div>
                </section>

                <section className="inspector-subsection">
                  <div className="inspector-subtitle-row">
                    <strong>Pod Labels</strong>
                    <span>{Object.keys(resourceDetailDeploymentInsight.templateLabels).length} labels</span>
                  </div>
                  <div className="tag-cloud">
                    {Object.entries(resourceDetailDeploymentInsight.templateLabels).length > 0 ? (
                      Object.entries(resourceDetailDeploymentInsight.templateLabels).map(([key, value]) => (
                        <Tag key={`${key}-${value}`}>{`${key}: ${value}`}</Tag>
                      ))
                    ) : (
                      <Typography.Text type="secondary">当前 Pod Template 没有 labels</Typography.Text>
                    )}
                  </div>
                </section>

                <section className="inspector-subsection">
                  <div className="inspector-subtitle-row">
                    <strong>Conditions</strong>
                    <span>{resourceDetailDeploymentInsight.conditions.length}</span>
                  </div>
                  <div className="deployment-condition-list">
                    {resourceDetailDeploymentInsight.conditions.length > 0 ? (
                      resourceDetailDeploymentInsight.conditions.map((condition) => (
                        <article
                          key={`${condition.type}-${condition.reason}-${condition.lastUpdateTime || 'na'}`}
                          className="deployment-condition-card"
                        >
                          <div className="deployment-condition-head">
                            <strong>{condition.type}</strong>
                            <Tag color={deploymentConditionTagColor(condition.status)}>
                              {condition.status || 'Unknown'}
                            </Tag>
                          </div>
                          <span>{condition.reason || '未提供 reason'}</span>
                          <p>{condition.message || '当前 condition 未提供详细信息。'}</p>
                          <em>{formatDate(condition.lastUpdateTime)}</em>
                        </article>
                      ))
                    ) : (
                      <Typography.Text type="secondary">当前 Deployment 没有 condition 信息</Typography.Text>
                    )}
                  </div>
                </section>
              </>
            ) : null}

            <section className="inspector-subsection">
              <div className="inspector-subtitle-row">
                <strong>对象 Labels</strong>
                <span>{Object.keys(resourceDetailResource.metadata?.labels ?? {}).length}</span>
              </div>
              <div className="tag-cloud">
                {Object.entries(resourceDetailResource.metadata?.labels ?? {}).length > 0 ? (
                  Object.entries(resourceDetailResource.metadata?.labels ?? {}).map(([key, value]) => (
                    <Tag key={key}>{`${key}: ${value}`}</Tag>
                  ))
                ) : (
                  <Typography.Text type="secondary">当前资源没有 labels</Typography.Text>
                )}
              </div>
            </section>
          </div>
        ) : (
          <Empty description="没有可展示的资源详情" />
        )}
      </Drawer>

      <Drawer
        title={resourceDrawerTitle(resourceDrawerMode, editingResource)}
        width={960}
        open={resourceDrawerOpen}
        onClose={closeResourceDrawer}
        extra={
          <Space>
            <Button onClick={closeResourceDrawer}>关闭</Button>
            <Button icon={<CopyOutlined />} onClick={() => void copyResourceYaml()}>
              复制 YAML
            </Button>
            <Button onClick={formatResourceYaml}>格式化</Button>
            {canWriteResources &&
            resourceDrawerMode !== 'inspect' ? (
              <Button type="primary" onClick={openResourceReview}>
                {resourceDrawerMode === 'edit'
                  ? '提交更新'
                  : resourceDrawerMode === 'clone'
                    ? '审查克隆'
                    : '审查创建'}
              </Button>
            ) : null}
          </Space>
        }
      >
        <div className="resource-editor-meta">
          <Descriptions column={2} size="small">
            <Descriptions.Item label="集群">{selectedCluster?.name || '-'}</Descriptions.Item>
            <Descriptions.Item label="资源类型">
              {selectedResourceDefinition?.label || '-'}
            </Descriptions.Item>
            <Descriptions.Item label="命名空间">
              {selectedResourceDefinition?.namespaced
                ? resourceNamespace(editingResource) || selectedNamespace || 'manifest 中指定'
                : 'Cluster Scope'}
            </Descriptions.Item>
            <Descriptions.Item label="模式">
              {resourceDrawerMode.toUpperCase()}
            </Descriptions.Item>
          </Descriptions>
        </div>
        <div className="drawer-panel-toolbar">
          <Segmented<ResourceDrawerPanel>
            value={resourceDrawerPanel}
            onChange={(value) => setResourceDrawerPanel(value)}
            options={[
              { label: 'YAML', value: 'yaml' },
              { label: 'Diff', value: 'diff' },
              { label: '审计', value: 'audit' },
            ]}
          />
          {resourceDrawerMode === 'edit' || resourceDrawerMode === 'clone' ? (
            <Tag color={resourceDraft === resourceBaselineDraft ? 'default' : 'gold'}>
              {resourceDraft === resourceBaselineDraft ? '未修改' : '草稿已变更'}
            </Tag>
          ) : null}
        </div>

        {resourceDrawerPanel === 'yaml' ? (
          <>
            {parsedResourceDraft.error ? (
              <Alert
                type="warning"
                showIcon
                message="当前 YAML 还不能被解析"
                description={parsedResourceDraft.error}
                style={{ marginBottom: 16 }}
              />
            ) : null}
            <Input.TextArea
              value={resourceDraft}
              onChange={(event) => setResourceDraft(event.target.value)}
              autoSize={{ minRows: 28, maxRows: 28 }}
              className="code-editor"
              placeholder="在这里编辑资源 YAML"
            />
          </>
        ) : null}

        {resourceDrawerPanel === 'diff' ? (
          <div className="diff-shell">
            <div className="diff-summary-strip">
              <div className="diff-summary-unit">
                <span>Added</span>
                <strong>{resourceDiffSummary.added}</strong>
              </div>
              <div className="diff-summary-unit">
                <span>Removed</span>
                <strong>{resourceDiffSummary.removed}</strong>
              </div>
              <div className="diff-summary-unit">
                <span>Unchanged</span>
                <strong>{resourceDiffSummary.unchanged}</strong>
              </div>
            </div>
            <div className="diff-panel">
              {resourceDiffLines.length > 0 ? (
                resourceDiffLines.map((line, index) => (
                  <div
                    key={`${line.type}-${index}-${line.leftNumber ?? 0}-${line.rightNumber ?? 0}`}
                    className={`diff-line diff-line-${line.type}`}
                  >
                    <span className="diff-gutter">
                      {line.leftNumber ?? ''}
                    </span>
                    <span className="diff-gutter">
                      {line.rightNumber ?? ''}
                    </span>
                    <span className="diff-marker">
                      {line.type === 'added' ? '+' : line.type === 'removed' ? '-' : ' '}
                    </span>
                    <span className="diff-content">{line.content || ' '}</span>
                  </div>
                ))
              ) : (
                <Empty description="当前没有可展示的差异" />
              )}
            </div>
          </div>
        ) : null}

        {resourceDrawerPanel === 'audit' ? (
          <div className="audit-shell">
            <Alert
              type="info"
              showIcon
              message="审计视图用于快速判断影响面"
              description="这里看元信息、managedFields 和草稿解析结果。"
              style={{ marginBottom: 16 }}
            />
            <div className="audit-mini-grid">
              <div className="audit-mini-card">
                <span>Kind</span>
                <strong>{resourceAuditTarget?.kind || '-'}</strong>
              </div>
              <div className="audit-mini-card">
                <span>Generation</span>
                <strong>{String(resourceAuditTarget?.metadata?.generation ?? '-')}</strong>
              </div>
              <div className="audit-mini-card">
                <span>Managed Fields</span>
                <strong>{resourceAuditTarget?.metadata?.managedFields?.length ?? 0}</strong>
              </div>
            </div>
            <Descriptions column={2} size="small" className="audit-descriptions">
              <Descriptions.Item label="资源名称">
                {resourceName(resourceAuditTarget)}
              </Descriptions.Item>
              <Descriptions.Item label="命名空间">
                {resourceNamespace(resourceAuditTarget) || 'Cluster Scope'}
              </Descriptions.Item>
              <Descriptions.Item label="UID">
                {resourceAuditTarget?.metadata?.uid || '-'}
              </Descriptions.Item>
              <Descriptions.Item label="资源版本">
                {resourceAuditTarget?.metadata?.resourceVersion || '-'}
              </Descriptions.Item>
            </Descriptions>
            <div className="audit-entry-list">
              {resourceAuditEntries.length > 0 ? (
                resourceAuditEntries.map((entry, index) => (
                  <div key={`${entry.title}-${index}`} className={`audit-entry audit-entry-${entry.accent}`}>
                    <div className="audit-entry-head">
                      <strong>{entry.title}</strong>
                      <span>{entry.time ? formatDate(entry.time) : '-'}</span>
                    </div>
                    <p>{entry.detail}</p>
                  </div>
                ))
              ) : (
                <Empty description="当前资源缺少可用的审计元信息" />
              )}
            </div>
          </div>
        ) : null}
      </Drawer>

      <Modal
        title="提交前审查"
        open={resourceReviewOpen}
        onCancel={() => setResourceReviewOpen(false)}
        onOk={() => void submitResource()}
        okText={
          resourceDrawerMode === 'edit'
            ? '确认提交更新'
            : resourceDrawerMode === 'clone'
              ? '确认创建克隆'
              : '确认创建资源'
        }
        cancelText="返回继续编辑"
        confirmLoading={resourceSubmitting}
        width={1180}
      >
        <div className="review-shell">
          <Alert
            type="warning"
            showIcon
            message="提交前确认影响范围"
            description="风险提示是启发式检查，不替代正式评审。"
          />

          <div className="diff-summary-strip">
            <div className="diff-summary-unit">
              <span>Added</span>
              <strong>{resourceDiffSummary.added}</strong>
            </div>
            <div className="diff-summary-unit">
              <span>Removed</span>
              <strong>{resourceDiffSummary.removed}</strong>
            </div>
            <div className="diff-summary-unit">
              <span>Changed Object</span>
              <strong>{resourceDraftSummaryLabel(resourceDraftDocuments)}</strong>
            </div>
          </div>

          <div className="review-main-grid">
            <section className="review-code-panel">
              <div className="review-code-head">
                <strong>当前版本</strong>
                <span>
                  {resourceBaselineDraft.trim()
                    ? '来自目标集群的当前对象'
                    : '新建资源没有现存对象'}
                </span>
              </div>
              <pre className="review-code-block">{resourceBaselineDraft || '# no live object'}</pre>
            </section>

            <section className="review-code-panel">
              <div className="review-code-head">
                <strong>提交草稿</strong>
                <span>即将发往 API Server 的 YAML 内容</span>
              </div>
              <pre className="review-code-block">{resourceDraft}</pre>
            </section>
          </div>

          <section className="risk-panel">
            <div className="review-code-head">
              <strong>风险提示</strong>
              <span>
                {resourceRiskItems.length > 0
                  ? `检测到 ${resourceRiskItems.length} 条风险提示`
                  : '本次变更未命中额外高风险规则'}
              </span>
            </div>
            <div className="risk-list">
              {resourceRiskItems.length > 0 ? (
                resourceRiskItems.map((item, index) => (
                  <div key={`${item.title}-${index}`} className={`risk-item risk-item-${item.level}`}>
                    <div className="risk-item-head">
                      <Tag color={riskTagColor(item.level)}>{item.level.toUpperCase()}</Tag>
                      <strong>{item.title}</strong>
                    </div>
                    <p>{item.detail}</p>
                  </div>
                ))
              ) : (
                <Alert
                  type="success"
                  showIcon
                  message="未命中额外高风险规则"
                  description="仍建议结合窗口、回滚和权限边界人工确认。"
                />
              )}
            </div>
          </section>
        </div>
      </Modal>
    </Layout>
  )
}

function LoginScreen(props: {
  loading: boolean
  onSubmit: (values: { username: string; password: string }) => Promise<void>
}) {
  const [form] = Form.useForm<{ username: string; password: string }>()

  return (
    <div className="login-shell">
      <section className="login-hero">
        <div className="login-hero-copy">
          <h1>KubeFeel 集群管理</h1>
        </div>

        <ul className="hero-feature-list">
          <li>
            <strong>多集群管理</strong>
            <span>统一接入、区域视图和运行状态集中掌握。</span>
          </li>
          <li>
            <strong>一键巡检</strong>
            <span>聚焦节点、Pod、容量、存储和告警项。</span>
          </li>
          <li>
            <strong>仓库集成</strong>
            <span>为镜像、制品与配置来源预留统一接入入口。</span>
          </li>
        </ul>
      </section>

      <section className="login-panel">
        <div className="login-panel-header">
          <div className="login-panel-copy">
            <div className="login-panel-brand">
              <div className="login-mark">
                <img src={platformMarkSrc} alt="平台图标" className="brand-mark-image" />
              </div>
              <span className="login-panel-kicker">KubeFeel Console</span>
            </div>
            <Typography.Text type="secondary">
              使用平台账号进入集群管理控制台。
            </Typography.Text>
          </div>
        </div>

        <Form
          className="login-form"
          layout="vertical"
          form={form}
          initialValues={{ username: 'admin', password: '' }}
          onFinish={(values) => void props.onSubmit(values)}
        >
          <Form.Item
            name="username"
            label="用户名"
            rules={[{ required: true, message: '请输入用户名' }]}
          >
            <Input placeholder="请输入用户名" />
          </Form.Item>
          <Form.Item
            name="password"
            label="密码"
            rules={[{ required: true, message: '请输入密码' }]}
          >
            <Input.Password placeholder="请输入密码" />
          </Form.Item>
          <Button type="primary" htmlType="submit" size="large" block loading={props.loading}>
            登录控制台
          </Button>
          <Typography.Text type="secondary" className="login-form-note">
            默认管理员账号通常是 `admin`，首次密码请查看部署环境中的初始化配置。
          </Typography.Text>
        </Form>
      </section>
    </div>
  )
}

function viewTitle(view: ViewKey) {
  switch (view) {
    case 'dashboard':
      return '平台工作台'
    case 'clusters':
      return '集群列表'
    case 'inspection':
      return '一键巡检'
    case 'resources':
      return '资源中心'
    case 'registryIntegrations':
      return '仓库集成'
    case 'registryArtifacts':
      return '镜像列表'
    case 'observabilityDashboards':
      return '仪表盘'
    case 'observabilitySources':
      return '数据源'
    case 'users':
      return '用户管理'
    case 'roles':
      return '角色权限'
    default:
      return 'KubeFeel'
  }
}

function buildDashboardPath(clusterId?: number) {
  if (!clusterId) {
    return '/dashboard'
  }

  const params = new URLSearchParams({ clusterId: String(clusterId) })
  return `/dashboard?${params.toString()}`
}

function buildGrafanaEmbedSrc(sourceId: number, rawPath: string) {
  const cleanedPath = normalizeGrafanaPath(rawPath)
  return `/api/observability/grafana/${sourceId}${cleanedPath}`
}

function buildGrafanaExternalHref(endpoint: string, rawPath: string) {
  const base = endpoint.trim().replace(/\/+$/, '')
  const path = normalizeGrafanaPath(rawPath)
  return `${base}${path}`
}

function buildGrafanaWorkspacePath(rawPath: string, section: GrafanaWorkspaceSection) {
  if (section === 'explore') {
    return '/explore'
  }

  const normalized = normalizeGrafanaPath(rawPath)
  if (normalized === '/' || normalized === '/login') {
    return '/dashboards'
  }
  return normalized
}

function buildGrafanaImmersivePath(rawPath: string) {
  const normalized = normalizeGrafanaPath(rawPath)
  if (
    normalized.startsWith('/dashboards') ||
    normalized.startsWith('/explore') ||
    normalized.startsWith('/d/') ||
    normalized.startsWith('/dashboard/')
  ) {
    const separator = normalized.includes('?') ? '&' : '?'
    return `${normalized}${separator}kiosk=tv`
  }
  return normalized
}

function normalizeGrafanaEmbedError(message: string) {
  const detail = message.trim()
  if (!detail) {
    return 'Grafana 页面加载失败，请检查数据源配置后重试。'
  }

  if (
    detail.includes('password-auth.failed') ||
    detail.includes('Invalid username or password')
  ) {
    return '当前 Grafana 账号或密码不正确，请到“数据源”里更新凭据后再试。'
  }

  if (detail.includes('missing auth token') || detail.includes('invalid token')) {
    return '平台登录态已失效，请重新登录后再试。'
  }

  if (detail.includes('session validation failed')) {
    return 'Grafana 会话校验失败，请检查访问地址和登录方式。'
  }

  return detail
}

function normalizeGrafanaPath(rawPath: string) {
  const trimmed = rawPath.trim()
  if (!trimmed) {
    return '/dashboards'
  }
  if (trimmed.startsWith('http://') || trimmed.startsWith('https://')) {
    try {
      const parsed = new URL(trimmed)
      return parsed.pathname + (parsed.search || '')
    } catch {
      return '/dashboards'
    }
  }
  return trimmed.startsWith('/') ? trimmed : `/${trimmed}`
}

function pickCurrentOrFirstId(ids: number[], current?: number) {
  if (current && ids.includes(current)) {
    return current
  }

  return ids[0]
}

function loadSession(): Session | null {
  const raw =
    window.localStorage.getItem(sessionStorageKey) ??
    window.localStorage.getItem(legacySessionStorageKey)
  if (!raw) {
    return null
  }

  try {
    const session = JSON.parse(raw) as Session
    window.localStorage.setItem(sessionStorageKey, raw)
    window.localStorage.removeItem(legacySessionStorageKey)
    return session
  } catch {
    window.localStorage.removeItem(sessionStorageKey)
    window.localStorage.removeItem(legacySessionStorageKey)
    return null
  }
}

function normalizeRegistryPayload(values: RegistryFormValues) {
  return {
    name: values.name.trim(),
    type: values.type,
    description: values.description?.trim() || '',
    endpoint: values.endpoint.trim(),
    namespace: values.namespace?.trim() || '',
    username: values.username?.trim() || '',
    secret: values.secret?.trim() || '',
    skipTLSVerify: Boolean(values.skipTLSVerify),
  }
}

function normalizeObservabilityPayload(values: ObservabilityFormValues) {
  return {
    name: values.name.trim(),
    type: values.type,
    description: values.description?.trim() || '',
    endpoint: values.endpoint.trim(),
    username: values.username?.trim() || '',
    secret: values.secret?.trim() || '',
    dashboardPath: values.dashboardPath?.trim() || '',
    skipTLSVerify: Boolean(values.skipTLSVerify),
  }
}

function persistSession(session: Session | null) {
  if (!session) {
    window.localStorage.removeItem(sessionStorageKey)
    window.localStorage.removeItem(legacySessionStorageKey)
    return
  }

  window.localStorage.setItem(sessionStorageKey, JSON.stringify(session))
  window.localStorage.removeItem(legacySessionStorageKey)
}

function formatDate(value?: string | null) {
  if (!value) {
    return '-'
  }

  return dayjs(value).format('YYYY-MM-DD HH:mm:ss')
}

function formatPercent(value?: number | null) {
  if (value == null || Number.isNaN(value)) {
    return '-'
  }

  return `${value.toFixed(1)}%`
}

function resourceName(resource?: K8sObject | null) {
  return resource?.metadata?.name || '-'
}

function resourceNamespace(resource?: K8sObject | null) {
  return resource?.metadata?.namespace || ''
}

function buildNamespaceQuery(definition: ResourceDefinition, namespace?: string) {
  if (!definition.namespaced || !namespace) {
    return ''
  }

  const params = new URLSearchParams({ namespace })
  return `?${params.toString()}`
}

function resolveResourceNamespace(
  definition: ResourceDefinition,
  selectedNamespace: string,
  resource?: K8sObject | null,
) {
  if (!definition.namespaced) {
    return ''
  }

  return resourceNamespace(resource) || selectedNamespace
}

function buildResourceSelectionKey(resource: K8sObject) {
  return `${resource.kind || 'unknown'}:${resourceNamespace(resource) || 'cluster'}:${resourceName(resource)}`
}

function buildResourceTemplate(
  definition: ResourceDefinition,
  namespace: string,
  variant: 'standard' | 'minimal',
) {
  const metadata = {
    name: `sample-${definition.key}`,
    ...(definition.namespaced ? { namespace: namespace || 'default' } : {}),
  }

  if (variant === 'minimal') {
    return {
      apiVersion: definition.apiVersion,
      kind: definition.kind,
      metadata,
    }
  }

  switch (definition.key) {
    case 'deployment':
      return {
        apiVersion: 'apps/v1',
        kind: 'Deployment',
        metadata,
        spec: {
          replicas: 1,
          selector: { matchLabels: { app: 'sample-app' } },
          template: {
            metadata: { labels: { app: 'sample-app' } },
            spec: {
              containers: [
                {
                  name: 'app',
                  image: 'nginx:1.27',
                  ports: [{ containerPort: 80 }],
                },
              ],
            },
          },
        },
      }
    case 'service':
      return {
        apiVersion: 'v1',
        kind: 'Service',
        metadata,
        spec: {
          selector: { app: 'sample-app' },
          ports: [{ port: 80, targetPort: 80 }],
        },
      }
    case 'configmap':
      return {
        apiVersion: 'v1',
        kind: 'ConfigMap',
        metadata,
        data: {
          APP_ENV: 'dev',
          FEATURE_FLAG: 'on',
        },
      }
    case 'secret':
      return {
        apiVersion: 'v1',
        kind: 'Secret',
        metadata,
        stringData: {
          username: 'demo',
          password: 'change-me',
        },
      }
    case 'cronjob':
      return {
        apiVersion: 'batch/v1',
        kind: 'CronJob',
        metadata,
        spec: {
          schedule: '*/15 * * * *',
          jobTemplate: {
            spec: {
              template: {
                spec: {
                  restartPolicy: 'OnFailure',
                  containers: [
                    {
                      name: 'runner',
                      image: 'busybox',
                      command: ['sh', '-c', 'date; echo kubefeel'],
                    },
                  ],
                },
              },
            },
          },
        },
      }
    default:
      return {
        apiVersion: definition.apiVersion,
        kind: definition.kind,
        metadata,
      }
  }
}

function buildStandardResourceDocuments(
  definition: ResourceDefinition,
  namespace: string,
): K8sObject[] {
  const resolvedNamespace = namespace || 'default'

  switch (definition.key) {
    case 'deployment':
      return [
        {
          apiVersion: 'apps/v1',
          kind: 'Deployment',
          metadata: {
            name: 'sample-deployment',
            namespace: resolvedNamespace,
            labels: { app: 'sample-app' },
          },
          spec: {
            replicas: 1,
            selector: { matchLabels: { app: 'sample-app' } },
            template: {
              metadata: { labels: { app: 'sample-app' } },
              spec: {
                containers: [
                  {
                    name: 'app',
                    image: 'nginx:1.27',
                    ports: [{ containerPort: 80 }],
                    volumeMounts: [{ name: 'data', mountPath: '/data' }],
                  },
                ],
                volumes: [
                  {
                    name: 'data',
                    persistentVolumeClaim: { claimName: 'sample-deployment-data' },
                  },
                ],
              },
            },
          },
        },
        {
          apiVersion: 'v1',
          kind: 'Service',
          metadata: {
            name: 'sample-deployment',
            namespace: resolvedNamespace,
          },
          spec: {
            selector: { app: 'sample-app' },
            ports: [{ name: 'http', port: 80, targetPort: 80 }],
            type: 'ClusterIP',
          },
        },
        {
          apiVersion: 'v1',
          kind: 'PersistentVolumeClaim',
          metadata: {
            name: 'sample-deployment-data',
            namespace: resolvedNamespace,
          },
          spec: {
            accessModes: ['ReadWriteOnce'],
            resources: {
              requests: {
                storage: '10Gi',
              },
            },
          },
        },
      ]
    case 'statefulset':
      return [
        {
          apiVersion: 'apps/v1',
          kind: 'StatefulSet',
          metadata: {
            name: 'sample-statefulset',
            namespace: resolvedNamespace,
            labels: { app: 'sample-stateful-app' },
          },
          spec: {
            serviceName: 'sample-statefulset',
            replicas: 1,
            selector: { matchLabels: { app: 'sample-stateful-app' } },
            template: {
              metadata: { labels: { app: 'sample-stateful-app' } },
              spec: {
                containers: [
                  {
                    name: 'app',
                    image: 'nginx:1.27',
                    ports: [{ containerPort: 80, name: 'http' }],
                    volumeMounts: [{ name: 'data', mountPath: '/data' }],
                  },
                ],
              },
            },
            volumeClaimTemplates: [
              {
                metadata: { name: 'data' },
                spec: {
                  accessModes: ['ReadWriteOnce'],
                  resources: {
                    requests: {
                      storage: '10Gi',
                    },
                  },
                },
              },
            ],
          },
        },
        {
          apiVersion: 'v1',
          kind: 'Service',
          metadata: {
            name: 'sample-statefulset',
            namespace: resolvedNamespace,
          },
          spec: {
            clusterIP: 'None',
            selector: { app: 'sample-stateful-app' },
            ports: [{ name: 'http', port: 80, targetPort: 80 }],
          },
        },
      ]
    default:
      return [buildResourceTemplate(definition, namespace, 'standard')]
  }
}

function buildStandardResourceManifest(
  definition: ResourceDefinition,
  namespace: string,
) {
  return stringifyYamlDocuments(buildStandardResourceDocuments(definition, namespace))
}

function stringifyYamlDocuments(documents: K8sObject[]) {
  return documents.map((document) => YAML.stringify(document)).join('---\n')
}

function clusterStatusColor(status: string) {
  return status === 'connected' ? 'green' : 'red'
}

function clusterStatusLabel(status: string) {
  return status === 'connected' ? '正常' : '异常'
}

function inspectionStatusColor(status: string) {
  switch (status) {
    case 'warning':
      return 'gold'
    case 'failed':
      return 'red'
    default:
      return 'green'
  }
}

function inspectionStatusLabel(status: string) {
  switch (status) {
    case 'warning':
      return '告警'
    case 'failed':
      return '失败'
    default:
      return '通过'
  }
}

function clusterModeLabel(mode: string) {
  return mode === 'maintenance' ? '维护' : '就绪'
}

function isFinalProvisionStatus(status?: string | null) {
  return status === 'succeeded' || status === 'failed'
}

function provisionJobStatusLabel(status?: string | null) {
  switch (status) {
    case 'pending':
      return '等待执行'
    case 'running':
      return '执行中'
    case 'succeeded':
      return '已完成'
    case 'failed':
      return '失败'
    default:
      return '未知'
  }
}

function provisionJobHeadline(status?: string | null) {
  switch (status) {
    case 'pending':
      return '任务已创建'
    case 'running':
      return 'Kubespray 正在执行'
    case 'succeeded':
      return '集群创建完成'
    case 'failed':
      return '集群创建失败'
    default:
      return '任务状态未知'
  }
}

function provisionJobAlertType(status?: string | null): 'info' | 'success' | 'error' | 'warning' {
  switch (status) {
    case 'succeeded':
      return 'success'
    case 'failed':
      return 'error'
    case 'running':
      return 'info'
    default:
      return 'warning'
  }
}

function buildClusterProvisionPayload(values: ClusterProvisionFormValues) {
  return {
    name: values.name.trim(),
    region: values.region.trim(),
    mode: values.mode || 'ready',
    description: values.description?.trim() || '',
    provisionTemplate: values.provisionTemplate || defaultProvisionTemplateKey,
    apiServerEndpoint: values.apiServerEndpoint.trim(),
    kubernetesVersion: values.kubernetesVersion?.trim() || '',
    imageRegistryPreset: values.imageRegistryPreset || defaultImageRegistryPresetKey,
    imageRegistry: values.imageRegistry?.trim() || '',
    networkPlugin: values.networkPlugin || 'calico',
    sshUser: values.sshUser.trim(),
    sshPort: Number(values.sshPort || 22),
    sshPrivateKey: values.sshPrivateKey.trim(),
    nodes: (values.nodes || []).map((node) => ({
      name: node.name?.trim() || '',
      address: node.address?.trim() || '',
      internalAddress: node.internalAddress?.trim() || '',
      role: node.role || 'worker',
    })),
  }
}

function normalizeClusterProvisionDraft(
  values?: Partial<ClusterProvisionFormValues> | null,
): ClusterProvisionFormValues {
  const fallback = defaultClusterProvisionFormValues()
  const nodes =
    values?.nodes && values.nodes.length > 0
      ? values.nodes.map((node, index) => ({
          name: node?.name ?? fallback.nodes[index]?.name ?? '',
          address: node?.address ?? '',
          internalAddress: node?.internalAddress ?? '',
          role: node?.role ?? fallback.nodes[index]?.role ?? 'worker',
        }))
      : fallback.nodes

  return {
    name: values?.name ?? fallback.name,
    region: values?.region ?? fallback.region,
    mode: values?.mode ?? fallback.mode,
    description: values?.description ?? fallback.description,
    provisionTemplate: values?.provisionTemplate ?? fallback.provisionTemplate,
    apiServerEndpoint: values?.apiServerEndpoint ?? fallback.apiServerEndpoint,
    kubernetesVersion: values?.kubernetesVersion ?? fallback.kubernetesVersion,
    imageRegistryPreset: values?.imageRegistryPreset ?? fallback.imageRegistryPreset,
    imageRegistry: values?.imageRegistry ?? fallback.imageRegistry,
    networkPlugin: values?.networkPlugin ?? fallback.networkPlugin,
    sshUser: values?.sshUser ?? fallback.sshUser,
    sshPort: Number(values?.sshPort ?? fallback.sshPort),
    sshPrivateKey: values?.sshPrivateKey ?? fallback.sshPrivateKey,
    nodes,
  }
}

function provisionPayloadFingerprint(
  payload: ReturnType<typeof buildClusterProvisionPayload>,
) {
  return JSON.stringify(payload)
}

function provisionCheckStatusLabel(status?: string | null) {
  switch (status) {
    case 'success':
      return '通过'
    case 'warning':
      return '注意'
    case 'error':
      return '异常'
    default:
      return '未知'
  }
}

function provisionCheckTagColor(status?: string | null) {
  switch (status) {
    case 'success':
      return 'green'
    case 'warning':
      return 'gold'
    case 'error':
      return 'red'
    default:
      return 'default'
  }
}

function provisionNodeRoleLabel(role?: string | null) {
  switch (role) {
    case 'control-plane':
      return '控制平面'
    case 'control-plane-worker':
      return '控制平面 + Worker'
    default:
      return 'Worker'
  }
}

function resolveProvisionTemplateLabel(
  templates: ProvisionTemplate[],
  key?: string | null,
) {
  if (!key) {
    return '-'
  }

  return templates.find((template) => template.key === key)?.label || key
}

function resolveImageRegistryPresetLabel(
  presets: ImageRegistryPreset[],
  key?: string | null,
) {
  if (!key) {
    return '-'
  }

  return presets.find((preset) => preset.key === key)?.label || key
}

function defaultClusterProvisionFormValues(): ClusterProvisionFormValues {
  return {
    name: '',
    region: '',
    mode: 'ready',
    description: '',
    provisionTemplate: defaultProvisionTemplateKey,
    apiServerEndpoint: '',
    kubernetesVersion: '',
    imageRegistryPreset: defaultImageRegistryPresetKey,
    imageRegistry: '',
    networkPlugin: 'calico',
    sshUser: '',
    sshPort: 22,
    sshPrivateKey: '',
    nodes: [
      { name: 'cp-1', address: '', internalAddress: '', role: 'control-plane' },
      { name: 'worker-1', address: '', internalAddress: '', role: 'worker' },
    ],
  }
}

function resolveClusterRuntimeState(cluster: Cluster) {
  if (cluster.status !== 'connected') {
    return {
      color: 'red',
      label: '异常',
      detail: cluster.lastError || 'API 检测未通过',
    }
  }

  if (cluster.mode === 'maintenance') {
    return {
      color: 'orange',
      label: '维护',
      detail: 'worker 节点已 cordon',
    }
  }

  return {
    color: 'green',
    label: '就绪',
    detail: 'API 正常，worker 可调度',
  }
}

function pickPreferredClusterId(clusters: Cluster[], currentId?: number) {
  if (typeof currentId === 'number' && clusters.some((cluster) => cluster.id === currentId)) {
    return currentId
  }

  const preferred =
    clusters.find((cluster) => cluster.status === 'connected') ??
    clusters[0]

  return preferred?.id
}

function buildDeploymentInsight(resource: K8sObject | null): DeploymentInsight | null {
  if (!resource || valueAsString(resource.kind) !== 'Deployment') {
    return null
  }

  const desired = readNumberValue(resource, ['spec', 'replicas']) ?? 1
  const updated = readNumberValue(resource, ['status', 'updatedReplicas']) ?? 0
  const ready = readNumberValue(resource, ['status', 'readyReplicas']) ?? 0
  const available = readNumberValue(resource, ['status', 'availableReplicas']) ?? 0
  const unavailable = Math.max(desired - available, 0)
  const revision = valueAsString(
    readNestedValue(resource, ['metadata', 'annotations', 'deployment.kubernetes.io/revision']),
  )
  const strategyType =
    valueAsString(readNestedValue(resource, ['spec', 'strategy', 'type'])) || 'RollingUpdate'
  const maxSurge =
    valueAsString(readNestedValue(resource, ['spec', 'strategy', 'rollingUpdate', 'maxSurge'])) ||
    '-'
  const maxUnavailable =
    valueAsString(
      readNestedValue(resource, ['spec', 'strategy', 'rollingUpdate', 'maxUnavailable']),
    ) || '-'
  const selectorLabels = readStringMap(resource, ['spec', 'selector', 'matchLabels'])
  const templateLabels = readStringMap(resource, ['spec', 'template', 'metadata', 'labels'])
  const conditions = readDeploymentConditions(resource)
  const containers = findContainerSpecs(resource).map((container) => ({
    name: valueAsString(container.name) || 'container',
    image: valueAsString(container.image),
    imagePullPolicy: valueAsString(container.imagePullPolicy),
  }))

  let rolloutTone: DeploymentInsight['rolloutTone'] = 'processing'
  let rolloutLabel = 'Rollout in Progress'

  if (desired === 0) {
    rolloutTone = 'warning'
    rolloutLabel = 'Scaled to Zero'
  } else if (available >= desired && ready >= desired && updated >= desired) {
    rolloutTone = 'success'
    rolloutLabel = 'Rollout Healthy'
  } else if (conditions.some((condition) => condition.status === 'False')) {
    rolloutTone = 'warning'
    rolloutLabel = 'Needs Attention'
  }

  const rolloutPercent =
    desired > 0 ? Math.max(0, Math.min(100, Math.round((available / desired) * 100))) : 100

  return {
    desired,
    updated,
    ready,
    available,
    unavailable,
    revision,
    strategyType,
    maxSurge,
    maxUnavailable,
    rolloutPercent,
    rolloutLabel,
    rolloutSummary: `${ready}/${desired} ready · ${available}/${desired} available`,
    rolloutTone,
    selectorLabels,
    templateLabels,
    containers,
    conditions,
  }
}

function readDeploymentConditions(resource: K8sObject | null): DeploymentConditionInsight[] {
  const rawConditions = readNestedValue(resource, ['status', 'conditions'])
  if (!Array.isArray(rawConditions)) {
    return []
  }

  return rawConditions
    .map((entry) => asRecord(entry))
    .filter((entry): entry is Record<string, unknown> => Boolean(entry))
    .map((entry) => ({
      type: valueAsString(entry.type) || 'Condition',
      status: valueAsString(entry.status) || 'Unknown',
      reason: valueAsString(entry.reason),
      message: valueAsString(entry.message),
      lastUpdateTime:
        valueAsString(entry.lastUpdateTime) || valueAsString(entry.lastTransitionTime) || undefined,
    }))
}

function deploymentProgressStatus(tone: DeploymentInsight['rolloutTone']) {
  switch (tone) {
    case 'success':
      return 'success'
    case 'warning':
      return 'exception'
    default:
      return 'active'
  }
}

function deploymentToneTagColor(tone: DeploymentInsight['rolloutTone']) {
  switch (tone) {
    case 'success':
      return 'green'
    case 'warning':
      return 'gold'
    default:
      return 'blue'
  }
}

function deploymentConditionTagColor(status: string) {
  switch (status) {
    case 'True':
      return 'green'
    case 'False':
      return 'red'
    default:
      return 'gold'
  }
}

function resourceDrawerTitle(mode: ResourceDrawerMode, resource: K8sObject | null) {
  switch (mode) {
    case 'create':
      return '创建 Kubernetes 资源'
    case 'clone':
      return `克隆资源 · ${resourceName(resource)}`
    case 'inspect':
      return `查看 YAML · ${resourceName(resource)}`
    default:
      return `编辑资源 · ${resourceName(resource)}`
  }
}

function renderLabelPreview(resource: K8sObject) {
  const labels = Object.entries(resource.metadata?.labels ?? {})
  if (labels.length === 0) {
    return <span className="table-note">无 labels</span>
  }

  return (
    <>
      {labels.slice(0, 2).map(([key, value]) => (
        <Tag key={key}>{`${key}: ${value}`}</Tag>
      ))}
      {labels.length > 2 ? <span className="table-note">+{labels.length - 2}</span> : null}
    </>
  )
}

function renderDeploymentReadySummary(resource: K8sObject) {
  const insight = buildDeploymentInsight(resource)
  if (!insight) {
    return '-'
  }

  return (
    <div className="entity-stack">
      <span className="entity-primary">{`${insight.ready}/${insight.desired}`}</span>
      <span className="entity-secondary">{insight.rolloutLabel}</span>
    </div>
  )
}

function primaryContainerImage(resource: K8sObject) {
  return buildDeploymentInsight(resource)?.containers[0]?.image ?? collectContainerImages(resource)[0] ?? ''
}

function renderExtraContainerSummary(resource: K8sObject) {
  const insight = buildDeploymentInsight(resource)
  if (!insight) {
    const images = collectContainerImages(resource)
    return images.length > 1 ? `+${images.length - 1} images` : '单容器'
  }

  if (insight.containers.length <= 1) {
    return '单容器'
  }

  return `+${insight.containers.length - 1} containers`
}

function sanitizeResourceForCreate(resource: K8sObject) {
  const next = JSON.parse(JSON.stringify(resource)) as K8sObject

  if (!next.metadata) {
    next.metadata = {}
  }

  next.metadata.name = `${resourceName(resource)}-copy`
  delete next.metadata.uid
  delete next.metadata.resourceVersion
  delete next.metadata.creationTimestamp

  delete (next.metadata as Record<string, unknown>).managedFields
  delete (next.metadata as Record<string, unknown>).generation
  delete (next.metadata as Record<string, unknown>).ownerReferences
  delete (next as Record<string, unknown>).status

  return next
}

function safeParseResourceDraft(value: string): {
  resource: K8sObject | null
  resources: K8sObject[]
  error?: string
} {
  if (!value.trim()) {
    return { resource: null, resources: [] }
  }

  try {
    const resources = parseYamlResourceDocuments(value)
    return {
      resource: resources[0] ?? null,
      resources,
    }
  } catch (error) {
    return {
      resource: null,
      resources: [],
      error: error instanceof Error ? error.message : 'YAML 解析失败',
    }
  }
}

function parseYamlResourceDocuments(value: string) {
  const documents = YAML.parseAllDocuments(value)
  return documents
    .map((document) => document.toJS())
    .filter((item): item is K8sObject => Boolean(item) && typeof item === 'object')
}

function resourceDraftSummaryLabel(resources: K8sObject[]) {
  if (resources.length === 0) {
    return '-'
  }
  if (resources.length === 1) {
    return resourceName(resources[0])
  }
  return `${resources.length} objects`
}

function buildYamlDiffLines(before: string, after: string): YamlDiffLine[] {
  const left = normalizeYamlLines(before)
  const right = normalizeYamlLines(after)
  const leftLength = left.length
  const rightLength = right.length

  const dp = Array.from({ length: leftLength + 1 }, () =>
    Array<number>(rightLength + 1).fill(0),
  )

  for (let i = leftLength - 1; i >= 0; i -= 1) {
    for (let j = rightLength - 1; j >= 0; j -= 1) {
      dp[i][j] =
        left[i] === right[j]
          ? dp[i + 1][j + 1] + 1
          : Math.max(dp[i + 1][j], dp[i][j + 1])
    }
  }

  const result: YamlDiffLine[] = []
  let i = 0
  let j = 0
  let leftNumber = 1
  let rightNumber = 1

  while (i < leftLength && j < rightLength) {
    if (left[i] === right[j]) {
      result.push({
        type: 'unchanged',
        content: left[i],
        leftNumber,
        rightNumber,
      })
      i += 1
      j += 1
      leftNumber += 1
      rightNumber += 1
      continue
    }

    if (dp[i + 1][j] >= dp[i][j + 1]) {
      result.push({
        type: 'removed',
        content: left[i],
        leftNumber,
      })
      i += 1
      leftNumber += 1
      continue
    }

    result.push({
      type: 'added',
      content: right[j],
      rightNumber,
    })
    j += 1
    rightNumber += 1
  }

  while (i < leftLength) {
    result.push({
      type: 'removed',
      content: left[i],
      leftNumber,
    })
    i += 1
    leftNumber += 1
  }

  while (j < rightLength) {
    result.push({
      type: 'added',
      content: right[j],
      rightNumber,
    })
    j += 1
    rightNumber += 1
  }

  return result
}

function summarizeYamlDiff(lines: YamlDiffLine[]) {
  return lines.reduce(
    (summary, line) => {
      summary[line.type] += 1
      return summary
    },
    { added: 0, removed: 0, unchanged: 0 },
  )
}

function normalizeYamlLines(value: string) {
  return value.replace(/\r\n/g, '\n').split('\n')
}

function buildResourceAuditEntries(
  liveResource: K8sObject | null,
  draftResource: K8sObject | null,
): AuditEntry[] {
  const entries: AuditEntry[] = []

  if (liveResource?.metadata?.creationTimestamp) {
    entries.push({
      title: '资源创建',
      detail: `${resourceName(liveResource)} 已在目标集群中存在，可据此追踪对象生存期起点。`,
      time: liveResource.metadata.creationTimestamp,
      accent: 'success',
    })
  }

  for (const field of liveResource?.metadata?.managedFields ?? []) {
    entries.push({
      title: field.manager ? `Managed by ${field.manager}` : 'Managed Field',
      detail: [
        field.operation ? `operation=${field.operation}` : '',
        field.apiVersion ? `apiVersion=${field.apiVersion}` : '',
        field.subresource ? `subresource=${field.subresource}` : '',
      ]
        .filter(Boolean)
        .join(' · '),
      time: field.time,
      accent: 'info',
    })
  }

  if (draftResource) {
    entries.push({
      title: '当前草稿解析',
      detail: `${draftResource.apiVersion || '-'} / ${draftResource.kind || '-'} / ${resourceName(draftResource)}`,
      accent: 'neutral',
    })
  }

  if (liveResource?.metadata?.resourceVersion) {
    entries.push({
      title: '资源版本',
      detail: `resourceVersion=${liveResource.metadata.resourceVersion}，用于标记当前对象在 API Server 中的版本快照。`,
      accent: 'neutral',
    })
  }

  return entries
}

function buildResourceRiskItems(
  mode: ResourceDrawerMode,
  definition: ResourceDefinition | undefined,
  liveResource: K8sObject | null,
  draftResource: K8sObject | null,
  draftResources: K8sObject[],
): ResourceRiskItem[] {
  if (!definition || !draftResource) {
    return []
  }

  const items: ResourceRiskItem[] = []
  const nextName = resourceName(draftResource)
  const previousName = resourceName(liveResource)
  const nextNamespace = resourceNamespace(draftResource)
  const previousNamespace = resourceNamespace(liveResource)

  if (!definition.namespaced) {
    items.push({
      level: 'high',
      title: '集群级资源变更',
      detail: `${definition.kind} 属于 Cluster Scope，对象变更会直接影响整个集群，而不是单个命名空间。`,
    })
  }

  if (mode === 'create' || mode === 'clone') {
    items.push({
      level: 'medium',
      title: mode === 'clone' ? '克隆会创建新对象' : '新建会立即落到目标集群',
      detail:
        mode === 'clone'
          ? '提交后平台会按当前草稿创建一个新资源，名称、命名空间和选择器冲突都需要提前确认。'
          : '提交后平台会直接向目标集群发起 CREATE，请确认命名空间、标签选择器和依赖对象已经准备好。',
    })
  }

  if (draftResources.length > 1) {
    items.push({
      level: 'medium',
      title: '本次会批量创建多个对象',
      detail: `草稿里共包含 ${draftResources.length} 个 YAML 文档，提交后会按顺序创建。请确认名称、依赖关系和回滚方式都已准备好。`,
    })
  }

  if (mode === 'edit' && liveResource) {
    if (previousName !== nextName) {
      items.push({
        level: 'high',
        title: '资源名称发生变化',
        detail: `当前编辑基于 ${previousName}，草稿名称已变成 ${nextName}。Kubernetes 通常不支持直接修改 metadata.name，这类提交很容易失败或偏离预期。`,
      })
    }

    if (definition.namespaced && previousNamespace !== nextNamespace && nextNamespace) {
      items.push({
        level: 'high',
        title: '命名空间目标发生变化',
        detail: `当前对象位于 ${previousNamespace || '未指定'}，草稿写成了 ${nextNamespace}。跨命名空间变更通常应按新建/迁移流程处理，而不是直接更新。`,
      })
    }

    if (
      valueAsString(liveResource.apiVersion) !== valueAsString(draftResource.apiVersion) ||
      valueAsString(liveResource.kind) !== valueAsString(draftResource.kind)
    ) {
      items.push({
        level: 'high',
        title: 'apiVersion 或 Kind 被修改',
        detail: '这会改变对象的资源类型语义。提交前建议重新确认目标资源类型和后端路由是否仍然匹配。',
      })
    }
  }

  if (definition.key === 'secret') {
    const liveSecretKeys = uniqueNonEmpty([
      ...Object.keys(readStringMap(liveResource, ['data'])),
      ...Object.keys(readStringMap(liveResource, ['stringData'])),
    ])
    const draftSecretKeys = uniqueNonEmpty([
      ...Object.keys(readStringMap(draftResource, ['data'])),
      ...Object.keys(readStringMap(draftResource, ['stringData'])),
    ])

    if (draftSecretKeys.length > 0) {
      items.push({
        level: 'high',
        title: 'Secret 含敏感数据',
        detail: `草稿包含 ${draftSecretKeys.length} 个 Secret 键（${draftSecretKeys.slice(0, 4).join(', ')}${draftSecretKeys.length > 4 ? ' ...' : ''}）。请确认来源、脱敏策略和最小暴露范围。`,
      })
    }

    if (
      liveSecretKeys.join('|') !== draftSecretKeys.join('|') &&
      liveSecretKeys.length > 0 &&
      draftSecretKeys.length > 0
    ) {
      items.push({
        level: 'medium',
        title: 'Secret 键集合发生变化',
        detail: '当 Secret key 被新增、移除或改名时，依赖它的 Pod、Job 或挂载点可能会出现启动失败或配置缺失。',
      })
    }
  }

  const containerImages = collectContainerImages(draftResource)
  const previousImages = collectContainerImages(liveResource)
  if (containerImages.length > 0 && containerImages.join('|') !== previousImages.join('|')) {
    items.push({
      level: 'medium',
      title: '工作负载镜像已变更',
      detail: `本次草稿涉及镜像版本调整：${containerImages.slice(0, 3).join(', ')}${containerImages.length > 3 ? ' ...' : ''}。请确认镜像可拉取、版本已验证且回滚策略明确。`,
    })
  }

  if (hasPrivilegedWorkload(draftResource)) {
    items.push({
      level: 'high',
      title: '检测到高权限容器配置',
      detail: '草稿中包含 privileged、hostNetwork、hostPID 或 hostIPC 之类的高权限工作负载字段，建议结合安全基线再次审查。',
    })
  }

  if (hasHostPathVolume(draftResource)) {
    items.push({
      level: 'high',
      title: '检测到 hostPath 挂载',
      detail: 'hostPath 会把节点文件系统暴露给容器，风险通常高于普通卷。请确认这是必要能力且路径受控。',
    })
  }

  const replicas = readNumberValue(draftResource, ['spec', 'replicas'])
  if (typeof replicas === 'number' && replicas === 0) {
    items.push({
      level: 'medium',
      title: '副本数被设置为 0',
      detail: '这通常意味着主动停服或流量切空，请确认业务窗口、探针策略和上游依赖已经同步。',
    })
  }

  if (definition.key === 'service') {
    const nextType = valueAsString(readNestedValue(draftResource, ['spec', 'type']))
    const previousType = valueAsString(readNestedValue(liveResource, ['spec', 'type']))
    if (nextType && nextType !== previousType) {
      items.push({
        level: nextType === 'LoadBalancer' ? 'high' : 'medium',
        title: 'Service 暴露类型发生变化',
        detail: `当前草稿的 Service 类型为 ${nextType || 'ClusterIP'}。当 Service 从内部流量切到 NodePort / LoadBalancer 时，网络暴露面会明显扩大。`,
      })
    }
  }

  if (definition.key === 'ingress' || definition.key === 'ingressclass') {
    items.push({
      level: definition.key === 'ingressclass' ? 'high' : 'medium',
      title: '入口流量对象变更',
      detail: 'Ingress / IngressClass 直接影响入口路由、域名和控制器行为，建议和证书、LB、DNS 配置一起核对。',
    })
  }

  if (definition.key === 'storageclass' || definition.key === 'pv' || definition.key === 'pvc') {
    items.push({
      level: 'high',
      title: '存储相关对象变更',
      detail: '存储类、持久卷和声明会影响数据保留、调度与挂载行为，提交前请确认回收策略和数据安全边界。',
    })
  }

  if (definition.key === 'cronjob' && valueAsString(readNestedValue(draftResource, ['spec', 'schedule']))) {
    items.push({
      level: 'medium',
      title: '定时任务计划已生效',
      detail: `当前 CronJob 调度为 ${valueAsString(readNestedValue(draftResource, ['spec', 'schedule']))}。请确认不会在非预期时间批量触发。`,
    })
  }

  if (
    definition.namespaced &&
    !nextNamespace &&
    !previousNamespace
  ) {
    items.push({
      level: 'low',
      title: '草稿未显式写入 metadata.namespace',
      detail: '如果资源类型需要命名空间，最终会依赖当前筛选命名空间或后端查询参数补全。为了审计清晰，建议在 YAML 中显式写明。 ',
    })
  }

  return dedupeRiskItems(items)
}

function riskTagColor(level: ResourceRiskItem['level']) {
  switch (level) {
    case 'high':
      return 'red'
    case 'medium':
      return 'gold'
    default:
      return 'blue'
  }
}

function buildPermissionMappings(permissionKeys: string[]): PermissionMapping[] {
  const mappingCatalog: Record<string, PermissionMapping> = {
    'dashboard:read': {
      permission: 'dashboard:read',
      label: '查看平台聚合态',
      effect: '可查看工作台和聚合指标。',
      kubernetesScope: '不直接调用 Kubernetes API。',
      note: '适合只读观察。',
    },
    'clusters:read': {
      permission: 'clusters:read',
      label: '查看集群列表',
      effect: '可查看集群列表、入口和状态。',
      kubernetesScope: '主要读取平台保存的接入信息。',
      note: '用于确认接入状态。',
    },
    'clusters:write': {
      permission: 'clusters:write',
      label: '管理集群列表',
      effect: '可导入、编辑、测试和删除集群。',
      kubernetesScope: '决定平台使用哪份集群凭据。',
      note: '应只给平台管理员。',
    },
    'resources:read': {
      permission: 'resources:read',
      label: '读取 Kubernetes 资源',
      effect: '可查看已支持的 Kubernetes 资源。',
      kubernetesScope: '会通过集群凭据执行 GET / LIST。',
      note: '是否读得到仍取决于集群权限。',
    },
    'resources:write': {
      permission: 'resources:write',
      label: '变更 Kubernetes 资源',
      effect: '可创建、更新、删除资源。',
      kubernetesScope: '会通过集群凭据执行 CREATE / UPDATE / DELETE。',
      note: '属于高风险权限。',
    },
    'registries:read': {
      permission: 'registries:read',
      label: '查看镜像仓库',
      effect: '可查看仓库接入状态与镜像版本清单。',
      kubernetesScope: '不直接调用 Kubernetes API。',
      note: '适合平台交付或镜像审计场景。',
    },
    'registries:write': {
      permission: 'registries:write',
      label: '维护镜像仓库',
      effect: '可新增、编辑、测试和删除仓库接入。',
      kubernetesScope: '不直接调用 Kubernetes API。',
      note: '会影响镜像浏览与交付入口。',
    },
    'observability:read': {
      permission: 'observability:read',
      label: '查看可观测性',
      effect: '可查看数据源状态和 Grafana 内嵌仪表盘。',
      kubernetesScope: '不直接调用 Kubernetes API。',
      note: '适合监控查看与巡检联动。',
    },
    'observability:write': {
      permission: 'observability:write',
      label: '维护可观测性接入',
      effect: '可新增、编辑、测试和删除数据源。',
      kubernetesScope: '不直接调用 Kubernetes API。',
      note: '会影响平台内的监控入口。',
    },
    'users:read': {
      permission: 'users:read',
      label: '查看平台用户',
      effect: '可查看用户、角色绑定和登录状态。',
      kubernetesScope: '只读平台身份数据。',
      note: '用于平台审计。',
    },
    'users:write': {
      permission: 'users:write',
      label: '维护平台用户',
      effect: '可创建、编辑、停用和删除用户。',
      kubernetesScope: '只影响平台登录与入口控制。',
      note: '与 roles:write 组合后风险更高。',
    },
    'roles:read': {
      permission: 'roles:read',
      label: '查看平台角色',
      effect: '可查看角色、权限集合和影响范围。',
      kubernetesScope: '只读平台权限模型。',
      note: '适合审计授权结构。',
    },
    'roles:write': {
      permission: 'roles:write',
      label: '维护平台角色',
      effect: '可创建、编辑和删除角色。',
      kubernetesScope: '改变的是平台入口授权。',
      note: '需要谨慎授予。',
    },
  }

  return permissionKeys
    .filter((key, index, all) => all.indexOf(key) === index)
    .map((key) => mappingCatalog[key])
    .filter(Boolean)
}

function dedupeRiskItems(items: ResourceRiskItem[]) {
  const seen = new Set<string>()
  return items.filter((item) => {
    const key = `${item.level}:${item.title}:${item.detail}`
    if (seen.has(key)) {
      return false
    }
    seen.add(key)
    return true
  })
}

function collectContainerImages(resource: K8sObject | null) {
  return uniqueNonEmpty(
    findContainerSpecs(resource)
      .map((container) => valueAsString(container.image))
      .filter(Boolean),
  )
}

function hasPrivilegedWorkload(resource: K8sObject | null) {
  if (!resource) {
    return false
  }

  const spec = getPodSpec(resource)
  if (!spec || typeof spec !== 'object') {
    return false
  }

  if (
    Boolean((spec as Record<string, unknown>).hostNetwork) ||
    Boolean((spec as Record<string, unknown>).hostPID) ||
    Boolean((spec as Record<string, unknown>).hostIPC)
  ) {
    return true
  }

  return findContainerSpecs(resource).some((container) => {
    const securityContext = asRecord(container.securityContext)
    return Boolean(securityContext?.privileged)
  })
}

function hasHostPathVolume(resource: K8sObject | null) {
  const spec = getPodSpec(resource)
  const volumes = Array.isArray(asRecord(spec)?.volumes)
    ? ((asRecord(spec)?.volumes as Array<unknown>) ?? [])
    : []

  return volumes.some((volume) => {
    const record = asRecord(volume)
    return Boolean(record?.hostPath)
  })
}

function findContainerSpecs(resource: K8sObject | null) {
  const spec = getPodSpec(resource)
  const record = asRecord(spec)
  const primary = Array.isArray(record?.containers) ? record.containers : []
  const init = Array.isArray(record?.initContainers) ? record.initContainers : []
  return [...primary, ...init].map((item) => asRecord(item)).filter(Boolean) as Array<Record<string, unknown>>
}

function getPodSpec(resource: K8sObject | null) {
  if (!resource) {
    return null
  }

  return (
    readNestedValue(resource, ['spec', 'template', 'spec']) ??
    readNestedValue(resource, ['spec', 'jobTemplate', 'spec', 'template', 'spec']) ??
    readNestedValue(resource, ['spec']) ??
    null
  )
}

function readStringMap(resource: K8sObject | null, path: string[]) {
  const value = readNestedValue(resource, path)
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return {}
  }

  return Object.fromEntries(
    Object.entries(value as Record<string, unknown>).map(([key, entry]) => [key, String(entry)]),
  )
}

function readNestedValue(value: unknown, path: string[]) {
  return path.reduce<unknown>((current, segment) => {
    if (!current || typeof current !== 'object' || Array.isArray(current)) {
      return undefined
    }

    return (current as Record<string, unknown>)[segment]
  }, value)
}

function readNumberValue(resource: K8sObject | null, path: string[]) {
  const value = readNestedValue(resource, path)
  return typeof value === 'number' ? value : undefined
}

function valueAsString(value: unknown) {
  return typeof value === 'string' ? value : ''
}

function uniqueNonEmpty(values: string[]) {
  return values.filter((value, index, all) => value && all.indexOf(value) === index)
}

function groupRegistryArtifactsBySpace(items: RegistryArtifactList['items']): RegistryImageSpace[] {
  const spaceMap = new Map<
    string,
    {
      name: string
      images: Map<
        string,
        {
          name: string
          repository: string
          versions: Array<{
            tag: string
            digest?: string
            buildTime?: string | null
          }>
        }
      >
    }
  >()

  items.forEach((item) => {
    const repository = (item.repository || '').trim()
    if (!repository) {
      return
    }

    const segments = repository.split('/').filter(Boolean)
    const spaceName = segments.length > 1 ? segments[0] : '未分组'
    const imageName = segments.length > 1 ? segments.slice(1).join('/') : repository

    if (!spaceMap.has(spaceName)) {
      spaceMap.set(spaceName, { name: spaceName, images: new Map() })
    }

    const space = spaceMap.get(spaceName)!
    if (!space.images.has(repository)) {
      space.images.set(repository, { name: imageName, repository, versions: [] })
    }

    space.images.get(repository)!.versions.push({
      tag: item.tag,
      digest: item.digest,
      buildTime: item.buildTime,
    })
  })

  return Array.from(spaceMap.values())
    .map((space) => {
      const images = Array.from(space.images.values())
        .map((image) => {
          const versions = image.versions.sort((left, right) => {
            const leftTime = parseDateValue(left.buildTime)
            const rightTime = parseDateValue(right.buildTime)
            if (leftTime && rightTime && leftTime !== rightTime) {
              return rightTime - leftTime
            }
            if (leftTime && !rightTime) {
              return -1
            }
            if (!leftTime && rightTime) {
              return 1
            }
            return right.tag.localeCompare(left.tag)
          })

          return {
            name: image.name,
            repository: image.repository,
            versionCount: versions.length,
            latestBuildTime: versions[0]?.buildTime ?? null,
            versions,
          }
        })
        .sort((left, right) => left.name.localeCompare(right.name))

      return {
        name: space.name,
        imageCount: images.length,
        versionCount: images.reduce((total, image) => total + image.versionCount, 0),
        images,
      }
    })
    .sort((left, right) => left.name.localeCompare(right.name))
}

function filterRegistryImageSpaces(spaces: RegistryImageSpace[], rawQuery: string): RegistryImageSpace[] {
  const query = rawQuery.trim().toLowerCase()
  if (!query) {
    return spaces
  }

  const filteredSpaces: RegistryImageSpace[] = []

  spaces.forEach((space) => {
    const matchedImages: RegistryImage[] = []

    space.images.forEach((image) => {
      const matchedVersions = image.versions.filter((version) => {
        const digest = version.digest?.toLowerCase() ?? ''
        return (
          version.tag.toLowerCase().includes(query) ||
          digest.includes(query) ||
          formatDate(version.buildTime).toLowerCase().includes(query)
        )
      })

      const imageMatched =
        image.name.toLowerCase().includes(query) || image.repository.toLowerCase().includes(query)

      if (!imageMatched && matchedVersions.length === 0) {
        return
      }

      const versions = imageMatched ? image.versions : matchedVersions
      matchedImages.push({
        ...image,
        versionCount: versions.length,
        latestBuildTime: versions[0]?.buildTime ?? null,
        versions,
      })
    })

    const spaceMatched = space.name.toLowerCase().includes(query)
    if (!spaceMatched && matchedImages.length === 0) {
      return
    }

    const images = spaceMatched ? space.images : matchedImages
    filteredSpaces.push({
      ...space,
      imageCount: images.length,
      versionCount: images.reduce((total, image) => total + image.versionCount, 0),
      images,
    })
  })

  return filteredSpaces
}

function parseDateValue(value?: string | null) {
  if (!value) {
    return 0
  }

  const parsed = Date.parse(value)
  return Number.isNaN(parsed) ? 0 : parsed
}

function asRecord(value: unknown) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return null
  }

  return value as Record<string, unknown>
}

function groupPermissions(permissionList: Permission[]): PermissionGroup[] {
  const definitions = [
    { key: 'dashboard', label: '工作台' },
    { key: 'clusters', label: '集群管理' },
    { key: 'resources', label: '资源管理' },
    { key: 'registries', label: '镜像仓库' },
    { key: 'observability', label: '可观测性' },
    { key: 'users', label: '用户管理' },
    { key: 'roles', label: '角色权限' },
  ]

  return definitions
    .map((definition) => ({
      key: definition.key,
      label: definition.label,
      permissions: permissionList.filter((permission) =>
        permission.key.startsWith(`${definition.key}:`),
      ),
    }))
    .filter((group) => group.permissions.length > 0)
}

function parseKubeconfigPreview(raw: string): KubeconfigPreview | null {
  if (!raw.trim()) {
    return null
  }

  try {
    const parsed = YAML.parse(raw) as {
      clusters?: Array<{ name?: string; cluster?: { server?: string } }>
      contexts?: Array<{ name?: string; context?: { cluster?: string; user?: string } }>
      users?: Array<{ name?: string }>
      ['current-context']?: string
    }

    const clusters = parsed.clusters ?? []
    const contexts = (parsed.contexts ?? []).map((item) => ({
      name: item.name || 'unnamed-context',
      cluster: item.context?.cluster || 'unknown-cluster',
      user: item.context?.user || 'unknown-user',
    }))
    const users = (parsed.users ?? []).map((item) => item.name || 'unknown-user')
    const currentContext = parsed['current-context'] || contexts[0]?.name || 'default'
    const currentContextRef = contexts.find((item) => item.name === currentContext)
    const primaryCluster = clusters.find((item) => item.name === currentContextRef?.cluster) ?? clusters[0]
    const primaryServer = primaryCluster?.cluster?.server || '-'
    const suggestedName = normalizeClusterName(currentContextRef?.cluster || currentContext)

    return {
      valid: true,
      suggestedName,
      currentContext,
      primaryServer,
      clusterNames: clusters.map((item) => item.name || 'unknown-cluster'),
      users,
      contexts,
      clusterCount: clusters.length,
      userCount: users.length,
      contextCount: contexts.length,
    }
  } catch (error) {
    return {
      valid: false,
      suggestedName: '',
      currentContext: '',
      primaryServer: '',
      clusterNames: [],
      users: [],
      contexts: [],
      clusterCount: 0,
      userCount: 0,
      contextCount: 0,
      error: error instanceof Error ? error.message : 'YAML 解析失败',
    }
  }
}

function normalizeClusterName(value: string) {
  return value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

async function readTextFile(file: File) {
  return await file.text()
}

async function copyText(value: string) {
  await navigator.clipboard.writeText(value)
}

function isFormValidationError(error: unknown): error is { errorFields: unknown[] } {
  return typeof error === 'object' && error !== null && 'errorFields' in error
}

export default RootApp
