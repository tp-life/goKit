package service

import (
	"context"
	"fmt"
	"time"

	"goKit/internal/modules/system/domain/repository"
	"goKit/pkg/kit/cache"
)

// permCacheTTL 权限缓存有效期，兜底防止版本失效机制遗漏
const permCacheTTL = 10 * time.Minute

// PermSet 用户权限集合
type PermSet struct {
	IsSuper bool
	Codes   map[string]struct{}
}

// Has 判断是否拥有权限点，超管放行一切
func (p *PermSet) Has(code string) bool {
	if p.IsSuper {
		return true
	}
	_, ok := p.Codes[code]
	return ok
}

// PermResolver 用户权限加载器，带进程内缓存。
// 角色/菜单/分配关系变更后必须调用 Invalidate。
type PermResolver struct {
	userRepo   repository.UserRepository
	assignRepo repository.AssignRepository
	menuRepo   repository.MenuRepository
	cache      *cache.Memory
}

func NewPermResolver(
	userRepo repository.UserRepository,
	assignRepo repository.AssignRepository,
	menuRepo repository.MenuRepository,
	cache *cache.Memory,
) *PermResolver {
	return &PermResolver{userRepo, assignRepo, menuRepo, cache}
}

// Invalidate 使全部用户权限缓存失效
func (r *PermResolver) Invalidate() {
	r.cache.BumpVersion()
}

// Resolve 加载用户权限集合（优先缓存）
func (r *PermResolver) Resolve(ctx context.Context, userID uint64) (*PermSet, error) {
	key := fmt.Sprintf("perm:%d", userID)
	if v, ok := r.cache.Get(key); ok {
		return v.(*PermSet), nil
	}
	set, err := r.load(ctx, userID)
	if err != nil {
		return nil, err
	}
	r.cache.Set(key, set, permCacheTTL)
	return set, nil
}

func (r *PermResolver) load(ctx context.Context, userID uint64) (*PermSet, error) {
	user, err := r.userRepo.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.Status != 1 {
		return &PermSet{Codes: map[string]struct{}{}}, nil
	}
	if user.IsSuper {
		return &PermSet{IsSuper: true}, nil
	}
	roleIDs, err := r.assignRepo.GetRoleIDsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	codes, err := r.menuRepo.FindPermCodesByRoleIDs(ctx, roleIDs)
	if err != nil {
		return nil, err
	}
	set := &PermSet{Codes: make(map[string]struct{}, len(codes))}
	for _, c := range codes {
		set.Codes[c] = struct{}{}
	}
	return set, nil
}

// LocalAuthorizer 本地授权器（单机模式），实现 auth.Authorizer 端口
type LocalAuthorizer struct {
	resolver *PermResolver
}

func NewLocalAuthorizer(resolver *PermResolver) *LocalAuthorizer {
	return &LocalAuthorizer{resolver: resolver}
}

func (a *LocalAuthorizer) CheckPerm(ctx context.Context, userID uint64, permCode string) (bool, error) {
	set, err := a.resolver.Resolve(ctx, userID)
	if err != nil {
		return false, err
	}
	return set.Has(permCode), nil
}
