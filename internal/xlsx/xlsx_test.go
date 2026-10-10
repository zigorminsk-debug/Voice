package xlsx

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
	data := Build([]NamedGrid{
		{Name: "Данные", Rows: [][]string{
			{"Имя", "Сумма"},
			{"Иванов", "100"},
			{"Петров", "200"},
		}},
		{Name: "Лист2", Rows: [][]string{
			{"только один столбец"},
		}},
	})
	b := openBundle(t, data)
	book, err := Parse(b)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(book.Sheets) != 2 {
		t.Fatalf("sheets = %d", len(book.Sheets))
	}
	if book.Sheets[0].Name != "Данные" || book.Sheets[1].Name != "Лист2" {
		t.Fatalf("names = %q, %q", book.Sheets[0].Name, book.Sheets[1].Name)
	}
	rows := book.Sheets[0].Rows
	if len(rows) != 3 {
		t.Fatalf("rows = %d: %+v", len(rows), rows)
	}
	if rows[1].Cells["A"] != "Иванов" || rows[1].Cells["B"] != "100" {
		t.Fatalf("row1 = %+v", rows[1])
	}
}

func TestDiffAndMark(t *testing.T) {
	oldData := Build([]NamedGrid{
		{Name: "Данные", Rows: [][]string{
			{"Имя", "Сумма", "Город"},
			{"Иванов", "100", "Москва"},
			{"Петров", "200", "Казань"},
			{"Сидоров", "300", "Омск"},
		}},
	})
	newData := Build([]NamedGrid{
		{Name: "Данные", Rows: [][]string{
			{"Имя", "Сумма", "Город"},
			{"Иванов", "150", "Москва"}, // изменена
			{"Петров", "200", "Казань"},
			{"Сидоров", "300", "Омск"},
			{"Кузнецов", "400", "Тула"}, // добавлена
		}},
	})

	bookA, err := Parse(openBundle(t, oldData))
	if err != nil {
		t.Fatal(err)
	}
	bookB, err := Parse(openBundle(t, newData))
	if err != nil {
		t.Fatal(err)
	}

	// Сравнение
	var changes []diff.RowChange
	d := &diff.ExcelDiff{FileA: "old.xlsx", FileB: "new.xlsx"}
	for _, sh := range bookA.Sheets {
		sd := diff.SheetDiff{Name: sh.Name, Status: "same"}
		bb := bookB.Sheets[0]
		ch := diff.DiffRows(sh.Rows, bb.Rows)
		sd.Changes = ch
		if len(ch) > 0 {
			sd.Status = "changed"
		}
		d.Sheets = append(d.Sheets, sd)
		for _, c := range ch {
			d.Summary.Add(c.Type)
			changes = append(changes, c)
		}
	}

	if d.Summary.Modified != 1 || d.Summary.Added != 1 {
		t.Fatalf("summary = %+v", d.Summary)
	}

	// Аннотация
	marks := DiffToMarks(d, bookB)
	report := BuildReportRows(d)
	out, err := Mark(openBundle(t, newData), marks, report)
	if err != nil {
		t.Fatalf("Mark: %v", err)
	}

	res := openBundle(t, out)
	// Комментарии созданы
	foundComments := false
	for _, name := range res.Names() {
		if strings.HasPrefix(name, "xl/commentsDiff") {
			foundComments = true
			xml, _ := res.GetText(name)
			if !strings.Contains(xml, "Изменено") && !strings.Contains(xml, "добавлена") {
				t.Fatalf("в комментариях нет ожидаемого текста: %s", xml)
			}
		}
	}
	if !foundComments {
		t.Fatalf("часть комментариев не создана: %v", res.Names())
	}
	// VML создан
	foundVml := false
	for _, name := range res.Names() {
		if strings.Contains(name, "vmlDrawingDiff") {
			foundVml = true
		}
	}
	if !foundVml {
		t.Fatal("VML-фигуры не созданы")
	}
	// Лист-отчёт добавлен
	wb, _ := res.GetText("xl/workbook.xml")
	if !strings.Contains(wb, reportSheetName) {
		t.Fatalf("лист-отчёт не добавлен в workbook: %s", wb)
	}
	rep, ok := res.GetText("xl/worksheets/sheetDiffReport.xml")
	if !ok || !strings.Contains(rep, "Строка изменена") || !strings.Contains(rep, "Строка добавлена") {
		t.Fatalf("отчёт некорректен: ok=%v %s", ok, rep)
	}
	// Content types
	ct, _ := res.GetText("[Content_Types].xml")
	if !strings.Contains(ct, "commentsDiff1.xml") || !strings.Contains(ct, "sheetDiffReport.xml") {
		t.Fatalf("content types не обновлены: %s", ct)
	}

	// Аннотированная книга должна читаться
	bookMarked, err := Parse(res)
	if err != nil {
		t.Fatalf("аннотированная книга не читается: %v", err)
	}
	_ = bookMarked

	// В листе появился legacyDrawing и стили ячеек
	sheet, _ := res.GetText("xl/worksheets/sheet1.xml")
	if !strings.Contains(sheet, "legacyDrawing") {
		t.Fatalf("нет legacyDrawing: %s", sheet)
	}
	if !strings.Contains(sheet, `s="`) {
		t.Fatalf("не выставлены стили ячеек")
	}
}

func TestMarkDeletedRowReport(t *testing.T) {
	oldData := Build([]NamedGrid{{Name: "S", Rows: [][]string{
		{"a"}, {"b"}, {"c"},
	}}})
	newData := Build([]NamedGrid{{Name: "S", Rows: [][]string{
		{"a"}, {"c"},
	}}})
	bookA, _ := Parse(openBundle(t, oldData))
	bookB, _ := Parse(openBundle(t, newData))
	d := &diff.ExcelDiff{}
	sd := diff.SheetDiff{Name: "S", Status: "changed"}
	sd.Changes = diff.DiffRows(bookA.Sheets[0].Rows, bookB.Sheets[0].Rows)
	d.Sheets = append(d.Sheets, sd)
	for _, c := range sd.Changes {
		d.Summary.Add(c.Type)
	}
	if d.Summary.Deleted != 1 {
		t.Fatalf("summary = %+v", d.Summary)
	}
	report := BuildReportRows(d)
	out, err := Mark(openBundle(t, newData), nil, report)
	if err != nil {
		t.Fatal(err)
	}
	res := openBundle(t, out)
	rep, _ := res.GetText("xl/worksheets/sheetDiffReport.xml")
	if !strings.Contains(rep, "Строка удалена") || !strings.Contains(rep, "A=«b»") {
		t.Fatalf("отчёт: %s", rep)
	}
}

func TestParseNumbersAndBool(t *testing.T) {
	// Числа и булевы значения через общий механизм Build недоступны —
	// проверим normalizeNumber напрямую.
	if normalizeNumber("1.0") != "1" || normalizeNumber("2.50") != "2.5" {
		t.Fatal("normalizeNumber")
	}
	if normalizeNumber("текст") != "текст" {
		t.Fatal("normalizeNumber text")
	}
}

// rawBookWithFormulas создаёт книгу с ячейками-формулами напрямую (как Excel).
func rawBookWithFormulas(cellXML string) []byte {
	b := ooxml.NewBundle()
	b.Set("[Content_Types].xml", []byte(
		`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`+
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`+
			`<Default Extension="xml" ContentType="application/xml"/>`+
			`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>`+
			`<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`+
			`</Types>`))
	b.Set("_rels/.rels", []byte(
		`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>`+
			`</Relationships>`))
	b.Set("xl/workbook.xml", []byte(
		`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
			`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`+
			`<sheets><sheet name="F" sheetId="1" r:id="rId1"/></sheets></workbook>`))
	b.Set("xl/_rels/workbook.xml.rels", []byte(
		`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>`+
			`</Relationships>`))
	b.Set("xl/worksheets/sheet1.xml", []byte(
		`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
			`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`+
			cellXML+`</sheetData></worksheet>`))
	out, err := b.Bytes()
	if err != nil {
		panic(err)
	}
	return out
}

func TestParseAndDiffFormulas(t *testing.T) {
	oldData := rawBookWithFormulas(
		`<row r="1"><c r="A1" t="inlineStr"><is><t>итог</t></is></c>` +
			`<c r="B1"><f>2+2</f><v>4</v></c></row>`)
	newData := rawBookWithFormulas(
		`<row r="1"><c r="A1" t="inlineStr"><is><t>итог</t></is></c>` +
			`<c r="B1"><f>1+3</f><v>4</v></c></row>`)

	bookA, err := Parse(openBundle(t, oldData))
	if err != nil {
		t.Fatal(err)
	}
	bookB, err := Parse(openBundle(t, newData))
	if err != nil {
		t.Fatal(err)
	}
	if f := bookA.Sheets[0].Rows[0].Forms["B"]; f != "2+2" {
		t.Fatalf("формула A не прочитана: %+v", bookA.Sheets[0].Rows[0])
	}
	if v := bookB.Sheets[0].Rows[0].Cells["B"]; v != "4" {
		t.Fatalf("значение B = %q", v)
	}

	d := DiffBooks(bookA, bookB, "a", "b", "")
	if d.Summary.Modified != 1 {
		t.Fatalf("summary = %+v", d.Summary)
	}
	cc := d.Sheets[0].Changes[0].Cells
	if len(cc) != 1 || !cc[0].FormulaOnly {
		t.Fatalf("cells = %+v", cc)
	}

	// В комментарии должна быть формула.
	marks := DiffToMarks(d, bookB)
	out, err := Mark(openBundle(t, newData), marks, BuildReportRows(d))
	if err != nil {
		t.Fatal(err)
	}
	res := openBundle(t, out)
	found := false
	for _, name := range res.Names() {
		if strings.HasPrefix(name, "xl/commentsDiff") {
			xml, _ := res.GetText(name)
			if strings.Contains(xml, "формула") && strings.Contains(xml, "2+2") && strings.Contains(xml, "1+3") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("в комментариях нет описания формулы")
	}
}

func TestDiffBooksKeyColumn(t *testing.T) {
	oldData := Build([]NamedGrid{{Name: "S", Rows: [][]string{
		{"Артикул", "Кол-во"},
		{"A-1", "10"},
		{"A-2", "20"},
	}}})
	newData := Build([]NamedGrid{{Name: "S", Rows: [][]string{
		{"Артикул", "Кол-во"},
		{"A-2", "25"},
		{"A-3", "30"},
	}}})
	bookA, _ := Parse(openBundle(t, oldData))
	bookB, _ := Parse(openBundle(t, newData))

	d := DiffBooks(bookA, bookB, "a", "b", "A")
	if d.Summary.Modified != 1 || d.Summary.Added != 1 || d.Summary.Deleted != 1 {
		t.Fatalf("summary = %+v changes=%+v", d.Summary, d.Sheets[0].Changes)
	}
	// A-2 сопоставлен несмотря на другой порядок.
	for _, ch := range d.Sheets[0].Changes {
		if ch.Type == diff.Modified {
			if ch.Old["A"] != "A-2" || ch.New["A"] != "A-2" {
				t.Fatalf("сопоставление не по ключу: %+v", ch)
			}
			if len(ch.Cells) != 1 || ch.Cells[0].New != "25" {
				t.Fatalf("cells = %+v", ch.Cells)
			}
		}
	}
}
