package docx

import (
	"fmt"
	"strings"

	"docdiff/internal/ooxml"
)

// Content — строительный блок документа.
type Content struct {
	Paragraph string     // текст абзаца (если Table == nil)
	Table     [][]string // таблица (если не nil)
}

// Build создаёт минимальный, но корректный документ .docx.
// Используется в тестах и для быстрого создания файлов.
func Build(items []Content) []byte {
	b := &ooxml.Bundle{}

	b.Set("[Content_Types].xml", []byte(
		`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`+
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`+
			`<Default Extension="xml" ContentType="application/xml"/>`+
			`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>`+
			`<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>`+
			`</Types>`))

	b.Set("_rels/.rels", []byte(
		`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>`+
			`</Relationships>`))

	b.Set("word/_rels/document.xml.rels", []byte(
		`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`+
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`+
			`</Relationships>`))

	b.Set("word/styles.xml", []byte(
		`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
			`<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`+
			`<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>`+
			`</w:styles>`))

	var body strings.Builder
	for _, it := range items {
		if it.Table != nil {
			body.WriteString(tableXML(it.Table))
		} else {
			fmt.Fprintf(&body, `<w:p><w:r><w:t xml:space="preserve">%s</w:t></w:r></w:p>`, ooxml.EscapeXML(it.Paragraph))
		}
	}
	doc := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		`<w:body>` + body.String() +
		`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/></w:sectPr>` +
		`</w:body></w:document>`
	b.Set("word/document.xml", []byte(doc))

	out, err := b.Bytes()
	if err != nil {
		panic(err)
	}
	return out
}

func tableXML(rows [][]string) string {
	var sb strings.Builder
	sb.WriteString(`<w:tbl><w:tblPr><w:tblW w:w="0" w:type="auto"/></w:tblPr><w:tblGrid>`)
	cols := 1
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	for i := 0; i < cols; i++ {
		sb.WriteString(`<w:gridCol/>`)
	}
	sb.WriteString(`</w:tblGrid>`)
	for _, r := range rows {
		sb.WriteString(`<w:tr>`)
		for _, c := range r {
			fmt.Fprintf(&sb, `<w:tc><w:p><w:r><w:t xml:space="preserve">%s</w:t></w:r></w:p></w:tc>`, ooxml.EscapeXML(c))
		}
		sb.WriteString(`</w:tr>`)
	}
	sb.WriteString(`</w:tbl>`)
	return sb.String()
}
