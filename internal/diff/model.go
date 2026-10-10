// Package diff содержит общие модели результатов сравнения и алгоритмы
// выравнивания строк/абзацев (LCS) для таблиц Excel и документов Word.
package diff

// ChangeType — тип изменения блока или строки.
type ChangeType string

const (
	// Added — строка/абзац присутствует только в новом файле.
	Added ChangeType = "added"
	// Deleted — строка/абзац присутствует только в старом файле.
	Deleted ChangeType = "deleted"
	// Modified — строка/абзац есть в обоих файлах, но содержимое изменилось.
	Modified ChangeType = "modified"
)

// Summary — сводка изменений.
type Summary struct {
	Added    int `json:"added"`
	Deleted  int `json:"deleted"`
	Modified int `json:"modified"`
	Total    int `json:"total"` // всего зафиксированных изменений
}

func (s *Summary) Add(t ChangeType) {
	switch t {
	case Added:
		s.Added++
	case Deleted:
		s.Deleted++
	case Modified:
		s.Modified++
	}
	s.Total = s.Added + s.Deleted + s.Modified
}

// CellChange — изменение одной ячейки строки таблицы.
type CellChange struct {
	Col         string `json:"col"` // буква столбца, например "B"
	Old         string `json:"old"`
	New         string `json:"new"`
	OldFormula  string `json:"oldFormula,omitempty"`  // формула в старом файле (без «=»)
	NewFormula  string `json:"newFormula,omitempty"`  // формула в новом файле (без «=»)
	FormulaOnly bool   `json:"formulaOnly,omitempty"` // значения совпадают, изменилась формула
}

// RowChange — изменение строки таблицы (Excel или таблицы Word).
type RowChange struct {
	Type  ChangeType        `json:"type"`
	RowA  int               `json:"rowA,omitempty"` // номер строки в старом файле
	RowB  int               `json:"rowB,omitempty"` // номер строки в новом файле
	Old   map[string]string `json:"old,omitempty"`  // столбец -> значение (старый файл)
	New   map[string]string `json:"new,omitempty"`  // столбец -> значение (новый файл)
	Cells []CellChange      `json:"cells,omitempty"`
}

// SheetDiff — различия одного листа Excel.
type SheetDiff struct {
	Name    string      `json:"name"`
	Status  string      `json:"status"` // same | changed | onlyA | onlyB
	Changes []RowChange `json:"changes,omitempty"`
}

// ExcelDiff — результат сравнения двух книг Excel.
type ExcelDiff struct {
	FileA   string      `json:"fileA"`
	FileB   string      `json:"fileB"`
	Summary Summary     `json:"summary"`
	Sheets  []SheetDiff `json:"sheets"`
}

// Segment — фрагмент текста внутри изменённого абзаца.
// Type: eq (без изменений), ins (добавлено), del (удалено).
type Segment struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// BlockChange — изменение смыслового блока документа Word
// (абзац или целиком таблица).
type BlockChange struct {
	Type     ChangeType  `json:"type"`
	Index    int         `json:"index"`            // позиция в новом файле (для удалённых — в старом)
	Anchor   int         `json:"anchor,omitempty"` // для удалённых: позиция вставки пометки в новом файле
	Kind     string      `json:"kind"`             // paragraph | table
	Old      string      `json:"old,omitempty"`
	New      string      `json:"new,omitempty"`
	Segments []Segment   `json:"segments,omitempty"`
	Rows     []RowChange `json:"rows,omitempty"` // для таблиц
}

// WordDiff — результат сравнения двух документов Word.
type WordDiff struct {
	FileA   string        `json:"fileA"`
	FileB   string        `json:"fileB"`
	Summary Summary       `json:"summary"`
	Blocks  []BlockChange `json:"blocks"`
}
