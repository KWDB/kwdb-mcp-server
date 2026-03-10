package db

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
)

// MultiPoolManager 按数据库 URI 管理多个连接池，用于无状态多租户场景。
// 目前实现为按需惰性创建连接池并常驻内存，连接数仍由各自的 PoolManager 控制。
type MultiPoolManager struct {
	mu    sync.RWMutex
	pools map[string]*PoolManager
}

var (
	globalMultiPoolMgr *MultiPoolManager
	multiPoolOnce      sync.Once
)

// GetMultiPoolManager 返回全局多池管理器实例。
func GetMultiPoolManager() *MultiPoolManager {
	multiPoolOnce.Do(func() {
		globalMultiPoolMgr = &MultiPoolManager{
			pools: make(map[string]*PoolManager),
		}
	})
	return globalMultiPoolMgr
}

// getOrCreatePool 根据给定的数据库 URI 获取或创建对应的连接池。
func (mm *MultiPoolManager) getOrCreatePool(connectionString string) (*PoolManager, error) {
	if connectionString == "" {
		return nil, fmt.Errorf("connection string cannot be empty")
	}

	// 快路径：先尝试读锁查找已有池
	mm.mu.RLock()
	if pool, ok := mm.pools[connectionString]; ok && pool != nil && pool.IsInitialized() {
		mm.mu.RUnlock()
		return pool, nil
	}
	mm.mu.RUnlock()

	// 慢路径：加写锁创建新池
	mm.mu.Lock()
	defer mm.mu.Unlock()

	// 双重检查，避免并发重复创建
	if pool, ok := mm.pools[connectionString]; ok && pool != nil && pool.IsInitialized() {
		return pool, nil
	}

	pool := &PoolManager{
		config: DefaultPoolConfig,
	}
	if err := pool.InitializePool(connectionString); err != nil {
		return nil, fmt.Errorf("failed to initialize pool for URI %q: %v", connectionString, err)
	}

	mm.pools[connectionString] = pool
	return pool, nil
}

// ExecuteWithURI 使用指定数据库 URI 执行数据库操作。
func (mm *MultiPoolManager) ExecuteWithURI(ctx context.Context, connectionString string, fn func(*sql.DB) error) error {
	pool, err := mm.getOrCreatePool(connectionString)
	if err != nil {
		return err
	}
	return pool.ExecuteWithConnection(ctx, fn)
}

// CloseAll 关闭所有已创建的连接池。
func (mm *MultiPoolManager) CloseAll() {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	for key, pool := range mm.pools {
		if pool != nil {
			_ = pool.Close()
		}
		delete(mm.pools, key)
	}
}
