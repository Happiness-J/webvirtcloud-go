package store

import "testing"

func TestValidTag(t *testing.T) {
	if err := validTag("prod"); err != nil {
		t.Fatal(err)
	}
	if err := validTag("has space"); err == nil {
		t.Fatal("space accepted")
	}
}
