// Package ooxml — вспомогательные утилиты для работы с контейнером OOXML
// (zip-архив с XML-частями) без внешних зависимостей.
package ooxml

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Part — одна часть (файл) внутри zip-контейнера.
type Part struct {
	Name string
	Data []byte
}

// Bundle — содержимое OOXML-контейнера с сохранением порядка частей.
type Bundle struct {
	Parts    []Part
	byName   map[string]int
	Comments map[string]bool // имена частей, созданных при чтении (для справки)
}

// OpenBundle читает zip-контейнер целиком в память.
func OpenBundle(r io.ReaderAt, size int64) (*Bundle, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть файл как zip: %w", err)
	}
	b := &Bundle{byName: map[string]int{}, Comments: map[string]bool{}}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		name := strings.TrimPrefix(f.Name, "/")
		b.add(Part{Name: name, Data: data})
	}
	if len(b.Parts) == 0 {
		return nil, errors.New("пустой архив")
	}
	return b, nil
}

func (b *Bundle) add(p Part) {
	if b.byName == nil {
		b.byName = map[string]int{}
	}
	if i, ok := b.byName[p.Name]; ok {
		b.Parts[i] = p
		return
	}
	b.byName[p.Name] = len(b.Parts)
	b.Parts = append(b.Parts, p)
}

// NewBundle создаёт пустой контейнер.
func NewBundle() *Bundle {
	return &Bundle{byName: map[string]int{}}
}

// Set добавляет или заменяет часть.
func (b *Bundle) Set(name string, data []byte) {
	b.add(Part{Name: name, Data: data})
}

// Get возвращает часть по имени.
func (b *Bundle) Get(name string) ([]byte, bool) {
	if i, ok := b.byName[name]; ok {
		return b.Parts[i].Data, true
	}
	return nil, false
}

// GetText возвращает часть как строку.
func (b *Bundle) GetText(name string) (string, bool) {
	d, ok := b.Get(name)
	if !ok {
		return "", false
	}
	return string(d), true
}

// Has сообщает, есть ли часть с таким именем.
func (b *Bundle) Has(name string) bool {
	_, ok := b.byName[name]
	return ok
}

// Names возвращает отсортированный список имён частей.
func (b *Bundle) Names() []string {
	out := make([]string, 0, len(b.Parts))
	for _, p := range b.Parts {
		out = append(out, p.Name)
	}
	sort.Strings(out)
	return out
}

// Bytes сериализует контейнер обратно в zip.
func (b *Bundle) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, p := range b.Parts {
		w, err := zw.Create(p.Name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(p.Data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ---------------------------------------------------------------------------
// Мини-сканер XML-тегов (для точечных правок без переписывания всего файла).
// ---------------------------------------------------------------------------

// Tag — один элемент XML: позиция в строке, имя (с префиксом, в нижнем регистре), флаги.
type Tag struct {
	Name      string // например "w:r" или "w:t" (без слэша)
	SelfClose bool   // <w:r/>
	Closing   bool   // </w:r>
	Start     int    // смещение '<'
	End       int    // смещение после '>'
}

// ScanTags перечисляет все теги в XML-фрагменте.
func ScanTags(s string) []Tag {
	var tags []Tag
	i := 0
	for i < len(s) {
		lt := strings.IndexByte(s[i:], '<')
		if lt < 0 {
			break
		}
		start := i + lt
		if start+1 >= len(s) {
			break
		}
		// Комментарии, CDATA, декларации, инструкции пропускаем.
		c := s[start+1]
		if c == '!' || c == '?' {
			end := strings.IndexByte(s[start:], '>')
			if end < 0 {
				break
			}
			i = start + end + 1
			continue
		}
		// Находим конец тега с учётом кавычек в атрибутах.
		j := start + 1
		inQuote := byte(0)
		for j < len(s) {
			ch := s[j]
			if inQuote != 0 {
				if ch == inQuote {
					inQuote = 0
				}
			} else if ch == '"' || ch == '\'' {
				inQuote = ch
			} else if ch == '>' {
				break
			}
			j++
		}
		if j >= len(s) {
			break
		}
		end := j + 1
		inner := strings.TrimSpace(s[start+1 : j])
		closing := strings.HasPrefix(inner, "/")
		name := strings.TrimPrefix(inner, "/")
		selfClose := strings.HasSuffix(name, "/")
		name = strings.TrimSuffix(name, "/")
		// Имя тега — до первого пробела/таба; нормализуем регистр.
		if sp := strings.IndexAny(name, " \t\r\n"); sp >= 0 {
			name = name[:sp]
		}
		name = strings.ToLower(name)
		tags = append(tags, Tag{Name: name, SelfClose: selfClose, Closing: closing, Start: start, End: end})
		i = end
	}
	return tags
}

// IsOpen возвращает true для начинающего тега (в т.ч. самозакрывающегося).
func (t Tag) IsOpen() bool {
	return !t.Closing
}

// IsClose возвращает true для закрывающего тега (</...>).
func (t Tag) IsClose() bool {
	return t.Closing
}

// IsPaired возвращает true для открывающего тега, имеющего закрывающую пару.
func (t Tag) IsPaired() bool {
	return !t.Closing && !t.SelfClose
}

// CloseName возвращает имя тега (для закрывающих — без слэша).
func (t Tag) CloseName() string {
	return t.Name
}

// ---------------------------------------------------------------------------
// Работа с XML-содержимым.
// ---------------------------------------------------------------------------

// EscapeXML экранирует текст для вставки в XML-элемент или атрибут.
func EscapeXML(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch r {
		case '<':
			sb.WriteString("&lt;")
		case '>':
			sb.WriteString("&gt;")
		case '&':
			sb.WriteString("&amp;")
		case '"':
			sb.WriteString("&quot;")
		case '\'':
			sb.WriteString("&apos;")
		default:
			if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
				sb.WriteString(fmt.Sprintf("&#x%X;", r))
				continue
			}
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// UnescapeXML — обратная операция к EscapeXML (базовые сущности).
func UnescapeXML(s string) string {
	repl := strings.NewReplacer(
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", "\"",
		"&apos;", "'",
		"&amp;", "&",
	)
	return repl.Replace(s)
}

// InsertBefore вставляет вставку перед первым вхождением маркера.
// Если маркер не найден, возвращает исходную строку.
func InsertBefore(s, marker, ins string) string {
	i := strings.Index(s, marker)
	if i < 0 {
		return s
	}
	return s[:i] + ins + s[i:]
}

// InsertAfter вставляет вставку после первого вхождения маркера.
func InsertAfter(s, marker, ins string) string {
	i := strings.Index(s, marker)
	if i < 0 {
		return s
	}
	j := i + len(marker)
	return s[:j] + ins + s[j:]
}
