// Package gorm 提供 GORM 数据库连接管理功能，支持插件扩展。
// 功能特性：
//   - MySQL 数据库连接管理
//   - 连接池配置（空闲连接、最大连接、生命周期）
//   - 慢查询监控插件
//   - SQL 执行追踪能力
//   - 表名前缀配置
//   - 健康检查支持
package gorm

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync/atomic"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// 编译期接口验证，确保 client 实现了 Client 接口
var _ Client = (*client)(nil)

// 定义输入验证错误
var (
	// ErrHostEmpty 主机地址为空
	ErrHostEmpty = fmt.Errorf("database host address cannot be empty")

	// ErrPortInvalid 端口无效
	ErrPortInvalid = fmt.Errorf("database port must be in range 1-65535")

	// ErrUserNameEmpty 用户名为空
	ErrUserNameEmpty = fmt.Errorf("database username cannot be empty")

	// ErrDbNameEmpty 数据库名为空
	ErrDbNameEmpty = fmt.Errorf("database name cannot be empty")

	// ErrDsnEmpty DSN为空
	ErrDsnEmpty = fmt.Errorf("data source name (DSN) cannot be empty")
)

// Client 定义数据库客户端接口
// 使用场景：
//   - 数据库 CRUD 操作
//   - 连接池管理
//   - SQL 执行监控
//   - 插件扩展
//   - 健康检查
type Client interface {
	// Options 获取配置选项
	// 返回值：当前客户端的配置信息
	Options() *option

	// DB 获取 GORM 数据库实例
	// 返回值：GORM DB 对象，用于执行数据库操作
	DB() *gorm.DB

	// SqlDB 获取原生 SQL 数据库实例
	// 返回值：标准库 sql.DB 对象，用于底层操作
	SqlDB() *sql.DB

	// Close 关闭数据库连接，释放资源
	// 使用场景：应用退出时调用，确保连接正确关闭
	// 返回值：关闭错误信息
	Close() error

	// WithLogger 设置 GORM 日志记录器
	// 参数：logger - GORM 日志接口实现
	WithLogger(logger logger.Interface)

	// WithPluginBeforeHandler 注册 SQL 执行前后回调插件
	// 功能说明：
	//   - before: SQL 执行前回调（可用于记录开始时间）
	//   - after: SQL 执行后回调（可用于记录执行时长、慢查询告警）
	// 参数：
	//   - before: SQL 执行前回调函数
	//   - after: SQL 执行后回调函数（包含 SQL 语句和开始时间）
	// 返回值：插件注册错误信息
	WithPluginBeforeHandler(before func(db *gorm.DB), after func(db *gorm.DB, sql string, ts time.Time)) error

	// HealthCheck 执行数据库健康检查
	// 功能说明：
	//   - 检查数据库连接是否正常
	//   - 使用 Ping 方法验证连接
	//   - 支持超时控制
	//
	// 参数：
	//   - ctx: 上下文，用于超时控制
	//
	// 返回值：
	//   - nil: 数据库连接健康
	//   - error: 连接异常时的错误信息
	//
	// 使用示例：
	//   ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	//   defer cancel()
	//   if err := client.HealthCheck(ctx); err != nil {
	//       log.Printf("数据库连接异常: %v", err)
	//   }
	HealthCheck(ctx context.Context) error

	// IsConnected 检查数据库连接状态
	// 功能说明：
	//   - 快速检查连接状态（不执行网络请求）
	//   - 返回内部连接状态标志
	//
	// 返回值：
	//   - true: 连接正常
	//   - false: 连接断开
	IsConnected() bool

	// PoolHealthCheck 执行连接池健康检查
	// 功能说明：
	//   - 检查连接池使用率和等待次数
	//   - 当使用率超过阈值或等待次数过多时返回错误
	//   - 用于监控和告警
	//
	// 参数：
	//   - usageRateThreshold: 连接池使用率阈值（如0.8表示80%）
	//   - waitCountThreshold: 等待次数阈值（如100）
	//
	// 返回值：
	//   - nil: 连接池健康
	//   - error: 连接池异常时的错误信息
	//
	// 使用示例：
	//   err := client.PoolHealthCheck(0.8, 100)
	//   if err != nil {
	//       log.Printf("连接池告警: %v", err)
	//   }
	PoolHealthCheck(usageRateThreshold float64, waitCountThreshold int64) error
}

// client 数据库客户端实现
// 字段说明：
//   - option: 配置选项
//   - db: GORM 数据库实例
//   - sqlDb: 原生 SQL 数据库实例
//   - connected: 连接状态标志
type client struct {
	*option            // 内嵌配置选项
	db        *gorm.DB // GORM 数据库实例
	sqlDb     *sql.DB  // 原生 SQL 数据库实例
	connected atomic.Bool
}

type PoolStatistics struct {
	OpenConnections   int   `json:"open_connections"`
	InUse             int   `json:"in_use"`
	Idle              int   `json:"idle"`
	WaitCount         int64 `json:"wait_count"`
	WaitDuration      int64 `json:"wait_duration_ms"`
	MaxIdleClosed     int64 `json:"max_idle_closed"`
	MaxIdleTimeClosed int64 `json:"max_idle_time_closed"`
	MaxLifetimeClosed int64 `json:"max_lifetime_closed"`
}

// NewClient 创建数据库客户端实例
// 功能说明：
//   - 创建配置好的数据库连接客户端
//   - 自动设置连接池参数（默认值或自定义）
//   - 支持 DSN 或分离参数两种配置方式
//   - 支持表名前缀配置
//   - 添加输入参数验证
//
// 参数：
//   - opts: 配置选项列表（使用选项模式）
//
// 返回值：
//   - Client: 数据库客户端实例
//   - error: 连接失败或参数验证失败时的错误信息
//
// 默认配置：
//   - 最大空闲连接数：10
//   - 最大打开连接数：100
//   - 连接生命周期：30 分钟
//   - 字符集：utf8mb4
//   - 时间解析：True
//   - 时区：Local
//
// 使用示例：
//
//	// 方式1：使用分离参数配置
//	client, err := gorm.NewClient(context.Background(),
//	    gorm.WithHost("localhost"),
//	    gorm.WithPort(3306),
//	    gorm.WithUserName("root"),
//	    gorm.WithPassword("password"),
//	    gorm.WithDbName("test"),
//	)
//
//	// 方式2：使用 DSN 配置
//	client, err := gorm.NewClient(context.Background(),
//	    gorm.WithDsn("root:password@tcp(localhost:3306)/test?charset=utf8mb4"),
//	)
//
//	// 方式3：自定义连接池配置
//	client, err := gorm.NewClient(context.Background(),
//	    gorm.WithHost("localhost"),
//	    gorm.WithPort(3306),
//	    gorm.WithUserName("root"),
//	    gorm.WithPassword("password"),
//	    gorm.WithDbName("test"),
//	    gorm.WithMaxIdleConn(20),      // 自定义空闲连接数
//	    gorm.WithMaxOpenConn(200),     // 自定义最大连接数
//	    gorm.WithMaxLifeTime(60 * time.Minute), // 自定义生命周期
//	)
//
// ctx 约束整个构造过程；失败时回收连接池，成功后业务操作不继承该上下文。
func NewClient(ctx context.Context, opts ...Option) (result Client, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	o := &option{
		Driver: "mysql", MaxIdleConn: 10, MaxOpenConn: 100,
		MaxLifeTime: 30 * time.Minute, Charset: "utf8mb4", ParseTime: "true", Loc: "Local",
	}
	for _, opt := range opts {
		opt(o)
	}
	if o.Driver != "" && o.Driver != "mysql" {
		return nil, fmt.Errorf("unsupported database driver: %s", o.Driver)
	}
	if o.MaxIdleConn < 0 || o.MaxOpenConn < 0 || o.MaxLifeTime < 0 {
		return nil, fmt.Errorf("database pool limits and lifetime cannot be negative")
	}

	configuration, err := mysqlConfiguration(o)
	if err != nil {
		return nil, err
	}

	connector, err := mysqldriver.NewConnector(configuration)
	if err != nil {
		return nil, fmt.Errorf("invalid database configuration: %w", err)
	}

	o.Dsn = configuration.FormatDSN()
	pool := sql.OpenDB(connector)
	defer func() {
		if err != nil {
			err = errors.Join(err, ctx.Err(), pool.Close())
		}
	}()
	pool.SetMaxIdleConns(o.MaxIdleConn)
	pool.SetMaxOpenConns(o.MaxOpenConn)
	pool.SetConnMaxLifetime(o.MaxLifeTime)
	if err := pool.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("database ping failed: %w", err)
	}

	// MySQL 方言的版本探测使用背景上下文，仅构造期间将它绑定到调用方期限。
	initializing := &initializationPool{DB: pool, ctx: ctx}
	dialector := &mysql.Dialector{Config: &mysql.Config{Conn: initializing, DSNConfig: configuration}}
	connection, err := gorm.Open(dialector, &gorm.Config{
		DisableAutomaticPing: true,
		NamingStrategy:       schema.NamingStrategy{TablePrefix: o.Prefix, SingularTable: true},
		Logger:               o.Logger,
	})
	if err != nil {
		return nil, fmt.Errorf("database initialization failed: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// 清除初始化上下文，包含 GORM 内部缓存可能保留的连接池引用。
	initializing.ctx = nil
	connection.ConnPool = pool
	connection.Statement.ConnPool = pool
	dialector.Conn = pool
	c := &client{option: o, db: connection, sqlDb: pool}
	c.connected.Store(true)
	return c, nil
}

// initializationPool 只在构造期间替换版本探测的上下文。
type initializationPool struct {
	*sql.DB
	ctx context.Context
}

// QueryRowContext 执行查询并返回单行结果，根据上下文替换连接。
func (pool *initializationPool) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	if pool.ctx != nil {
		ctx = pool.ctx
	}
	return pool.DB.QueryRowContext(ctx, query, args...)
}

// GetDBConn 获取数据库连接，根据上下文替换。
func (pool *initializationPool) GetDBConn() (*sql.DB, error) { return pool.DB, nil }

// mysqlConfiguration 配置 MySQL 数据库连接
func mysqlConfiguration(o *option) (*mysqldriver.Config, error) {
	if o.Dsn != "" {
		configuration, err := mysqldriver.ParseDSN(o.Dsn)
		if err != nil {
			return nil, fmt.Errorf("invalid database DSN: %w", err)
		}
		return configuration, nil
	}
	if o.Host == "" {
		return nil, ErrHostEmpty
	}
	if o.Port < 1 || o.Port > 65535 {
		return nil, ErrPortInvalid
	}
	if o.UserName == "" {
		return nil, ErrUserNameEmpty
	}
	if o.DbName == "" {
		return nil, ErrDbNameEmpty
	}
	configuration := mysqldriver.NewConfig()
	configuration.User, configuration.Passwd, configuration.DBName = o.UserName, o.Password, o.DbName
	configuration.Net = "tcp"
	configuration.Addr = net.JoinHostPort(o.Host, strconv.Itoa(o.Port))
	configuration.ParseTime = true
	if o.ParseTime != "" {
		parseTime, err := strconv.ParseBool(o.ParseTime)
		if err != nil {
			return nil, fmt.Errorf("invalid parse_time: %w", err)
		}
		configuration.ParseTime = parseTime
	}
	if o.Loc != "" {
		location, err := time.LoadLocation(o.Loc)
		if err != nil {
			return nil, fmt.Errorf("invalid database location: %w", err)
		}
		configuration.Loc = location
	}
	charset := o.Charset
	if charset == "" {
		charset = "utf8mb4"
	}
	
	configuration.Params = map[string]string{"charset": charset}
	return configuration, nil
}

// Options 获取配置选项
// 返回值：当前客户端的配置信息
func (c *client) Options() *option {
	return c.option
}

// DB 获取 GORM 数据库实例
// 返回值：GORM DB 对象，用于执行数据库操作
// 使用示例：
//
//	db := client.DB()
//	db.Find(&users) // 查询所有用户
func (c *client) DB() *gorm.DB {
	return c.db
}

// SqlDB 获取原生 SQL 数据库实例
// 返回值：标准库 sql.DB 对象，用于底层操作
// 使用场景：
//   - 执行原生 SQL 查询
//   - 获取连接池统计信息
//   - 数据库健康检查
func (c *client) SqlDB() *sql.DB {
	return c.sqlDb
}

// Close 关闭数据库连接，释放资源
// 使用场景：应用退出时调用，确保连接正确关闭
// 返回值：
//   - nil: 关闭成功
//   - error: 关闭失败时的错误信息
func (c *client) Close() error {
	if c.sqlDb != nil {
		err := c.sqlDb.Close()
		if err != nil {
			return err
		}
		c.connected.Store(false)
	}
	return nil
}

func (c *client) Stats() PoolStatistics {
	if c.sqlDb == nil {
		return PoolStatistics{}
	}
	stats := c.sqlDb.Stats()
	return PoolStatistics{
		OpenConnections:   stats.OpenConnections,
		InUse:             stats.InUse,
		Idle:              stats.Idle,
		WaitCount:         stats.WaitCount,
		WaitDuration:      stats.WaitDuration.Milliseconds(),
		MaxIdleClosed:     stats.MaxIdleClosed,
		MaxIdleTimeClosed: stats.MaxIdleTimeClosed,
		MaxLifetimeClosed: stats.MaxLifetimeClosed,
	}
}

func (c *client) SetMaxOpenConns(n int) {
	if c.sqlDb != nil {
		c.sqlDb.SetMaxOpenConns(n)
	}
}

func (c *client) SetMaxIdleConns(n int) {
	if c.sqlDb != nil {
		c.sqlDb.SetMaxIdleConns(n)
	}
}

func (c *client) SetConnMaxLifetime(d time.Duration) {
	if c.sqlDb != nil {
		c.sqlDb.SetConnMaxLifetime(d)
	}
}

func (c *client) SetConnMaxIdleTime(d time.Duration) {
	if c.sqlDb != nil {
		c.sqlDb.SetConnMaxIdleTime(d)
	}
}

// WithLogger 设置 GORM 日志记录器
// 功能说明：
//   - 配置 GORM 的日志输出
//   - 可用于记录 SQL 执行日志、错误日志等
//
// 参数：
//   - logger: GORM 日志接口实现
func (c *client) WithLogger(logger logger.Interface) {
	c.db.Logger = logger
}

// WithPluginBeforeHandler 注册 SQL 执行前后回调插件
// 功能说明：
//   - 注册 SQL 执行监控插件
//   - 支持自定义 SQL 执行前后的回调函数
//   - 可用于慢查询监控、SQL 日志记录等
//
// 参数：
//   - before: SQL 执行前回调函数
//     参数：db - GORM DB 实例，可获取 SQL 信息
//   - after: SQL 执行后回调函数
//     参数：
//   - db: GORM DB 实例
//   - sql: 完整的 SQL 语句（已解析参数）
//   - ts: SQL 执行开始时间
//
// 返回值：
//   - nil: 插件注册成功
//   - error: 插件注册失败时的错误信息
//
// 使用示例：
//
//	// 添加慢查询监控
//	err = client.WithPluginBeforeHandler(
//	    nil, // 执行前回调（可选）
//	    func(db *gorm.DB, sql string, ts time.Time) {
//	        duration := time.Since(ts)
//	        if duration > 200 * time.Millisecond {
//	            log.Printf("慢查询告警: SQL=%s, 耗时=%v", sql, duration)
//	        }
//	    },
//	)
func (c *client) WithPluginBeforeHandler(before func(db *gorm.DB), after func(db *gorm.DB, sql string, ts time.Time)) error {
	// 插件处理 - 必须显式指定字段名，避免字段顺序不匹配
	return c.db.Use(&Plugin{
		Before: before,
		After:  after,
	})
}

// HealthCheck 执行数据库健康检查
// 功能说明：
//   - 使用 Ping 方法验证数据库连接是否正常
//   - 支持超时控制（通过 context）
//   - 更新内部连接状态
//
// 参数：
//   - ctx: 上下文，用于超时控制
//
// 返回值：
//   - nil: 数据库连接健康
//   - error: 连接异常时的错误信息
//
// 使用示例：
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//	err := client.HealthCheck(ctx)
//	if err != nil {
//	    log.Printf("数据库健康检查失败: %v", err)
//	}
func (c *client) HealthCheck(ctx context.Context) error {
	if c.sqlDb == nil {
		c.connected.Store(false)
		return fmt.Errorf("database connection not initialized")
	}

	err := c.sqlDb.PingContext(ctx)
	if err != nil {
		c.connected.Store(false)
		return fmt.Errorf("database health check failed: %w", err)
	}

	c.connected.Store(true)
	return nil
}

// IsConnected 检查数据库连接状态
// 功能说明：
//   - 快速检查内部连接状态标志
//   - 不执行实际网络请求
//   - 用于快速判断是否需要重连
//
// 返回值：
//   - true: 连接正常（上次健康检查成功）
//   - false: 连接断开（上次健康检查失败或未初始化）
//
// 注意：
//   - 此方法返回的状态基于上次健康检查结果
//   - 如需实时检查，请使用 HealthCheck 方法
func (c *client) IsConnected() bool {
	return c.connected.Load()
}

func (c *client) PoolHealthCheck(usageRateThreshold float64, waitCountThreshold int64) error {
	stats := c.Stats()

	if stats.OpenConnections == 0 {
		return fmt.Errorf("connection pool not initialized")
	}

	usageRate := float64(stats.InUse) / float64(stats.OpenConnections)
	if usageRate > usageRateThreshold {
		return fmt.Errorf("connection pool usage rate too high: %.2f%% (threshold: %.2f%%)", usageRate*100, usageRateThreshold*100)
	}

	if stats.WaitCount > waitCountThreshold {
		return fmt.Errorf("connection pool wait count too high: %d (threshold: %d)", stats.WaitCount, waitCountThreshold)
	}

	return nil
}
