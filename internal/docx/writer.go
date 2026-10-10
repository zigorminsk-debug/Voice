package docx

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"docdiff/internal/diff"
	"docdiff/internal/ooxml"
)

// Цвета подсветки Word.
const (
	hlModified = "yellow"
	hlAdded    = "green"
	hlDeleted  = "red"
)

const maxComments = 1500

type blockSpan struct {
	kind  string // paragraph | table
	start int    // смещение '<'
	end   int    // смещение после закрывающего тега
}

type commentEntry struct {
	id   int
	text string
}

// edit — правка документа: заменить смещения [start,end) на text.
type edit struct {
	start, end int
	text       string
}

// Mark аннотирует копию документа B: комментарии и подсветка изменений.
func Mark(bundle *ooxml.Bundle, d *diff.WordDiff) ([]byte, error) {
	xml, ok := bundle.GetText("word/document.xml")
	if !ok {
		return nil, fmt.Errorf("в файле нет word/document.xml")
	}
	p := detectW(xml)
	spans := bodySpans(xml, p)

	var comments []commentEntry
	nextID := 0
	newComment := func(text string) int {
		if nextID >= maxComments {
			return -1
		}
		id := nextID
		nextID++
		comments = append(comments, commentEntry{id, text})
		return id
	}

	var edits []edit

	for _, ch := range d.Blocks {
		text := DescribeChange(ch)
		switch ch.Type {
		case diff.Modified, diff.Added:
			if ch.Index < 0 || ch.Index >= len(spans) {
				continue
			}
			sp := spans[ch.Index]
			id := newComment(text)
			var repl string
			if ch.Kind == "table" && ch.Type == diff.Modified {
				// В таблице подсвечиваем только изменённые/добавленные строки.
				repl = highlightTableFragment(xml[sp.start:sp.end], p, ch.Rows)
			} else {
				color := hlModified
				if ch.Type == diff.Added {
					color = hlAdded
				}
				repl = highlightFragment(xml[sp.start:sp.end], p, color)
			}
			var frag string
			if id >= 0 {
				frag = fmt.Sprintf(`<%scommentRangeStart %sid="%d"/>`, p, p, id) + repl +
					fmt.Sprintf(`<%scommentRangeEnd %sid="%d"/><%sr><%scommentReference %sid="%d"/></%sr>`, p, p, id, p, p, p, id, p)
			} else {
				frag = repl
			}
			edits = append(edits, edit{sp.start, sp.end, frag})

		case diff.Deleted:
			id := newComment(text)
			ins := deletedParagraph(p, id, ch.Old)
			pos := -1
			if ch.Anchor >= 0 && ch.Anchor < len(spans) {
				pos = spans[ch.Anchor].start
			}
			if pos < 0 {
				pos = insertAtEnd(xml, p)
			}
			edits = append(edits, edit{pos, pos, ins})
		}
	}

	// Применяем правки от конца к началу — смещения не сдвигаются.
	sort.SliceStable(edits, func(i, j int) bool {
		if edits[i].start != edits[j].start {
			return edits[i].start > edits[j].start
		}
		return edits[i].end > edits[j].end
	})
	for _, e := range edits {
		xml = xml[:e.start] + e.text + xml[e.end:]
	}

	bundle.Set("word/document.xml", []byte(xml))

	if len(comments) > 0 {
		if err := writeComments(bundle, p, comments); err != nil {
			return nil, err
		}
	}
	return bundle.Bytes()
}

// bodySpans находит блоки верхнего уровня с их смещениями.
func bodySpans(xml, p string) []blockSpan {
	d := newDoc(xml)
	var spans []blockSpan
	inBody := false
	for i := 0; i < len(d.tags); i++ {
		t := d.tags[i]
		local := strings.TrimPrefix(t.Name, p)
		isW := strings.HasPrefix(t.Name, p) || (p == "" && !strings.Contains(t.Name, ":"))
		if !isW {
			continue
		}
		switch {
		case t.IsOpen() && local == "body":
			inBody = true
		case t.IsClose() && local == "body":
			inBody = false
		case !inBody:
		case t.IsOpen() && local == "tbl":
			end := blockEnd(d, i, p+"tbl")
			spans = append(spans, blockSpan{kind: "table", start: t.Start, end: d.tags[end-1].End})
			i = end - 1
		case t.IsOpen() && local == "p":
			end := blockEnd(d, i, p+"p")
			spans = append(spans, blockSpan{kind: "paragraph", start: t.Start, end: d.tags[end-1].End})
			i = end - 1
		}
	}
	return spans
}

// insertAtEnd — смещение для вставки в конец тела документа.
func insertAtEnd(xml, p string) int {
	if i := strings.Index(xml, "<"+p+"sectPr"); i >= 0 {
		return i
	}
	if i := strings.Index(xml, "</"+p+"body>"); i >= 0 {
		return i
	}
	return len(xml)
}

// highlightFragment подсвечивает runs фрагмента и возвращает его копию.
func highlightFragment(frag, p, color string) string {
	runName := p + "r"
	rprName := p + "rpr"
	tags := ooxml.ScanTags(frag)
	var sb strings.Builder
	prev := 0
	for i := 0; i < len(tags); i++ {
		t := tags[i]
		if !t.IsOpen() || t.Name != runName || t.SelfClose {
			continue
		}
		sb.WriteString(frag[prev:t.End])
		prev = t.End
		hasRPr := i+1 < len(tags) && tags[i+1].IsOpen() && tags[i+1].Name == rprName && !tags[i+1].SelfClose
		if hasRPr {
			end := findTagEnd(tags, i+1, rprName)
			rpr := tags[i+1]
			rprClose := tags[end-1]
			rprXML := frag[rpr.Start:rprClose.End]
			if !strings.Contains(rprXML, "highlight") {
				ins := fmt.Sprintf(`<%shighlight %sval="%s"/>`, p, p, color)
				sb.WriteString(frag[rpr.Start:rpr.End])
				sb.WriteString(ins)
				sb.WriteString(frag[rpr.End:rprClose.Start])
				sb.WriteString(frag[rprClose.Start:rprClose.End])
			} else {
				sb.WriteString(frag[rpr.Start:rprClose.End])
			}
			prev = rprClose.End
			i = end - 1
		} else {
			fmt.Fprintf(&sb, `<%srPr><%shighlight %sval="%s"/></%srPr>`, p, p, p, color, p)
		}
	}
	sb.WriteString(frag[prev:])
	return sb.String()
}

// findTagEnd возвращает индекс тега после закрывающего парного (по списку тегов).
func findTagEnd(tags []ooxml.Tag, start int, name string) int {
	depth := 0
	for j := start; j < len(tags); j++ {
		t := tags[j]
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
	return len(tags)
}

// highlightTableFragment подсвечивает только изменённые строки таблицы.
func highlightTableFragment(frag, p string, rows []diff.RowChange) string {
	colors := map[int]string{}
	for _, rc := range rows {
		switch rc.Type {
		case diff.Modified:
			colors[rc.RowB] = hlModified
		case diff.Added:
			colors[rc.RowB] = hlAdded
		}
	}
	tags := ooxml.ScanTags(frag)
	type span struct{ start, end, row int }
	var spans []span
	row := 0
	for i := 0; i < len(tags); i++ {
		t := tags[i]
		if t.IsOpen() && t.Name == p+"tr" && !t.SelfClose {
			row++
			end := findTagEnd(tags, i, p+"tr")
			spans = append(spans, span{t.Start, tags[end-1].End, row})
			i = end - 1
		}
	}
	out := frag
	for i := len(spans) - 1; i >= 0; i-- {
		sp := spans[i]
		color, ok := colors[sp.row]
		if !ok {
			continue
		}
		sub := highlightFragment(out[sp.start:sp.end], p, color)
		out = out[:sp.start] + sub + out[sp.end:]
	}
	return out
}

// deletedParagraph создаёт абзац-пометку об удалённом содержимом.
func deletedParagraph(p string, id int, text string) string {
	esc := ooxml.EscapeXML(trunc(text, 500))
	var sb strings.Builder
	fmt.Fprintf(&sb, `<%sp><%spPr><%srPr><%shighlight %sval="%s"/></%srPr></%spPr>`, p, p, p, p, p, hlDeleted, p, p)
	if id >= 0 {
		fmt.Fprintf(&sb, `<%scommentRangeStart %sid="%d"/>`, p, p, id)
	}
	fmt.Fprintf(&sb, `<%sr><%srPr><%sstrike/><%scolor %sval="C00000"/></%srPr><%st xml:space="preserve">[Удалено] %s</%st></%sr>`,
		p, p, p, p, p, p, p, esc, p, p)
	if id >= 0 {
		fmt.Fprintf(&sb, `<%scommentRangeEnd %sid="%d"/><%sr><%scommentReference %sid="%d"/></%sr>`, p, p, id, p, p, p, id, p)
	}
	fmt.Fprintf(&sb, `</%sp>`, p)
	return sb.String()
}

// writeComments создаёт или дополняет word/comments.xml и связи.
func writeComments(bundle *ooxml.Bundle, docPrefix string, comments []commentEntry) error {
	const part = "word/comments.xml"
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")

	commentXML := func(p string, c commentEntry) string {
		return fmt.Sprintf(
			`<%scomment %sid="%d" %sauthor="DocDiff" %sdate="%s" %sinitials="DD">`+
				`<%sp><%sr><%st xml:space="preserve">%s</%st></%sr></%sp></%scomment>`,
			p, p, c.id, p, p, now, p, p, p, p, ooxml.EscapeXML(c.text), p, p, p, p)
	}

	if xml, ok := bundle.GetText(part); ok {
		// Слияние с существующими комментариями.
		p := detectW(xml)
		var sb strings.Builder
		for _, c := range comments {
			sb.WriteString(commentXML(p, c))
		}
		xml = ooxml.InsertBefore(xml, "</"+p+"comments>", sb.String())
		bundle.Set(part, []byte(xml))
		return nil
	}

	p := docPrefix
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	if p == "" {
		sb.WriteString(`<comments xmlns="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`)
	} else {
		fmt.Fprintf(&sb, `<%scomments xmlns:%s="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`, p, strings.TrimSuffix(p, ":"))
	}
	for _, c := range comments {
		sb.WriteString(commentXML(p, c))
	}
	fmt.Fprintf(&sb, `</%scomments>`, p)
	bundle.Set(part, []byte(sb.String()))

	// Content_Types
	ct, ok := bundle.GetText("[Content_Types].xml")
	if !ok {
		return fmt.Errorf("нет [Content_Types].xml")
	}
	if !strings.Contains(ct, `PartName="/word/comments.xml"`) {
		ct = ooxml.InsertBefore(ct, "</Types>",
			`<Override PartName="/word/comments.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.comments+xml"/>`)
		bundle.Set("[Content_Types].xml", []byte(ct))
	}

	// Связи document.xml.
	relsPath := "word/_rels/document.xml.rels"
	rels, ok := bundle.GetText(relsPath)
	if !ok {
		rels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`
	}
	if !strings.Contains(rels, "/comments") {
		id := "rIdDiffComments"
		for i := 2; strings.Contains(rels, `Id="`+id+`"`); i++ {
			id = fmt.Sprintf("rIdDiffComments%d", i)
		}
		rels = ooxml.InsertBefore(rels, "</Relationships>",
			fmt.Sprintf(`<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/comments" Target="comments.xml"/>`, id))
		bundle.Set(relsPath, []byte(rels))
	}
	return nil
}
