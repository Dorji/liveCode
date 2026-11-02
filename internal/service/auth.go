package service

import (
	"context"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"taskmanager/internal/domain"
	"taskmanager/internal/store"
)

type AuthService struct {
	store  *store.Store
	secret []byte
	expiry time.Duration
}

func NewAuthService(st *store.Store, secret string, expiry time.Duration) *AuthService {
	return &AuthService{store: st, secret: []byte(secret), expiry: expiry}
}

func (a *AuthService) Register(ctx context.Context, email, password, name string) (domain.User, error) {
	if email == "" || password == "" || name == "" {
		return domain.User{}, domain.ErrBadRequest
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return domain.User{}, err
	}
	return a.store.CreateUser(ctx, email, string(hash), name)
}

func (a *AuthService) Login(ctx context.Context, email, password string) (string, error) {
	u, err := a.store.GetUserByEmail(ctx, email)
	if err != nil {
		if err == domain.ErrNotFound {
			return "", domain.ErrUnauthorized
		}
		return "", err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return "", domain.ErrUnauthorized
	}
	return a.TokenFor(u)
}

func (a *AuthService) TokenFor(u domain.User) (string, error) {
	claims := Claims{
		Email: u.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(u.ID, 10),
			Issuer:    "taskmanager",
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
			ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(a.expiry)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(a.secret)
}

func (a *AuthService) ParseToken(tokenStr string) (int64, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, domain.ErrUnauthorized
		}
		return a.secret, nil
	})
	if err != nil || !token.Valid {
		return 0, domain.ErrUnauthorized
	}
	claims, ok := token.Claims.(*Claims)
	if !ok {
		return 0, domain.ErrUnauthorized
	}
	id, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		return 0, domain.ErrUnauthorized
	}
	return id, nil
}

type Claims struct {
	Email string `json:"email"`
	jwt.RegisteredClaims
}
