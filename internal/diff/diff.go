package diff

import (
	"sort"
	"strings"
	"unicode"
)

// opKind — операция выравнивания последовательностей.
type opKind int

const (
	opEqual opKind = iota
	opDelete
	opInsert
)

type alignOp struct {
	kind opKind
	a, b int // индексы элементов (для delete b=-1, для insert a=-1)
}

// maxLCSCells ограничивает размер таблицы ДП; при превышении
// используется дешёвое попозиционное выравнивание.
const maxLCSCells = 4_000_000

// align выравнивает две последовательности ключей по LCS.
func align(a, b []string) []alignOp {
	// Обрезаем общий префикс и суффикс — типичный случай «немного правок».
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	am, bm := a[p:len(a)-s], b[p:len(b)-s]

	ops := make([]alignOp, 0, p+s+len(am)+len(bm))
	for i := 0; i < p; i++ {
		ops = append(ops, alignOp{opEqual, i, i})
	}

	if len(am) > 0 && len(bm) > 0 && len(am)*len(bm) > maxLCSCells {
		// Попозиционное выравнивание для очень больших блоков.
		n := len(am)
		if len(bm) > n {
			n = len(bm)
		}
		for i := 0; i < n; i++ {
			switch {
			case i >= len(am):
				ops = append(ops, alignOp{opInsert, -1, p + i})
			case i >= len(bm):
				ops = append(ops, alignOp{opDelete, p + i, -1})
			default:
				ops = append(ops, alignOp{opEqual, p + i, p + i})
			}
		}
	} else {
		ops = append(ops, lcsOps(am, bm, p)...)
	}

	for i := 0; i < s; i++ {
		ops = append(ops, alignOp{opEqual, len(a) - s + i, len(b) - s + i})
	}
	return ops
}

// lcsOps — классическое динамическое программирование по LCS.
// Индексы сдвигаются на off.
func lcsOps(a, b []string, off int) []alignOp {
	n, m := len(a), len(b)
	// dp[i][j] — длина LCS a[i:] и b[j:].
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var ops []alignOp
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, alignOp{opEqual, off + i, off + j})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			ops = append(ops, alignOp{opDelete, off + i, -1})
			i++
		default:
			ops = append(ops, alignOp{opInsert, -1, off + j})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, alignOp{opDelete, off + i, -1})
	}
	for ; j < m; j++ {
		ops = append(ops, alignOp{opInsert, -1, off + j})
	}
	return ops
}

// Row — строка таблицы с значениями по столбцам (буква -> значение).
// Forms — формулы по столбцам (буква -> формула без «=»), может быть nil.
type Row struct {
	Num   int
	Cells map[string]string
	Forms map[string]string
}

// rowKey — канонический отпечаток строки для сравнения.
// Пустые значения не учитываются: отсутствующая ячейка и пустая ячейка
// эквивалентны. Формулы учитываются отдельно: изменение формулы при том же
// результате — это изменение строки.
func rowKey(r Row) string {
	cols := make([]string, 0, len(r.Cells))
	for c := range r.Cells {
		if r.Cells[c] == "" {
			continue
		}
		cols = append(cols, c)
	}
	for c := range r.Forms {
		if r.Forms[c] == "" {
			continue
		}
		if _, ok := r.Cells[c]; !ok {
			cols = append(cols, c)
		}
	}
	sortCols(cols)
	var sb strings.Builder
	for _, c := range cols {
		sb.WriteString(c)
		sb.WriteByte('=')
		sb.WriteString(r.Cells[c])
		if f := r.Forms[c]; f != "" {
			sb.WriteByte('\x01')
			sb.WriteString(f)
		}
		sb.WriteByte('\x00')
	}
	return sb.String()
}

// sortCols сортирует буквы столбцов по их числовому индексу (A, B, ..., Z, AA...).
func sortCols(cols []string) {
	sort.Slice(cols, func(i, j int) bool {
		return ColIndex(cols[i]) < ColIndex(cols[j])
	})
}

// ColIndex превращает букву столбца ("A", "AB") в номер (1, 28).
func ColIndex(col string) int {
	n := 0
	for _, r := range col {
		if r >= 'a' && r <= 'z' {
			r -= 'a' - 'A'
		}
		if r < 'A' || r > 'Z' {
			continue
		}
		n = n*26 + int(r-'A'+1)
	}
	return n
}

// ColName превращает номер столбца (1) в букву ("A").
func ColName(n int) string {
	if n < 1 {
		return ""
	}
	s := ""
	for n > 0 {
		n--
		s = string(rune('A'+n%26)) + s
		n /= 26
	}
	return s
}

// greedyPair сопоставляет элементы двух серий (удалённые × добавленные)
// по максимальной схожести: жадно выбираются лучшие пары не ниже порога.
// Возвращает пары (i удалённого -> j добавленного).
func greedyPair(nDel, nIns int, sim func(i, j int) float64, threshold float64) map[int]int {
	pairs := map[int]int{}
	usedDel, usedIns := map[int]bool{}, map[int]bool{}
	for {
		bestI, bestJ, best := -1, -1, threshold-1e-9
		for i := 0; i < nDel; i++ {
			if usedDel[i] {
				continue
			}
			for j := 0; j < nIns; j++ {
				if usedIns[j] {
					continue
				}
				if s := sim(i, j); s > best {
					best, bestI, bestJ = s, i, j
				}
			}
		}
		if bestI < 0 {
			return pairs
		}
		pairs[bestI] = bestJ
		usedDel[bestI] = true
		usedIns[bestJ] = true
	}
}

// similarityRow — доля совпавших значений ячеек (0..1).
func similarityRow(a, b Row) float64 {
	cols := map[string]bool{}
	for c := range a.Cells {
		cols[c] = true
	}
	for c := range b.Cells {
		cols[c] = true
	}
	total, same := 0, 0
	for c := range cols {
		av, bv := a.Cells[c], b.Cells[c]
		if av == "" && bv == "" {
			continue
		}
		total++
		if av == bv {
			same++
		}
	}
	if total == 0 {
		return 1
	}
	return float64(same) / float64(total)
}

// DiffRows сравнивает два набора строк таблицы и возвращает
// только изменённые/добавленные/удалённые строки.
func DiffRows(a, b []Row) []RowChange {
	keysA := make([]string, len(a))
	for i := range a {
		keysA[i] = rowKey(a[i])
	}
	keysB := make([]string, len(b))
	for i := range b {
		keysB[i] = rowKey(b[i])
	}

	ops := align(keysA, keysB)

	var changes []RowChange
	// Проходим по ops и склеиваем соседние delete/insert в modified.
	for i := 0; i < len(ops); {
		switch ops[i].kind {
		case opEqual:
			i++
		case opDelete, opInsert:
			j := i
			var dels, inss []alignOp
			for j < len(ops) && ops[j].kind != opEqual {
				if ops[j].kind == opDelete {
					dels = append(dels, ops[j])
				} else {
					inss = append(inss, ops[j])
				}
				j++
			}
			// Пары «удалён + добавлен» рядом трактуем как изменённые строки,
			// только если содержимое достаточно похоже.
			pairs := greedyPair(len(dels), len(inss), func(i, j int) float64 {
				return similarityRow(a[dels[i].a], b[inss[j].b])
			}, 0.5)
			pairedIns := map[int]bool{}
			for k := 0; k < len(dels); k++ {
				if pj, ok := pairs[k]; ok {
					changes = append(changes, diffRowPair(a[dels[k].a], b[inss[pj].b]))
					pairedIns[pj] = true
				}
			}
			for k := 0; k < len(dels); k++ {
				if _, ok := pairs[k]; ok {
					continue
				}
				r := a[dels[k].a]
				changes = append(changes, RowChange{
					Type: Deleted, RowA: r.Num, Old: cloneCells(r.Cells),
				})
			}
			for k := 0; k < len(inss); k++ {
				if pairedIns[k] {
					continue
				}
				r := b[inss[k].b]
				changes = append(changes, RowChange{
					Type: Added, RowB: r.Num, New: cloneCells(r.Cells),
				})
			}
			i = j
		}
	}
	// Стабильный порядок: по строкам нового файла, затем старого.
	sort.SliceStable(changes, func(i, j int) bool {
		ci, cj := changes[i], changes[j]
		mi, mj := ci.RowB, cj.RowB
		if ci.Type == Deleted {
			mi = ci.RowA
		}
		if cj.Type == Deleted {
			mj = cj.RowA
		}
		return mi < mj
	})
	return changes
}

// diffRowPair сравнивает две строки поячеечно.
func diffRowPair(oldR, newR Row) RowChange {
	rc := RowChange{Type: Modified, RowA: oldR.Num, RowB: newR.Num}
	colSet := map[string]bool{}
	for c := range oldR.Cells {
		colSet[c] = true
	}
	for c := range newR.Cells {
		colSet[c] = true
	}
	for c := range oldR.Forms {
		colSet[c] = true
	}
	for c := range newR.Forms {
		colSet[c] = true
	}
	cols := make([]string, 0, len(colSet))
	for c := range colSet {
		cols = append(cols, c)
	}
	sortCols(cols)
	for _, c := range cols {
		ov, nv := oldR.Cells[c], newR.Cells[c]
		of, nf := oldR.Forms[c], newR.Forms[c]
		if ov != nv || of != nf {
			rc.Cells = append(rc.Cells, CellChange{
				Col: c, Old: ov, New: nv,
				OldFormula: of, NewFormula: nf,
				FormulaOnly: ov == nv,
			})
		}
	}
	rc.Old = cloneCells(oldR.Cells)
	rc.New = cloneCells(newR.Cells)
	return rc
}

// DiffRowsByKey сравнивает строки по значению ключевого столбца, а не по
// порядку: строка считается той же, если совпадает ключ (например, артикул
// или наименование). Строки с одинаковым ключом сопоставляются в порядке
// появления. Пустой keyCol означает столбец "A".
func DiffRowsByKey(a, b []Row, keyCol string) []RowChange {
	if keyCol == "" {
		keyCol = "A"
	}
	keyCol = strings.ToUpper(strings.TrimSpace(keyCol))

	type posRow struct {
		idx int
		r   Row
	}
	byA := map[string][]posRow{}
	for i, r := range a {
		k := r.Cells[keyCol]
		byA[k] = append(byA[k], posRow{i, r})
	}
	used := map[string]int{}
	pairedA := map[int]bool{}

	var changes []RowChange
	// Сначала — всё по порядку строк нового файла.
	for _, r := range b {
		k := r.Cells[keyCol]
		list := byA[k]
		u := used[k]
		if u < len(list) {
			ca := list[u]
			used[k] = u + 1
			pairedA[ca.idx] = true
			if rowKey(ca.r) != rowKey(r) {
				changes = append(changes, diffRowPair(ca.r, r))
			}
		} else {
			changes = append(changes, RowChange{Type: Added, RowB: r.Num, New: cloneCells(r.Cells)})
		}
	}
	// Затем — удалённые строки старого файла (в его порядке).
	for i, r := range a {
		if !pairedA[i] {
			changes = append(changes, RowChange{Type: Deleted, RowA: r.Num, Old: cloneCells(r.Cells)})
		}
	}
	return changes
}

func cloneCells(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// ---------------------------------------------------------------------------
// Построчный диф текста (для абзацев Word).
// ---------------------------------------------------------------------------

// splitTokens режет текст на слова, пробелы и знаки препинания.
func splitTokens(s string) []string {
	var toks []string
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		r := runes[i]
		switch {
		case unicode.IsSpace(r):
			j := i
			for j < len(runes) && unicode.IsSpace(runes[j]) {
				j++
			}
			toks = append(toks, string(runes[i:j]))
			i = j
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
			j := i
			for j < len(runes) && (unicode.IsLetter(runes[j]) || unicode.IsDigit(runes[j]) || runes[j] == '_') {
				j++
			}
			toks = append(toks, string(runes[i:j]))
			i = j
		default:
			toks = append(toks, string(r))
			i++
		}
	}
	return toks
}

// TextSegments возвращает посегментное различие двух строк текста.
func TextSegments(old, new string) []Segment {
	if old == new {
		return []Segment{{Type: "eq", Text: old}}
	}
	toksA, toksB := splitTokens(old), splitTokens(new)
	ops := align(toksA, toksB)

	var segs []Segment
	push := func(typ, text string) {
		if text == "" {
			return
		}
		if n := len(segs); n > 0 && segs[n-1].Type == typ {
			segs[n-1].Text += text
			return
		}
		segs = append(segs, Segment{Type: typ, Text: text})
	}
	for _, op := range ops {
		switch op.kind {
		case opEqual:
			push("eq", toksA[op.a])
		case opDelete:
			push("del", toksA[op.a])
		case opInsert:
			push("ins", toksB[op.b])
		}
	}
	return segs
}
