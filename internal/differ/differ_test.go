package differ

import (
	"testing"
)

func TestDiff(t *testing.T) {
	oldSource := []byte("package p\nfunc keep() {}\nfunc legacyConvert() {}\n")
	newSource := []byte("package p\nfunc keep() {}\nfunc apiUserFull() {}\nfunc aNew() {}\n")
	added, removed, err := Diff(oldSource, newSource)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 2 || added[0] != "aNew" || added[1] != "apiUserFull" {
		t.Fatalf("added = %#v", added)
	}
	if len(removed) != 1 || removed[0] != "legacyConvert" {
		t.Fatalf("removed = %#v", removed)
	}
}

func TestDiffEmpty(t *testing.T) {
	added, removed, err := Diff(nil, []byte("package p\nfunc New() {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 || added[0] != "New" || len(removed) != 0 {
		t.Fatalf("added=%#v removed=%#v", added, removed)
	}
}
