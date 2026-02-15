package auth

// handle authentication via JWT

import (
	"crypto/rsa"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var publicKey *rsa.PublicKey

func InitJWT() error {
	// Skip JWT initialization if auth is disabled
	if os.Getenv("DISABLE_AUTH") == "1" {
		return nil
	}

	keyData := os.Getenv("JWT_PUBLIC_KEY")
	if keyData == "" {
		return errors.New("JWT_PUBLIC_KEY not set")
	}

	parsedKey, err := jwt.ParseRSAPublicKeyFromPEM([]byte(keyData))
	if err != nil {
		return err
	}

	publicKey = parsedKey
	return nil
}

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Bypass authentication if DISABLE_AUTH=1
		if os.Getenv("DISABLE_AUTH") == "1" {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Missing Authorization header", http.StatusUnauthorized)
			return
		}

		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

		token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, errors.New("unexpected signing method")
			}
			return publicKey, nil
		})

		if err != nil || !token.Valid {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			http.Error(w, "Invalid claims", http.StatusUnauthorized)
			return
		}

		// Validate issuer
		if claims["iss"] != "node-api" {
			http.Error(w, "Invalid issuer", http.StatusUnauthorized)
			return
		}

		// Validate audience
		if claims["aud"] != "go-db-service" {
			http.Error(w, "Invalid audience", http.StatusUnauthorized)
			return
		}

		// Validate expiration
		exp, ok := claims["exp"].(float64)
		if !ok || time.Now().Unix() > int64(exp) {
			http.Error(w, "Token expired", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}
