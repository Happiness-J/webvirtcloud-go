package web

import (
	"errors"
	"strings"
	"testing"

	"webvirtcloud-go/internal/store"
)

func TestQuotaNearlyFullAtEightyPercent(t *testing.T) {
	quota := store.UserQuota{MaxVMs: 10, MaxVCPUs: 10, MaxMemoryGB: 10, MaxDiskGB: 10}
	got := quotaNearlyFull(store.Usage{VMs: 8, VCPUs: 7, MemoryGB: 10, DiskGB: 0}, quota)
	if strings.Join(got, ",") != "虚拟机,内存" {
		t.Fatalf("items = %#v", got)
	}
	if len(quotaNearlyFull(store.Usage{}, store.UserQuota{})) != 0 {
		t.Fatal("a zero limit was treated as nearly full")
	}
}

func TestNearlyFullQuotasSkipsLookupErrors(t *testing.T) {
	users := []store.User{{ID: "full", Username: "full"}, {ID: "broken", Username: "broken"}, {ID: "spare", Username: "spare"}}
	got := nearlyFullQuotas(users,
		func(userID string) (store.UserQuota, error) {
			if userID == "broken" {
				return store.UserQuota{}, errors.New("unavailable")
			}
			return store.UserQuota{MaxVMs: 5}, nil
		},
		func(userID string) (store.Usage, error) {
			if userID == "full" {
				return store.Usage{VMs: 4}, nil
			}
			return store.Usage{}, nil
		})
	if len(got) != 1 || got[0].Username != "full" || strings.Join(got[0].Items, ",") != "虚拟机" {
		t.Fatalf("accounts = %#v", got)
	}
}

func TestUsersPageRendersNearlyFullQuotas(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	page := pageData{Title: "users", CSRF: "csrf", User: &store.User{Role: "admin"}, Data: map[string]any{
		"Users": []userRow{}, "Page": 1, "HasNext": false, "LDAP": false, "Webhook": false,
		"NearlyFull": []nearlyFullQuota{{UserID: "user-1", Username: "full", Items: []string{"虚拟机", "磁盘"}}},
	}}
	var rendered strings.Builder
	if err := templates["users"].ExecuteTemplate(&rendered, "base", page); err != nil {
		t.Fatal(err)
	}
	body := rendered.String()
	for _, want := range []string{"配额快用完", "full", "虚拟机", "磁盘", "不改配额", "/users/user-1/access"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
