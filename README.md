# KubeFeel

KubeFeel 是一个面向企业场景的 Kubernetes 多集群管理平台，聚焦在统一接入、资源操作、集群巡检、镜像仓库集成与可观测性入口。

## 功能概览

- 多集群接入与管理
- 集群健康巡检
- Kubernetes 常用资源 CRUD
- 平台 RBAC 用户与角色体系
- 镜像仓库集成
- 可观测性数据源接入
- Grafana 仪表盘沉浸式入口
- 基于 Kubespray 的集群创建链路

## 技术栈

- 后端：Go、Gin、GORM
- 前端：React、TypeScript、Vite、Ant Design
- 集群访问：client-go、dynamic client
- 集群创建：Kubespray

## 目录结构

```text
.
├── cmd/server                # 服务启动入口
├── internal/api             # HTTP API
├── internal/config          # 配置加载
├── internal/database        # 数据库初始化
├── internal/integration     # 仓库、可观测性等外部集成
├── internal/kube            # Kubernetes 访问与统计
├── internal/model           # 数据模型
├── internal/provision       # Kubespray 创建集群
├── internal/rbac            # 权限目录与初始化
├── internal/security        # JWT 与加密
├── frontend                 # 前端工程
└── scripts                  # 本地安装与调试脚本
```

## 快速开始

### 1. 启动后端

```bash
go run ./cmd/server
```

默认会在当前工作目录下创建本地数据库，并从 `frontend/dist` 提供前端静态资源。

### 2. 启动前端开发模式

```bash
cd frontend
npm install
npm run dev
```

### 3. 本地安装常驻服务

```bash
./scripts/install_local_service.sh
```

脚本会完成这些事情：

- 构建前端产物
- 编译 Go 服务
- 将运行时文件安装到 `~/Library/Application Support/KubeFeel`
- 生成本地 `launchd` 服务
- 自动生成 JWT、加密密钥和管理员初始密码

## 默认登录信息

- 默认管理员用户名：`admin`
- 初始密码不会写入仓库，也不会硬编码在代码中
- 本地安装后，可从下面的文件读取初始密码：

```bash
~/Library/Application Support/KubeFeel/secrets/bootstrap_admin_password
```

## 关键环境变量

```bash
APP_ADDR
APP_DB_PATH
APP_FRONTEND_DIR
APP_PROVISION_ROOT
APP_JWT_SECRET
APP_ENCRYPTION_SECRET
APP_BOOTSTRAP_ADMIN_USER
APP_BOOTSTRAP_ADMIN_PASSWORD
APP_KUBESPRAY_IMAGE
APP_KUBESPRAY_PLATFORM
```

如果没有显式传入：

- JWT 密钥会自动生成并落盘
- 加密密钥会自动生成并落盘
- 初始管理员密码会自动生成并落盘

## 安全说明

- 集群 `kubeconfig`、仓库凭据、可观测性凭据都会在服务端加密保存
- 仓库中不包含固定 JWT 密钥、固定加密密钥、固定管理员密码
- 推荐通过环境变量或运行时密钥文件管理敏感配置

## 适用场景

- 企业内部多集群统一管理
- 平台化的 Kubernetes 运维入口
- 集群资源、镜像仓库、Grafana 的集中工作台

