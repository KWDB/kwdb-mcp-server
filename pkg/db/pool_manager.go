package db

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver
)

// PoolManager manages database connection pool
type PoolManager struct {
	mu               sync.RWMutex
	db               *sql.DB
	connectionString string
	initialized      bool
	config           PoolConfig
}

// PoolConfig represents connection pool configuration
type PoolConfig struct {
	MaxOpenConns    int           // Maximum number of open connections
	MaxIdleConns    int           // Maximum number of idle connections
	ConnMaxLifetime time.Duration // Maximum lifetime of connections
	ConnMaxIdleTime time.Duration // Maximum idle time of connections
}

// DefaultPoolConfig provides default connection pool configuration
var DefaultPoolConfig = PoolConfig{
	MaxOpenConns:    25,               // Maximum 25 connections
	MaxIdleConns:    5,                // Maximum 5 idle connections
	ConnMaxLifetime: 5 * time.Minute,  // Connection lives for max 5 minutes
	ConnMaxIdleTime: 30 * time.Second, // Idle connections closed after 30 seconds
}

var globalPoolMgr *PoolManager
var poolMgrOnce sync.Once

// GetPoolManager returns the global connection pool manager
func GetPoolManager() *PoolManager {
	poolMgrOnce.Do(func() {
		globalPoolMgr = &PoolManager{
			config: DefaultPoolConfig,
		}
	})
	return globalPoolMgr
}

// InitializePool initializes connection pool (but doesn't establish actual connections)
func (pm *PoolManager) InitializePool(connectionString string, config ...PoolConfig) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if connectionString == "" {
		return fmt.Errorf("connection string cannot be empty")
	}

	pm.connectionString = connectionString

	// Use provided config or default config
	if len(config) > 0 {
		pm.config = config[0]
	}

	// Create database connection pool with lazy loading
	var err error
	pm.db, err = sql.Open("postgres", pm.connectionString)
	if err != nil {
		return fmt.Errorf("failed to create database pool: %v", err)
	}

	// Configure connection pool parameters
	pm.db.SetMaxOpenConns(pm.config.MaxOpenConns)
	pm.db.SetMaxIdleConns(pm.config.MaxIdleConns)
	pm.db.SetConnMaxLifetime(pm.config.ConnMaxLifetime)
	pm.db.SetConnMaxIdleTime(pm.config.ConnMaxIdleTime)

	pm.initialized = true
	log.Printf("Database connection pool initialized (max_open: %d, max_idle: %d)",
		pm.config.MaxOpenConns, pm.config.MaxIdleConns)

	return nil
}

// GetConnection retrieves database connection from the pool
func (pm *PoolManager) GetConnection(ctx context.Context) (*sql.DB, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if !pm.initialized {
		return nil, fmt.Errorf("database pool not initialized")
	}

	if pm.db == nil {
		return nil, fmt.Errorf("database pool is nil")
	}

	// Use ping with timeout to test connection
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if err := pm.db.PingContext(pingCtx); err != nil {
		// Connection failed, attempt to reinitialize pool
		log.Printf("Database connection test failed, attempting to reinitialize: %v", err)

		if reinitErr := pm.reinitializePool(); reinitErr != nil {
			return nil, fmt.Errorf("failed to reinitialize database pool: %v (original error: %v)", reinitErr, err)
		}

		// Test connection again
		if err := pm.db.PingContext(pingCtx); err != nil {
			return nil, fmt.Errorf("database connection still unavailable after reinitialize: %v", err)
		}
	}

	return pm.db, nil
}

// reinitializePool reinitializes the connection pool
func (pm *PoolManager) reinitializePool() error {
	// Close old connection pool
	if pm.db != nil {
		pm.db.Close()
	}

	// Recreate connection pool
	var err error
	pm.db, err = sql.Open("postgres", pm.connectionString)
	if err != nil {
		return fmt.Errorf("failed to recreate database pool: %v", err)
	}

	// Reconfigure connection pool parameters
	pm.db.SetMaxOpenConns(pm.config.MaxOpenConns)
	pm.db.SetMaxIdleConns(pm.config.MaxIdleConns)
	pm.db.SetConnMaxLifetime(pm.config.ConnMaxLifetime)
	pm.db.SetConnMaxIdleTime(pm.config.ConnMaxIdleTime)

	log.Println("Database connection pool reinitialized")
	return nil
}

// ExecuteWithConnection executes database operations using connection pool
func (pm *PoolManager) ExecuteWithConnection(ctx context.Context, fn func(*sql.DB) error) error {
	db, err := pm.GetConnection(ctx)
	if err != nil {
		return fmt.Errorf("failed to get database connection: %v", err)
	}

	return fn(db)
}

// GetStats returns connection pool statistics
func (pm *PoolManager) GetStats() sql.DBStats {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if pm.db == nil {
		return sql.DBStats{}
	}

	return pm.db.Stats()
}

// Close closes the connection pool
func (pm *PoolManager) Close() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.db != nil {
		err := pm.db.Close()
		pm.db = nil
		pm.initialized = false
		log.Println("Database connection pool closed")
		return err
	}

	return nil
}

// IsInitialized checks if the connection pool is initialized
func (pm *PoolManager) IsInitialized() bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.initialized
}
