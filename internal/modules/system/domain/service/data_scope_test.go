package service

import (
	"testing"

	"goKit/internal/modules/system/domain/entity"
)

func TestMergeDataScope(t *testing.T) {
	tests := []struct {
		name     string
		userID   uint64
		isSuper  bool
		roles    []RoleDataScope
		wantAll  bool
		wantDeny bool
		wantDept []uint64
		wantSelf uint64
	}{
		{
			name:    "超管放行全部",
			userID:  1,
			isSuper: true,
			wantAll: true,
		},
		{
			name:    "任一角色为全部则全部",
			userID:  1,
			roles:   []RoleDataScope{{Scope: entity.DataScopeSelf}, {Scope: entity.DataScopeAll}},
			wantAll: true,
		},
		{
			name:   "自定义与本部门合并取并集",
			userID: 1,
			roles: []RoleDataScope{
				{Scope: entity.DataScopeCustom, DeptIDs: []uint64{10, 11}},
				{Scope: entity.DataScopeDept, DeptIDs: []uint64{11, 12}},
			},
			wantDept: []uint64{10, 11, 12},
		},
		{
			name:     "仅本人",
			userID:   7,
			roles:    []RoleDataScope{{Scope: entity.DataScopeSelf}},
			wantSelf: 7,
		},
		{
			name:   "部门范围叠加仅本人",
			userID: 7,
			roles: []RoleDataScope{
				{Scope: entity.DataScopeDeptAndChildren, DeptIDs: []uint64{20}},
				{Scope: entity.DataScopeSelf},
			},
			wantDept: []uint64{20},
			wantSelf: 7,
		},
		{
			name:     "无角色拒绝全部",
			userID:   1,
			roles:    nil,
			wantDeny: true,
		},
		{
			name:     "自定义范围为空且无本人则拒绝",
			userID:   1,
			roles:    []RoleDataScope{{Scope: entity.DataScopeCustom}},
			wantDeny: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := MergeDataScope(tt.userID, tt.isSuper, tt.roles)
			if f.All != tt.wantAll {
				t.Errorf("All = %v, want %v", f.All, tt.wantAll)
			}
			if f.DenyAll != tt.wantDeny {
				t.Errorf("DenyAll = %v, want %v", f.DenyAll, tt.wantDeny)
			}
			if f.SelfID != tt.wantSelf {
				t.Errorf("SelfID = %v, want %v", f.SelfID, tt.wantSelf)
			}
			if len(f.DeptIDs) != len(tt.wantDept) {
				t.Fatalf("DeptIDs = %v, want %v", f.DeptIDs, tt.wantDept)
			}
			set := make(map[uint64]bool, len(f.DeptIDs))
			for _, id := range f.DeptIDs {
				set[id] = true
			}
			for _, id := range tt.wantDept {
				if !set[id] {
					t.Errorf("DeptIDs missing %d", id)
				}
			}
		})
	}
}
