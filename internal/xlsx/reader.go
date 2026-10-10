// Package xlsx — чтение и аннотирование книг Excel (.xlsx/.xlsm)
// средствами стандартной библиотеки Go (OOXML поверх zip+XML).
package xlsx

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"docdiff/internal/diff"
	"docdiff/internal/ooxml"
)

// Sheet — данные одного листа.
type Sheet struct {
	Name string
	Rows []diff.Row
}

// Book — прочитанная книга.
type Book struct {
	Sheets []Sheet
}

type rel struct {
	ID     string
	Target string
}

type wbSheet struct {
	Name string
	RID  string
}

// Parse читает книгу из OOXML-контейнера.
func Parse(b *ooxml.Bundle) (*Book, error) {
	shared := parseSharedStrings(b)
	dateStyles := parseDateStyles(b)

	wb, ok := b.GetText("xl/workbook.xml")
	if !ok {
		return nil, fmt.Errorf("в файле нет xl/workbook.xml — это не книга Excel")
	}
	rels := parseRels(b, "xl/_rels/workbook.xml.rels")

	book := &Book{}
	for _, sh := range workbookSheets(wb) {
		target := relTarget(rels, sh.RID)
		if target == "" {
			continue
		}
		path := normalizeSheetPath(target)
		data, ok := b.GetText(path)
		if !ok {
			continue
		}
		rows := parseSheet(data, shared, dateStyles)
		book.Sheets = append(book.Sheets, Sheet{Name: sh.Name, Rows: rows})
	}
	if len(book.Sheets) == 0 {
		return nil, fmt.Errorf("в книге не найдено ни одного листа")
	}
	return book, nil
}

// ---------------------------------------------------------------------------
// Разбор XML с одним предварительным проходом по тегам.
// ---------------------------------------------------------------------------

// doc — строка XML с заранее посчитанным списком тегов.
type doc struct {
	xml  string
	tags []ooxml.Tag
}

func newDoc(xml string) *doc {
	return &doc{xml: xml, tags: ooxml.ScanTags(xml)}
}

// textOf возвращает текст элемента (без вложенных тегов) и индекс
// следующего тега после закрывающего. i — индекс открывающего тега.
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

// innerText собирает текст элемента, включая вложенные <t> (rich text).
func (d *doc) innerText(i int) (string, int) {
	open := d.tags[i]
	if !open.IsOpen() || open.SelfClose {
		return "", i + 1
	}
	name := open.Name
	depth := 0
	var sb strings.Builder
	for j := i; j < len(d.tags); {
		t := d.tags[j]
		switch {
		case t.IsPaired() && t.Name == name:
			depth++
			j++
		case t.IsClose() && t.CloseName() == name:
			depth--
			if depth == 0 {
				return sb.String(), j + 1
			}
			j++
		case t.IsOpen() && t.Name == "t" && !t.SelfClose && depth >= 1:
			txt, nj := d.textOf(j)
			sb.WriteString(stripMarkup(txt))
			j = nj
		default:
			j++
		}
	}
	return sb.String(), len(d.tags)
}

// stripMarkup убирает теги, оставляя только текст (для фрагментов rich text).
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

// ---------------------------------------------------------------------------
// Атрибуты.
// ---------------------------------------------------------------------------

// tagAttrs разбирает атрибуты тега; имена нормализуются к нижнему регистру.
func tagAttrs(tag string) map[string]string {
	out := map[string]string{}
	i := strings.IndexAny(tag, " \t\r\n>")
	if i < 0 {
		return out
	}
	rest := tag[i+1:]
	rest = strings.TrimSpace(rest)
	rest = strings.TrimSuffix(rest, "/>")
	rest = strings.TrimSuffix(rest, ">")
	j := 0
	for j < len(rest) {
		for j < len(rest) && (rest[j] == ' ' || rest[j] == '\t' || rest[j] == '\r' || rest[j] == '\n') {
			j++
		}
		if j >= len(rest) {
			break
		}
		k := j
		for k < len(rest) && rest[k] != '=' && rest[k] != ' ' && rest[k] != '\t' {
			k++
		}
		name := strings.ToLower(rest[j:k])
		j = k
		for j < len(rest) && (rest[j] == ' ' || rest[j] == '\t') {
			j++
		}
		if j >= len(rest) || rest[j] != '=' {
			out[name] = ""
			continue
		}
		j++
		for j < len(rest) && (rest[j] == ' ' || rest[j] == '\t') {
			j++
		}
		if j >= len(rest) {
			break
		}
		quote := rest[j]
		if quote != '"' && quote != '\'' {
			k = j
			for k < len(rest) && rest[k] != ' ' && rest[k] != '\t' {
				k++
			}
			out[name] = ooxml.UnescapeXML(rest[j:k])
			j = k
			continue
		}
		j++
		k = j
		for k < len(rest) && rest[k] != quote {
			k++
		}
		out[name] = ooxml.UnescapeXML(rest[j:k])
		j = k + 1
	}
	return out
}

// ---------------------------------------------------------------------------
// Книга: листы, связи, общие строки, стили.
// ---------------------------------------------------------------------------

func workbookSheets(xml string) []wbSheet {
	d := newDoc(xml)
	var out []wbSheet
	for i := 0; i < len(d.tags); i++ {
		t := d.tags[i]
		if t.IsOpen() && t.Name == "sheet" {
			attrs := tagAttrs(xml[t.Start:t.End])
			id := attrs["r:id"]
			if id == "" {
				id = attrs["id"]
			}
			out = append(out, wbSheet{Name: ooxml.UnescapeXML(attrs["name"]), RID: id})
		}
	}
	return out
}

func parseRels(b *ooxml.Bundle, path string) []rel {
	xml, ok := b.GetText(path)
	if !ok {
		return nil
	}
	d := newDoc(xml)
	var out []rel
	for _, t := range d.tags {
		if t.IsOpen() && t.Name == "relationship" {
			attrs := tagAttrs(xml[t.Start:t.End])
			out = append(out, rel{ID: attrs["id"], Target: attrs["target"]})
		}
	}
	return out
}

func relTarget(rels []rel, id string) string {
	for _, r := range rels {
		if strings.EqualFold(r.ID, id) {
			return r.Target
		}
	}
	return ""
}

func normalizeSheetPath(target string) string {
	t := strings.TrimPrefix(target, "/")
	t = strings.ReplaceAll(t, "\\", "/")
	if strings.HasPrefix(t, "xl/") {
		return t
	}
	return "xl/" + t
}

func parseSharedStrings(b *ooxml.Bundle) []string {
	xml, ok := b.GetText("xl/sharedStrings.xml")
	if !ok {
		return nil
	}
	d := newDoc(xml)
	var out []string
	for i := 0; i < len(d.tags); i++ {
		t := d.tags[i]
		if t.IsOpen() && t.Name == "si" {
			txt, ni := d.innerText(i)
			out = append(out, ooxml.UnescapeXML(txt))
			i = ni - 1
		}
	}
	return out
}

// parseDateStyles вычисляет множество индексов стилей с форматом даты.
func parseDateStyles(b *ooxml.Bundle) map[int]bool {
	out := map[int]bool{}
	xml, ok := b.GetText("xl/styles.xml")
	if !ok {
		return out
	}
	custom := map[int]string{}
	d := newDoc(xml)
	for _, t := range d.tags {
		if t.IsOpen() && t.Name == "numfmt" {
			attrs := tagAttrs(xml[t.Start:t.End])
			id, _ := strconv.Atoi(attrs["numfmtid"])
			custom[id] = attrs["formatcode"]
		}
	}
	xfIndex := -1
	inXfs := false
	for _, t := range d.tags {
		switch {
		case t.IsOpen() && t.Name == "cellxfs":
			inXfs = true
		case t.IsClose() && t.CloseName() == "cellxfs":
			inXfs = false
		case inXfs && t.IsOpen() && t.Name == "xf":
			xfIndex++
			attrs := tagAttrs(xml[t.Start:t.End])
			id, _ := strconv.Atoi(attrs["numfmtid"])
			if isDateFormatID(id, custom[id]) {
				out[xfIndex] = true
			}
		}
	}
	return out
}

// isDateFormatID — встроенные и пользовательские форматы даты/времени.
func isDateFormatID(id int, code string) bool {
	switch id {
	case 14, 15, 16, 17, 18, 19, 20, 21, 22, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 45, 46, 47, 50, 51, 52, 53, 54, 55, 56, 57, 58:
		return true
	}
	if code == "" {
		return false
	}
	inQuote := false
	for i := 0; i < len(code); i++ {
		c := code[i]
		switch {
		case c == '"':
			inQuote = !inQuote
		case inQuote:
		case c == 'y' || c == 'Y' || c == 'd' || c == 'D' || c == 'h' || c == 'H' || c == 's' || c == 'S':
			return true
		case c == 'm' || c == 'M':
			return true
		}
	}
	return false
}

func serialToDate(v float64) time.Time {
	base := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
	return base.Add(time.Duration(v * 24 * float64(time.Hour)))
}

func formatSerial(v string, isDate bool) string {
	if !isDate {
		return normalizeNumber(v)
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return v
	}
	t := serialToDate(f)
	if f == float64(int64(f)) {
		return t.Format("02.01.2006")
	}
	return t.Format("02.01.2006 15:04:05")
}

// normalizeNumber делает "1.0" и "1" одинаковыми при сравнении.
func normalizeNumber(v string) string {
	if v == "" {
		return v
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return v
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// ---------------------------------------------------------------------------
// Лист.
// ---------------------------------------------------------------------------

func parseSheet(xml string, shared []string, dateStyles map[int]bool) []diff.Row {
	d := newDoc(xml)
	var rows []diff.Row

	rowNum := 0
	var cells map[string]string
	var forms map[string]string

	flushRow := func() {
		if len(cells) > 0 {
			r := diff.Row{Num: rowNum, Cells: cells}
			if len(forms) > 0 {
				r.Forms = forms
			}
			rows = append(rows, r)
		}
		cells = nil
		forms = nil
	}

	for i := 0; i < len(d.tags); i++ {
		t := d.tags[i]
		switch {
		case t.IsOpen() && t.Name == "row":
			flushRow()
			attrs := tagAttrs(xml[t.Start:t.End])
			rowNum = 0
			if r, err := strconv.Atoi(attrs["r"]); err == nil {
				rowNum = r
			}
			cells = map[string]string{}
			forms = map[string]string{}

		case t.IsClose() && t.CloseName() == "row":
			flushRow()
			rowNum++

		case t.IsOpen() && t.Name == "c":
			attrs := tagAttrs(xml[t.Start:t.End])
			ref := attrs["r"]
			typ := attrs["t"]
			style := -1
			if s, err := strconv.Atoi(attrs["s"]); err == nil {
				style = s
			}
			col, rnum := splitRef(ref)
			if cells == nil {
				cells = map[string]string{}
			}
			if rnum > 0 && rowNum == 0 {
				rowNum = rnum
			}
			if t.SelfClose {
				continue
			}
			// Собираем содержимое ячейки до </c>.
			var raw, formula strings.Builder
			inIS := false
			j := i + 1
			for ; j < len(d.tags); j++ {
				u := d.tags[j]
				if u.IsClose() && u.CloseName() == "c" {
					break
				}
				switch {
				case u.IsOpen() && u.Name == "v" && !u.SelfClose:
					txt, nj := d.textOf(j)
					raw.WriteString(strings.TrimSpace(stripMarkup(txt)))
					j = nj - 1
				case u.IsOpen() && u.Name == "f" && !u.SelfClose:
					txt, nj := d.textOf(j)
					formula.WriteString(strings.TrimSpace(stripMarkup(txt)))
					j = nj - 1
				case u.IsOpen() && u.Name == "is":
					inIS = true
				case u.IsClose() && u.CloseName() == "is":
					inIS = false
				case u.IsOpen() && u.Name == "t" && !u.SelfClose:
					if inIS || typ == "inlineStr" || typ == "str" {
						txt, nj := d.textOf(j)
						raw.WriteString(stripMarkup(txt))
						j = nj - 1
					}
				}
			}
			i = j
			f := strings.TrimPrefix(strings.TrimSpace(formula.String()), "=")
			val := cellValue(typ, raw.String(), style, dateStyles, shared)
			if val == "" && f != "" {
				val = "=" + f
			}
			if f != "" && col != "" {
				if forms == nil {
					forms = map[string]string{}
				}
				forms[col] = f
			}
			if val != "" && col != "" {
				if cells == nil {
					cells = map[string]string{}
				}
				cells[col] = val
			}
		}
	}
	flushRow()
	return rows
}

func cellValue(typ, raw string, style int, dateStyles map[int]bool, shared []string) string {
	raw = strings.TrimSpace(raw)
	switch typ {
	case "s":
		idx, err := strconv.Atoi(raw)
		if err != nil || idx < 0 || idx >= len(shared) {
			return ""
		}
		return shared[idx]
	case "inlineStr", "str":
		return ooxml.UnescapeXML(raw)
	case "b":
		if raw == "1" {
			return "TRUE"
		}
		return "FALSE"
	case "e":
		return ooxml.UnescapeXML(raw)
	default:
		return formatSerial(raw, dateStyles[style])
	}
}

// splitRef разбирает ссылку "B5" на столбец и номер строки.
func splitRef(ref string) (col string, row int) {
	if ref == "" {
		return "", 0
	}
	i := 0
	for i < len(ref) && (ref[i] < '0' || ref[i] > '9') {
		i++
	}
	col = strings.ToUpper(ref[:i])
	if i < len(ref) {
		row, _ = strconv.Atoi(ref[i:])
	}
	return col, row
}

// Ref строит ссылку ячейки: ColName + номер строки.
func Ref(col string, row int) string {
	return col + strconv.Itoa(row)
}
