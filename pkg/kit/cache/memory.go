// Package cache 提供极简的进程内缓存，用于权限点等热数据。
// 通过版本号实现批量失效：变更方 BumpVersion 后，旧版本缓存全部作废。
package cache

import (
	"sync"
	"time"
)

type entry struct {
	value     any
	version   uint64
	expiresAt time.Time
}

// Memory 线程安全的进程内缓存
type Memory struct {
	mu      sync.RWMutex
	items   map[string]entry
	version uint64
}

func NewMemory() *Memory {
	return &Memory{items: make(map[string]entry)}
}

// Get 命中且未过期、版本未失效时返回 value
func (m *Memory) Get(key string) (any, bool) {
	m.mu.RLock()
	e, ok := m.items[key]
	m.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if e.version != m.currentVersion() {
		return nil, false
	}
	if !e.expiresAt.IsZero() && time.Now().After(e.expiresAt) {
		return nil, false
	}
	return e.value, true
}

// Set 写入缓存，ttl<=0 表示不过期（仅靠版本失效）
func (m *Memory) Set(key string, value any, ttl time.Duration) {
	e := entry{value: value, version: m.currentVersion()}
	if ttl > 0 {
		e.expiresAt = time.Now().Add(ttl)
	}
	m.mu.Lock()
	m.items[key] = e
	m.mu.Unlock()
}

// BumpVersion 版本号 +1，所有旧缓存即刻失效
func (m *Memory) BumpVersion() {
	m.mu.Lock()
	m.version++
	m.mu.Unlock()
}

func (m *Memory) currentVersion() uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.version
}
