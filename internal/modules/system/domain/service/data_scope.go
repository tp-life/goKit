// Package service 领域服务：不依赖任何框架的纯业务逻辑
package service

import "goKit/internal/modules/system/domain/entity"

// RoleDataScope 单个角色解析好的数据范围。
// DeptIDs 由应用层预先展开（自定义部门 / 本部门及以下），领域层不做 IO。
type RoleDataScope struct {
	Scope   int8
	DeptIDs []uint64
}

// Filter 数据权限过滤结果，由持久层翻译成 SQL 条件。
type Filter struct {
	All     bool     // 放行全部
	DeptIDs []uint64 // 可见部门集合
	SelfID  uint64   // >0 表示额外可见本人数据
	DenyAll bool     // 无任何可见范围
}

// MergeDataScope 合并用户所有角色的数据范围，取最宽结果。
// 规则：超管或任一角色为“全部”→ 全部；自定义/本部门及以下/本部门 → 部门集合并集；“仅本人”→ 附加本人；
// 无任何角色或没有任何范围 → DenyAll。
func MergeDataScope(userID uint64, isSuper bool, roles []RoleDataScope) Filter {
	if isSuper {
		return Filter{All: true}
	}
	deptSet := make(map[uint64]struct{})
	self := false
	for _, r := range roles {
		switch r.Scope {
		case entity.DataScopeAll:
			return Filter{All: true}
		case entity.DataScopeCustom, entity.DataScopeDeptAndChildren, entity.DataScopeDept:
			for _, id := range r.DeptIDs {
				deptSet[id] = struct{}{}
			}
		case entity.DataScopeSelf:
			self = true
		}
	}

	f := Filter{}
	if len(deptSet) > 0 {
		f.DeptIDs = make([]uint64, 0, len(deptSet))
		for id := range deptSet {
			f.DeptIDs = append(f.DeptIDs, id)
		}
	}
	if self {
		f.SelfID = userID
	}
	if len(f.DeptIDs) == 0 && f.SelfID == 0 {
		f.DenyAll = true
	}
	return f
}
