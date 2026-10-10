package xlsx

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"docdiff/internal/diff"
	"docdiff/internal/ooxml"
)

// CellMark — одна пометка (комментарий + заливка) на ячейке.
type CellMark struct {
	Ref     string // ссылка ячейки, например "B5"
	Text    string // текст комментария
	Color   string // ARGB-цвет заливки, например "FFF2CC"
	Comment bool   // добавлять ли комментарий
}

// SheetMarks — пометки для одного листа.
type SheetMarks struct {
	Sheet string
	Marks []CellMark
}

const (
	// ColorModified — жёлтая заливка изменённых ячеек.
	ColorModified = "FFFFF2CC"
	// ColorAdded — зелёная заливка добавленных строк.
	ColorAdded = "FFC6EFCE"
)

const reportSheetName = "Отчёт о сравнении"

// DiffToMarks превращает результат сравнения в пометки для книги B.
func DiffToMarks(d *diff.ExcelDiff, bookB *Book) []SheetMarks {
	var out []SheetMarks
	for _, sh := range d.Sheets {
		if sh.Status == "onlyA" || sh.Status == "onlyB" {
			continue
		}
		var marks []CellMark
		for _, ch := range sh.Changes {
			switch ch.Type {
			case diff.Modified:
				if len(ch.Cells) == 0 {
					continue
				}
				// Комментарий — на первой изменённой ячейке; заливка — на всех.
				for i, cc := range ch.Cells {
					ref := Ref(cc.Col, ch.RowB)
					m := CellMark{Ref: ref, Color: ColorModified, Comment: i == 0}
					if i == 0 {
						m.Text = fmt.Sprintf("Изменено в строке %d: %s", ch.RowB, describeCells(ch.Cells))
					}
					marks = append(marks, m)
				}
			case diff.Added:
				cols := sortedCols(ch.New)
				for i, col := range cols {
					ref := Ref(col, ch.RowB)
					m := CellMark{Ref: ref, Color: ColorAdded, Comment: i == 0}
					if i == 0 {
						m.Text = fmt.Sprintf("Строка %d добавлена (отсутствует в оригинале): %s", ch.RowB, describeRow(ch.New))
					}
					marks = append(marks, m)
				}
			}
		}
		if len(marks) > 0 {
			out = append(out, SheetMarks{Sheet: sh.Name, Marks: marks})
		}
	}
	return out
}

func describeCells(cells []diff.CellChange) string {
	parts := make([]string, 0, len(cells))
	for _, c := range cells {
		if c.FormulaOnly {
			parts = append(parts, fmt.Sprintf("%s: формула «=%s» → «=%s» (значение %q не изменилось)",
				c.Col, truncate(c.OldFormula, 50), truncate(c.NewFormula, 50), truncate(c.Old, 40)))
			continue
		}
		s := fmt.Sprintf("%s: «%s» → «%s»", c.Col, truncate(c.Old, 60), truncate(c.New, 60))
		if c.OldFormula != "" || c.NewFormula != "" {
			if c.OldFormula != c.NewFormula {
				s += fmt.Sprintf(" (формула: =%s → =%s)", truncate(c.OldFormula, 40), truncate(c.NewFormula, 40))
			} else {
				s += fmt.Sprintf(" (формула: =%s)", truncate(c.NewFormula, 40))
			}
		}
		parts = append(parts, s)
	}
	return truncate(strings.Join(parts, "; "), 400)
}

func describeRow(cells map[string]string) string {
	cols := sortedCols(cells)
	parts := make([]string, 0, len(cols))
	for _, c := range cols {
		parts = append(parts, fmt.Sprintf("%s=«%s»", c, truncate(cells[c], 40)))
	}
	return truncate(strings.Join(parts, ", "), 400)
}

func sortedCols(m map[string]string) []string {
	cols := make([]string, 0, len(m))
	for c := range m {
		cols = append(cols, c)
	}
	sort.Slice(cols, func(i, j int) bool {
		return diff.ColIndex(cols[i]) < diff.ColIndex(cols[j])
	})
	return cols
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ReportRow — строка листа-отчёта.
type ReportRow struct {
	Type, Sheet, RowA, RowB, Details string
}

// BuildReportRows формирует строки отчёта из результата сравнения.
func BuildReportRows(d *diff.ExcelDiff) []ReportRow {
	var rows []ReportRow
	for _, sh := range d.Sheets {
		switch sh.Status {
		case "onlyA":
			rows = append(rows, ReportRow{Type: "Лист удалён", Sheet: sh.Name, Details: "лист присутствует только в оригинале"})
			continue
		case "onlyB":
			rows = append(rows, ReportRow{Type: "Лист добавлен", Sheet: sh.Name, Details: "лист присутствует только в новом файле"})
			continue
		}
		for _, ch := range sh.Changes {
			r := ReportRow{Sheet: sh.Name}
			switch ch.Type {
			case diff.Added:
				r.Type = "Строка добавлена"
				r.RowB = strconv.Itoa(ch.RowB)
				r.Details = describeRow(ch.New)
			case diff.Deleted:
				r.Type = "Строка удалена"
				r.RowA = strconv.Itoa(ch.RowA)
				r.Details = describeRow(ch.Old)
			case diff.Modified:
				r.Type = "Строка изменена"
				r.RowA = strconv.Itoa(ch.RowA)
				r.RowB = strconv.Itoa(ch.RowB)
				r.Details = describeCells(ch.Cells)
			}
			rows = append(rows, r)
		}
	}
	return rows
}

// Mark аннотирует копию книги B: комментарии, заливки и лист-отчёт.
func Mark(bundle *ooxml.Bundle, marks []SheetMarks, report []ReportRow) ([]byte, error) {
	sheetPaths := sheetPartPaths(bundle)

	// --- стили: добавляем заливки и производные xf ---
	st := newStylesEditor(bundle)
	cellStyle := map[string]map[string]int{} // лист -> ref -> индекс стиля
	skippedComments := map[string]bool{}

	for _, sm := range marks {
		path, ok := sheetPaths[sm.Sheet]
		if !ok {
			continue
		}
		styles := map[string]int{}
		for _, m := range sm.Marks {
			idx, err := st.styleFor(m.Color)
			if err != nil {
				// Слишком сложные стили — помечаем только комментариями.
				continue
			}
			styles[m.Ref] = idx
		}
		cellStyle[path] = styles

		// Проверяем, нет ли уже комментариев/фигур на листе.
		xml, _ := bundle.GetText(path)
		hasLegacy := strings.Contains(xml, "<legacyDrawing ") || strings.Contains(xml, "<legacyDrawing>")
		if hasLegacy || sheetHasComments(bundle, path) {
			skippedComments[path] = true
		}
	}

	// --- правка листов ---
	commentSeq := 0
	for _, sm := range marks {
		path, ok := sheetPaths[sm.Sheet]
		if !ok {
			continue
		}
		xml, _ := bundle.GetText(path)
		styles := cellStyle[path]

		// 1) заливка ячеек.
		if len(styles) > 0 {
			xml = applyCellStyle(xml, styles)
		}

		// 2) комментарии.
		if !skippedComments[path] {
			var active []CellMark
			for _, m := range sm.Marks {
				if m.Comment && m.Text != "" {
					active = append(active, m)
				}
			}
			if len(active) > 0 {
				commentSeq++
				commentsPart := fmt.Sprintf("xl/commentsDiff%d.xml", commentSeq)
				vmlPart := fmt.Sprintf("xl/drawings/vmlDrawingDiff%d.vml", commentSeq)
				relC := fmt.Sprintf("rIdDiffC%d", commentSeq)
				relV := fmt.Sprintf("rIdDiffV%d", commentSeq)

				bundle.Set(commentsPart, buildCommentsXML(active))
				bundle.Set(vmlPart, buildCommentsVML(active))

				sheetRelsPath := sheetRelsFor(path)
				relsXML, exists := bundle.GetText(sheetRelsPath)
				if !exists {
					relsXML = relsHeader()
				}
				relEntry := fmt.Sprintf(
					`<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/comments" Target="../%s"/>`+
						`<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/vmlDrawing" Target="../%s"/>`,
					relC, strings.TrimPrefix(commentsPart, "xl/"),
					relV, strings.TrimPrefix(vmlPart, "xl/"),
				)
				relsXML = ooxml.InsertBefore(relsXML, "</Relationships>", relEntry)
				bundle.Set(sheetRelsPath, []byte(relsXML))

				legacy := fmt.Sprintf(`<legacyDrawing xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" r:id="%s"/>`, relV)
				xml = insertLegacyDrawing(xml, legacy)
			}
		}
		bundle.Set(path, []byte(xml))
	}

	// --- стили: сохраняем изменения ---
	if err := st.save(); err != nil {
		return nil, err
	}

	// --- лист-отчёт ---
	if len(report) > 0 {
		if err := addReportSheet(bundle, report); err != nil {
			return nil, err
		}
	}

	// --- типы содержимого ---
	ct, ok := bundle.GetText("[Content_Types].xml")
	if !ok {
		return nil, fmt.Errorf("нет [Content_Types].xml")
	}
	if !strings.Contains(strings.ToLower(ct), `extension="vml"`) {
		ct = ooxml.InsertBefore(ct, "</Types>",
			`<Default Extension="vml" ContentType="application/vnd.openxmlformats-officedocument.vmlDrawing"/>`)
	}
	for i := 1; i <= commentSeq; i++ {
		name := fmt.Sprintf("xl/commentsDiff%d.xml", i)
		ct = ooxml.InsertBefore(ct, "</Types>",
			fmt.Sprintf(`<Override PartName="/%s" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.comments+xml"/>`, name))
	}
	if len(report) > 0 {
		ct = ooxml.InsertBefore(ct, "</Types>",
			`<Override PartName="/xl/worksheets/sheetDiffReport.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`)
	}
	bundle.Set("[Content_Types].xml", []byte(ct))

	return bundle.Bytes()
}

// ---------------------------------------------------------------------------
// Стили: заливки и производные xf.
// ---------------------------------------------------------------------------

type stylesEditor struct {
	bundle   *ooxml.Bundle
	xml      string
	changed  bool
	fillIDs  map[string]int // rgb -> fillId
	nextFill int
	nextXF   int
	xfs      []string // «сырые» xf из cellXfs
}

func newStylesEditor(b *ooxml.Bundle) *stylesEditor {
	se := &stylesEditor{bundle: b, fillIDs: map[string]int{}}
	if xml, ok := b.GetText("xl/styles.xml"); ok {
		se.xml = xml
	} else {
		se.xml = defaultStylesXML()
	}
	se.scan()
	return se
}

func (se *stylesEditor) scan() {
	d := newDoc(se.xml)
	// границы секций
	for _, t := range d.tags {
		switch {
		case t.IsOpen() && t.Name == "fills":
			attrs := tagAttrs(se.xml[t.Start:t.End])
			n, _ := strconv.Atoi(attrs["count"])
			se.nextFill = n
		case t.IsOpen() && t.Name == "cellxfs":
			attrs := tagAttrs(se.xml[t.Start:t.End])
			n, _ := strconv.Atoi(attrs["count"])
			se.nextXF = n
		}
	}
	// существующие fill (для дедупликации не ищем — новые всегда добавляем)
	// существующие xf — копируем «сырыми» строками
	start := strings.Index(se.xml, "<cellXfs")
	end := strings.Index(se.xml, "</cellXfs>")
	if start >= 0 && end > start {
		seg := se.xml[start:end]
		tags := ooxml.ScanTags(seg)
		for i := 0; i < len(tags); i++ {
			t := tags[i]
			if t.IsOpen() && t.Name == "xf" {
				var frag string
				if t.SelfClose {
					frag = seg[t.Start:t.End]
				} else {
					// ищем закрывающий
					depth := 0
					j := i
					for ; j < len(tags); j++ {
						u := tags[j]
						if u.IsPaired() && u.Name == "xf" {
							depth++
						} else if u.IsClose() && u.CloseName() == "xf" {
							depth--
							if depth == 0 {
								break
							}
						}
					}
					endOff := len(seg)
					if j < len(tags) {
						endOff = tags[j].End
					}
					frag = seg[t.Start:endOff]
					i = j
				}
				se.xfs = append(se.xfs, frag)
			}
		}
	}
	if se.nextXF == 0 {
		se.nextXF = len(se.xfs)
	}
}

// styleFor возвращает индекс стиля с заливкой цвета (создаёт при необходимости).
func (se *stylesEditor) styleFor(rgb string) (int, error) {
	if !strings.Contains(se.xml, "</cellXfs>") || !strings.Contains(se.xml, "</fills>") {
		return 0, fmt.Errorf("styles.xml без секций fills/cellXfs")
	}
	fillID, ok := se.fillIDs[rgb]
	if !ok {
		fillID = se.nextFill
		se.nextFill++
		se.fillIDs[rgb] = fillID
		fillXML := fmt.Sprintf(
			`<fill><patternFill patternType="solid"><fgColor rgb="%s"/><bgColor indexed="64"/></patternFill></fill>`, rgb)
		se.xml = ooxml.InsertBefore(se.xml, "</fills>", fillXML)
		se.bumpCount("<fills", se.nextFill)
		se.changed = true
	}

	// производный xf от xf[0] (или пустой)
	base := `<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>`
	if len(se.xfs) > 0 {
		base = se.xfs[0]
	}
	xf := setXFAttrs(base, fillID)
	se.xml = ooxml.InsertBefore(se.xml, "</cellXfs>", xf)
	idx := se.nextXF
	se.nextXF++
	se.bumpCount("<cellXfs", se.nextXF)
	se.changed = true
	return idx, nil
}

// setXFAttrs выставляет fillId/applyFill в «сыром» xf.
func setXFAttrs(xf string, fillID int) string {
	gt := strings.Index(xf, ">")
	if gt < 0 {
		return xf
	}
	open := strings.TrimSpace(xf[:gt])
	rest := xf[gt+1:] // дочерние элементы и </xf> (или пусто)
	selfClose := strings.HasSuffix(open, "/")
	if selfClose {
		open = strings.TrimSuffix(open, "/")
	}
	// убираем старые fillId/applyFill
	open = stripAttr(open, "fillId")
	open = stripAttr(open, "applyFill")
	open = strings.TrimSpace(open)
	open += fmt.Sprintf(` fillId="%d" applyFill="1"`, fillID)
	if selfClose {
		return open + "/>"
	}
	return open + ">" + rest
}

func stripAttr(tag, name string) string {
	lower := strings.ToLower(tag)
	name = strings.ToLower(name)
	for {
		i := strings.Index(lower, name+"=")
		if i < 0 {
			return tag
		}
		// атрибут должен начинаться с пробела
		if i > 0 && tag[i-1] != ' ' && tag[i-1] != '\t' && tag[i-1] != '\n' && tag[i-1] != '\r' {
			// часть другого имени (например borderId для Id) — ищем дальше
			lower = lower[:i] + strings.Repeat("\x00", len(name)) + lower[i+len(name):]
			continue
		}
		j := i + len(name) + 1
		for j < len(tag) && (tag[j] == ' ' || tag[j] == '\t') {
			j++
		}
		if j < len(tag) && (tag[j] == '"' || tag[j] == '\'') {
			q := tag[j]
			j++
			for j < len(tag) && tag[j] != q {
				j++
			}
			j++
		} else {
			for j < len(tag) && tag[j] != ' ' && tag[j] != '\t' && tag[j] != '>' {
				j++
			}
		}
		begin := i
		for begin > 0 && (tag[begin-1] == ' ' || tag[begin-1] == '\t') {
			begin--
		}
		return tag[:begin] + tag[j:]
	}
}

// bumpCount обновляет count="N" в открывающем теге секции.
func (se *stylesEditor) bumpCount(sectionPrefix string, n int) {
	i := strings.Index(se.xml, sectionPrefix)
	if i < 0 {
		return
	}
	j := strings.Index(se.xml[i:], ">")
	if j < 0 {
		return
	}
	open := se.xml[i : i+j]
	newOpen := replaceAttr(open, "count", strconv.Itoa(n))
	se.xml = se.xml[:i] + newOpen + se.xml[i+j:]
}

func replaceAttr(tag, name, value string) string {
	lower := strings.ToLower(tag)
	name = strings.ToLower(name)
	i := strings.Index(lower, name+"=")
	if i > 0 {
		// проверяем границу имени
		if tag[i-1] == ' ' || tag[i-1] == '\t' || tag[i-1] == '\n' || tag[i-1] == '\r' {
			j := i + len(name) + 1
			for j < len(tag) && (tag[j] == ' ' || tag[j] == '\t') {
				j++
			}
			if j < len(tag) && (tag[j] == '"' || tag[j] == '\'') {
				q := tag[j]
				j++
				for j < len(tag) && tag[j] != q {
					j++
				}
				j++
				return tag[:i] + name + `="` + value + `"` + tag[j:]
			}
		}
	}
	// добавляем новый атрибут перед закрывающей частью тега
	trimmed := strings.TrimSuffix(strings.TrimSpace(tag), "/>")
	selfClose := strings.HasSuffix(strings.TrimSpace(tag), "/>")
	trimmed = strings.TrimSuffix(trimmed, ">")
	out := trimmed + fmt.Sprintf(` %s="%s"`, name, value)
	if selfClose {
		return out + "/>"
	}
	return out + ">"
}

func (se *stylesEditor) save() error {
	if se.changed {
		se.bundle.Set("xl/styles.xml", []byte(se.xml))
	}
	return nil
}

func defaultStylesXML() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
		`<fonts count="1"><font><sz val="11"/><name val="Calibri"/></font></fonts>` +
		`<fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>` +
		`<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>` +
		`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
		`<cellXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/></cellXfs>` +
		`</styleSheet>`
}

// ---------------------------------------------------------------------------
// Правка листа: стили ячеек и legacyDrawing.
// ---------------------------------------------------------------------------

// applyCellStyle выставляет атрибут s у перечисленных ячеек.
func applyCellStyle(xml string, styles map[string]int) string {
	tags := ooxml.ScanTags(xml)
	var sb strings.Builder
	prev := 0
	for _, t := range tags {
		if !t.IsOpen() || t.Name != "c" {
			continue
		}
		raw := xml[t.Start:t.End]
		attrs := tagAttrs(raw)
		idx, ok := styles[attrs["r"]]
		if !ok {
			continue
		}
		sb.WriteString(xml[prev:t.Start])
		sb.WriteString(setTagStyle(raw, idx))
		prev = t.End
	}
	sb.WriteString(xml[prev:])
	return sb.String()
}

// setTagStyle заменяет/добавляет атрибут s в теге ячейки.
func setTagStyle(tag string, styleIdx int) string {
	if strings.Contains(strings.ToLower(tag), " s=") {
		return replaceAttr(tag, "s", strconv.Itoa(styleIdx))
	}
	open := strings.TrimSuffix(tag, "/>")
	open = strings.TrimSuffix(strings.TrimSpace(open), ">")
	self := strings.HasSuffix(strings.TrimSpace(tag), "/>")
	out := open + fmt.Sprintf(` s="%d"`, styleIdx)
	if self {
		out += "/>"
	} else {
		out += ">"
	}
	return out
}

// insertLegacyDrawing вставляет <legacyDrawing .../> в правильное место листа.
func insertLegacyDrawing(xml, legacy string) string {
	if strings.Contains(xml, "<legacyDrawing") {
		return xml
	}
	for _, marker := range []string{"<legacyDrawingHF", "<picture", "<oleObjects", "<controls", "<webPublishItems", "<tableParts", "<extLst"} {
		if i := strings.Index(xml, marker); i >= 0 {
			return xml[:i] + legacy + xml[i:]
		}
	}
	return ooxml.InsertBefore(xml, "</worksheet>", legacy)
}

// ---------------------------------------------------------------------------
// Комментарии и VML-фигуры.
// ---------------------------------------------------------------------------

func buildCommentsXML(marks []CellMark) []byte {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	sb.WriteString(`<comments xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	sb.WriteString(`<authors><author>DocDiff</author></authors><commentList>`)
	for _, m := range marks {
		fmt.Fprintf(&sb, `<comment ref="%s" authorId="0"><text><t xml:space="preserve">%s</t></text></comment>`,
			m.Ref, ooxml.EscapeXML(m.Text))
	}
	sb.WriteString(`</commentList></comments>`)
	return []byte(sb.String())
}

func buildCommentsVML(marks []CellMark) []byte {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	sb.WriteString(`<xml xmlns:v="urn:schemas-microsoft-com:vml" xmlns:o="urn:schemas-microsoft-com:office:office" xmlns:x="urn:schemas-microsoft-com:office:excel">`)
	sb.WriteString(`<o:shapelayout v:ext="edit"><o:idmap v:ext="edit" data="1"/></o:shapelayout>`)
	sb.WriteString(`<v:shapetype id="_x0000_t202" coordsize="21600,21600" o:spt="202" path="m,l,21600r21600,l21600,xe">`)
	sb.WriteString(`<v:stroke joinstyle="miter"/><v:path gradientshapeok="t" o:connecttype="rect"/></v:shapetype>`)
	for i, m := range marks {
		col, row := splitRef(m.Ref)
		rowIdx := row - 1
		colIdx := diff.ColIndex(col) - 1
		if rowIdx < 0 {
			rowIdx = 0
		}
		if colIdx < 0 {
			colIdx = 0
		}
		fmt.Fprintf(&sb, `<v:shape id="_x0000_s%d" type="#_x0000_t202" style="position:absolute;margin-left:60pt;margin-top:2pt;width:220pt;height:90pt;z-index:%d;visibility:hidden" fillcolor="#ffffe1" o:insetmode="auto">`, 1024+i, i+1)
		sb.WriteString(`<v:fill color2="#ffffe1"/><v:shadow on="t" color="black" obscured="t"/><v:path o:connecttype="none"/>`)
		sb.WriteString(`<v:textbox style="mso-direction-alt:auto"><div style="text-align:left"/></v:textbox>`)
		fmt.Fprintf(&sb, `<x:ClientData ObjectType="Note"><x:MoveWithCells/><x:SizeWithCells/><x:Anchor>2, 15, 0, 2, 4, 15, 4, 16</x:Anchor><x:AutoFill>False</x:AutoFill><x:Row>%d</x:Row><x:Column>%d</x:Column></x:ClientData>`, rowIdx, colIdx)
		sb.WriteString(`</v:shape>`)
	}
	sb.WriteString(`</xml>`)
	return []byte(sb.String())
}

// ---------------------------------------------------------------------------
// Лист-отчёт.
// ---------------------------------------------------------------------------

func addReportSheet(b *ooxml.Bundle, rows []ReportRow) error {
	// сам лист
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	sb.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`)
	fmt.Fprintf(&sb, `<dimension ref="A1:E%d"/>`, len(rows)+1)
	sb.WriteString(`<sheetViews><sheetView tabSelected="0" workbookViewId="0"/></sheetViews>`)
	sb.WriteString(`<sheetFormatPr defaultRowHeight="15"/>`)
	sb.WriteString(`<cols><col min="1" max="1" width="18" customWidth="1"/><col min="2" max="2" width="22" customWidth="1"/><col min="3" max="4" width="10" customWidth="1"/><col min="5" max="5" width="80" customWidth="1"/></cols>`)
	sb.WriteString(`<sheetData>`)
	headers := []string{"Тип изменения", "Лист", "Строка A", "Строка B", "Детали"}
	sb.WriteString(reportRowXML(1, headers))
	for i, r := range rows {
		sb.WriteString(reportRowXML(i+2, []string{r.Type, r.Sheet, r.RowA, r.RowB, r.Details}))
	}
	sb.WriteString(`</sheetData></worksheet>`)
	b.Set("xl/worksheets/sheetDiffReport.xml", []byte(sb.String()))

	// связь в книге
	relsPath := "xl/_rels/workbook.xml.rels"
	relsXML, ok := b.GetText(relsPath)
	if !ok {
		relsXML = relsHeader()
	}
	relID := uniqueRelID(relsXML, "rIdDiffReport")
	relsXML = ooxml.InsertBefore(relsXML, "</Relationships>",
		fmt.Sprintf(`<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheetDiffReport.xml"/>`, relID))
	b.Set(relsPath, []byte(relsXML))

	// лист в workbook.xml
	wb, ok := b.GetText("xl/workbook.xml")
	if !ok {
		return fmt.Errorf("нет xl/workbook.xml")
	}
	sheetID := 1
	for _, sh := range workbookSheets(wb) {
		_ = sh
		sheetID++
	}
	// точнее: max sheetId
	for _, t := range newDoc(wb).tags {
		if t.IsOpen() && t.Name == "sheet" {
			attrs := tagAttrs(wb[t.Start:t.End])
			if id, err := strconv.Atoi(attrs["sheetid"]); err == nil && id >= sheetID {
				sheetID = id + 1
			}
		}
	}
	entry := fmt.Sprintf(`<sheet name="%s" sheetId="%d" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" r:id="%s"/>`, ooxml.EscapeXML(reportSheetName), sheetID, relID)
	wb = ooxml.InsertBefore(wb, "</sheets>", entry)
	b.Set("xl/workbook.xml", []byte(wb))
	return nil
}

func reportRowXML(n int, cells []string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, `<row r="%d">`, n)
	cols := []string{"A", "B", "C", "D", "E"}
	for i, v := range cells {
		if i >= len(cols) {
			break
		}
		if v == "" {
			continue
		}
		fmt.Fprintf(&sb, `<c r="%s%d" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, cols[i], n, ooxml.EscapeXML(v))
	}
	sb.WriteString(`</row>`)
	return sb.String()
}

func relsHeader() string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"></Relationships>`
}

func uniqueRelID(relsXML, base string) string {
	id := base
	for i := 2; strings.Contains(relsXML, `Id="`+id+`"`); i++ {
		id = fmt.Sprintf("%s%d", base, i)
	}
	return id
}

// sheetPartPaths связывает имена листов с путями их частей.
func sheetPartPaths(b *ooxml.Bundle) map[string]string {
	out := map[string]string{}
	wb, ok := b.GetText("xl/workbook.xml")
	if !ok {
		return out
	}
	rels := parseRels(b, "xl/_rels/workbook.xml.rels")
	for _, sh := range workbookSheets(wb) {
		t := relTarget(rels, sh.RID)
		if t != "" {
			out[sh.Name] = normalizeSheetPath(t)
		}
	}
	return out
}

func sheetRelsFor(sheetPath string) string {
	i := strings.LastIndex(sheetPath, "/")
	if i < 0 {
		return "_rels/" + sheetPath + ".rels"
	}
	return sheetPath[:i] + "/_rels/" + sheetPath[i+1:] + ".rels"
}

func sheetHasComments(b *ooxml.Bundle, sheetPath string) bool {
	rels, ok := b.GetText(sheetRelsFor(sheetPath))
	if !ok {
		return false
	}
	return strings.Contains(rels, "/comments")
}
