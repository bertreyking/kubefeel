package rbac

import (
	"fmt"

	"multikube-manager/internal/model"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	PermissionDashboardRead      = "dashboard:read"
	PermissionClustersRead       = "clusters:read"
	PermissionClustersWrite      = "clusters:write"
	PermissionResourcesRead      = "resources:read"
	PermissionResourcesWrite     = "resources:write"
	PermissionRegistriesRead     = "registries:read"
	PermissionRegistriesWrite    = "registries:write"
	PermissionObservabilityRead  = "observability:read"
	PermissionObservabilityWrite = "observability:write"
	PermissionUsersRead          = "users:read"
	PermissionUsersWrite         = "users:write"
	PermissionRolesRead          = "roles:read"
	PermissionRolesWrite         = "roles:write"
)

type builtInRoleSpec struct {
	Name           string
	Description    string
	PermissionKeys []string
}

var permissionCatalog = []model.Permission{
	{Key: PermissionDashboardRead, Name: "Dashboard Read", Description: "查看平台工作台"},
	{Key: PermissionClustersRead, Name: "Clusters Read", Description: "查看集群与集群状态"},
	{Key: PermissionClustersWrite, Name: "Clusters Write", Description: "新增、更新和删除集群"},
	{Key: PermissionResourcesRead, Name: "Resources Read", Description: "查看 Kubernetes 资源"},
	{Key: PermissionResourcesWrite, Name: "Resources Write", Description: "创建、更新和删除 Kubernetes 资源"},
	{Key: PermissionRegistriesRead, Name: "Registries Read", Description: "查看镜像仓库接入和镜像清单"},
	{Key: PermissionRegistriesWrite, Name: "Registries Write", Description: "新增、更新和删除镜像仓库接入"},
	{Key: PermissionObservabilityRead, Name: "Observability Read", Description: "查看可观测性数据源和仪表盘"},
	{Key: PermissionObservabilityWrite, Name: "Observability Write", Description: "新增、更新和删除可观测性数据源"},
	{Key: PermissionUsersRead, Name: "Users Read", Description: "查看用户与角色绑定"},
	{Key: PermissionUsersWrite, Name: "Users Write", Description: "新增、更新和停用用户"},
	{Key: PermissionRolesRead, Name: "Roles Read", Description: "查看角色和权限"},
	{Key: PermissionRolesWrite, Name: "Roles Write", Description: "创建、更新和删除角色"},
}

var builtInRoles = []builtInRoleSpec{
	{
		Name:           "admin",
		Description:    "平台管理员，拥有全部平台权限",
		PermissionKeys: allPermissionKeys(),
	},
	{
		Name:        "operator",
		Description: "集群运维角色，可管理集群和 Kubernetes 资源",
		PermissionKeys: []string{
			PermissionDashboardRead,
			PermissionClustersRead,
			PermissionClustersWrite,
			PermissionResourcesRead,
			PermissionResourcesWrite,
			PermissionRegistriesRead,
			PermissionRegistriesWrite,
			PermissionObservabilityRead,
			PermissionObservabilityWrite,
			PermissionUsersRead,
			PermissionRolesRead,
		},
	},
	{
		Name:        "viewer",
		Description: "只读角色，可浏览集群和 Kubernetes 资源",
		PermissionKeys: []string{
			PermissionDashboardRead,
			PermissionClustersRead,
			PermissionResourcesRead,
			PermissionRegistriesRead,
			PermissionObservabilityRead,
			PermissionRolesRead,
		},
	},
}

func Seed(db *gorm.DB, adminUsername, adminPassword string) error {
	for _, permission := range permissionCatalog {
		var existing model.Permission
		if err := db.Where("key = ?", permission.Key).First(&existing).Error; err != nil {
			if err != gorm.ErrRecordNotFound {
				return err
			}

			if err := db.Create(&permission).Error; err != nil {
				return err
			}
			continue
		}

		existing.Name = permission.Name
		existing.Description = permission.Description
		if err := db.Save(&existing).Error; err != nil {
			return err
		}
	}

	permissions, err := listPermissionsByKey(db)
	if err != nil {
		return err
	}

	for _, spec := range builtInRoles {
		var role model.Role
		if err := db.Preload("Permissions").Where("name = ?", spec.Name).First(&role).Error; err != nil {
			if err != gorm.ErrRecordNotFound {
				return err
			}

			role = model.Role{
				Name:        spec.Name,
				Description: spec.Description,
				BuiltIn:     true,
			}
			if err := db.Create(&role).Error; err != nil {
				return err
			}
		}

		role.Description = spec.Description
		role.BuiltIn = true
		role.Permissions = lookupPermissions(permissions, spec.PermissionKeys)
		if err := db.Session(&gorm.Session{FullSaveAssociations: true}).Updates(&role).Error; err != nil {
			return err
		}
		if err := db.Model(&role).Association("Permissions").Replace(role.Permissions); err != nil {
			return err
		}
	}

	var adminRole model.Role
	if err := db.Where("name = ?", "admin").First(&adminRole).Error; err != nil {
		return err
	}

	var adminUser model.User
	err = db.Preload("Roles").Where("username = ?", adminUsername).First(&adminUser).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}

	if err == gorm.ErrRecordNotFound {
		passwordHash, hashErr := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
		if hashErr != nil {
			return hashErr
		}

		adminUser = model.User{
			Username:     adminUsername,
			DisplayName:  "Platform Admin",
			PasswordHash: string(passwordHash),
			Active:       true,
			Roles:        []model.Role{adminRole},
		}
		return db.Create(&adminUser).Error
	}

	if err := db.Model(&adminUser).Association("Roles").Append(&adminRole); err != nil {
		return fmt.Errorf("append admin role: %w", err)
	}

	return nil
}

func Catalog() []model.Permission {
	result := make([]model.Permission, len(permissionCatalog))
	copy(result, permissionCatalog)
	return result
}

func allPermissionKeys() []string {
	keys := make([]string, 0, len(permissionCatalog))
	for _, permission := range permissionCatalog {
		keys = append(keys, permission.Key)
	}

	return keys
}

func listPermissionsByKey(db *gorm.DB) (map[string]model.Permission, error) {
	var permissions []model.Permission
	if err := db.Find(&permissions).Error; err != nil {
		return nil, err
	}

	result := map[string]model.Permission{}
	for _, permission := range permissions {
		result[permission.Key] = permission
	}

	return result, nil
}

func lookupPermissions(permissions map[string]model.Permission, keys []string) []model.Permission {
	result := make([]model.Permission, 0, len(keys))
	for _, key := range keys {
		if permission, ok := permissions[key]; ok {
			result = append(result, permission)
		}
	}

	return result
}
