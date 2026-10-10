// Package app — HTTP-сервер с HTML-интерфейсом и API сравнения документов.
package app

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"docdiff/internal/docx"
	"docdiff/internal/ooxml"
	"docdiff/internal/report"
	"docdiff/internal/xlsx"
)

//go:embed web/*
var webFS embed.FS

const (
	maxUploadSize = 100 << 20 // 100 МБ на файл
	resultTTL     = 30 * time.Minute
)

type result struct {
	data     []byte
	filename string
	diff     interface{} // *diff.ExcelDiff | *diff.WordDiff
	typ      string      // excel | word
	created  time.Time
}

type sessionStore struct {
	mu    sync.Mutex
	items map[string]result
}

func newStore() *sessionStore {
	s := &sessionStore{items: map[string]result{}}
	go s.cleanup()
	return s
}

func (s *sessionStore) put(data []byte, filename string, diffValue interface{}, typ string) string {
	id := newID()
	s.mu.Lock()
	s.items[id] = result{data: data, filename: filename, diff: diffValue, typ: typ, created: time.Now()}
	s.mu.Unlock()
	return id
}

func (s *sessionStore) get(id string) (result, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.items[id]
	return r, ok
}

func (s *sessionStore) cleanup() {
	for range time.Tick(5 * time.Minute) {
		s.mu.Lock()
		for k, v := range s.items {
			if time.Since(v.created) > resultTTL {
				delete(s.items, k)
			}
		}
		s.mu.Unlock()
	}
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Server — HTTP-приложение DocDiff.
type Server struct {
	store *sessionStore
	mux   *http.ServeMux
}

// New создаёт сервер приложения.
func New() *Server {
	s := &Server{store: newStore(), mux: http.NewServeMux()}
	s.routes()
	return s
}

// Handler возвращает http.Handler сервера.
func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) routes() {
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/static/", s.handleStatic)
	s.mux.HandleFunc("/api/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	s.mux.HandleFunc("/api/compare", s.handleCompare)
	s.mux.HandleFunc("/api/download", s.handleDownload)
	s.mux.HandleFunc("/api/report", s.handleReport)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		http.Error(w, "index.html не найден", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/static/")
	if strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(webFS, "web/"+name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch filepath.Ext(name) {
	case ".js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	}
	_, _ = w.Write(data)
}

// ---------------------------------------------------------------------------
// API
// ---------------------------------------------------------------------------

type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, apiError{Error: msg})
}

// compareResponse — ответ /api/compare.
type compareResponse struct {
	ID   string      `json:"id"`
	Type string      `json:"type"` // excel | word
	Diff interface{} `json:"diff"`
	Name string      `json:"name"` // имя выходного файла с комментариями
}

func (s *Server) handleCompare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "используйте POST")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize*2+10<<20)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		writeErr(w, http.StatusBadRequest, "файлы слишком велики или форма некорректна (лимит 100 МБ)")
		return
	}

	fileA, nameA, err := readUpload(r, "fileA")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "не удалось прочитать первый файл: "+err.Error())
		return
	}
	fileB, nameB, err := readUpload(r, "fileB")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "не удалось прочитать второй файл: "+err.Error())
		return
	}

	extA := strings.ToLower(filepath.Ext(nameA))
	extB := strings.ToLower(filepath.Ext(nameB))
	if extA != extB {
		writeErr(w, http.StatusBadRequest, "типы файлов не совпадают: "+extA+" и "+extB)
		return
	}

	keyCol := strings.TrimSpace(r.FormValue("keyCol"))
	if keyCol != "" && !isColLetter(keyCol) {
		writeErr(w, http.StatusBadRequest, "ключевой столбец задан неверно: "+keyCol+" (укажите букву, например A)")
		return
	}

	switch extA {
	case ".xlsx", ".xlsm":
		s.compareExcel(w, fileA, fileB, nameA, nameB, keyCol)
	case ".docx":
		s.compareWord(w, fileA, fileB, nameA, nameB)
	case ".doc", ".xls":
		writeErr(w, http.StatusBadRequest, "старый формат "+extA+" не поддерживается — сохраните файл в "+extA+"x")
	case "":
		writeErr(w, http.StatusBadRequest, "у файлов нет расширения")
	default:
		writeErr(w, http.StatusBadRequest, "неподдерживаемый формат "+extA+" (нужен .xlsx, .xlsm или .docx)")
	}
}

func readUpload(r *http.Request, field string) ([]byte, string, error) {
	f, hdr, err := r.FormFile(field)
	if err != nil {
		return nil, "", fmt.Errorf("поле %s отсутствует", field)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxUploadSize+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxUploadSize {
		return nil, "", fmt.Errorf("файл больше 100 МБ")
	}
	name := hdr.Filename
	if name == "" {
		name = field
	}
	return data, name, nil
}

func (s *Server) compareExcel(w http.ResponseWriter, fileA, fileB []byte, nameA, nameB, keyCol string) {
	bookA, _, err := parseExcel(fileA)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "файл «"+nameA+"»: "+err.Error())
		return
	}
	bookB, bundleB, err := parseExcel(fileB)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "файл «"+nameB+"»: "+err.Error())
		return
	}

	d := xlsx.DiffBooks(bookA, bookB, nameA, nameB, keyCol)

	marks := xlsx.DiffToMarks(d, bookB)
	report := xlsx.BuildReportRows(d)
	out, err := xlsx.Mark(bundleB, marks, report)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "не удалось пометить файл: "+err.Error())
		return
	}

	id := s.store.put(out, markedName(nameB), d, "excel")
	writeJSON(w, http.StatusOK, compareResponse{ID: id, Type: "excel", Diff: d, Name: markedName(nameB)})
}

func (s *Server) compareWord(w http.ResponseWriter, fileA, fileB []byte, nameA, nameB string) {
	bundleA, err := ooxml.OpenBundle(newBytesReader(fileA), int64(len(fileA)))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "файл «"+nameA+"»: "+err.Error())
		return
	}
	docA, err := docx.Parse(bundleA)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "файл «"+nameA+"»: "+err.Error())
		return
	}
	bundleB, err := ooxml.OpenBundle(newBytesReader(fileB), int64(len(fileB)))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "файл «"+nameB+"»: "+err.Error())
		return
	}
	docB, err := docx.Parse(bundleB)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "файл «"+nameB+"»: "+err.Error())
		return
	}

	d := docx.Diff(docA, docB, nameA, nameB)
	out, err := docx.Mark(bundleB, d)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "не удалось пометить файл: "+err.Error())
		return
	}
	id := s.store.put(out, markedName(nameB), d, "word")
	writeJSON(w, http.StatusOK, compareResponse{ID: id, Type: "word", Diff: d, Name: markedName(nameB)})
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "не задан id")
		return
	}
	res, ok := s.store.get(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "результат не найден или устарел")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+urlFilename(res.filename))
	_, _ = w.Write(res.data)
}

// handleReport отдаёт самодостаточный HTML-отчёт (печатается в PDF).
func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "не задан id")
		return
	}
	res, ok := s.store.get(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "результат не найден или устарел")
		return
	}
	html, err := report.Generate(res.diff)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	name := strings.TrimSuffix(res.filename, filepath.Ext(res.filename)) + "_отчёт.html"
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+urlFilename(name))
	_, _ = w.Write(html)
}

// isColLetter проверяет, что строка — буква(-ы) столбца Excel ("A", "BC").
func isColLetter(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range strings.ToUpper(s) {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

func markedName(name string) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	return base + "_с_комментариями" + ext
}

// urlFilename экранирует имя файла для Content-Disposition.
func urlFilename(name string) string {
	var sb strings.Builder
	for _, b := range []byte(name) {
		if b >= 0x20 && b < 0x7f && b != '%' {
			sb.WriteByte(b)
		} else {
			fmt.Fprintf(&sb, "%%%02X", b)
		}
	}
	return sb.String()
}

// --- вспомогательные ---

type bytesReaderAt struct{ data []byte }

func newBytesReader(data []byte) *bytesReaderAt { return &bytesReaderAt{data: data} }

func (b *bytesReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(b.data)) {
		return 0, io.EOF
	}
	n := copy(p, b.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func parseExcel(data []byte) (*xlsx.Book, *ooxml.Bundle, error) {
	bundle, err := ooxml.OpenBundle(newBytesReader(data), int64(len(data)))
	if err != nil {
		return nil, nil, err
	}
	book, err := xlsx.Parse(bundle)
	if err != nil {
		return nil, nil, err
	}
	return book, bundle, nil
}
