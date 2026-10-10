// Package docx — чтение и аннотирование документов Word (.docx)
// средствами стандартной библиотеки Go (OOXML поверх zip+XML).
package docx

import (
	"fmt"
	"strings"

	"docdiff/internal/diff"
	"docdiff/internal/ooxml"
)

// Block — смысловой блок документа: абзац или таблица.
type Block struct {
	Kind string // paragraph | table
	Text string // текст абзаца (для paragraph)
	Rows []diff.Row
}

// Document — прочитанный документ.
type Document struct {
	Blocks []Block
}

// Parse читает документ из OOXML-контейнера.
func Parse(b *ooxml.Bundle) (*Document, error) {
	xml, ok := b.GetText("word/document.xml")
	if !ok {
		return nil, fmt.Errorf("в файле нет word/document.xml — это не документ Word")
	}
	blocks := parseBody(xml)
	return &Document{Blocks: blocks}, nil
}

// detectW возвращает префикс пространства имён Word ("w:" или "").
func detectW(xml string) string {
	i := strings.Index(xml, "<")
	if i < 0 {
		return "w:"
	}
	j := strings.IndexByte(xml[i:], '>')
	if j < 0 {
		return "w:"
	}
	root := xml[i : i+j]
	lower := strings.ToLower(root)
	if strings.Contains(lower, `xmlns:w="`) || strings.Contains(lower, `xmlns:w='`) {
		return "w:"
	}
	// дефолтный namespace = wordprocessingml?
	if strings.Contains(lower, `xmlns="http://schemas.openxmlformats.org/wordprocessingml/2006/main`) ||
		strings.Contains(lower, `xmlns="http://purl.oclc.org/ooxml/wordprocessingml/main`) {
		return ""
	}
	return "w:"
}

// parseBody извлекает блоки верхнего уровня из word/document.xml.
func parseBody(xml string) []Block {
	p := detectW(xml)
	d := newDoc(xml)
	var blocks []Block
	tblDepth := 0
	inBody := false

	for i := 0; i < len(d.tags); i++ {
		t := d.tags[i]
		name := strings.TrimPrefix(t.Name, "/")
		isW := strings.HasPrefix(t.Name, p) || (p == "" && !strings.Contains(t.Name, ":"))
		if !isW {
			continue
		}
		local := strings.TrimPrefix(name, p)
		switch {
		case t.IsOpen() && local == "body":
			inBody = true
		case t.IsClose() && local == "body":
			inBody = false
		case !inBody:
			continue
		case t.IsOpen() && local == "tbl":
			tblDepth++
			if tblDepth == 1 {
				end := blockEnd(d, i, p+"tbl")
				blocks = append(blocks, parseTable(d, i, end, p))
				i = end - 1
				tblDepth = 0
			}
		case t.IsClose() && local == "tbl":
			tblDepth--
		case t.IsOpen() && local == "p" && tblDepth == 0:
			end := blockEnd(d, i, p+"p")
			blocks = append(blocks, Block{Kind: "paragraph", Text: paraText(d, i, end, p)})
			i = end - 1
		}
	}
	return blocks
}

// blockEnd возвращает индекс тега за концом блока (учитывает самозакрывающиеся теги).
func blockEnd(d *doc, start int, name string) int {
	if d.tags[start].SelfClose {
		return start + 1
	}
	return findBalanced(d, start, name)
}

// findBalanced возвращает индекс тега после закрывающего парного тега.
func findBalanced(d *doc, start int, name string) int {
	depth := 0
	for j := start; j < len(d.tags); j++ {
		t := d.tags[j]
		switch {
		case t.IsPaired() && t.Name == name:
			depth++
		case t.IsClose() && t.CloseName() == name:
			depth--
			if depth == 0 {
				return j + 1
			}
		}
	}
	return len(d.tags)
}

// paraText собирает видимый текст абзаца.
func paraText(d *doc, start, end int, p string) string {
	var sb strings.Builder
	for j := start; j < end; j++ {
		t := d.tags[j]
		local := strings.TrimPrefix(t.Name, p)
		switch {
		case t.IsOpen() && local == "t" && !t.SelfClose:
			txt, nj := d.textOf(j)
			sb.WriteString(ooxml.UnescapeXML(stripMarkup(txt)))
			j = nj - 1
		case t.IsOpen() && local == "tab":
			sb.WriteString("\t")
		case t.IsOpen() && (local == "br" || local == "cr"):
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// parseTable разбирает таблицу в строки с ячейками по столбцам A, B, C...
func parseTable(d *doc, start, end int, p string) Block {
	tbl := Block{Kind: "table"}
	rowNum := 0
	for j := start; j < end; j++ {
		t := d.tags[j]
		local := strings.TrimPrefix(t.Name, p)
		if t.IsOpen() && local == "tr" {
			trEnd := findBalanced(d, j, p+"tr")
			row := diff.Row{Num: rowNum + 1, Cells: map[string]string{}}
			colIdx := 0
			for k := j + 1; k < trEnd; k++ {
				u := d.tags[k]
				uLocal := strings.TrimPrefix(u.Name, p)
				if u.IsOpen() && uLocal == "tc" {
					tcEnd := findBalanced(d, k, p+"tc")
					row.Cells[diff.ColName(colIdx+1)] = cellText(d, k, tcEnd, p)
					colIdx++
					k = tcEnd - 1
				}
			}
			tbl.Rows = append(tbl.Rows, row)
			rowNum++
			j = trEnd - 1
		}
	}
	return tbl
}

// cellText — текст ячейки таблицы (абзацы через пробел).
func cellText(d *doc, start, end int, p string) string {
	var parts []string
	for j := start; j < end; j++ {
		t := d.tags[j]
		local := strings.TrimPrefix(t.Name, p)
		if t.IsOpen() && local == "p" {
			pEnd := findBalanced(d, j, p+"p")
			if s := paraText(d, j, pEnd, p); s != "" {
				parts = append(parts, s)
			}
			j = pEnd - 1
		}
	}
	return strings.Join(parts, " ")
}

// ---------------------------------------------------------------------------
// Мини-сканер (та же логика, что в xlsx, но для docx).
// ---------------------------------------------------------------------------

type doc struct {
	xml  string
	tags []ooxml.Tag
}

func newDoc(xml string) *doc {
	return &doc{xml: xml, tags: ooxml.ScanTags(xml)}
}

func (d *doc) textOf(i int) (string, int) {
	open := d.tags[i]
	if !open.IsOpen() || open.SelfClose {
		return "", i + 1
	}
	name := open.Name
	depth := 0
	for j := i; j < len(d.tags); j++ {
		t := d.tags[j]
		switch {
		case t.IsPaired() && t.Name == name:
			depth++
		case t.IsClose() && t.CloseName() == name:
			depth--
			if depth == 0 {
				return d.xml[open.End:t.Start], j + 1
			}
		}
	}
	return d.xml[open.End:], len(d.tags)
}

func stripMarkup(s string) string {
	if !strings.Contains(s, "<") {
		return s
	}
	var sb strings.Builder
	prev := 0
	for _, t := range ooxml.ScanTags(s) {
		sb.WriteString(s[prev:t.Start])
		prev = t.End
	}
	sb.WriteString(s[prev:])
	return sb.String()
}

// RowText собирает строку таблицы в читаемый вид.
func RowText(cells map[string]string) string {
	keys := make([]string, 0, len(cells))
	for k := range cells {
		keys = append(keys, k)
	}
	// сортировка по индексу столбца
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if diff.ColIndex(keys[j]) < diff.ColIndex(keys[i]) {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, cells[k])
	}
	return strings.Join(parts, " | ")
}

// TableText собирает всю таблицу в читаемый вид.
func TableText(rows []diff.Row) string {
	parts := make([]string, 0, len(rows))
	for _, r := range rows {
		parts = append(parts, RowText(r.Cells))
	}
	return strings.Join(parts, "\n")
}
