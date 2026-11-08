# Go-Utils

[![Go Version](https://img.shields.io/badge/Go-1.23%2B-00ADD8?style=flat&logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

通用、模块化的 Go 语言工具组件库，提供多种可复用的功能模块，简化日常开发工作，提高代码复用率。

---

## 特性

- **模块化设计**：每个模块独立封装，按需引入
- **配置方式**：支持 Functional Options 和 Options 结构体，按组件选择
- **接口抽象**：清晰的接口定义，便于扩展和测试
- **健康检查**：统一的 `HealthChecker` 接口，支持服务监控
- **完善文档**：详细的代码注释和使用示例
- **生产可用**：经过实际项目验证，稳定可靠

---

## 安装

```bash
go get github.com/devpety/go-utils/v3@v3.0.0
```

---

## 模块概览

| 模块 | 功能 | 核心依赖 |
|------|------|----------|
| `auth` | JWT 认证（默认 HS256，支持 HMAC、RSA、RSA-PSS、ECDSA、EdDSA） | `golang-jwt/jwt/v5` |
| `crypto` | 密码哈希与验证（bcrypt） | `golang.org/x/crypto` |
| `cache/redis` | Redis 客户端封装 | `redis/go-redis/v9` |
| `config` | YAML 配置加载 | `gopkg.in/yaml.v3` |
| `db/gorm` | GORM 数据库客户端 | `gorm.io/gorm` |
| `dingtalk` | 钉钉机器人消息推送 | 内置 HTTP 客户端 |
| `errors` | 错误包装与堆栈追踪 | `pkg/errors` |
| `filesystem` | 目录创建与权限管理 | 无外部依赖 |
| `http` | HTTP 客户端/服务端 | `gojek/heimdall` |
| `logger` | JSON 日志（Zap）+ 日志轮转 | `uber-go/zap` |
| `mail` | SMTP 邮件发送 | `gopkg.in/gomail.v2` |
| `middleware` | 框架无关的中间件注册、过滤与优先级排序 | 无外部依赖 |
| `netx` | 网络地址提取 | 无外部依赖 |
| `server` | 服务生命周期管理 | 无外部依赖 |
| `timeutil` | 重试机制 + 超时配置 | 无外部依赖 |
| `upload/qiniu` | 七牛云存储操作 | `qiniu/go-sdk/v7` |
| `validator` | 结构体数据验证 | `playground/validator/v10` |

---

## 快速开始

### 导入模块

```go
import (
    "github.com/devpety/go-utils/v3/auth"
    "github.com/devpety/go-utils/v3/cache/redis"
    "github.com/devpety/go-utils/v3/config"
    "github.com/devpety/go-utils/v3/crypto"
    "github.com/devpety/go-utils/v3/db/gorm"
    "github.com/devpety/go-utils/v3/logger"
    "github.com/devpety/go-utils/v3/mail"
    "github.com/devpety/go-utils/v3/timeutil"
    "github.com/devpety/go-utils/v3/validator"
)
```

---

## 核心模块使用指南

以下带有 `package main` 的代码块可分别保存为独立程序。数据库、Redis、HTTP、邮件及云服务示例需要配置对应的外部服务。

### 🔐 JWT 认证（auth）

提供令牌签发与验证，默认使用 HS256。构造签名为 `NewJWT(app, issuer string, key any, opts ...JWTOption) (JWT, error)`，固定唯一算法及密钥，验签不会根据令牌中的 `alg` 自动切换算法。

```go
package main

import (
    "errors"
    "os"
    "time"

    "github.com/devpety/go-utils/v3/auth"
)

func main() {
    tokenService, err := auth.NewJWT(
        "my-app",                 // 应用标识，对应 sub
        "auth-service",           // 签发者标识，对应 iss
        os.Getenv("JWT_SECRET"),   // 默认 HS256，至少 32 字节
        auth.WithJWTAudience("my-api"),
    )
    if err != nil {
        panic(err)
    }

    token, err := tokenService.GenerateToken("user-123", time.Hour)
    if err != nil {
        panic(err)
    }
    claims, err := tokenService.ParseToken(token)
    if err != nil {
        if errors.Is(err, auth.ErrTokenExpired) {
            panic("Token 已过期")
        }
        panic(err)
    }
    println("用户 ID:", claims.ID) // GenerateToken 的 id 对应 jti
}
```

**算法与密钥**：

| 算法常量 | 接受的密钥类型及约束 |
|---|---|
| `auth.HS256` / `auth.HS384` / `auth.HS512` | `string` 或 `[]byte`，分别至少 32 / 48 / 64 字节 |
| `auth.RS256` / `auth.RS384` / `auth.RS512` | `*rsa.PrivateKey` 或 `*rsa.PublicKey`，至少 2048 位 |
| `auth.PS256` / `auth.PS384` / `auth.PS512` | RSA-PSS，密钥要求与 RSA 相同 |
| `auth.ES256` / `auth.ES384` / `auth.ES512` | `*ecdsa.PrivateKey` 或 `*ecdsa.PublicKey`，分别使用 P-256 / P-384 / P-521 |
| `auth.EdDSA` | `ed25519.PrivateKey` 或 `ed25519.PublicKey`，分别为 64 / 32 字节 |

使用 `auth.WithJWTAlgorithm(...)` 显式选择算法。非对称私钥用于签发并自动提取公钥验签；只提供公钥的实例只能验签。

```go
package main

import (
    "crypto/rand"
    "crypto/rsa"
    "time"

    "github.com/devpety/go-utils/v3/auth"
)

func main() {
    // 示例生成临时密钥；实际使用时读取并解析持久化密钥。
    privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
    if err != nil {
        panic(err)
    }
    signer, err := auth.NewJWT("my-app", "auth-service", privateKey,
        auth.WithJWTAlgorithm(auth.RS256), auth.WithJWTAudience("my-api"))
    if err != nil {
        panic(err)
    }
    verifier, err := auth.NewJWT("my-app", "auth-service", &privateKey.PublicKey,
        auth.WithJWTAlgorithm(auth.RS256), auth.WithJWTAudience("my-api"))
    if err != nil {
        panic(err)
    }
    token, err := signer.GenerateToken("user-123", time.Hour)
    if err != nil {
        panic(err)
    }
    claims, err := verifier.ParseToken(token)
    if err != nil {
        panic(err)
    }
    println("用户 ID:", claims.ID)
}
```

组件接受已解析的 Go 密钥对象，不自动读取 PEM 文件。可使用 `crypto/x509` 或 `golang-jwt/jwt/v5` 的 PEM 解析函数加载密钥；构造后不要修改传入的 RSA、ECDSA 密钥对象。

**验证约定**：

- `app`、`issuer` 必填，解析时分别验证 `sub`、`iss`；`exp` 必须存在，并验证过期时间及存在的 `nbf`。
- `WithJWTAudience(...)` 配置允许的接收方，任一匹配即可。签发未传入 audience 时使用该配置；未配置时不校验 audience，签发默认使用用户 ID。
- `GenerateToken` 的有效期必须为正，且按秒精度生成的 `exp` 必须位于未来。
- 每个实例只接受所配置的算法，拒绝 `none`；不同信任域使用独立密钥。

**错误类型**（使用 `errors.Is` 判断，解析错误保留底层错误链）：

- `ErrAppEmpty` / `ErrKeyEmpty`：应用标识 / 签发者标识为空。
- `ErrJWTAlgorithm`：构造时配置了不支持的算法。
- `ErrJWTKey` / `ErrSecretTooShort`：密钥类型、内容或 HMAC 长度无效。
- `ErrSigningKeyUnavailable`：只有公钥时尝试签发。
- `ErrTokenLifetime`：有效期无法生成未来的过期时间。
- `ErrTokenMalformed` / `ErrTokenExpired` / `ErrTokenNotActive` / `ErrTokenInvalid`：令牌格式错误、已过期、尚未生效或签名 / 声明校验失败。

---

### 🔒 密码哈希（crypto）

基于 bcrypt 算法的密码哈希与验证。

```go
package main

import (
    "github.com/devpety/go-utils/v3/crypto"
)

func main() {
    hash, err := crypto.BcryptPasswordHash("mypassword")
    if err != nil {
        panic(err)
    }
    println("哈希值:", hash)

    if crypto.BcryptPasswordVerify("mypassword", hash) {
        println("密码验证成功")
    }

    min, max, defaultCost := crypto.BcryptCostRange()
    println("Cost范围:", min, "-", max, "默认:", defaultCost)
}
```

---

### 📦 Redis 客户端（cache/redis）

封装 go-redis 命令、健康检查和连接管理。`redis.Options` 嵌入原生 `go-redis.Options`，初始化结构体时需要显式填写嵌入字段。

```go
package main

import (
    "context"
    "time"

    "github.com/devpety/go-utils/v3/cache/redis"
    goredis "github.com/redis/go-redis/v9"
)

func main() {
    initCtx, cancelInit := context.WithTimeout(context.Background(), 5*time.Second)
    client, err := redis.NewClient(initCtx, &redis.Options{
        Options: goredis.Options{
            Addr:       "localhost:6379",
            DB:         0,
            PoolSize:   10,
            MaxRetries: -1, // 禁用命令自动重试
        },
    })
    cancelInit()
    if err != nil {
        panic(err)
    }
    defer client.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    if err := client.Set(ctx, "key", "value", 10*time.Minute).Err(); err != nil {
        panic(err)
    }
    value, err := client.Get(ctx, "key").Result()
    if err != nil {
        panic(err)
    }
    println("值:", value)

    keys, err := client.SafeScanKeys(ctx, "user:*", 100)
    if err != nil {
        panic(err)
    }
    println("匹配键数量:", len(keys))
}
```

**初始化与期限约定**：

- `NewClient(ctx, options)` 在同一上下文内完成拨号、认证和 PING 探活；失败时回收已创建连接。
- 构造成功后解除初始化上下文绑定，取消 `initCtx` 不会关闭已交付的客户端；运行期命令及重连使用各次调用的上下文。
- 组件复制 Options，并强制启用 `ContextTimeoutEnabled`，运行期命令遵守调用方的 Deadline。提前 `cancel()` 能否立即打断在途读写仍受底层 go-redis 实现约束。
- `ReadTimeout` / `WriteTimeout` 使用 `time.Duration`，`0` 使用底层默认值；`-1` 仅禁用默认 socket 超时，保留上下文 Deadline。拒绝会跳过期限设置的 `-2`。
- `MaxRetries` 控制命令失败后的重试次数，`0` 使用底层默认 3 次，`-1` 禁用，小于 `-1` 无效。多次客户端构造重试由调用方编排。
- 时长字段类型为 `time.Duration`，禁用开关写作 `-1`，不要写成 `-1*time.Second`。

**核心方法**：

- `Set/Get/Del`：基础键操作，使用返回命令的 `.Err()` 或 `.Result()` 读取执行结果。
- `HSet/HGet/HGetAll`、`LPush/RPush/LPop/RPop`、`SAdd/SRem/SMembers`、`ZAdd/ZRem/ZRange`：哈希、列表、集合和有序集合操作。
- `Pipeline/TxPipeline`：管道 / 事务管道。
- `SafeScanKeys`：通过 SCAN 遍历匹配键，`count` 是单次扫描数量提示，最终结果汇总到内存。
- `HealthCheck(ctx)` / `IsConnected()`：探活及最近记录的连接状态。

---

### 🗄️ 数据库连接（db/gorm）

GORM MySQL 客户端，支持连接池配置、构造期取消和按需 SQL 监控。

```go
package main

import (
    "context"
    "fmt"
    "time"

    utilsgorm "github.com/devpety/go-utils/v3/db/gorm"
    "gorm.io/gorm"
    gormlogger "gorm.io/gorm/logger"
)

func main() {
    initCtx, cancelInit := context.WithTimeout(context.Background(), 5*time.Second)
    client, err := utilsgorm.NewClient(initCtx,
        utilsgorm.WithHost("localhost"),
        utilsgorm.WithPort(3306),
        utilsgorm.WithUserName("root"),
        utilsgorm.WithPassword("password"),
        utilsgorm.WithDbName("test"),
        utilsgorm.WithMaxIdleConn(10),
        utilsgorm.WithMaxOpenConn(100),
        utilsgorm.WithMaxLifeTime(30*time.Minute),
        utilsgorm.WithLogger(gormlogger.Default.LogMode(gormlogger.Info)),
    )
    cancelInit()
    if err != nil {
        panic(err)
    }
    defer client.Close()

    // 仅在超过阈值时展开 SQL 并调用慢查询处理器。
    err = client.DB().Use(&utilsgorm.Plugin{
        SlowThreshold: 200*time.Millisecond,
        OnSlowQuery: func(_ *gorm.DB, sql string, duration time.Duration) {
            fmt.Printf("慢查询: %s，耗时: %s\n", sql, duration)
        },
    })
    if err != nil {
        panic(err)
    }

    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    var users []struct{ ID uint }
    if err := client.DB().WithContext(ctx).Table("users").Find(&users).Error; err != nil {
        panic(err)
    }
    fmt.Println("用户数量:", len(users))
}
```

**初始化约定**：

- 构造签名为 `NewClient(ctx context.Context, opts ...Option) (Client, error)`，调用方传入上下文和可接受的期限。
- `ctx` 覆盖拨号、握手、PING 和 MySQL 版本探测；构造失败时关闭连接池。
- 成功交付后不保留初始化上下文，业务查询通过 `client.DB().WithContext(ctx)` 单独传入上下文。
- 当前仅支持 MySQL。完整 DSN 优先于 Host、Port、UserName、Password、DbName、Charset、ParseTime、Loc 等连接参数；连接池、表名前缀和日志配置仍单独生效。

**配置选项**：

- `WithDsn`：使用完整 MySQL DSN；分离参数时用 `WithHost/WithPort/WithUserName/WithPassword/WithDbName`。
- `WithDriver`：默认 `mysql`，拒绝其他非空驱动名。
- `WithCharset/WithParseTime/WithLoc`：字符集、时间解析及位置配置；`WithParseTime` 接受布尔字符串。
- `WithMaxIdleConn/WithMaxOpenConn`：默认 10 / 100，不接受负数。
- `WithMaxLifeTime`：`time.Duration`，默认 30 分钟，`0` 不限制寿命，不接受负数。
- `WithPrefix`：表名前缀。
- `WithLogger`：构造及运行期间使用的 GORM logger；`client.WithLogger(...)` 只替换已构造客户端的 logger。

**SQL 插件**：

- `client.WithPluginBeforeHandler(before, after)` 注册前后回调并返回注册错误；`after` 签名为 `func(*gorm.DB, string, time.Time)`。
- 需要慢查询阈值时使用 `client.DB().Use(&utilsgorm.Plugin{...})`。两种注册方式使用同一插件名称，选择一种即可。
- 无回调且未启用慢查询处理时不注册 SQL 回调；仅有 `Before` 时不计时、不展开 SQL。
- 配置 `After` 时每次执行后展开 SQL；只配置有效的 `SlowThreshold` 和 `OnSlowQuery` 时，仅慢查询展开 SQL。不要注册非 nil 的空回调函数。

---

### 📝 YAML 配置（config）

YAML 配置文件加载，支持文件路径和字符串两种方式。

```go
package main

import (
    "github.com/devpety/go-utils/v3/config"
)

type AppConfig struct {
    Server struct {
        Port int `yaml:"port"`
    } `yaml:"server"`
    Database struct {
        Host string `yaml:"host"`
        Port int    `yaml:"port"`
    } `yaml:"database"`
}

func main() {
    var cfg AppConfig
    err := config.LoadYaml("config.yaml", &cfg)
    if err != nil {
        panic(err)
    }
    println("端口:", cfg.Server.Port)

    yamlStr := "server:\n  port: 8080"
    var cfg2 AppConfig
    err = config.LoadYamlByString(yamlStr, &cfg2)
}
```

---

### 📊 日志记录（logger）

基于 Zap 的高性能 JSON 日志，支持日志轮转和压缩。

```go
package main

import (
    "github.com/devpety/go-utils/v3/logger"
    "go.uber.org/zap"
)

func main() {
    log, err := logger.NewJSONLogger(
        logger.WithInfoLevel(),
        logger.WithField("app", "my-service"),
        logger.WithField("environment", "production"),
        logger.WithPathFileRotation("/var/log/app.log", logger.PathFileRotationOption{
            MaxSize:    100,  // MB
            MaxBackups: 10,   // 保留文件数
            MaxAge:     30,   // 保留天数
            LocalTime:  true,
            Compress:   true,
        }),
    )
    if err != nil {
        panic(err)
    }
    defer log.Close()

    log.Info("服务启动",
        zap.String("version", "1.0.0"),
        zap.Int("port", 8080))

    log.Error("操作失败",
        zap.String("operation", "query"),
        zap.Error(err))

    logger.SetDebugLevel(log)
    log.Debug("调试信息")
}
```

**日志级别**：
- `WithDebugLevel` - 调试级别
- `WithInfoLevel` - 信息级别（默认）
- `WithWarnLevel` - 警告级别
- `WithErrorLevel` - 错误级别

---

### 📧 邮件发送（mail）

SMTP HTML 邮件发送。下面使用支持 STARTTLS 的 SMTP 端口；`SendTextHtmlWithContext` 的上下文参与拨号，当前实现不将该期限设置为整个 SMTP 会话的读写截止时间。

```go
package main

import (
    "context"
    "time"
    "github.com/devpety/go-utils/v3/mail"
)

func main() {
    m, err := mail.New(
        mail.WithMailHost("smtp.qq.com"),
        mail.WithMailPort(587),
        mail.WithMailUser("your@qq.com"),
        mail.WithMailPass("your-password"),
        mail.WithMailTimeout(30*time.Second),
    )
    if err != nil {
        panic(err)
    }
    defer m.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    err = m.SendTextHtmlWithContext(ctx,
        "测试邮件",
        "<h1>Hello World</h1>",
        []string{"recipient@example.com"})
    if err != nil {
        panic(err)
    }
}
```

---

### ✅ 数据验证（validator）

结构体数据验证，支持中英文错误消息。`validator.New(...)` 返回的公开接口提供 `Validate`，验证失败时返回首个字段错误。

```go
package main

import (
    "fmt"
    "github.com/devpety/go-utils/v3/validator"
)

type User struct {
    Name  string `label:"姓名" validate:"required"`
    Email string `label:"邮箱" validate:"required,email"`
    Age   int    `label:"年龄" validate:"gte=0,lte=150"`
    Phone string `label:"电话" validate:"omitempty,len=11"`
}

func main() {
    v := validator.New(validator.WithLocale("zh"))

    user := User{Name: "", Email: "invalid", Age: 200}

    err := v.Validate(user)
    if err != nil {
        fmt.Println("验证失败:", err.Error())
    }

}
```

**验证规则**：
- `required` - 必填字段
- `email` - 邮箱格式
- `gte/lte` - 数值范围
- `min/max` - 字符串长度
- `omitempty` - 可选字段

---

### 🔄 重试机制（timeutil）

提供指数退避、固定延迟和抖动重试策略。Context 可取消重试等待；操作回调仍需自行使用同一个上下文，才能在执行期间响应取消。

```go
package main

import (
    "context"
    "errors"
    "net"
    "time"

    "github.com/devpety/go-utils/v3/timeutil"
)

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    policy := timeutil.DefaultRetryConfig
    policy.MaxAttempts = 3 // 包含首次尝试
    policy.RetryCondition = func(err error) bool {
        var networkError net.Error
        return errors.As(err, &networkError) && ctx.Err() == nil
    }
    err := timeutil.RetryWithBackoff(ctx, policy, func() error {
        dialer := net.Dialer{}
        connection, err := dialer.DialContext(ctx, "tcp", "localhost:3306")
        if err != nil {
            return err
        }
        return connection.Close()
    })
    if err != nil {
        if timeutil.IsRetryError(err) {
            println("尝试次数:", timeutil.GetRetryAttempts(err))
        }
        panic(err)
    }
}
```

**重试策略**：

- `RetryWithBackoff`：指数退避。
- `RetryWithFixedDelay`：固定间隔。
- `RetryWithJitter`：在退避间隔上增加抖动。

回调签名为 `func() error`，组件不会替调用方回收失败操作创建的连接；需要该行为时在回调内完成清理。

---

### ⏱️ 超时配置（timeutil）

提供超时常量及 Context 创建工具；创建上下文后，仍需将其传给实际的阻塞操作。

```go
package main

import (
    "context"
    "time"

    "github.com/devpety/go-utils/v3/timeutil"
)

func main() {
    ctx, cancel := timeutil.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    ctx2, cancel2 := timeutil.WithConnectTimeout(context.Background())
    defer cancel2()
    ctx3, cancel3 := timeutil.WithQueryTimeout(context.Background())
    defer cancel3()

    deadline, _ := ctx.Deadline()
    connectionDeadline, _ := ctx2.Deadline()
    queryDeadline, _ := ctx3.Deadline()
    println("截止时间:", deadline.String(), connectionDeadline.String(), queryDeadline.String())
}
```

**超时常量**：

- `DefaultConnectTimeout`：5s。
- `DefaultReadTimeout` / `DefaultWriteTimeout`：10s。
- `DefaultQueryTimeout`：15s。
- `DefaultOperationTimeout`：30s。
- `DefaultHealthTimeout`：3s。

---

### 🌐 HTTP 客户端（http）

支持超时、重试策略和自定义底层 HTTP 客户端。

```go
package main

import (
    "io"
    "net/http"
    "strings"
    "time"

    utilshttp "github.com/devpety/go-utils/v3/http"
)

func main() {
    client := utilshttp.NewClient(
        utilshttp.WithClientHTTPTimeout(30*time.Second),
        utilshttp.WithClientRetryCount(3),
    )
    response, err := client.GET("https://api.example.com/data", http.Header{
        "Authorization": []string{"Bearer token"},
    })
    if err != nil {
        panic(err)
    }
    if _, err := io.Copy(io.Discard, response.Body); err != nil {
        response.Body.Close()
        panic(err)
    }
    if err := response.Body.Close(); err != nil {
        panic(err)
    }

    response, err = client.POST("https://api.example.com/create",
        strings.NewReader(`{"name":"example"}`),
        http.Header{"Content-Type": []string{"application/json"}})
    if err != nil {
        panic(err)
    }
    defer response.Body.Close()
    println("状态码:", response.StatusCode)
}
```

`GET/POST/PUT/PATCH/DELETE` 接收 URL、原生 `net/http.Header` 及适用的请求体；这些封装方法没有 Context 参数，按 `WithClientHTTPTimeout` 配置请求期限。`HealthCheck(ctx)` 只检查客户端是否初始化，不会发送网络探测请求。

---

### 🖥️ HTTP 服务端（http）

HTTP 服务器管理，支持 TLS、Endpoint 查询及显式优雅关闭。

```go
package main

import (
    "context"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    utilshttp "github.com/devpety/go-utils/v3/http"
)

func main() {
    mux := http.NewServeMux()
    mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
        w.WriteHeader(http.StatusNoContent)
    })
    srv := utilshttp.NewServer(&http.Server{
        Handler: mux,
        ReadHeaderTimeout: 5*time.Second,
    }, utilshttp.WithServerAddress("127.0.0.1:8080"))
    defer srv.Close()

    endpoint, err := srv.Endpoint()
    if err != nil {
        panic(err)
    }
    println("服务端点:", endpoint.String())

    signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stopSignals()
    results := make(chan error, 1)
    go func() { results <- srv.Start(context.Background()) }()
    select {
    case err := <-results:
        if err != nil {
            panic(err)
        }
        return
    case <-signalCtx.Done():
    }

    shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    if err := srv.Stop(shutdownCtx); err != nil {
        // 排空超时后立即关闭残留连接。
        if closeErr := srv.Close(); closeErr != nil {
            panic(closeErr)
        }
        panic(err)
    }
    if err := <-results; err != nil {
        panic(err)
    }
}
```

**配置与生命周期约定**：

- `NewServer(hs *net/http.Server, opts ...ServerOption)` 接受 nil 服务器并使用空配置；默认网络为 `tcp`。`WithServerAddress` 覆盖 `hs.Addr`，未配置时回退到 `hs.Addr`。
- 默认继承 `hs.TLSConfig`；`WithServerTLSConfig` 显式覆盖，包括传入 nil 禁用 TLS。使用 TLS 时需在有效 TLS 配置中提供证书或证书回调。
- 自动 `Endpoint()` 会提前绑定监听器以解析实际端口（包括端口 `0`），因此调用后即使没有执行 `Start`，也应调用 `Stop` 或 `Close` 释放端口。
- `WithServerEndpoint(*url.URL)` 设置对外公布的地址；它不会改变监听地址，也不会替代 `Start` 创建监听器。`Endpoint()` 返回 URL 副本。
- 同一实例只运行一个生命周期。正在运行时重复 `Start` 返回 `ErrServerAlreadyStarted`；停止后再次 `Start` 返回 nil，不会重新监听，需要重建实例。
- `Start(ctx)` 的上下文用于启动取消及请求基础上下文；启动成功后取消它不会自动调用 `Stop`。优雅排空时使用独立的运行上下文，并显式调用 `Stop(shutdownCtx)`。
- `Stop` 等待请求排空并回收监听器；`Close` 立即关闭连接和监听器。启动过程中发生错误也会释放已创建的监听器。

---

### 🔔 钉钉机器人（dingtalk）

钉钉机器人消息推送，消息内容通过对应结构体的嵌套字段设置。

```go
package main

import (
    "net/http"

    "github.com/devpety/go-utils/v3/dingtalk"
)

func main() {
    robot := dingtalk.NewRobot("your-access-token")
    text := dingtalk.RobotTextMessageType{}
    text.Text.Content = "服务告警：数据库连接异常"
    printResponse(robot.SendTextMessage(text))

    markdown := dingtalk.RobotMarkdownMessageType{}
    markdown.Markdown.Title = "服务状态报告"
    markdown.Markdown.Text = "## 服务状态\n- **状态**: 正常"
    printResponse(robot.SendMarkdownMessage(markdown))

    link := dingtalk.RobotLinkMessageType{}
    link.Link.Title = "监控详情"
    link.Link.Text = "点击查看详细监控数据"
    link.Link.MessageUrl = "https://monitor.example.com"
    link.Link.PicUrl = "https://example.com/pic.png"
    printResponse(robot.SendLinkMessage(link))
}

func printResponse(response *http.Response, err error) {
    if err != nil {
        panic(err)
    }
    defer response.Body.Close()
    println("状态码:", response.StatusCode)
}
```

---

### ☁️ 七牛云存储（upload/qiniu）

七牛云存储操作，支持上传、下载、管理。

```go
package main

import (
    "github.com/devpety/go-utils/v3/upload/qiniu"
)

func main() {
    q := qiniu.New(
        "your-access-key",
        "your-secret-key",
        "your-bucket",
        "huanan",
        nil,
    )

    _, err := q.FormUploaderPutFile("/path/to/file.txt", "file.txt")
    if err != nil {
        panic(err)
    }

    publicURL := q.MakePublicURL("https://cdn.example.com", "file.txt")

    privateURL := q.MakePrivateURL("https://cdn.example.com", "file.txt", 3600)
    println("公开地址:", publicURL, "私有地址:", privateURL)

    info, err := q.GetFileInfo("file.txt")
    if err != nil {
        panic(err)
    }
    println("文件大小:", info.Fsize)

    if err := q.Delete("file.txt"); err != nil {
        panic(err)
    }
}
```

---

### 🌍 网络工具（netx）

网络地址提取和 IP 获取。

```go
package main

import (
    "github.com/devpety/go-utils/v3/netx"
)

func main() {
    addr, err := netx.ExtractAddress("0.0.0.0:8080", nil)
    if err != nil {
        panic(err)
    }
    println("地址:", addr)

    host, port, err := netx.ExtractHostPort("localhost:8080")
    if err != nil {
        panic(err)
    }
    println("主机:", host, "端口:", port)

    ip := netx.GetLocalServerIp()
    println("本机 IP:", ip)
}
```

---

### 📁 文件系统（filesystem）

目录创建与权限管理。

```go
package main

import (
    "github.com/devpety/go-utils/v3/filesystem"
)

func main() {
    err := filesystem.CreateDirectory("/var/log/myapp")
    if err != nil {
        panic(err)
    }

    err = filesystem.CreateDirectoryWithPerm("/tmp/shared", 0777)
    if err != nil {
        panic(err)
    }
}
```

---

### 🔗 中间件链（middleware）

框架无关的中间件注册和排序工具。`Handler` 是通用接口，由接入方约定具体函数类型并组装调用链；当前 API 不提供 `HTTPChain` 或 gRPC 链构造函数。

```go
package main

import (
    "net/http"

    "github.com/devpety/go-utils/v3/middleware"
)

func main() {
    manager := middleware.NewManager()
    manager.UseFunc("logging", middleware.PriorityHigh, loggingMiddleware)
    manager.UseFunc("auth", middleware.PriorityNormal, authMiddleware)

    var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
        w.WriteHeader(http.StatusNoContent)
    })
    handlers := manager.Build()
    // 反向包裹，优先级较高的中间件在请求进入时先执行。
    for i := len(handlers)-1; i >= 0; i-- {
        wrap := handlers[i].(func(http.Handler) http.Handler)
        handler = wrap(handler)
    }
    if err := http.ListenAndServe(":8080", handler); err != nil {
        panic(err)
    }
}

func loggingMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        println("请求:", r.URL.Path)
        next.ServeHTTP(w, r)
    })
}

func authMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Header.Get("Authorization") == "" {
            http.Error(w, "Unauthorized", http.StatusUnauthorized)
            return
        }
        next.ServeHTTP(w, r)
    })
}
```

- `Middleware` 需要实现 `Name()`、`Priority()`、`Handler()`；可选实现 `Config.Enabled()`，禁用项在 `Use` 注册时跳过。
- `Build()` 按优先级数值从小到大排序；预置优先级为 `PriorityHighest`、`PriorityHigh`、`PriorityNormal`、`PriorityLow`。相同优先级的先后顺序没有稳定性保证。
- `List/Count/Get/Has/Remove/Clear` 用于查询或调整注册项；Manager 用于启动阶段配置，不提供并发同步。

---

### 🛡️ 优雅关闭（server/system）

服务优雅关闭，支持信号处理。

```go
package main

import (
    "syscall"

    "github.com/devpety/go-utils/v3/server/system"
)

func main() {
    hook := system.NewShutdown()

    hook.WithSignals(syscall.SIGINT, syscall.SIGTERM)

    hook.Close(
        func() { println("关闭数据库连接") },
        func() { println("关闭 Redis 连接") },
        func() { println("关闭日志文件") },
    )
}
```

---

### ⚠️ 错误处理（errors）

错误包装与堆栈追踪。

```go
package main

import (
    "github.com/devpety/go-utils/v3/errors"
)

func main() {
    err := errors.New("基础错误")

    err = errors.Wrap(err, "附加信息")

    err = errors.Errorf("格式化错误: %s", "detail")

    err = errors.WithStack(err)

    println(err.Error())
}
```

---

## 设计原则

### 选项模式（Functional Options）

JWT、GORM、HTTP、日志、邮件等组件使用选项模式；Redis 使用嵌入原生 Options 的结构体配置。以下为 GORM 的配置片段：

```go
client, err := gorm.NewClient(ctx,
    gorm.WithDsn(dsn),
    gorm.WithMaxOpenConn(100),
)
```

### 接口抽象

每个模块定义清晰的接口，便于扩展和模拟测试：

```go
type Client interface {
    DoSomething() error
}

var _ Client = (*client)(nil)
```

### 健康检查接口

Redis、GORM 和 HTTP 客户端提供统一的健康检查方法。Redis、GORM 会执行 PING；HTTP 客户端只检查初始化状态。`IsConnected()` 返回内部记录的状态，不执行实时网络探测。根包 `health.go` 定义：

```go
type HealthChecker interface {
    HealthCheck(ctx context.Context) error
    IsConnected() bool
}
```

---

## 项目结构

```
go-utils/
├── auth/               # JWT 认证
│   ├── jwt.go          # JWT 实现
│   ├── jwt_key.go      # 算法及密钥校验
│   └── jwt_test.go     # 测试
├── cache/
│   └── redis/          # Redis 客户端
│       ├── client.go   # 客户端实现
│       ├── initialization.go # 构造期取消及清理
│       └── cmd/        # 命令封装
├── config/             # YAML 配置
│   └── yaml.go
├── crypto/             # 密码哈希
│   └── bcrypt.go
├── db/
│   └── gorm/           # GORM 客户端
│       ├── client.go
│       ├── option.go   # 配置选项
│       └── plugin.go   # SQL 监控插件
├── dingtalk/           # 钉钉机器人
│   ├── robot.go
│   └── robot_messagetype.go
├── errors/             # 错误处理
│   └── error.go
├── filesystem/         # 文件系统
│   └── directory.go
├── http/               # HTTP 组件
│   ├── client.go       # HTTP 客户端
│   └── server.go       # HTTP 服务端
├── logger/             # 日志模块
│   ├── logger.go
│   ├── option.go       # 配置选项
│   └── meta.go
├── mail/               # 邮件模块
│   └── gomail.go
├── middleware/         # 中间件注册及排序
│   ├── middleware.go
│   ├── manager.go
│   └── priority.go
├── netx/               # 网络工具
│   └── host.go
├── server/             # 服务管理
│   ├── endpoint.go
│   └── system/
│       └── shutdown.go
├── timeutil/           # 时间工具
│   ├── retry.go        # 重试机制
│   └── timeout.go      # 超时配置
├── upload/
│   └── qiniu/          # 七牛云
│       └── qiniu.go
├── validator/          # 数据验证
│   └ validator.go
├── health.go           # 健康检查接口
├── go.mod
├── go.sum
└── README.md
```

---

## 安全建议

1. **JWT 密钥**：按所选算法使用足够强度的密钥；HMAC 最少 32 / 48 / 64 字节，RSA 最少 2048 位，ECDSA 使用匹配曲线。
2. **敏感配置**：密码等敏感信息通过环境变量传递，不要硬编码
3. **文件权限**：日志文件权限 `0640`，目录权限 `0750`
4. **超时控制**：Redis、GORM 构造与运行操作分别传入上下文；HTTP 客户端配置请求超时，HTTP 服务端显式调用 `Stop` 排空。
5. **错误处理**：使用 `errors.Is()` 判断具体错误类型

---

## 测试

```bash
go test ./...

go test ./auth/...
go test ./db/gorm/...

go test -cover ./...

go test -bench=. ./timeutil/...
```

---

## 依赖版本

以下版本对应本仓库 `go.mod` 声明；接入方的实际选用版本由其模块依赖图决定。

| 依赖 | 版本 |
|------|------|
| gorm.io/gorm | v1.25.2-0.20230530020048-26663ab9bf55 |
| github.com/redis/go-redis/v9 | v9.0.5 |
| go.uber.org/zap | v1.27.0 |
| github.com/golang-jwt/jwt/v5 | v5.3.1 |
| github.com/go-playground/validator/v10 | v10.26.0 |
| github.com/qiniu/go-sdk/v7 | v7.25.6 |
| github.com/gojek/heimdall/v7 | v7.0.2 |
| gopkg.in/gomail.v2 | v2.0.0-20160411212932-81ebce5c23df |
| golang.org/x/crypto | v0.39.0 |
| github.com/pkg/errors | v0.9.1 |
| gopkg.in/yaml.v3 | v3.0.1 |

---

## 许可证

MIT License

---

## 贡献指南

1. Fork 本仓库
2. 创建特性分支 (`git checkout -b feature/amazing-feature`)
3. 提交更改 (`git commit -m 'Add amazing feature'`)
4. 推送到分支 (`git push origin feature/amazing-feature`)
5. 创建 Pull Request
