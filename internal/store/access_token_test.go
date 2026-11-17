package store

import (
	"context"
	"testing"
	"time"

	"webvirtcloud-go/internal/id"
)

func TestHostAccessAndAPITokenLifecycle(t *testing.T) {
	db := newTestStore(t)
	ctx := context.Background()
	user := User{ID: id.New("user"), Username: "scoped", PasswordHash: "hash", Role: "operator"}
	if err := db.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	host := Host{ID: id.New("host"), Name: "lab", URICiphertext: "cipher", Enabled: true}
	if err := db.CreateHost(ctx, host); err != nil {
		t.Fatal(err)
	}
	if err := db.GrantHostAccess(ctx, user.ID, host.ID); err != nil {
		t.Fatal(err)
	}
	ok, err := db.UserCanAccessHost(ctx, user.ID, host.ID)
	if err != nil || !ok {
		t.Fatalf("access %v %v", ok, err)
	}
	list, err := db.ListHostAccess(ctx, user.ID)
	if err != nil || len(list) != 1 || list[0].HostName != "lab" {
		t.Fatalf("list %#v %v", list, err)
	}
	exp := time.Now().Add(time.Hour)
	token := APIToken{ID: id.New("tok"), UserID: user.ID, Name: "script", TokenHash: "hash-1", Role: "admin", ExpiresAt: &exp}
	if err := db.CreateAPIToken(ctx, token); err != nil {
		t.Fatal(err)
	}
	_, got, err := db.LookupAPIToken(ctx, "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Role != "operator" || got.Username != "scoped" {
		t.Fatalf("role was not capped: %+v", got)
	}
	if err := db.RevokeAPIToken(ctx, user.ID, token.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.LookupAPIToken(ctx, "hash-1"); !IsNotFound(err) {
		t.Fatalf("revoked token still resolved: %v", err)
	}
	if err := db.SetTOTPSecret(ctx, user.ID, "SECRET"); err != nil {
		t.Fatal(err)
	}
	loaded, err := db.GetUserByID(ctx, user.ID)
	if err != nil || loaded.TOTPSecret != "SECRET" {
		t.Fatalf("secret %q %v", loaded.TOTPSecret, err)
	}
}
