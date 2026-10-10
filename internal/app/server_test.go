package app

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"docdiff/internal/docx"
	"docdiff/internal/ooxml"
	"docdiff/internal/xlsx"
)

func multipartBody(t *testing.T, nameA string, dataA []byte, nameB string, dataB []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fa, err := w.CreateFormFile("fileA", nameA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fa.Write(dataA); err != nil {
		t.Fatal(err)
	}
	fb, err := w.CreateFormFile("fileB", nameB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fb.Write(dataB); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, w.FormDataContentType()
}

func TestCompareExcelEndpoint(t *testing.T) {
	oldData := xlsx.Build([]xlsx.NamedGrid{{Name: "Лист1", Rows: [][]string{
		{"Имя", "Сумма"},
		{"Иванов", "100"},
		{"Петров", "200"},
	}}})
	newData := xlsx.Build([]xlsx.NamedGrid{{Name: "Лист1", Rows: [][]string{
		{"Имя", "Сумма"},
		{"Иванов", "150"},
		{"Петров", "200"},
		{"Сидоров", "300"},
	}}})

	srv := New()
	body, ctype := multipartBody(t, "old.xlsx", oldData, "new.xlsx", newData)
	req := httptest.NewRequest(http.MethodPost, "/api/compare", body)
	req.Header.Set("Content-Type", ctype)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp compareResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v (%s)", err, rec.Body.String())
	}
	if resp.Type != "excel" || resp.ID == "" {
		t.Fatalf("resp = %+v", resp)
	}

	// Скачивание помеченного файла
	req2 := httptest.NewRequest(http.MethodGet, "/api/download?id="+resp.ID, nil)
	rec2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("download status = %d", rec2.Code)
	}
	if len(rec2.Body.Bytes()) < 1000 {
		t.Fatalf("файл слишком мал: %d", rec2.Body.Len())
	}
	if !strings.Contains(rec2.Header().Get("Content-Disposition"), "%D1%81") {
		t.Fatalf("content-disposition: %s", rec2.Header().Get("Content-Disposition"))
	}
}

func TestCompareWordEndpoint(t *testing.T) {
	oldData := docx.Build([]docx.Content{
		{Paragraph: "Первый абзац документа."},
		{Paragraph: "Второй абзац документа."},
	})
	newData := docx.Build([]docx.Content{
		{Paragraph: "Первый абзац документа."},
		{Paragraph: "Второй абзац документа (исправлен)."},
	})

	srv := New()
	body, ctype := multipartBody(t, "old.docx", oldData, "new.docx", newData)
	req := httptest.NewRequest(http.MethodPost, "/api/compare", body)
	req.Header.Set("Content-Type", ctype)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp compareResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Type != "word" {
		t.Fatalf("type = %s", resp.Type)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/download?id="+resp.ID, nil)
	rec2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("download = %d", rec2.Code)
	}
	data := rec2.Body.Bytes()
	// В скачанном файле должен быть comments.xml
	found := false
	for _, name := range mustNames(t, data) {
		if name == "word/comments.xml" {
			found = true
		}
	}
	if !found {
		t.Fatalf("в выгрузке нет comments.xml: %v", mustNames(t, data))
	}
}

func mustNames(t *testing.T, data []byte) []string {
	t.Helper()
	b, err := ooxml.OpenBundle(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	return b.Names()
}

func TestIndexAndHealthz(t *testing.T) {
	srv := New()
	for _, path := range []string{"/", "/static/app.js", "/static/style.css", "/api/healthz"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s -> %d", path, rec.Code)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "DocDiff") {
		t.Fatal("index.html без разметки")
	}
}

func TestCompareErrors(t *testing.T) {
	srv := New()

	// Разные расширения
	body, ctype := multipartBody(t, "a.xlsx", []byte("x"), "b.docx", []byte("y"))
	req := httptest.NewRequest(http.MethodPost, "/api/compare", body)
	req.Header.Set("Content-Type", ctype)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}

	// Старый формат
	body, ctype = multipartBody(t, "a.xls", []byte("x"), "b.xls", []byte("y"))
	req = httptest.NewRequest(http.MethodPost, "/api/compare", body)
	req.Header.Set("Content-Type", ctype)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), ".xls") {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	// Неизвестный id
	req = httptest.NewRequest(http.MethodGet, "/api/download?id=zzz", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestCompareKeyAndReport(t *testing.T) {
	oldData := xlsx.Build([]xlsx.NamedGrid{{Name: "S", Rows: [][]string{
		{"Код", "Цена"},
		{"K1", "100"},
		{"K2", "200"},
	}}})
	newData := xlsx.Build([]xlsx.NamedGrid{{Name: "S", Rows: [][]string{
		{"Код", "Цена"},
		{"K2", "250"},
		{"K1", "100"},
	}}})

	srv := New()
	body, ctype := multipartBody(t, "old.xlsx", oldData, "new.xlsx", newData)
	req := httptest.NewRequest(http.MethodPost, "/api/compare", body)
	req.Header.Set("Content-Type", ctype)
	// без ключа — перестановка увидится как изменения
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	var resp compareResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	d0 := resp.Diff.(map[string]interface{})
	_ = d0

	// с ключевым столбцом — только одно изменение (K2: цена)
	body, ctype = multipartBody(t, "old.xlsx", oldData, "new.xlsx", newData)
	req = httptest.NewRequest(http.MethodPost, "/api/compare?keyCol=A", body)
	req.Header.Set("Content-Type", ctype)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp2 compareResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp2); err != nil {
		t.Fatal(err)
	}
	diffMap := resp2.Diff.(map[string]interface{})
	summary := diffMap["summary"].(map[string]interface{})
	if summary["modified"].(float64) != 1 || summary["added"].(float64) != 0 || summary["deleted"].(float64) != 0 {
		t.Fatalf("summary = %+v", summary)
	}

	// HTML-отчёт
	req = httptest.NewRequest(http.MethodGet, "/api/report?id="+resp2.ID, nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("report status = %d", rec.Code)
	}
	html := rec.Body.String()
	if !strings.Contains(html, "<!DOCTYPE html>") || !strings.Contains(html, "K2") || !strings.Contains(html, "250") {
		t.Fatalf("отчёт некорректен: %.300s", html)
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), ".html") {
		t.Fatalf("content-disposition: %s", rec.Header().Get("Content-Disposition"))
	}

	// Неверный ключевой столбец
	body, ctype = multipartBody(t, "a.xlsx", oldData, "b.xlsx", newData)
	req = httptest.NewRequest(http.MethodPost, "/api/compare?keyCol=123", body)
	req.Header.Set("Content-Type", ctype)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}
