// Package seed 提供首次启动的数据播种：根部门、admin 账号、管理员角色与完整菜单权限树。
package seed

import (
	"context"
	"log/slog"

	"golang.org/x/crypto/bcrypt"

	"goKit/internal/domain/dept"
	"goKit/internal/domain/menu"
	"goKit/internal/domain/role"
	"goKit/internal/domain/user"
)

// DefaultAdminPassword admin 初始密码，请在首次登录后立即修改
const DefaultAdminPassword = "Admin@123"

type Seeder struct {
	userRepo   user.UserRepository
	roleRepo   role.RoleRepository
	deptRepo   dept.DeptRepository
	menuRepo   menu.MenuRepository
	assignRepo role.AssignRepository
	logger     *slog.Logger
}

func NewSeeder(
	userRepo user.UserRepository,
	roleRepo role.RoleRepository,
	deptRepo dept.DeptRepository,
	menuRepo menu.MenuRepository,
	assignRepo role.AssignRepository,
	logger *slog.Logger,
) *Seeder {
	return &Seeder{userRepo, roleRepo, deptRepo, menuRepo, assignRepo, logger}
}

// Run 幂等播种：admin 用户已存在则直接跳过
func (s *Seeder) Run(ctx context.Context) error {
	existing, err := s.userRepo.FindByUsername(ctx, "admin")
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}

	// 1. 根部门
	rootDept := &dept.Dept{ParentID: 0, Name: "总公司", Sort: 0, Status: 1}
	if err := s.deptRepo.Create(ctx, rootDept); err != nil {
		return err
	}

	// 2. admin 用户
	hashed, err := bcrypt.GenerateFromPassword([]byte(DefaultAdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	admin := &user.User{
		Username: "admin",
		Password: string(hashed),
		Nickname: "超级管理员",
		DeptID:   rootDept.ID,
		Status:   1,
		IsSuper:  true,
	}
	if err := s.userRepo.Create(ctx, admin); err != nil {
		return err
	}

	// 3. 管理员角色（全部数据权限）
	adminRole := &role.Role{
		Name:      "超级管理员",
		Code:      "admin",
		DataScope: role.DataScopeAll,
		Status:    1,
		Remark:    "内置角色，拥有全部权限",
	}
	if err := s.roleRepo.Create(ctx, adminRole); err != nil {
		return err
	}
	if err := s.assignRepo.SetUserRoles(ctx, admin.ID, []uint64{adminRole.ID}); err != nil {
		return err
	}

	// 4. 菜单权限树
	menuIDs, err := s.seedMenus(ctx)
	if err != nil {
		return err
	}
	// 5. 管理员角色关联全部菜单
	if err := s.assignRepo.SetRoleMenus(ctx, adminRole.ID, menuIDs); err != nil {
		return err
	}

	s.logger.Info("seed_completed", slog.String("admin", "admin/"+DefaultAdminPassword))
	return nil
}

// seedMenus 创建系统管理菜单树，返回全部菜单 ID
func (s *Seeder) seedMenus(ctx context.Context) ([]uint64, error) {
	var allIDs []uint64

	create := func(parentID uint64, title string, typ int8, path, perm string, sort int) (uint64, error) {
		m := &menu.Menu{ParentID: parentID, Title: title, Type: typ, Path: path, PermCode: perm, Sort: sort, Status: 1}
		if err := s.menuRepo.Create(ctx, m); err != nil {
			return 0, err
		}
		allIDs = append(allIDs, m.ID)
		return m.ID, nil
	}

	sysDir, err := create(0, "系统管理", menu.MenuTypeDir, "/system", "", 1)
	if err != nil {
		return nil, err
	}

	// 每个资源：菜单 + 标准按钮权限点
	resources := []struct {
		title string
		path  string
		code  string // 权限点前缀，如 system:user
		sort  int
		extra []string // 额外动作，如 assign-role
	}{
		{"用户管理", "/system/users", "system:user", 1, []string{"assign-role", "reset-pwd"}},
		{"角色管理", "/system/roles", "system:role", 2, []string{"assign-menu", "assign-user"}},
		{"部门管理", "/system/depts", "system:dept", 3, nil},
		{"菜单管理", "/system/menus", "system:menu", 4, nil},
	}
	actions := []string{"list", "create", "update", "delete"}

	for _, res := range resources {
		menuID, err := create(sysDir, res.title, menu.MenuTypeMenu, res.path, res.code+":list", res.sort)
		if err != nil {
			return nil, err
		}
		for i, act := range append(actions[1:], res.extra...) {
			if _, err := create(menuID, res.title+"-"+act, menu.MenuTypeButton, "", res.code+":"+act, i+1); err != nil {
				return nil, err
			}
		}
	}

	// 操作日志：只读，仅有 list 权限点
	if _, err := create(sysDir, "操作日志", menu.MenuTypeMenu, "/system/logs", "system:log:list", 5); err != nil {
		return nil, err
	}
	return allIDs, nil
}
