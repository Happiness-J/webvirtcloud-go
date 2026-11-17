package store

import "testing"

func TestValidBackupTarget(t *testing.T) {
	if err := validBackupTarget("local", ""); err != nil {
		t.Fatal(err)
	}
	if err := validBackupTarget("nfs", "nas:/exports/backups"); err != nil {
		t.Fatal(err)
	}
	if err := validBackupTarget("s3", "s3://backups/vm"); err != nil {
		t.Fatal(err)
	}
	for _, item := range [][2]string{{"nfs", "../x"}, {"s3", "http://bucket"}, {"local", "secret"}, {"file", "/tmp"}} {
		if err := validBackupTarget(item[0], item[1]); err == nil {
			t.Fatalf("accepted %v", item)
		}
	}
}
