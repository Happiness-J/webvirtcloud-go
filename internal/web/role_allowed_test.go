package web

import "testing"

func TestRoleAllowed(t *testing.T) {
	tests := []struct {
		name    string
		actual  string
		minimum string
		want    bool
	}{
		{name: "admin meets viewer", actual: "admin", minimum: "viewer", want: true},
		{name: "admin meets desktop", actual: "admin", minimum: "desktop", want: true},
		{name: "admin meets operator", actual: "admin", minimum: "operator", want: true},
		{name: "admin meets admin", actual: "admin", minimum: "admin", want: true},
		{name: "operator meets viewer", actual: "operator", minimum: "viewer", want: true},
		{name: "operator meets desktop", actual: "operator", minimum: "desktop", want: true},
		{name: "operator meets operator", actual: "operator", minimum: "operator", want: true},
		{name: "operator misses admin", actual: "operator", minimum: "admin", want: false},
		{name: "desktop meets viewer", actual: "desktop", minimum: "viewer", want: true},
		{name: "desktop meets desktop", actual: "desktop", minimum: "desktop", want: true},
		{name: "desktop misses operator", actual: "desktop", minimum: "operator", want: false},
		{name: "desktop misses admin", actual: "desktop", minimum: "admin", want: false},
		{name: "viewer meets viewer", actual: "viewer", minimum: "viewer", want: true},
		{name: "viewer misses desktop", actual: "viewer", minimum: "desktop", want: false},
		{name: "viewer misses operator", actual: "viewer", minimum: "operator", want: false},
		{name: "viewer misses admin", actual: "viewer", minimum: "admin", want: false},
		{name: "unknown role denied", actual: "guest", minimum: "viewer", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := roleAllowed(tt.actual, tt.minimum); got != tt.want {
				t.Fatalf("roleAllowed(%q, %q) = %v, want %v", tt.actual, tt.minimum, got, tt.want)
			}
		})
	}
}
