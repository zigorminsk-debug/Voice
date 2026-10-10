package main

import (
	"path/filepath"
	"testing"
)

func TestDiagramLifecycle(t *testing.T) {
	d := NewDiagram()
	a := d.AddNode(tools[0], 10, 20)
	b := d.AddNode(tools[1], 50, 60)
	if !d.AddLink(a.ID, b.ID) {
		t.Fatal("expected link to be added")
	}
	if d.AddLink(a.ID, b.ID) {
		t.Fatal("duplicate link must be rejected")
	}
	if d.AddLink(a.ID, a.ID) {
		t.Fatal("self-link must be rejected")
	}
	d.DeleteNode(a.ID)
	if len(d.Nodes) != 1 || len(d.Links) != 0 {
		t.Fatalf("delete left nodes=%d links=%d", len(d.Nodes), len(d.Links))
	}
}

func TestSaveLoad(t *testing.T) {
	d := SampleDiagram()
	path := filepath.Join(t.TempDir(), "scheme.json")
	if err := d.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadDiagram(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Nodes) != 6 || len(got.Links) != 5 {
		t.Fatalf("unexpected restored diagram: %#v", got)
	}
	if got.NextID != 7 {
		t.Fatalf("next id = %d", got.NextID)
	}
}
