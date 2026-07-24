package persistence

import (
	"context"

	"goKit/internal/domain/role"
	"goKit/pkg/kit/db"
)

type assignRepo struct {
	client *db.Client
}

func NewAssignRepository(client *db.Client) role.AssignRepository {
	return &assignRepo{client: client}
}

// setRelations 通用关联重置：清空 leftID 的全部关联后批量写入。
func (r *assignRepo) setRelations(ctx context.Context, model any, table, leftField, rightField string, leftID uint64, rightIDs []uint64) error {
	tx := r.client.GetDB(ctx)
	if err := tx.Table(table).Where(leftField+" = ?", leftID).Delete(model).Error; err != nil {
		return err
	}
	if len(rightIDs) == 0 {
		return nil
	}
	rows := make([]map[string]any, 0, len(rightIDs))
	for _, rid := range rightIDs {
		rows = append(rows, map[string]any{leftField: leftID, rightField: rid})
	}
	return tx.Table(table).Create(&rows).Error
}

func (r *assignRepo) getRightIDs(ctx context.Context, table, leftField, rightField string, leftID uint64) ([]uint64, error) {
	var ids []uint64
	err := r.client.GetDB(ctx).Table(table).
		Where(leftField+" = ?", leftID).
		Pluck(rightField, &ids).Error
	return ids, err
}

func (r *assignRepo) getLeftIDs(ctx context.Context, table, leftField, rightField string, rightID uint64) ([]uint64, error) {
	var ids []uint64
	err := r.client.GetDB(ctx).Table(table).
		Where(rightField+" = ?", rightID).
		Pluck(leftField, &ids).Error
	return ids, err
}

func (r *assignRepo) SetUserRoles(ctx context.Context, userID uint64, roleIDs []uint64) error {
	return r.setRelations(ctx, &role.UserRole{}, "sys_user_roles", "user_id", "role_id", userID, roleIDs)
}

func (r *assignRepo) GetRoleIDsByUser(ctx context.Context, userID uint64) ([]uint64, error) {
	return r.getRightIDs(ctx, "sys_user_roles", "user_id", "role_id", userID)
}

func (r *assignRepo) SetRoleMenus(ctx context.Context, roleID uint64, menuIDs []uint64) error {
	return r.setRelations(ctx, &role.RoleMenu{}, "sys_role_menus", "role_id", "menu_id", roleID, menuIDs)
}

func (r *assignRepo) GetMenuIDsByRole(ctx context.Context, roleID uint64) ([]uint64, error) {
	return r.getRightIDs(ctx, "sys_role_menus", "role_id", "menu_id", roleID)
}

func (r *assignRepo) SetRoleDepts(ctx context.Context, roleID uint64, deptIDs []uint64) error {
	return r.setRelations(ctx, &role.RoleDept{}, "sys_role_depts", "role_id", "dept_id", roleID, deptIDs)
}

func (r *assignRepo) GetDeptIDsByRole(ctx context.Context, roleID uint64) ([]uint64, error) {
	return r.getRightIDs(ctx, "sys_role_depts", "role_id", "dept_id", roleID)
}

func (r *assignRepo) SetRoleUsers(ctx context.Context, roleID uint64, userIDs []uint64) error {
	return r.setRelations(ctx, &role.UserRole{}, "sys_user_roles", "role_id", "user_id", roleID, userIDs)
}

func (r *assignRepo) GetUserIDsByRole(ctx context.Context, roleID uint64) ([]uint64, error) {
	return r.getLeftIDs(ctx, "sys_user_roles", "user_id", "role_id", roleID)
}
