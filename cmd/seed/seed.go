package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/nulzo/model-router-api/internal/store/model"
	"github.com/nulzo/model-router-api/internal/store/sqlite"
	"go.uber.org/zap"
)

func main() {
	repo, err := sqlite.NewSQLiteStorage("router.db", &zap.Logger{})
	if err != nil {
		log.Fatal(err)
	}
	defer repo.Close()

	ctx := context.Background()

	userID := uuid.New().String()
	user := &model.User{
		ID:        userID,
		Email:     "test@example.com",
		Name:      "Test User",
		Role:      "user",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := repo.Users().Create(ctx, user); err != nil {
		log.Printf("User might already exist: %v", err)
	} else {
		fmt.Printf("Created User: %s\n", userID)
	}

	rawKey := "sk-test-1234567890"
	hash := sha256.Sum256([]byte(rawKey))
	hashedHex := hex.EncodeToString(hash[:])

	key := &model.APIKey{
		ID:        uuid.New().String(),
		UserID:    userID,
		Name:      "Test Key",
		KeyHash:   hashedHex,
		KeyPrefix: "sk-test-",
		IsActive:  true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := repo.APIKeys().Create(ctx, key); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("\nSuccessfully seeded database!\n")
	fmt.Printf("API Key: %s\n", rawKey)
	fmt.Printf("Use this key in your Authorization header: Bearer %s\n", rawKey)
}
