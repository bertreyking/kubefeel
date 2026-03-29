package model

import (
	"strings"
	"time"
)

type Permission struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	Key         string    `json:"key" gorm:"size:120;uniqueIndex;not null"`
	Name        string    `json:"name" gorm:"size:120;not null"`
	Description string    `json:"description" gorm:"size:255;not null"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Role struct {
	ID          uint         `json:"id" gorm:"primaryKey"`
	Name        string       `json:"name" gorm:"size:80;uniqueIndex;not null"`
	Description string       `json:"description" gorm:"size:255;not null"`
	BuiltIn     bool         `json:"builtIn"`
	Permissions []Permission `json:"permissions,omitempty" gorm:"many2many:role_permissions;constraint:OnDelete:CASCADE;"`
	CreatedAt   time.Time    `json:"createdAt"`
	UpdatedAt   time.Time    `json:"updatedAt"`
}

type User struct {
	ID           uint       `json:"id" gorm:"primaryKey"`
	Username     string     `json:"username" gorm:"size:80;uniqueIndex;not null"`
	DisplayName  string     `json:"displayName" gorm:"size:120;not null"`
	PasswordHash string     `json:"-" gorm:"size:255;not null"`
	Active       bool       `json:"active" gorm:"default:true"`
	LastLoginAt  *time.Time `json:"lastLoginAt"`
	Roles        []Role     `json:"roles,omitempty" gorm:"many2many:user_roles;constraint:OnDelete:CASCADE;"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

type Cluster struct {
	ID                  uint       `json:"id" gorm:"primaryKey"`
	Name                string     `json:"name" gorm:"size:120;uniqueIndex;not null"`
	Description         string     `json:"description" gorm:"size:255"`
	Region              string     `json:"region" gorm:"size:120"`
	Server              string     `json:"server" gorm:"size:255;not null"`
	CurrentContext      string     `json:"currentContext" gorm:"size:255;not null"`
	Version             string     `json:"version" gorm:"size:120"`
	CRIVersion          string     `json:"criVersion" gorm:"size:120"`
	Mode                string     `json:"mode" gorm:"size:40;not null;default:ready"`
	Status              string     `json:"status" gorm:"size:60;not null"`
	LastError           string     `json:"lastError" gorm:"type:text"`
	LastConnectedAt     *time.Time `json:"lastConnectedAt"`
	KubeconfigEncrypted string     `json:"-" gorm:"type:text;not null"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
}

type ClusterProvisionJob struct {
	ID                  uint       `json:"id" gorm:"primaryKey"`
	Name                string     `json:"name" gorm:"size:120;not null"`
	Region              string     `json:"region" gorm:"size:120;not null"`
	Description         string     `json:"description" gorm:"size:255"`
	Mode                string     `json:"mode" gorm:"size:40;not null;default:ready"`
	Provider            string     `json:"provider" gorm:"size:40;not null;default:kubespray"`
	ProvisionTemplate   string     `json:"provisionTemplate" gorm:"size:80"`
	KubesprayVersion    string     `json:"kubesprayVersion" gorm:"size:80"`
	KubesprayImage      string     `json:"kubesprayImage" gorm:"size:255"`
	ImageRegistryPreset string     `json:"imageRegistryPreset" gorm:"size:80"`
	ImageRegistry       string     `json:"imageRegistry" gorm:"size:255"`
	Status              string     `json:"status" gorm:"size:40;not null"`
	Step                string     `json:"step" gorm:"size:120"`
	KubernetesVersion   string     `json:"kubernetesVersion" gorm:"size:120"`
	NetworkPlugin       string     `json:"networkPlugin" gorm:"size:80"`
	APIServerEndpoint   string     `json:"apiServerEndpoint" gorm:"size:255"`
	SSHUser             string     `json:"sshUser" gorm:"size:120"`
	ControlPlaneCount   int        `json:"controlPlaneCount"`
	WorkerCount         int        `json:"workerCount"`
	LastError           string     `json:"lastError" gorm:"type:text"`
	ResultClusterID     *uint      `json:"resultClusterId"`
	StartedAt           *time.Time `json:"startedAt"`
	CompletedAt         *time.Time `json:"completedAt"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
}

type RegistryIntegration struct {
	ID              uint       `json:"id" gorm:"primaryKey"`
	Name            string     `json:"name" gorm:"size:120;uniqueIndex;not null"`
	Type            string     `json:"type" gorm:"size:40;not null"`
	Description     string     `json:"description" gorm:"size:255"`
	Endpoint        string     `json:"endpoint" gorm:"size:255;not null"`
	Namespace       string     `json:"namespace" gorm:"size:255"`
	Username        string     `json:"username" gorm:"size:120"`
	SecretEncrypted string     `json:"-" gorm:"type:text"`
	SkipTLSVerify   bool       `json:"skipTLSVerify"`
	Status          string     `json:"status" gorm:"size:40;not null;default:unknown"`
	LastError       string     `json:"lastError" gorm:"type:text"`
	LastCheckedAt   *time.Time `json:"lastCheckedAt"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

type ObservabilitySource struct {
	ID              uint       `json:"id" gorm:"primaryKey"`
	Name            string     `json:"name" gorm:"size:120;uniqueIndex;not null"`
	Type            string     `json:"type" gorm:"size:40;not null"`
	Description     string     `json:"description" gorm:"size:255"`
	Endpoint        string     `json:"endpoint" gorm:"size:255;not null"`
	Username        string     `json:"username" gorm:"size:120"`
	SecretEncrypted string     `json:"-" gorm:"type:text"`
	DashboardPath   string     `json:"dashboardPath" gorm:"size:255"`
	SkipTLSVerify   bool       `json:"skipTLSVerify"`
	Status          string     `json:"status" gorm:"size:40;not null;default:unknown"`
	LastError       string     `json:"lastError" gorm:"type:text"`
	LastCheckedAt   *time.Time `json:"lastCheckedAt"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

func (u *User) HasRole(name string) bool {
	for _, role := range u.Roles {
		if strings.EqualFold(role.Name, name) {
			return true
		}
	}

	return false
}

func (u *User) PermissionKeys() []string {
	keys := map[string]struct{}{}
	for _, role := range u.Roles {
		for _, permission := range role.Permissions {
			keys[permission.Key] = struct{}{}
		}
	}

	result := make([]string, 0, len(keys))
	for key := range keys {
		result = append(result, key)
	}

	return result
}
