package docx

import (
	"bytes"
	"strings"
	"testing"

	"docdiff/internal/diff"
	"docdiff/internal/ooxml"
)

func openBundle(t *testing.T, data []byte) *ooxml.Bundle {
	t.Helper()
	b, err := ooxml.OpenBundle(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("OpenBundle: %v", err)
	}
	return b
}

func TestBuildAndParse(t *testing.T) {
	data := Build([]Content{
		{Paragraph: "Первый абзац"},
		{Paragraph: "Второй абзац"},
		{Table: [][]string{
			{"Имя", "Сумма"},
			{"Иванов", "100"},
		}},
		{Paragraph: "После таблицы"},
	})
	doc, err := Parse(openBundle(t, data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(doc.Blocks) != 4 {
		t.Fatalf("blocks = %d: %+v", len(doc.Blocks), doc.Blocks)
	}
	if doc.Blocks[0].Kind != "paragraph" || doc.Blocks[0].Text != "Первый абзац" {
		t.Fatalf("block0 = %+v", doc.Blocks[0])
	}
	if doc.Blocks[2].Kind != "table" || len(doc.Blocks[2].Rows) != 2 {
		t.Fatalf("block2 = %+v", doc.Blocks[2])
	}
	if doc.Blocks[2].Rows[1].Cells["A"] != "Иванов" || doc.Blocks[2].Rows[1].Cells["B"] != "100" {
		t.Fatalf("table row = %+v", doc.Blocks[2].Rows[1])
	}
	if doc.Blocks[3].Text != "После таблицы" {
		t.Fatalf("block3 = %+v", doc.Blocks[3])
	}
}

func TestDiffAndMark(t *testing.T) {
	oldData := Build([]Content{
		{Paragraph: "Заголовок договора"},
		{Paragraph: "Сумма сделки составляет 1000 рублей."},
		{Paragraph: "Абзац будет удалён полностью."},
		{Paragraph: "Заключительный абзац."},
	})
	newData := Build([]Content{
		{Paragraph: "Заголовок договора"},
		{Paragraph: "Сумма сделки составляет 2000 рублей."}, // изменён
		{Paragraph: "Заключительный абзац."},
		{Paragraph: "Добавленный новый абзац."}, // добавлен
	})

	docA, err := Parse(openBundle(t, oldData))
	if err != nil {
		t.Fatal(err)
	}
	docB, err := Parse(openBundle(t, newData))
	if err != nil {
		t.Fatal(err)
	}

	d := Diff(docA, docB, "old.docx", "new.docx")
	if d.Summary.Modified != 1 || d.Summary.Deleted != 1 || d.Summary.Added != 1 {
		t.Fatalf("summary = %+v, blocks = %+v", d.Summary, d.Blocks)
	}

	out, err := Mark(openBundle(t, newData), d)
	if err != nil {
		t.Fatalf("Mark: %v", err)
	}
	res := openBundle(t, out)

	// comments.xml создан
	cx, ok := res.GetText("word/comments.xml")
	if !ok {
		t.Fatal("нет word/comments.xml")
	}
	for _, want := range []string{"Изменено", "Удалено", "Добавлено"} {
		if !strings.Contains(cx, want) {
			t.Fatalf("в комментариях нет %q: %s", want, cx)
		}
	}

	// document.xml содержит пометки
	dx, _ := res.GetText("word/document.xml")
	if !strings.Contains(dx, "commentRangeStart") || !strings.Contains(dx, "commentReference") {
		t.Fatalf("нет пометок комментариев в document.xml")
	}
	if !strings.Contains(dx, "[Удалено]") {
		t.Fatalf("нет пометки об удалении: %s", dx)
	}
	if !strings.Contains(dx, `w:highlight w:val="yellow"`) {
		t.Fatalf("нет подсветки изменённого")
	}
	if !strings.Contains(dx, `w:highlight w:val="green"`) {
		t.Fatalf("нет подсветки добавленного")
	}

	// content types и rels обновлены
	ct, _ := res.GetText("[Content_Types].xml")
	if !strings.Contains(ct, "/word/comments.xml") {
		t.Fatal("content types не обновлены")
	}
	rels, _ := res.GetText("word/_rels/document.xml.rels")
	if !strings.Contains(rels, "comments") {
		t.Fatal("rels не обновлены")
	}

	// Аннотированный документ должен читаться и содержать те же
	// изменённые/добавленные блоки (плюс синтетические удалённые).
	docMarked, err := Parse(res)
	if err != nil {
		t.Fatalf("аннотированный документ не читается: %v", err)
	}
	if len(docMarked.Blocks) != 5 {
		t.Fatalf("blocks marked = %d", len(docMarked.Blocks))
	}
}

func TestDiffTable(t *testing.T) {
	oldData := Build([]Content{
		{Paragraph: "Таблица:"},
		{Table: [][]string{
			{"А", "Б"},
			{"1", "2"},
			{"3", "4"},
		}},
	})
	newData := Build([]Content{
		{Paragraph: "Таблица:"},
		{Table: [][]string{
			{"А", "Б"},
			{"1", "20"}, // изменена
			{"3", "4"},
			{"5", "6"}, // добавлена
		}},
	})
	docA, _ := Parse(openBundle(t, oldData))
	docB, _ := Parse(openBundle(t, newData))
	d := Diff(docA, docB, "a", "b")
	if d.Summary.Modified != 1 {
		t.Fatalf("summary = %+v blocks=%+v", d.Summary, d.Blocks)
	}
	var tbl *diff.BlockChange
	for i := range d.Blocks {
		if d.Blocks[i].Kind == "table" {
			tbl = &d.Blocks[i]
		}
	}
	if tbl == nil {
		t.Fatal("изменение таблицы не найдено")
	}
	if len(tbl.Rows) != 2 {
		t.Fatalf("rows = %+v", tbl.Rows)
	}

	out, err := Mark(openBundle(t, newData), d)
	if err != nil {
		t.Fatal(err)
	}
	res := openBundle(t, out)
	dx, _ := res.GetText("word/document.xml")
	if !strings.Contains(dx, "commentRangeStart") {
		t.Fatal("таблица не помечена")
	}
}

func TestDeletedAnchorPlacement(t *testing.T) {
	// Удалённый абзац стоял между двумя изменёнными — пометка должна
	// встать на его месте (перед добавленным блоком), а не в начале.
	oldData := Build([]Content{
		{Paragraph: "Общий заголовок"},
		{Paragraph: "Сумма 1000"},
		{Paragraph: "Уникальный текст про удаление"},
	})
	newData := Build([]Content{
		{Paragraph: "Общий заголовок"},
		{Paragraph: "Сумма 2000"},
		{Paragraph: "Совсем иное содержание добавлено"},
	})
	docA, _ := Parse(openBundle(t, oldData))
	docB, _ := Parse(openBundle(t, newData))
	d := Diff(docA, docB, "a", "b")
	var del *diff.BlockChange
	for i := range d.Blocks {
		if d.Blocks[i].Type == diff.Deleted {
			del = &d.Blocks[i]
		}
	}
	if del == nil {
		t.Fatal("удалённый блок не найден")
	}
	// Пометка должна стоять перед блоком 2 («Новый абзац»), а не перед «Сумма 2000».
	if del.Anchor != 2 {
		t.Fatalf("anchor = %d, want 2; blocks=%+v", del.Anchor, d.Blocks)
	}
	out, err := Mark(openBundle(t, newData), d)
	if err != nil {
		t.Fatal(err)
	}
	dx, _ := openBundle(t, out).GetText("word/document.xml")
	iDel := strings.Index(dx, "[Удалено]")
	iSum := strings.Index(dx, "Сумма 2000")
	iNew := strings.Index(dx, "Совсем иное содержание")
	if iSum < 0 || iDel < 0 || iNew < 0 {
		t.Fatalf("элементы не найдены: sum=%d del=%d new=%d", iSum, iDel, iNew)
	}
	if !(iSum < iDel && iDel < iNew) {
		t.Fatalf("порядок неверен: sum=%d del=%d new=%d", iSum, iDel, iNew)
	}
}

func TestTableHighlightOnlyChangedRows(t *testing.T) {
	oldData := Build([]Content{{Table: [][]string{
		{"А", "Б"},
		{"1", "2"},
		{"3", "4"},
	}}})
	newData := Build([]Content{{Table: [][]string{
		{"А", "Б"},
		{"1", "20"},
		{"3", "4"},
	}}})
	docA, _ := Parse(openBundle(t, oldData))
	docB, _ := Parse(openBundle(t, newData))
	d := Diff(docA, docB, "a", "b")
	out, err := Mark(openBundle(t, newData), d)
	if err != nil {
		t.Fatal(err)
	}
	dx, _ := openBundle(t, out).GetText("word/document.xml")
	// Подсвечена только строка с изменённой ячейкой "20" (обе её ячейки).
	rows := strings.Split(dx, "<w:tr")
	changed, same := "", ""
	for _, r := range rows {
		if strings.Contains(r, ">20<") {
			changed = r
		}
		if strings.Contains(r, ">3<") && strings.Contains(r, ">4<") {
			same = r
		}
	}
	if changed == "" || same == "" {
		t.Fatalf("строки таблицы не найдены")
	}
	if !strings.Contains(changed, `w:highlight w:val="yellow"`) {
		t.Fatal("изменённая строка не подсвечена")
	}
	if strings.Contains(same, "yellow") {
		t.Fatal("неизменённая строка подсвечена лишний раз")
	}
}

func TestMarkEmptyDiff(t *testing.T) {
	data := Build([]Content{{Paragraph: "Без изменений"}})
	doc, _ := Parse(openBundle(t, data))
	d := Diff(doc, doc, "a", "b")
	if d.Summary.Total != 0 {
		t.Fatalf("summary = %+v", d.Summary)
	}
	out, err := Mark(openBundle(t, data), d)
	if err != nil {
		t.Fatal(err)
	}
	res := openBundle(t, out)
	if _, ok := res.GetText("word/comments.xml"); ok {
		t.Fatal("comments.xml не должен создаваться")
	}
}
