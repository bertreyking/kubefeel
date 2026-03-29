package api

import (
	"fmt"
	"net/http"
	"strings"

	"multikube-manager/internal/model"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type userPayload struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Password    string `json:"password"`
	Active      *bool  `json:"active"`
	RoleIDs     []uint `json:"roleIds"`
}

type rolePayload struct {
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	PermissionKeys []string `json:"permissionKeys"`
}

func (s *Server) listUsers(c *gin.Context) {
	var users []model.User
	if err := s.db.Preload("Roles.Permissions").Order("created_at desc").Find(&users).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "查询用户失败")
		return
	}

	result := make([]gin.H, 0, len(users))
	for _, user := range users {
		result = append(result, serializeUser(user))
	}

	respondData(c, http.StatusOK, result)
}

func (s *Server) createUser(c *gin.Context) {
	var input userPayload
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, http.StatusBadRequest, "invalid user payload")
		return
	}

	if strings.TrimSpace(input.Username) == "" || strings.TrimSpace(input.Password) == "" {
		respondError(c, http.StatusBadRequest, "username and password are required")
		return
	}

	roles, err := s.loadRolesByIDs(input.RoleIDs)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "密码加密失败")
		return
	}

	user := model.User{
		Username:     strings.TrimSpace(input.Username),
		DisplayName:  fallbackDisplayName(input.DisplayName, input.Username),
		PasswordHash: string(passwordHash),
		Active:       true,
		Roles:        roles,
	}
	if input.Active != nil {
		user.Active = *input.Active
	}

	if err := s.db.Create(&user).Error; err != nil {
		respondError(c, http.StatusBadRequest, "创建用户失败，用户名可能已存在")
		return
	}

	if err := s.db.Preload("Roles.Permissions").First(&user, user.ID).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "查询新建用户失败")
		return
	}

	respondData(c, http.StatusCreated, serializeUser(user))
}

func (s *Server) updateUser(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid user id")
		return
	}

	var user model.User
	if err := s.db.Preload("Roles.Permissions").First(&user, id).Error; err != nil {
		respondError(c, http.StatusNotFound, "user not found")
		return
	}

	var input userPayload
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, http.StatusBadRequest, "invalid user payload")
		return
	}

	if trimmed := strings.TrimSpace(input.Username); trimmed != "" {
		user.Username = trimmed
	}
	if trimmed := strings.TrimSpace(input.DisplayName); trimmed != "" {
		user.DisplayName = trimmed
	}
	if input.Active != nil {
		user.Active = *input.Active
	}
	if strings.TrimSpace(input.Password) != "" {
		passwordHash, hashErr := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
		if hashErr != nil {
			respondError(c, http.StatusInternalServerError, "密码加密失败")
			return
		}
		user.PasswordHash = string(passwordHash)
	}
	if input.RoleIDs != nil {
		roles, roleErr := s.loadRolesByIDs(input.RoleIDs)
		if roleErr != nil {
			respondError(c, http.StatusBadRequest, roleErr.Error())
			return
		}
		user.Roles = roles
	}

	if err := s.db.Session(&gorm.Session{FullSaveAssociations: true}).Save(&user).Error; err != nil {
		respondError(c, http.StatusBadRequest, "更新用户失败")
		return
	}
	if input.RoleIDs != nil {
		if err := s.db.Model(&user).Association("Roles").Replace(user.Roles); err != nil {
			respondError(c, http.StatusBadRequest, "更新用户角色失败")
			return
		}
	}
	if err := s.db.Preload("Roles.Permissions").First(&user, user.ID).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "重新加载用户失败")
		return
	}

	respondData(c, http.StatusOK, serializeUser(user))
}

func (s *Server) deleteUser(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid user id")
		return
	}

	current := currentUserFromContext(c)
	if current != nil && current.ID == id {
		respondError(c, http.StatusBadRequest, "不能删除当前登录用户")
		return
	}

	if err := s.db.Delete(&model.User{}, id).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "删除用户失败")
		return
	}

	respondNoContent(c)
}

func (s *Server) listRoles(c *gin.Context) {
	var roles []model.Role
	if err := s.db.Preload("Permissions").Order("created_at asc").Find(&roles).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "查询角色失败")
		return
	}

	respondData(c, http.StatusOK, roles)
}

func (s *Server) getRole(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid role id")
		return
	}

	var role model.Role
	if err := s.db.Preload("Permissions").First(&role, id).Error; err != nil {
		respondError(c, http.StatusNotFound, "role not found")
		return
	}

	respondData(c, http.StatusOK, role)
}

func (s *Server) createRole(c *gin.Context) {
	var input rolePayload
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, http.StatusBadRequest, "invalid role payload")
		return
	}

	if strings.TrimSpace(input.Name) == "" {
		respondError(c, http.StatusBadRequest, "name is required")
		return
	}

	permissions, err := s.loadPermissionsByKeys(input.PermissionKeys)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return
	}

	role := model.Role{
		Name:        strings.TrimSpace(input.Name),
		Description: strings.TrimSpace(input.Description),
		BuiltIn:     false,
		Permissions: permissions,
	}

	if err := s.db.Create(&role).Error; err != nil {
		respondError(c, http.StatusBadRequest, "创建角色失败，名称可能已存在")
		return
	}
	if err := s.db.Model(&role).Association("Permissions").Replace(role.Permissions); err != nil {
		respondError(c, http.StatusBadRequest, "绑定角色权限失败")
		return
	}
	if err := s.db.Preload("Permissions").First(&role, role.ID).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "重新加载角色失败")
		return
	}

	respondData(c, http.StatusCreated, role)
}

func (s *Server) updateRole(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid role id")
		return
	}

	var role model.Role
	if err := s.db.Preload("Permissions").First(&role, id).Error; err != nil {
		respondError(c, http.StatusNotFound, "role not found")
		return
	}

	var input rolePayload
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, http.StatusBadRequest, "invalid role payload")
		return
	}

	if trimmed := strings.TrimSpace(input.Name); trimmed != "" {
		role.Name = trimmed
	}
	role.Description = strings.TrimSpace(input.Description)

	if input.PermissionKeys != nil {
		permissions, permissionErr := s.loadPermissionsByKeys(input.PermissionKeys)
		if permissionErr != nil {
			respondError(c, http.StatusBadRequest, permissionErr.Error())
			return
		}
		role.Permissions = permissions
	}

	if err := s.db.Save(&role).Error; err != nil {
		respondError(c, http.StatusBadRequest, "更新角色失败")
		return
	}
	if input.PermissionKeys != nil {
		if err := s.db.Model(&role).Association("Permissions").Replace(role.Permissions); err != nil {
			respondError(c, http.StatusBadRequest, "更新角色权限失败")
			return
		}
	}
	if err := s.db.Preload("Permissions").First(&role, role.ID).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "重新加载角色失败")
		return
	}

	respondData(c, http.StatusOK, role)
}

func (s *Server) deleteRole(c *gin.Context) {
	id, err := parseUintParam(c, "id")
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid role id")
		return
	}

	var role model.Role
	if err := s.db.First(&role, id).Error; err != nil {
		respondError(c, http.StatusNotFound, "role not found")
		return
	}

	if role.BuiltIn {
		respondError(c, http.StatusBadRequest, "内置角色不允许删除")
		return
	}

	if err := s.db.Delete(&role).Error; err != nil {
		respondError(c, http.StatusInternalServerError, "删除角色失败")
		return
	}

	respondNoContent(c)
}

func (s *Server) loadRolesByIDs(ids []uint) ([]model.Role, error) {
	if len(ids) == 0 {
		return []model.Role{}, nil
	}

	var roles []model.Role
	if err := s.db.Preload("Permissions").Find(&roles, ids).Error; err != nil {
		return nil, err
	}
	if len(roles) != len(ids) {
		return nil, fmt.Errorf("some roles were not found")
	}

	return roles, nil
}

func (s *Server) loadPermissionsByKeys(keys []string) ([]model.Permission, error) {
	if len(keys) == 0 {
		return []model.Permission{}, nil
	}

	var permissions []model.Permission
	if err := s.db.Where("key IN ?", keys).Find(&permissions).Error; err != nil {
		return nil, err
	}
	if len(permissions) != len(keys) {
		return nil, fmt.Errorf("some permissions were not found")
	}

	return permissions, nil
}

func fallbackDisplayName(displayName, username string) string {
	if trimmed := strings.TrimSpace(displayName); trimmed != "" {
		return trimmed
	}

	return strings.TrimSpace(username)
}
