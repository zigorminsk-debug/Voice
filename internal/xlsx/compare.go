package xlsx

import "docdiff/internal/diff"

// DiffBooks сравнивает две книги по именам листов и возвращает
// полный результат сравнения (для API и CLI).
// keyCol != "" включает режим сравнения по ключевому столбцу
// (например, "A"); пустая строка — попозиционное сравнение (LCS).
func DiffBooks(a, b *Book, nameA, nameB string, keyCol string) *diff.ExcelDiff {
	d := &diff.ExcelDiff{FileA: nameA, FileB: nameB}
	byName := map[string]*Sheet{}
	for i := range b.Sheets {
		byName[b.Sheets[i].Name] = &b.Sheets[i]
	}
	used := map[string]bool{}

	for i := range a.Sheets {
		sh := &a.Sheets[i]
		used[sh.Name] = true
		sd := diff.SheetDiff{Name: sh.Name}
		target, ok := byName[sh.Name]
		if !ok {
			sd.Status = "onlyA"
			d.Sheets = append(d.Sheets, sd)
			continue
		}
		if keyCol != "" {
			sd.Changes = diff.DiffRowsByKey(sh.Rows, target.Rows, keyCol)
		} else {
			sd.Changes = diff.DiffRows(sh.Rows, target.Rows)
		}
		if len(sd.Changes) > 0 {
			sd.Status = "changed"
		} else {
			sd.Status = "same"
		}
		for _, ch := range sd.Changes {
			d.Summary.Add(ch.Type)
		}
		d.Sheets = append(d.Sheets, sd)
	}
	for i := range b.Sheets {
		if !used[b.Sheets[i].Name] {
			d.Sheets = append(d.Sheets, diff.SheetDiff{Name: b.Sheets[i].Name, Status: "onlyB"})
		}
	}
	return d
}
