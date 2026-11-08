// Package auth 提供使用显式算法与密钥策略的 JWT 签发和验证。
package auth

import (
	"errors"
	"fmt"
	"slices"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
)

var (
	// ErrTokenMalformed 表示令牌格式错误。
	ErrTokenMalformed = errors.New("token is malformed")
	// ErrTokenExpired 表示令牌已过期。
	ErrTokenExpired = errors.New("token is expired")
	// ErrTokenNotActive 表示令牌尚未生效。
	ErrTokenNotActive = errors.New("token not active yet")
	// ErrTokenInvalid 表示签名或声明不符合配置。
	ErrTokenInvalid = errors.New("token is invalid")
	// ErrAppEmpty 表示应用名称为空。
	ErrAppEmpty = errors.New("app name cannot be empty")
	// ErrKeyEmpty 表示签发者标识为空。
	ErrKeyEmpty = errors.New("issuer cannot be empty")
	// ErrSecretTooShort 表示 HMAC 密钥短于所选算法的哈希长度。
	ErrSecretTooShort = errors.New("HMAC secret is shorter than the algorithm hash size")
	// ErrJWTAlgorithm 表示配置了不支持的算法。
	ErrJWTAlgorithm = errors.New("unsupported JWT algorithm")
	// ErrJWTKey 表示密钥类型、长度或内容无效。
	ErrJWTKey = errors.New("invalid JWT key")
	// ErrSigningKeyUnavailable 表示当前实例只有验签公钥。
	ErrSigningKeyUnavailable = errors.New("JWT signing key is unavailable")
	// ErrTokenLifetime 表示令牌有效时长不能生成未来的过期时间。
	ErrTokenLifetime = errors.New("token lifetime must produce a future expiration")
)

// JWTAlgorithm 表示构造时显式选择的 JWT 签名算法。
type JWTAlgorithm string

// 支持的算法；默认使用 HS256，不接受未签名的令牌。
const (
	HS256 JWTAlgorithm = "HS256"
	HS384 JWTAlgorithm = "HS384"
	HS512 JWTAlgorithm = "HS512"
	RS256 JWTAlgorithm = "RS256"
	RS384 JWTAlgorithm = "RS384"
	RS512 JWTAlgorithm = "RS512"
	PS256 JWTAlgorithm = "PS256"
	PS384 JWTAlgorithm = "PS384"
	PS512 JWTAlgorithm = "PS512"
	ES256 JWTAlgorithm = "ES256"
	ES384 JWTAlgorithm = "ES384"
	ES512 JWTAlgorithm = "ES512"
	EdDSA JWTAlgorithm = "EdDSA"
)

// JWT 提供令牌签发与验证，解析严格使用构造时配置的算法及身份约束。
type JWT interface {
	GenerateToken(id string, expireDuration time.Duration, audience ...string) (string, error)
	ParseToken(tokenString string) (*jwtClaims, error)
}

// JWTOption 配置算法或接收方策略。
type JWTOption func(*jwtOptions)

type jwtOptions struct {
	algorithm JWTAlgorithm
	audience  []string
}

// WithJWTAlgorithm 设置签发和验签使用的唯一算法。
func WithJWTAlgorithm(algorithm JWTAlgorithm) JWTOption {
	return func(options *jwtOptions) { options.algorithm = algorithm }
}

// WithJWTAudience 设置验签允许的接收方，任一匹配即可；未显式签发 audience 时也使用该列表。
// 不配置时不约束接收方，签发仍默认使用用户 ID。
func WithJWTAudience(audience ...string) JWTOption {
	copied := slices.Clone(audience)
	return func(options *jwtOptions) { options.audience = slices.Clone(copied) }
}

type jwt struct {
	app             string
	issuer          string
	method          gojwt.SigningMethod
	signingKey      any
	verificationKey any
	audience        []string
	parser          *gojwt.Parser
}

type jwtClaims struct {
	gojwt.RegisteredClaims
}

var _ JWT = (*jwt)(nil)

// NewJWT 根据算法校验密钥，默认 HS256。
// HMAC 接受 string 或 []byte；RSA、ECDSA 接受标准库私钥或公钥，EdDSA 接受 ed25519 密钥。
// 私钥用于签发并自动提取公钥验签；只提供公钥时，GenerateToken 返回 ErrSigningKeyUnavailable。
// 不同信任域应使用独立密钥，调用方不应在构造后修改传入的非对称密钥。
func NewJWT(app, issuer string, key any, opts ...JWTOption) (JWT, error) {
	if app == "" {
		return nil, ErrAppEmpty
	}
	if issuer == "" {
		return nil, ErrKeyEmpty
	}

	options := jwtOptions{algorithm: HS256}
	for _, opt := range opts {
		if opt == nil {
			return nil, fmt.Errorf("%w: nil option", ErrJWTAlgorithm)
		}
		opt(&options)
	}

	for _, audience := range options.audience {
		if audience == "" {
			return nil, fmt.Errorf("%w: audience cannot be empty", ErrTokenInvalid)
		}
	}

	method, signingKey, verificationKey, err := jwtKeys(options.algorithm, key)
	if err != nil {
		return nil, err
	}

	parserOptions := []gojwt.ParserOption{
		gojwt.WithValidMethods([]string{method.Alg()}),
		gojwt.WithIssuer(issuer), gojwt.WithSubject(app), gojwt.WithExpirationRequired(),
	}

	if len(options.audience) > 0 {
		parserOptions = append(parserOptions, gojwt.WithAudience(options.audience...))
	}

	return &jwt{app: app, issuer: issuer, method: method, signingKey: signingKey,
		verificationKey: verificationKey, audience: options.audience, parser: gojwt.NewParser(parserOptions...)}, nil
}

// GenerateToken 使用配置的算法签发令牌；过期时间必须位于未来。
func (t *jwt) GenerateToken(id string, expireDuration time.Duration, audience ...string) (string, error) {
	if t.signingKey == nil {
		return "", ErrSigningKeyUnavailable
	}

	ts := time.Now()
	expiration := gojwt.NewNumericDate(ts.Add(expireDuration))
	if expireDuration <= 0 || !expiration.After(ts) {
		return "", ErrTokenLifetime
	}

	if len(audience) == 0 {
		audience = t.audience
		if len(audience) == 0 {
			audience = []string{id}
		}
	}

	claims := jwtClaims{gojwt.RegisteredClaims{
		Issuer: t.issuer, Subject: t.app, NotBefore: gojwt.NewNumericDate(ts), IssuedAt: gojwt.NewNumericDate(ts),
		ExpiresAt: expiration, ID: id, Audience: slices.Clone(audience),
	}}

	return gojwt.NewWithClaims(t.method, claims).SignedString(t.signingKey)
}

// ParseToken 校验配置的算法、签名、签发者、应用、生效与过期时间及可选接收方。
func (t *jwt) ParseToken(tokenString string) (*jwtClaims, error) {
	token, err := t.parser.ParseWithClaims(tokenString, &jwtClaims{}, func(token *gojwt.Token) (any, error) {
		if token.Method.Alg() != t.method.Alg() {
			return nil, ErrJWTAlgorithm
		}
		return t.verificationKey, nil
	})

	if err != nil {
		classification := ErrTokenInvalid
		switch {
		case errors.Is(err, gojwt.ErrTokenMalformed):
			classification = ErrTokenMalformed
		case errors.Is(err, gojwt.ErrTokenExpired):
			classification = ErrTokenExpired
		case errors.Is(err, gojwt.ErrTokenNotValidYet):
			classification = ErrTokenNotActive
		}
		return nil, errors.Join(classification, err)
	}

	if token != nil && token.Valid {
		if claims, ok := token.Claims.(*jwtClaims); ok {
			return claims, nil
		}
	}
	
	return nil, ErrTokenInvalid
}
