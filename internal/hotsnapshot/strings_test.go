package hotsnapshot

import (
	"bytes"
	"errors"
	"sync"
	"testing"
)

func TestStringInternerDeduplicatesAndFreezes(t *testing.T) {
	interner := NewStringInterner()
	first, err := interner.Intern("repository")
	if err != nil || first != 0 {
		t.Fatalf("first Intern() = %d, %v; want 0, nil", first, err)
	}
	duplicate, err := interner.Intern("repository")
	if err != nil || duplicate != first {
		t.Fatalf("duplicate Intern() = %d, %v; want %d, nil", duplicate, err, first)
	}
	second, err := interner.Intern("")
	if err != nil || second != 1 {
		t.Fatalf("second Intern() = %d, %v; want 1, nil", second, err)
	}
	table := interner.Freeze()
	if value, found := table.String(first); !found || value != "repository" {
		t.Fatalf("String(%d) = %q, %t", first, value, found)
	}
	if value, found := table.String(InvalidInternedString); found || value != "" {
		t.Fatalf("String(InvalidInternedString) = %q, %t", value, found)
	}
	if id, found := table.Lookup(""); !found || id != second {
		t.Fatalf("Lookup(empty) = %d, %t; want %d, true", id, found, second)
	}
	if got, want := table.Stats(), (StringTableStats{Entries: 2, Bytes: uint64(len("repository"))}); got != want {
		t.Fatalf("Stats() = %#v, want %#v", got, want)
	}
	if _, err := interner.Intern("after-freeze"); !errors.Is(err, ErrInternerFrozen) {
		t.Fatalf("Intern(after Freeze) error = %v, want ErrInternerFrozen", err)
	}
}

func TestStringTableRoundTripIsDeterministic(t *testing.T) {
	interner := NewStringInterner()
	for _, value := range []string{"repo", "路径", "", "repo"} {
		if _, err := interner.Intern(value); err != nil {
			t.Fatal(err)
		}
	}
	table := interner.Freeze()
	encoded, err := table.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := UnmarshalStringTable(encoded)
	if err != nil {
		t.Fatal(err)
	}
	reencoded, err := restored.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, reencoded) {
		t.Fatalf("round trip changed encoding\n got: %x\nwant: %x", reencoded, encoded)
	}
	for _, value := range []string{"repo", "路径", ""} {
		id, found := table.Lookup(value)
		if !found {
			t.Fatalf("original Lookup(%q) not found", value)
		}
		restoredID, restoredFound := restored.Lookup(value)
		if !restoredFound || restoredID != id {
			t.Fatalf("restored Lookup(%q) = %d, %t; want %d, true", value, restoredID, restoredFound, id)
		}
	}
}

func TestUnmarshalStringTableRejectsMalformedData(t *testing.T) {
	for _, data := range [][]byte{
		nil,
		{1, 0, 0, 0},
		{1, 0, 0, 0, 2, 0, 0, 0, 'x'},
		{0, 0, 0, 0, 1},
		{2, 0, 0, 0, 1, 0, 0, 0, 'x', 1, 0, 0, 0, 'x'},
	} {
		if _, err := UnmarshalStringTable(data); !errors.Is(err, ErrMalformedStringTable) {
			t.Fatalf("UnmarshalStringTable(%x) error = %v, want ErrMalformedStringTable", data, err)
		}
	}
}

func TestStringTableSupportsConcurrentReads(t *testing.T) {
	interner := NewStringInterner()
	for _, value := range []string{"a", "b", "c"} {
		if _, err := interner.Intern(value); err != nil {
			t.Fatal(err)
		}
	}
	table := interner.Freeze()
	var group sync.WaitGroup
	for worker := 0; worker < 32; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for iteration := 0; iteration < 1_000; iteration++ {
				if id, found := table.Lookup("b"); !found || id != 1 {
					t.Errorf("Lookup(b) = %d, %t", id, found)
					return
				}
				if value, found := table.String(2); !found || value != "c" {
					t.Errorf("String(2) = %q, %t", value, found)
					return
				}
			}
		}()
	}
	group.Wait()
}
