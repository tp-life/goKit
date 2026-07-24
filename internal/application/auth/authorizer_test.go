package auth

import "testing"

func TestPermSetHas(t *testing.T) {
	super := &PermSet{IsSuper: true}
	if !super.Has("anything") {
		t.Error("super should pass any perm")
	}
	normal := &PermSet{Codes: map[string]struct{}{"system:user:list": {}}}
	if !normal.Has("system:user:list") {
		t.Error("should have system:user:list")
	}
	if normal.Has("system:user:delete") {
		t.Error("should not have system:user:delete")
	}
}
