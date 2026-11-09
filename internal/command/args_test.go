package command

import (
	"reflect"
	"testing"
)

func TestParseArgs(t *testing.T) {
	got, err := ParseArgs(`dominfo "my vm" 'a b' plain\ word 'it'\''s'`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"dominfo", "my vm", "a b", "plain word", "it's"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestParseArgsRejectsUnclosedQuote(t *testing.T) {
	if _, err := ParseArgs(`dominfo "vm`); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseArgsNeverExpands(t *testing.T) {
	got, err := ParseArgs(`list $(id) | cat`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"list", "$(id)", "|", "cat"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}
