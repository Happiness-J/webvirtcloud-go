package web

import "testing"

func TestRenameArgs(t *testing.T) {
	args, err := renameArgs("old", "new")
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 3 || args[0] != "domrename" || args[1] != "old" || args[2] != "new" {
		t.Fatalf("args = %#v", args)
	}
	if _, err := renameArgs("old", "old"); err == nil {
		t.Fatal("same name accepted")
	}
}
