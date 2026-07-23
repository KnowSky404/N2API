package store

import (
	"context"
	"testing"
)

func TestEncryptionRotationGateRepositorySerializesInventoryChecks(t *testing.T) {
	adminRepo := newTestAdminRepository(t)
	repository := NewEncryptionRotationGateRepository(adminRepo.pool)
	ctx := context.Background()

	first, acquired, err := repository.TryAcquire(ctx)
	if err != nil || !acquired || first == nil {
		t.Fatalf("first TryAcquire = lease:%v acquired:%v err:%v", first, acquired, err)
	}
	values, err := first.ListEncryptedValues(ctx)
	if err != nil {
		t.Fatalf("ListEncryptedValues returned error: %v", err)
	}
	if values == nil {
		t.Fatal("ListEncryptedValues returned nil slice")
	}
	second, acquired, err := repository.TryAcquire(ctx)
	if err != nil || acquired || second != nil {
		t.Fatalf("contended TryAcquire = lease:%v acquired:%v err:%v", second, acquired, err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close returned error: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("repeated Close returned error: %v", err)
	}

	third, acquired, err := repository.TryAcquire(ctx)
	if err != nil || !acquired || third == nil {
		t.Fatalf("reacquire = lease:%v acquired:%v err:%v", third, acquired, err)
	}
	if err := third.Close(); err != nil {
		t.Fatalf("third Close returned error: %v", err)
	}
}
