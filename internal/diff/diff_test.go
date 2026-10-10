package diff

import (
	"reflect"
	"testing"
)

func TestColIndexAndName(t *testing.T) {
	cases := map[string]int{"A": 1, "B": 2, "Z": 26, "AA": 27, "AB": 28, "XFD": 16384}
	for name, want := range cases {
		if got := ColIndex(name); got != want {
			t.Errorf("ColIndex(%q) = %d, want %d", name, got, want)
		}
		if got := ColName(want); got != name {
			t.Errorf("ColName(%d) = %q, want %q", want, got, name)
		}
	}
}

func TestDiffRowsBasic(t *testing.T) {
	a := []Row{
		{Num: 1, Cells: map[string]string{"A": "Иванов", "B": "100"}},
		{Num: 2, Cells: map[string]string{"A": "Петров", "B": "200"}},
		{Num: 3, Cells: map[string]string{"A": "Сидоров", "B": "300"}},
	}
	b := []Row{
		{Num: 1, Cells: map[string]string{"A": "Иванов", "B": "100"}},
		{Num: 2, Cells: map[string]string{"A": "Петров", "B": "250"}},   // изменена
		{Num: 3, Cells: map[string]string{"A": "Кузнецов", "B": "300"}}, // добавлена вместо Сидорова
	}
	changes := DiffRows(a, b)
	if len(changes) != 2 {
		t.Fatalf("ожидалось 2 изменения, получено %d: %+v", len(changes), changes)
	}
	// Первая изменённая строка: 2 (A) -> 2 (B)
	if changes[0].Type != Modified || changes[0].RowA != 2 || changes[0].RowB != 2 {
		t.Errorf("changes[0] = %+v", changes[0])
	}
	if len(changes[0].Cells) != 1 || changes[0].Cells[0].Col != "B" || changes[0].Cells[0].New != "250" {
		t.Errorf("cells = %+v", changes[0].Cells)
	}
}

func TestDiffRowsAddDelete(t *testing.T) {
	a := []Row{
		{Num: 1, Cells: map[string]string{"A": "один"}},
		{Num: 2, Cells: map[string]string{"A": "два"}},
	}
	b := []Row{
		{Num: 1, Cells: map[string]string{"A": "один"}},
		{Num: 2, Cells: map[string]string{"A": "два"}},
		{Num: 3, Cells: map[string]string{"A": "три"}},
	}
	changes := DiffRows(a, b)
	if len(changes) != 1 || changes[0].Type != Added || changes[0].RowB != 3 {
		t.Fatalf("changes = %+v", changes)
	}

	changes = DiffRows(b, a)
	if len(changes) != 1 || changes[0].Type != Deleted || changes[0].RowA != 3 {
		t.Fatalf("changes = %+v", changes)
	}
}

func TestDiffRowsNoChange(t *testing.T) {
	rows := []Row{{Num: 1, Cells: map[string]string{"A": "x"}}}
	if ch := DiffRows(rows, rows); len(ch) != 0 {
		t.Fatalf("changes = %+v", ch)
	}
}

func TestTextSegments(t *testing.T) {
	// Смежные однотипные сегменты склеиваются.
	segs := TextSegments("Привет, мир!", "Привет, большой мир!")
	want := []Segment{
		{Type: "eq", Text: "Привет, "},
		{Type: "ins", Text: "большой "},
		{Type: "eq", Text: "мир!"},
	}
	if !reflect.DeepEqual(segs, want) {
		t.Fatalf("segs = %+v\nwant = %+v", segs, want)
	}

	segs = TextSegments("abc def", "abc xyz")
	foundDel, foundIns := false, false
	for _, s := range segs {
		if s.Type == "del" && s.Text == "def" {
			foundDel = true
		}
		if s.Type == "ins" && s.Text == "xyz" {
			foundIns = true
		}
	}
	if !foundDel || !foundIns {
		t.Fatalf("segs = %+v", segs)
	}

	if segs := TextSegments("same", "same"); len(segs) != 1 || segs[0].Type != "eq" {
		t.Fatalf("segs = %+v", segs)
	}
}

func TestDiffRowsModifiedWithoutCellChanges(t *testing.T) {
	// Строки различаются только пустыми ячейками — не должно паниковать.
	a := []Row{{Num: 1, Cells: map[string]string{"A": "x", "B": ""}}}
	b := []Row{{Num: 1, Cells: map[string]string{"A": "x"}}}
	if ch := DiffRows(a, b); len(ch) != 0 {
		t.Fatalf("changes = %+v", ch)
	}
}

func TestDiffRowsByKey(t *testing.T) {
	// Строки переставлены местами — ключевой режим должен их сопоставить.
	a := []Row{
		{Num: 1, Cells: map[string]string{"A": "ART-1", "B": "10"}},
		{Num: 2, Cells: map[string]string{"A": "ART-2", "B": "20"}},
		{Num: 3, Cells: map[string]string{"A": "ART-3", "B": "30"}},
	}
	b := []Row{
		{Num: 1, Cells: map[string]string{"A": "ART-3", "B": "30"}},
		{Num: 2, Cells: map[string]string{"A": "ART-1", "B": "99"}}, // изменена
		{Num: 3, Cells: map[string]string{"A": "ART-4", "B": "40"}}, // добавлена
	}
	changes := DiffRowsByKey(a, b, "A")
	if len(changes) != 3 {
		t.Fatalf("changes = %+v", changes)
	}
	var mod, add, del *RowChange
	for i := range changes {
		switch changes[i].Type {
		case Modified:
			mod = &changes[i]
		case Added:
			add = &changes[i]
		case Deleted:
			del = &changes[i]
		}
	}
	if del == nil || del.RowA != 2 || del.Old["A"] != "ART-2" {
		t.Fatalf("deleted = %+v", del)
	}
	if mod == nil || mod.RowA != 1 || mod.RowB != 2 {
		t.Fatalf("modified = %+v", mod)
	}
	if len(mod.Cells) != 1 || mod.Cells[0].Col != "B" || mod.Cells[0].New != "99" {
		t.Fatalf("cells = %+v", mod.Cells)
	}
	if add == nil || add.RowB != 3 || add.New["A"] != "ART-4" {
		t.Fatalf("added = %+v", add)
	}

	// Удаление строки с уникальным ключом.
	changes = DiffRowsByKey(b, a, "A")
	del = nil
	for i := range changes {
		if changes[i].Type == Deleted {
			del = &changes[i]
		}
	}
	if del == nil || del.RowA != 3 || del.Old["A"] != "ART-4" {
		t.Fatalf("deleted = %+v", del)
	}

	// Порядок не важен: полное совпадение при перестановке.
	changes = DiffRowsByKey(a, []Row{a[2], a[0], a[1]}, "A")
	if len(changes) != 0 {
		t.Fatalf("перестановка распознана как изменения: %+v", changes)
	}
}

func TestRowKeyFormulas(t *testing.T) {
	// Изменение формулы при равных значениях — это изменение строки.
	a := []Row{{Num: 1, Cells: map[string]string{"A": "x", "B": "4"},
		Forms: map[string]string{"B": "2+2"}}}
	b := []Row{{Num: 1, Cells: map[string]string{"A": "x", "B": "4"},
		Forms: map[string]string{"B": "1+3"}}}
	changes := DiffRows(a, b)
	if len(changes) != 1 || changes[0].Type != Modified {
		t.Fatalf("changes = %+v", changes)
	}
	c := changes[0].Cells
	if len(c) != 1 || !c[0].FormulaOnly || c[0].OldFormula != "2+2" || c[0].NewFormula != "1+3" {
		t.Fatalf("cells = %+v", c)
	}

	// Одинаковые значения и формулы — без изменений.
	if ch := DiffRows(a, a); len(ch) != 0 {
		t.Fatalf("changes = %+v", ch)
	}
}
