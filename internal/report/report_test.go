package report

import (
	"strings"
	"testing"

	"docdiff/internal/diff"
)

func TestGenerateExcel(t *testing.T) {
	d := &diff.ExcelDiff{FileA: "old.xlsx", FileB: "new.xlsx"}
	d.Summary.Add(diff.Modified)
	d.Summary.Add(diff.Added)
	d.Sheets = []diff.SheetDiff{{
		Name:   "Продажи",
		Status: "changed",
		Changes: []diff.RowChange{
			{Type: diff.Modified, RowA: 2, RowB: 2,
				Cells: []diff.CellChange{{Col: "B", Old: "10", New: "12", OldFormula: "5*2", NewFormula: "6*2"}}},
			{Type: diff.Added, RowB: 3, New: map[string]string{"A": "Сливы"}},
		},
	}}
	html, err := Generate(d)
	if err != nil {
		t.Fatal(err)
	}
	s := string(html)
	for _, want := range []string{"old.xlsx", "Продажи", "10", "12", "5*2", "6*2", "Сливы", "Изменено", "Добавлено"} {
		if !strings.Contains(s, want) {
			t.Fatalf("в отчёте нет %q", want)
		}
	}
}

func TestGenerateWord(t *testing.T) {
	d := &diff.WordDiff{FileA: "old.docx", FileB: "new.docx"}
	d.Summary.Add(diff.Modified)
	d.Summary.Add(diff.Deleted)
	d.Blocks = []diff.BlockChange{
		{Type: diff.Modified, Index: 0, Kind: "paragraph",
			Segments: []diff.Segment{{Type: "eq", Text: "Сумма "}, {Type: "del", Text: "100"}, {Type: "ins", Text: "200"}}},
		{Type: diff.Deleted, Index: 1, Anchor: 1, Kind: "paragraph", Old: "Удалённый абзац"},
	}
	html, err := Generate(d)
	if err != nil {
		t.Fatal(err)
	}
	s := string(html)
	for _, want := range []string{"old.docx", "Сумма", "Удалённый абзац", "Изменено", "Удалено"} {
		if !strings.Contains(s, want) {
			t.Fatalf("в отчёте нет %q", want)
		}
	}
}

func TestGenerateBadType(t *testing.T) {
	if _, err := Generate(42); err == nil {
		t.Fatal("ожидалась ошибка")
	}
}
