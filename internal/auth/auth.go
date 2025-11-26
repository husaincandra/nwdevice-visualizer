package auth

import (
	"fmt"
	"log"
	"os"
	"regexp"

	"network-switch-visualizer/internal/models"

	"github.com/golang-jwt/jwt/v5"
)

var JwtKey []byte

func InitAuth() {
	if secret := os.Getenv("JWT_SECRET"); secret != "" {
		JwtKey = []byte(secret)
	} else {
		log.Println("WARNING: JWT_SECRET not set, using default insecure key. Set JWT_SECRET in production.")
		JwtKey = []byte("my_secret_key")
	}
}

func ValidatePassword(password string) error {
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters long")
	}
	hasNumber := regexp.MustCompile(`[0-9]`).MatchString(password)
	hasSpecial := regexp.MustCompile(`[!@#$%^&*(),.?":{}|<>]`).MatchString(password)
	if !hasNumber && !hasSpecial {
		return fmt.Errorf("password must contain at least one number or special character")
	}
	return nil
}

func ParseToken(tokenStr string) (*models.Claims, error) {
	claims := &models.Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
		return JwtKey, nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return claims, nil
}
