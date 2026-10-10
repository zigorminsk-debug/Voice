package docx

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"docdiff/internal/diff"
)

// Diff сравнивает два документа и возвращает список изменений.
// Для удалённых блоков Anchor содержит позицию в новом документе,
// перед которой следует поместить пометку об удалении.
func Diff(a, b *Document, fileA, fileB string) *diff.WordDiff {
	keysA := blockKeys(a.Blocks)
	keysB := blockKeys(b.Blocks)
	ops := alignBlocks(keysA, keysB)

	out := &diff.WordDiff{FileA: fileA, FileB: fileB}
	bIdx := 0 // сколько блоков B уже пройдено

	for i := 0; i < len(ops); {
		switch ops[i].kind {
		case opEqual:
			bIdx++
			i++
		default:
			// Смешанная группа delete/insert внутри одного прогона.
			j := i
			var dels, inss []blockOp
			for j < len(ops) && ops[j].kind != opEqual {
				if ops[j].kind == opDelete {
					dels = append(dels, ops[j])
				} else {
					inss = append(inss, ops[j])
				}
				j++
			}
			anchor := bIdx
			// k-й добавленный блок занимает позицию anchor+k в новом документе.
			pairs := greedyPairBlocks(len(dels), len(inss), func(x, y int) float64 {
				return blockSimilarity(a.Blocks[dels[x].a], b.Blocks[inss[y].b])
			}, 0.3)
			pairedIns := map[int]bool{}
			for k, jj := range pairs {
				pairedIns[jj] = true
				ca := a.Blocks[dels[k].a]
				cb := b.Blocks[inss[jj].b]
				ch := diff.BlockChange{Type: diff.Modified, Index: anchor + jj, Kind: blockKind(cb)}
				switch cb.Kind {
				case "paragraph":
					ch.Old, ch.New = ca.Text, cb.Text
					ch.Segments = diff.TextSegments(ca.Text, cb.Text)
				default:
					ch.Old, ch.New = TableText(ca.Rows), TableText(cb.Rows)
					ch.Rows = diff.DiffRows(ca.Rows, cb.Rows)
				}
				out.Blocks = append(out.Blocks, ch)
			}
			for k := 0; k < len(dels); k++ {
				if _, ok := pairs[k]; ok {
					continue
				}
				ca := a.Blocks[dels[k].a]
				ch := diff.BlockChange{Type: diff.Deleted, Index: dels[k].a, Kind: blockKind(ca),
					Anchor: deletionAnchor(pairs, k, anchor, len(inss))}
				if ca.Kind == "paragraph" {
					ch.Old = ca.Text
				} else {
					ch.Old = TableText(ca.Rows)
				}
				out.Blocks = append(out.Blocks, ch)
			}
			for k := 0; k < len(inss); k++ {
				if pairedIns[k] {
					continue
				}
				cb := b.Blocks[inss[k].b]
				ch := diff.BlockChange{Type: diff.Added, Index: anchor + k, Kind: blockKind(cb)}
				if cb.Kind == "paragraph" {
					ch.New = cb.Text
				} else {
					ch.New = TableText(cb.Rows)
				}
				out.Blocks = append(out.Blocks, ch)
			}
			bIdx = anchor + len(inss)
			i = j
		}
	}

	// Стабильный порядок вывода — по позиции в новом документе.
	sort.SliceStable(out.Blocks, func(x, y int) bool {
		return blockOrder(out.Blocks[x]) < blockOrder(out.Blocks[y])
	})

	for _, ch := range out.Blocks {
		out.Summary.Add(ch.Type)
	}
	return out
}

// deletionAnchor выбирает позицию в новом документе для пометки об удалении:
// сразу после парного соседа слева или перед парным соседом справа.
func deletionAnchor(pairs map[int]int, k, anchor, nIns int) int {
	bestPrev := -1
	for k2, j2 := range pairs {
		if k2 < k && j2 > bestPrev {
			bestPrev = j2
		}
	}
	if bestPrev >= 0 {
		return anchor + bestPrev + 1
	}
	bestNext := nIns
	for k2, j2 := range pairs {
		if k2 > k && j2 < bestNext {
			bestNext = j2
		}
	}
	return anchor + bestNext
}

// blockOrder — позиция блока для сортировки (по новому документу).
func blockOrder(ch diff.BlockChange) int {
	if ch.Type == diff.Deleted {
		return ch.Anchor*1000 - 1
	}
	return ch.Index * 1000
}

// greedyPairBlocks — жадное сопоставление блоков по схожести.
func greedyPairBlocks(nDel, nIns int, sim func(i, j int) float64, threshold float64) map[int]int {
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

// blockSimilarity — схожесть двух блоков (0..1) по словам.
func blockSimilarity(a, b Block) float64 {
	if a.Kind != b.Kind {
		return 0
	}
	var ta, tb string
	if a.Kind == "table" {
		ta, tb = TableText(a.Rows), TableText(b.Rows)
	} else {
		ta, tb = a.Text, b.Text
	}
	wa := tokenSet(ta)
	wb := tokenSet(tb)
	if len(wa) == 0 && len(wb) == 0 {
		return 1
	}
	common := 0
	for w := range wa {
		if wb[w] {
			common++
		}
	}
	max := len(wa)
	if len(wb) > max {
		max = len(wb)
	}
	if max == 0 {
		return 1
	}
	return float64(common) / float64(max)
}

// tokenSet — множество слов в нижнем регистре.
func tokenSet(s string) map[string]bool {
	out := map[string]bool{}
	var sb strings.Builder
	flush := func() {
		if sb.Len() > 0 {
			out[strings.ToLower(sb.String())] = true
			sb.Reset()
		}
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(unicode.ToLower(r))
		} else {
			flush()
		}
	}
	flush()
	return out
}

func blockKind(b Block) string {
	if b.Kind == "table" {
		return "table"
	}
	return "paragraph"
}

func blockKeys(blocks []Block) []string {
	keys := make([]string, len(blocks))
	for i, b := range blocks {
		if b.Kind == "table" {
			keys[i] = "t\x00" + TableText(b.Rows)
		} else {
			keys[i] = "p\x00" + b.Text
		}
	}
	return keys
}

// --- локальная копия выравнивания (те же типы, что в internal/diff) ---

type blockOp struct {
	kind int // 0 equal, 1 delete, 2 insert
	a, b int
}

const (
	opEqual  = 0
	opDelete = 1
	opInsert = 2
)

func alignBlocks(a, b []string) []blockOp {
	// общий префикс/суффикс
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	am, bm := a[p:len(a)-s], b[p:len(b)-s]

	var ops []blockOp
	for i := 0; i < p; i++ {
		ops = append(ops, blockOp{opEqual, i, i})
	}

	if len(am) > 0 && len(bm) > 0 && len(am)*len(bm) > 4_000_000 {
		n := len(am)
		if len(bm) > n {
			n = len(bm)
		}
		for i := 0; i < n; i++ {
			switch {
			case i >= len(am):
				ops = append(ops, blockOp{opInsert, -1, p + i})
			case i >= len(bm):
				ops = append(ops, blockOp{opDelete, p + i, -1})
			default:
				ops = append(ops, blockOp{opEqual, p + i, p + i})
			}
		}
	} else {
		n, m := len(am), len(bm)
		dp := make([][]int, n+1)
		for i := range dp {
			dp[i] = make([]int, m+1)
		}
		for i := n - 1; i >= 0; i-- {
			for j := m - 1; j >= 0; j-- {
				switch {
				case am[i] == bm[j]:
					dp[i][j] = dp[i+1][j+1] + 1
				case dp[i+1][j] >= dp[i][j+1]:
					dp[i][j] = dp[i+1][j]
				default:
					dp[i][j] = dp[i][j+1]
				}
			}
		}
		i2, j2 := 0, 0
		for i2 < n && j2 < m {
			switch {
			case am[i2] == bm[j2]:
				ops = append(ops, blockOp{opEqual, p + i2, p + j2})
				i2++
				j2++
			case dp[i2+1][j2] >= dp[i2][j2+1]:
				ops = append(ops, blockOp{opDelete, p + i2, -1})
				i2++
			default:
				ops = append(ops, blockOp{opInsert, -1, p + j2})
				j2++
			}
		}
		for ; i2 < n; i2++ {
			ops = append(ops, blockOp{opDelete, p + i2, -1})
		}
		for ; j2 < m; j2++ {
			ops = append(ops, blockOp{opInsert, -1, p + j2})
		}
	}

	for i := 0; i < s; i++ {
		ops = append(ops, blockOp{opEqual, len(a) - s + i, len(b) - s + i})
	}
	return ops
}

// DescribeChange — короткое описание изменения для комментария.
func DescribeChange(ch diff.BlockChange) string {
	switch ch.Type {
	case diff.Added:
		return fmt.Sprintf("Добавлено: «%s»", trunc(ch.New, 250))
	case diff.Deleted:
		return fmt.Sprintf("Удалено: «%s»", trunc(ch.Old, 250))
	default:
		return fmt.Sprintf("Изменено: было «%s», стало «%s»", trunc(ch.Old, 200), trunc(ch.New, 200))
	}
}

func trunc(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
