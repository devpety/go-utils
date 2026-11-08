package auth

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"fmt"
	"slices"

	gojwt "github.com/golang-jwt/jwt/v5"
)

// jwtKeys 从 JWT 算法和密钥创建签名方法、签名密钥和验证密钥
func jwtKeys(algorithm JWTAlgorithm, key any) (gojwt.SigningMethod, any, any, error) {
	var method gojwt.SigningMethod
	switch algorithm {
	case HS256:
		method = gojwt.SigningMethodHS256
	case HS384:
		method = gojwt.SigningMethodHS384
	case HS512:
		method = gojwt.SigningMethodHS512
	case RS256:
		method = gojwt.SigningMethodRS256
	case RS384:
		method = gojwt.SigningMethodRS384
	case RS512:
		method = gojwt.SigningMethodRS512
	case PS256:
		method = gojwt.SigningMethodPS256
	case PS384:
		method = gojwt.SigningMethodPS384
	case PS512:
		method = gojwt.SigningMethodPS512
	case ES256:
		method = gojwt.SigningMethodES256
	case ES384:
		method = gojwt.SigningMethodES384
	case ES512:
		method = gojwt.SigningMethodES512
	case EdDSA:
		method = gojwt.SigningMethodEdDSA
	default:
		return nil, nil, nil, fmt.Errorf("%w: %q", ErrJWTAlgorithm, algorithm)
	}

	// 处理 HMAC 算法
	if hmac, ok := method.(*gojwt.SigningMethodHMAC); ok {
		var secret []byte
		switch value := key.(type) {
		case string:
			secret = []byte(value)
		case []byte:
			secret = slices.Clone(value)
		default:
			return nil, nil, nil, fmt.Errorf("%w: %s requires a string or []byte secret", ErrJWTKey, algorithm)
		}
		if len(secret) < hmac.Hash.Size() {
			return nil, nil, nil, fmt.Errorf("%w: %s requires at least %d bytes", ErrSecretTooShort, algorithm, hmac.Hash.Size())
		}
		return method, secret, secret, nil
	}

	var signingKey, verificationKey any
	var err error
	switch algorithm {
	// 处理 RSA 算法
	case RS256, RS384, RS512, PS256, PS384, PS512:
		signingKey, verificationKey, err = rsaKeys(key)
	// 处理 ECDSA 算法
	case ES256, ES384, ES512:
		curve := elliptic.P256()
		if algorithm == ES384 {
			curve = elliptic.P384()
		}
		if algorithm == ES512 {
			curve = elliptic.P521()
		}
		signingKey, verificationKey, err = ecdsaKeys(key, curve)
	// 处理 EdDSA 算法
	case EdDSA:
		switch value := key.(type) {
		// 处理 EdDSA 私钥
		case ed25519.PrivateKey:
			if len(value) != ed25519.PrivateKeySize {
				err = ErrJWTKey
				break
			}
			privateKey := ed25519.NewKeyFromSeed(value.Seed())
			if !bytes.Equal(privateKey, value) {
				err = ErrJWTKey
				break
			}
			signingKey, verificationKey = privateKey, privateKey.Public().(ed25519.PublicKey)
		// 处理 EdDSA 公钥
		case ed25519.PublicKey:
			if len(value) != ed25519.PublicKeySize {
				err = ErrJWTKey
				break
			}
			verificationKey = slices.Clone(value)
		default:
			err = ErrJWTKey
		}
	}
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: invalid key for %s: %w", ErrJWTKey, algorithm, err)
	}
	return method, signingKey, verificationKey, nil
}

// rsaKeys 从 RSA 私钥和公钥创建签名密钥和验证密钥
func rsaKeys(key any) (any, any, error) {
	var privateKey *rsa.PrivateKey
	var publicKey *rsa.PublicKey
	switch value := key.(type) {
	case *rsa.PrivateKey:
		if value == nil {
			return nil, nil, ErrJWTKey
		}
		privateKey, publicKey = value, &value.PublicKey
	case *rsa.PublicKey:
		publicKey = value
	default:
		return nil, nil, ErrJWTKey
	}
	if publicKey == nil || publicKey.N == nil || publicKey.N.Sign() <= 0 || publicKey.N.BitLen() < 2048 ||
		publicKey.E < 3 || publicKey.E > 1<<31-1 || publicKey.E%2 == 0 {
		return nil, nil, fmt.Errorf("RSA requires a valid key of at least 2048 bits")
	}
	if privateKey != nil {
		if privateKey.D == nil || privateKey.D.Sign() <= 0 || len(privateKey.Primes) < 2 {
			return nil, nil, ErrJWTKey
		}
		for _, prime := range privateKey.Primes {
			if prime == nil || prime.Sign() <= 0 {
				return nil, nil, ErrJWTKey
			}
		}
		if err := privateKey.Validate(); err != nil {
			return nil, nil, err
		}
		return privateKey, publicKey, nil
	}
	return nil, publicKey, nil
}

// ecdsaKeys 从 ECDSA 私钥和公钥创建签名密钥和验证密钥
func ecdsaKeys(key any, curve elliptic.Curve) (any, any, error) {
	var privateKey *ecdsa.PrivateKey
	var publicKey *ecdsa.PublicKey
	switch value := key.(type) {
	case *ecdsa.PrivateKey:
		if value == nil {
			return nil, nil, ErrJWTKey
		}
		privateKey, publicKey = value, &value.PublicKey
	case *ecdsa.PublicKey:
		publicKey = value
	default:
		return nil, nil, ErrJWTKey
	}
	if publicKey == nil || publicKey.Curve != curve || publicKey.X == nil || publicKey.Y == nil || !curve.IsOnCurve(publicKey.X, publicKey.Y) {
		return nil, nil, fmt.Errorf("ECDSA requires a valid %s key", curve.Params().Name)
	}
	if privateKey != nil {
		if privateKey.D == nil || privateKey.D.Sign() <= 0 || privateKey.D.Cmp(curve.Params().N) >= 0 {
			return nil, nil, ErrJWTKey
		}
		x, y := curve.ScalarBaseMult(privateKey.D.Bytes())
		if x.Cmp(publicKey.X) != 0 || y.Cmp(publicKey.Y) != 0 {
			return nil, nil, ErrJWTKey
		}
		return privateKey, publicKey, nil
	}
	return nil, publicKey, nil
}
