package sync

import "testing"

func TestMergeTextDifferentLines(t *testing.T) {
	base := "title\none\ntwo\n"
	local := "title\nONE\ntwo\n"
	server := "title\none\nTWO\n"
	got, ok := MergeText(base, local, server)
	if !ok {
		t.Fatal("expected merge success")
	}
	want := "title\nONE\nTWO\n"
	if got != want {
		t.Fatalf("merged = %q, want %q", got, want)
	}
}

func TestMergeTextSameLineConflict(t *testing.T) {
	base := "title\none\n"
	local := "title\nlocal\n"
	server := "title\nserver\n"
	if got, ok := MergeText(base, local, server); ok {
		t.Fatalf("expected conflict, got %q", got)
	}
}

func TestMergeTextSameInsertConflict(t *testing.T) {
	base := "a\nb\n"
	local := "a\nlocal\nb\n"
	server := "a\nserver\nb\n"
	if got, ok := MergeText(base, local, server); ok {
		t.Fatalf("expected conflict, got %q", got)
	}
}

func TestMergeTextInsertAtDeletedAnchorConflict(t *testing.T) {
	base := "ab"
	local := "aXb"
	server := "a"
	if got, ok := MergeText(base, local, server); ok {
		t.Fatalf("expected conflict, got %q", got)
	}
}

func TestMergeTextIdenticalChange(t *testing.T) {
	base := "a\nb\n"
	local := "a\nB\n"
	server := "a\nB\n"
	got, ok := MergeText(base, local, server)
	if !ok || got != local {
		t.Fatalf("got %q ok=%v", got, ok)
	}
}
