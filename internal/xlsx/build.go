package xlsx

import (
	"fmt"
	"strings"

	"docdiff/internal/diff"
	"docdiff/internal/ooxml"
)

// NamedGrid — именованный лист с данными (каждая строка — ячейки A, B, C...).
type NamedGrid struct {
	Name string
	Rows [][]string
}

// Build создаёт минимальную, но корректную книгу .xlsx с inline-строками.
// Используется в тестах и для быстрого создания файлов.
func Build(sheets []NamedGrid) []byte {
	b := &ooxml.Bundle{}

	ct := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
		`<Default Extension="xml" ContentType="application/xml"/>` +
		`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
		`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`
	for i := range sheets {
		ct += fmt.Sprintf(`<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, i+1)
	}
	ct += `</Types>`
	b.Set("[Content_Types].xml", []byte(ct))

	b.Set("_rels/.rels", []byte(
		`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>`+
			`</Relationships>`))

	var wb strings.Builder
	wb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	wb.WriteString(`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`)
	for i, sh := range sheets {
		fmt.Fprintf(&wb, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, ooxml.EscapeXML(sh.Name), i+1, i+1)
	}
	wb.WriteString(`</sheets></workbook>`)
	b.Set("xl/workbook.xml", []byte(wb.String()))

	var rels strings.Builder
	rels.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	rels.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i := range sheets {
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, i+1, i+1)
	}
	rels.WriteString(`<Relationship Id="rIdStyles" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`)
	rels.WriteString(`</Relationships>`)
	b.Set("xl/_rels/workbook.xml.rels", []byte(rels.String()))

	b.Set("xl/styles.xml", []byte(defaultStylesXML()))

	for i, sh := range sheets {
		b.Set(fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), []byte(gridXML(sh.Rows)))
	}

	out, err := b.Bytes()
	if err != nil {
		panic(err)
	}
	return out
}

func gridXML(rows [][]string) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	sb.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	fmt.Fprintf(&sb, `<dimension ref="A1:%s%d"/>`, diff.ColName(maxLen(rows)), len(rows))
	sb.WriteString(`<sheetViews><sheetView workbookViewId="0"/></sheetViews>`)
	sb.WriteString(`<sheetFormatPr defaultRowHeight="15"/><sheetData>`)
	for r, row := range rows {
		fmt.Fprintf(&sb, `<row r="%d">`, r+1)
		for c, v := range row {
			if v == "" {
				continue
			}
			fmt.Fprintf(&sb, `<c r="%s%d" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`,
				diff.ColName(c+1), r+1, ooxml.EscapeXML(v))
		}
		sb.WriteString(`</row>`)
	}
	sb.WriteString(`</sheetData></worksheet>`)
	return sb.String()
}

func maxLen(rows [][]string) int {
	n := 1
	for _, r := range rows {
		if len(r) > n {
			n = len(r)
		}
	}
	return n
}
