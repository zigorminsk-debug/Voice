// Package report — формирование самодостаточного HTML-отчёта о сравнении.
// Отчёт не требует сети и JavaScript: его можно открыть в любом браузере
// и распечатать в PDF через «Печать» (Ctrl+P).
package report

import (
	"fmt"
	"strings"
	"time"

	"docdiff/internal/diff"
	"docdiff/internal/ooxml"
)

// Generate создаёт HTML-отчёт по результатам сравнения Excel или Word.
// Принимает *diff.ExcelDiff или *diff.WordDiff.
func Generate(d interface{}) ([]byte, error) {
	switch v := d.(type) {
	case *diff.ExcelDiff:
		return generate("Таблицы Excel: "+v.FileA+" → "+v.FileB, v.Summary, excelBody(v)), nil
	case *diff.WordDiff:
		return generate("Документы Word: "+v.FileA+" → "+v.FileB, v.Summary, wordBody(v)), nil
	default:
		return nil, fmt.Errorf("неподдерживаемый тип отчёта %T", d)
	}
}

func generate(title string, s diff.Summary, body string) []byte {
	var sb strings.Builder
	sb.WriteString(`<!DOCTYPE html>
<html lang="ru"><head><meta charset="UTF-8">`)
	fmt.Fprintf(&sb, `<title>%s</title>`, ooxml.EscapeXML(title))
	sb.WriteString(css)
	sb.WriteString(`</head><body>`)
	sb.WriteString(`<header><h1>Отчёт о сравнении</h1><p class="subtitle">`)
	sb.WriteString(ooxml.EscapeXML(title))
	fmt.Fprintf(&sb, `</p><p class="date">Сформировано: %s · DocDiff</p></header>`,
		time.Now().Format("02.01.2006 15:04"))

	sb.WriteString(`<div class="summary">`)
	fmt.Fprintf(&sb, `<div class="card modified"><div class="num">%d</div><div>Изменено</div></div>`, s.Modified)
	fmt.Fprintf(&sb, `<div class="card added"><div class="num">%d</div><div>Добавлено</div></div>`, s.Added)
	fmt.Fprintf(&sb, `<div class="card deleted"><div class="num">%d</div><div>Удалено</div></div>`, s.Deleted)
	sb.WriteString(`</div>`)

	sb.WriteString(body)

	sb.WriteString(`<footer>DocDiff — локальное сравнение документов</footer>`)
	sb.WriteString(`</body></html>`)
	return []byte(sb.String())
}

// ---------------------------------------------------------------------------

func excelBody(d *diff.ExcelDiff) string {
	var sb strings.Builder
	for _, sh := range d.Sheets {
		status := map[string]string{
			"same":    "без изменений",
			"changed": fmt.Sprintf("%d изменений", len(sh.Changes)),
			"onlyA":   "лист только в оригинале",
			"onlyB":   "лист только в новом файле",
		}[sh.Status]
		fmt.Fprintf(&sb, `<section><h2>%s <span class="muted">— %s</span></h2>`,
			ooxml.EscapeXML(sh.Name), status)

		if len(sh.Changes) == 0 {
			sb.WriteString(`</section>`)
			continue
		}
		sb.WriteString(`<table><thead><tr><th>Тип</th><th>Строка</th><th>Изменения</th></tr></thead><tbody>`)
		for _, ch := range sh.Changes {
			typ, cls := labels(ch.Type)
			pos := ""
			switch ch.Type {
			case diff.Added:
				pos = fmt.Sprintf("B:%d", ch.RowB)
			case diff.Deleted:
				pos = fmt.Sprintf("A:%d", ch.RowA)
			default:
				pos = fmt.Sprintf("%d → %d", ch.RowA, ch.RowB)
			}
			fmt.Fprintf(&sb, `<tr class="%s"><td><span class="badge %s">%s</span></td><td class="rownum">%s</td><td>`,
				cls, cls, typ, pos)
			if ch.Type == diff.Modified {
				if ctx := rowContext(ch); ctx != "" {
					fmt.Fprintf(&sb, `<div class="cell muted">строка: %s</div>`, ctx)
				}
				for _, c := range ch.Cells {
					if c.FormulaOnly {
						fmt.Fprintf(&sb, `<div class="cell"><b>%s</b>: формула <code>=%s</code> → <code>=%s</code> `+
							`<span class="muted">(значение «%s» не изменилось)</span></div>`,
							ooxml.EscapeXML(c.Col), ooxml.EscapeXML(c.OldFormula), ooxml.EscapeXML(c.NewFormula),
							ooxml.EscapeXML(c.Old))
						continue
					}
					fmt.Fprintf(&sb, `<div class="cell"><b>%s</b>: <del>%s</del> → <ins>%s</ins>`,
						ooxml.EscapeXML(c.Col), ooxml.EscapeXML(c.Old), ooxml.EscapeXML(c.New))
					if c.OldFormula != "" || c.NewFormula != "" {
						if c.OldFormula != c.NewFormula {
							fmt.Fprintf(&sb, ` <span class="muted">формула <code>=%s</code> → <code>=%s</code></span>`,
								ooxml.EscapeXML(c.OldFormula), ooxml.EscapeXML(c.NewFormula))
						} else {
							fmt.Fprintf(&sb, ` <span class="muted">формула <code>=%s</code></span>`,
								ooxml.EscapeXML(c.NewFormula))
						}
					}
					sb.WriteString(`</div>`)
				}
			} else {
				vals := ch.New
				if ch.Type == diff.Deleted {
					vals = ch.Old
				}
				keys := sortedKeys(vals)
				for _, k := range keys {
					fmt.Fprintf(&sb, `<span class="kv"><b>%s</b>: %s</span> `,
						ooxml.EscapeXML(k), ooxml.EscapeXML(vals[k]))
				}
			}
			sb.WriteString(`</td></tr>`)
		}
		sb.WriteString(`</tbody></table></section>`)
	}
	return sb.String()
}

func wordBody(d *diff.WordDiff) string {
	var sb strings.Builder
	if len(d.Blocks) == 0 {
		return `<section><p class="muted">Изменений не найдено.</p></section>`
	}
	sb.WriteString(`<section><h2>Изменения документа</h2><div class="blocks">`)
	for _, b := range d.Blocks {
		typ, cls := labels(b.Type)
		kind := "Абзац"
		if b.Kind == "table" {
			kind = "Таблица"
		}
		fmt.Fprintf(&sb, `<div class="block %s"><div class="block-head"><span class="badge %s">%s</span> `+
			`<span class="muted">%s</span></div>`, cls, cls, typ, kind)
		if b.Type == diff.Modified && b.Segments != nil {
			sb.WriteString(`<p class="text">`)
			for _, seg := range b.Segments {
				switch seg.Type {
				case "eq":
					sb.WriteString(ooxml.EscapeXML(seg.Text))
				case "ins":
					fmt.Fprintf(&sb, `<ins>%s</ins>`, ooxml.EscapeXML(seg.Text))
				case "del":
					fmt.Fprintf(&sb, `<del>%s</del>`, ooxml.EscapeXML(seg.Text))
				}
			}
			sb.WriteString(`</p>`)
		} else if b.Type == diff.Added {
			fmt.Fprintf(&sb, `<p class="text"><ins>%s</ins></p>`, ooxml.EscapeXML(b.New))
		} else {
			fmt.Fprintf(&sb, `<p class="text"><del>%s</del></p>`, ooxml.EscapeXML(b.Old))
		}
		if len(b.Rows) > 0 {
			sb.WriteString(`<div class="table-changes">`)
			for _, r := range b.Rows {
				rTyp, _ := labels(r.Type)
				fmt.Fprintf(&sb, `<div>%s, строка %d: `, rTyp, r.RowB)
				if r.Type == diff.Deleted {
					fmt.Fprintf(&sb, `%d: `, r.RowA)
				}
				for i, c := range r.Cells {
					if i > 0 {
						sb.WriteString(`; `)
					}
					fmt.Fprintf(&sb, `%s: <del>%s</del> → <ins>%s</ins>`,
						ooxml.EscapeXML(c.Col), ooxml.EscapeXML(c.Old), ooxml.EscapeXML(c.New))
				}
				sb.WriteString(`</div>`)
			}
			sb.WriteString(`</div>`)
		}
		sb.WriteString(`</div>`)
	}
	sb.WriteString(`</div></section>`)
	return sb.String()
}

// rowContext — идентификация строки по неизменённым столбцам (например, ключу).
func rowContext(ch diff.RowChange) string {
	changed := map[string]bool{}
	for _, c := range ch.Cells {
		changed[c.Col] = true
	}
	src := ch.New
	if len(src) == 0 {
		src = ch.Old
	}
	var parts []string
	for _, k := range sortedKeys(src) {
		if changed[k] || src[k] == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %s", ooxml.EscapeXML(k), ooxml.EscapeXML(src[k])))
	}
	return strings.Join(parts, " · ")
}

func labels(t diff.ChangeType) (string, string) {
	switch t {
	case diff.Added:
		return "Добавлено", "added"
	case diff.Deleted:
		return "Удалено", "deleted"
	default:
		return "Изменено", "modified"
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
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
	return keys
}

const css = `<style>
:root{--ink:#1c2333;--muted:#68718a;--line:#e3e8f2;--modified:#b7791f;--modified-bg:#fff8e6;
--added:#1f7a4d;--added-bg:#e9f9f0;--deleted:#c0392b;--deleted-bg:#fdecea;}
*{box-sizing:border-box}
body{font-family:"Segoe UI",Roboto,Arial,sans-serif;color:var(--ink);margin:0;background:#f4f6fb;}
header{background:#1d2b4f;color:#fff;padding:28px 36px;}
h1{margin:0;font-size:24px}
.subtitle{margin:8px 0 0;opacity:.9}
.date{margin:6px 0 0;font-size:12px;opacity:.7}
.summary{display:flex;gap:16px;padding:24px 36px 0;flex-wrap:wrap}
.card{background:#fff;border-radius:12px;padding:16px 28px;text-align:center;box-shadow:0 2px 8px rgba(0,0,0,.06);border-top:4px solid var(--line);min-width:150px}
.card .num{font-size:30px;font-weight:700}
.card.modified{border-color:var(--modified)}.card.modified .num{color:var(--modified)}
.card.added{border-color:var(--added)}.card.added .num{color:var(--added)}
.card.deleted{border-color:var(--deleted)}.card.deleted .num{color:var(--deleted)}
section{background:#fff;margin:20px 36px;border-radius:12px;padding:20px 24px;box-shadow:0 2px 8px rgba(0,0,0,.06)}
h2{margin:0 0 14px;font-size:17px}
.muted{color:var(--muted);font-weight:400;font-size:13px}
table{width:100%;border-collapse:collapse;font-size:14px}
th{text-align:left;color:var(--muted);font-size:12px;text-transform:uppercase;letter-spacing:.5px;padding:8px 10px;border-bottom:2px solid var(--line)}
td{padding:9px 10px;border-bottom:1px solid var(--line);vertical-align:top}
.rownum{font-weight:600;color:var(--muted);white-space:nowrap}
.badge{display:inline-block;font-size:11px;font-weight:700;padding:3px 10px;border-radius:999px;white-space:nowrap}
.badge.modified{background:var(--modified-bg);color:var(--modified)}
.badge.added{background:var(--added-bg);color:var(--added)}
.badge.deleted{background:var(--deleted-bg);color:var(--deleted)}
.cell{margin:2px 0;line-height:1.6}
.kv{margin-right:14px;white-space:nowrap}
del{color:var(--deleted);background:var(--deleted-bg);text-decoration:line-through;border-radius:3px;padding:0 2px}
ins{color:var(--added);background:var(--added-bg);text-decoration:none;border-radius:3px;padding:0 2px}
code{background:#f0f3fa;padding:1px 5px;border-radius:4px;font-size:12.5px}
.block{border-left:4px solid var(--line);padding:12px 16px;margin:10px 0;border-radius:8px;background:#fbfcff}
.block.modified{border-color:var(--modified)}.block.added{border-color:var(--added)}.block.deleted{border-color:var(--deleted)}
.block-head{margin-bottom:6px;display:flex;gap:10px;align-items:center}
.text{margin:4px 0;line-height:1.65;font-size:14.5px}
.table-changes{margin-top:8px;font-size:13px;color:var(--muted);line-height:1.7}
footer{text-align:center;color:var(--muted);font-size:12px;padding:18px}
@media print{
 body{background:#fff}
 header{background:#1d2b4f !important;-webkit-print-color-adjust:exact;print-color-adjust:exact}
 section,.card{box-shadow:none;border:1px solid var(--line)}
 .badge,del,ins{-webkit-print-color-adjust:exact;print-color-adjust:exact}
 section{break-inside:avoid}
}
</style>`
