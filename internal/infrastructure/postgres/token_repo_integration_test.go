//go:build integration

package postgres_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/infrastructure/postgres"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
	"github.com/google/uuid"
)

func setupTokenRepo(t *testing.T) (*postgres.APITokenRepository, string) {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)
	return postgres.NewAPITokenRepository(pool), userID
}

func TestTokenRepo_CreateAndFindByHash(t *testing.T) {
	repo, userID := setupTokenRepo(t)
	ctx := context.Background()

	rawToken := "fliq_sk_" + uuid.New().String()[:32] + uuid.New().String()[:32]
	sum := sha256.Sum256([]byte(rawToken))
	tokenHash := fmt.Sprintf("%x", sum)
	prefix := rawToken[:16]

	tok, err := repo.Create(ctx, userID, "my-token", tokenHash, prefix)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if tok.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if tok.Name != "my-token" {
		t.Fatalf("name = %s, want my-token", tok.Name)
	}

	// Find by hash
	found, err := repo.FindByTokenHash(ctx, tokenHash)
	if err != nil {
		t.Fatalf("find by hash: %v", err)
	}
	if found.ID != tok.ID {
		t.Fatalf("found wrong token")
	}
}

func TestTokenRepo_FindByHash_NotFound(t *testing.T) {
	repo, _ := setupTokenRepo(t)
	_, err := repo.FindByTokenHash(context.Background(), "nonexistent-hash")
	if err != domain.ErrTokenNotFound {
		t.Fatalf("err = %v, want ErrTokenNotFound", err)
	}
}

func TestTokenRepo_ListByUserID(t *testing.T) {
	repo, userID := setupTokenRepo(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		raw := fmt.Sprintf("fliq_sk_%064d", i)
		sum := sha256.Sum256([]byte(raw))
		hash := fmt.Sprintf("%x", sum)
		if _, err := repo.Create(ctx, userID, fmt.Sprintf("token-%d", i), hash, raw[:16]); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}

	tokens, err := repo.ListByUserID(ctx, userID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tokens) != 3 {
		t.Fatalf("got %d tokens, want 3", len(tokens))
	}
}

func TestTokenRepo_Delete(t *testing.T) {
	repo, userID := setupTokenRepo(t)
	ctx := context.Background()

	raw := "fliq_sk_" + uuid.New().String()[:32] + uuid.New().String()[:32]
	sum := sha256.Sum256([]byte(raw))
	hash := fmt.Sprintf("%x", sum)
	tok, err := repo.Create(ctx, userID, "to-delete", hash, raw[:16])
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := repo.Delete(ctx, tok.ID, userID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Verify gone
	_, err = repo.FindByTokenHash(ctx, hash)
	if err != domain.ErrTokenNotFound {
		t.Fatalf("err = %v, want ErrTokenNotFound", err)
	}
}

func TestTokenRepo_Delete_NotFound(t *testing.T) {
	repo, userID := setupTokenRepo(t)
	err := repo.Delete(context.Background(), "nonexistent", userID)
	if err != domain.ErrTokenNotFound {
		t.Fatalf("err = %v, want ErrTokenNotFound", err)
	}
}

func TestTokenRepo_UpdateLastUsed(t *testing.T) {
	repo, userID := setupTokenRepo(t)
	ctx := context.Background()

	raw := "fliq_sk_" + uuid.New().String()[:32] + uuid.New().String()[:32]
	sum := sha256.Sum256([]byte(raw))
	hash := fmt.Sprintf("%x", sum)
	tok, err := repo.Create(ctx, userID, "test", hash, raw[:16])
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if tok.LastUsedAt != nil {
		t.Fatal("expected nil last_used_at on creation")
	}

	if err := repo.UpdateLastUsed(ctx, tok.ID); err != nil {
		t.Fatalf("update last used: %v", err)
	}

	found, err := repo.FindByTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found.LastUsedAt == nil {
		t.Fatal("expected non-nil last_used_at after update")
	}
}
