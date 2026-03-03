package store

import (
	"context"
	"testing"
)

func TestMemoryRepositoryUserRoundTrip(t *testing.T) {
	repo := NewMemoryRepository()
	created, err := repo.CreateUser(context.Background(), User{Username: "admin", Role: RoleAdmin})
	if err != nil {
		t.Fatalf("CreateUser error: %v", err)
	}
	got, err := repo.GetUserByUsername(context.Background(), "admin")
	if err != nil {
		t.Fatalf("GetUserByUsername error: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("unexpected user id")
	}
}
