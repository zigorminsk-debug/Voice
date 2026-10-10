// DocDiff — настольное приложение для сравнения таблиц Excel и документов Word.
//
// Режимы запуска:
//
//	docdiff                     запуск веб-интерфейса (откроется браузер)
//	docdiff -addr :8765         запуск на указанном адресе
//	docdiff old.xlsx new.xlsx   сравнение из командной строки
//	docdiff old.docx new.docx -o report.docx
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"docdiff/internal/app"
	"docdiff/internal/diff"
	"docdiff/internal/docx"
	"docdiff/internal/ooxml"
	"docdiff/internal/report"
	"docdiff/internal/xlsx"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8765", "адрес веб-интерфейса (host:port)")
	openBrowser := flag.Bool("open", true, "открывать браузер автоматически")
	out := flag.String("o", "", "имя выходного файла (режим командной строки)")
	keyCol := flag.String("key", "", "ключевой столбец для таблиц Excel (например A); пусто — по порядку строк")
	reportName := flag.String("report", "", "дополнительно сохранить HTML-отчёт (например report.html)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "DocDiff — сравнение Excel и Word с комментариями\n\n")
		fmt.Fprintf(os.Stderr, "Использование:\n")
		fmt.Fprintf(os.Stderr, "  %s [флаги]                       запуск веб-интерфейса\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s [флаги] <старый> <новый>      сравнение файлов из консоли\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "                                    (флаги указываются ДО имён файлов)\n\n")
		fmt.Fprintf(os.Stderr, "Примеры:\n")
		fmt.Fprintf(os.Stderr, "  %s old.xlsx new.xlsx\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -key A -report report.html old.xlsx new.xlsx\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Флаги:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	// Режим командной строки: два файла.
	if flag.NArg() == 2 {
		if err := runCLI(flag.Arg(0), flag.Arg(1), *out, *keyCol, *reportName); err != nil {
			fmt.Fprintln(os.Stderr, "Ошибка:", err)
			os.Exit(1)
		}
		return
	}
	if flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}

	runServer(*addr, *openBrowser)
}

// ---------------------------------------------------------------------------
// Веб-интерфейс
// ---------------------------------------------------------------------------

func runServer(addr string, open bool) {
	srv := app.New()
	server := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Не удалось занять адрес %s: %v", addr, err)
	}
	url := browseURL(ln.Addr().String())
	fmt.Println("╔══════════════════════════════════════════════╗")
	fmt.Println("║  DocDiff — сравнение Excel и Word            ║")
	fmt.Println("╚══════════════════════════════════════════════╝")
	fmt.Printf("Интерфейс: %s\n", url)
	fmt.Println("Для завершения нажмите Ctrl+C")

	if open {
		go func() {
			time.Sleep(300 * time.Millisecond)
			openURL(url)
		}()
	}

	go func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
		<-ch
		fmt.Println("\nЗавершение работы...")
		_ = server.Close()
		os.Exit(0)
	}()

	if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Ошибка сервера: %v", err)
	}
}

// browseURL формирует удобный адрес для браузера.
func browseURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return fmt.Sprintf("http://%s:%s", host, port)
}

// openURL открывает URL в браузере по умолчанию.
func openURL(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// rundll32 работает без ожидания и не блокирует приложение.
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		fmt.Println("Не удалось открыть браузер автоматически — откройте", url, "вручную")
		return
	}
	go func() { _ = cmd.Wait() }()
}

// ---------------------------------------------------------------------------
// CLI
// ---------------------------------------------------------------------------

func runCLI(pathA, pathB, outName, keyCol, reportName string) error {
	dataA, err := os.ReadFile(pathA)
	if err != nil {
		return err
	}
	dataB, err := os.ReadFile(pathB)
	if err != nil {
		return err
	}
	ext := strings.ToLower(filepath.Ext(pathB))
	if strings.ToLower(filepath.Ext(pathA)) != ext {
		return fmt.Errorf("типы файлов не совпадают")
	}

	var marked []byte
	switch ext {
	case ".xlsx", ".xlsm":
		bookA, _, err := parseExcel(dataA)
		if err != nil {
			return fmt.Errorf("%s: %w", pathA, err)
		}
		bookB, bundleB, err := parseExcel(dataB)
		if err != nil {
			return fmt.Errorf("%s: %w", pathB, err)
		}
		d := xlsx.DiffBooks(bookA, bookB, filepath.Base(pathA), filepath.Base(pathB), keyCol)
		printExcelSummary(d)
		marked, err = xlsx.Mark(bundleB, xlsx.DiffToMarks(d, bookB), xlsx.BuildReportRows(d))
		if err != nil {
			return err
		}
		if reportName != "" {
			if err := writeReport(d, reportName); err != nil {
				return err
			}
		}
	case ".docx":
		docA, err := parseDocx(dataA)
		if err != nil {
			return fmt.Errorf("%s: %w", pathA, err)
		}
		docB, err := parseDocx(dataB)
		if err != nil {
			return fmt.Errorf("%s: %w", pathB, err)
		}
		d := docx.Diff(docA, docB, filepath.Base(pathA), filepath.Base(pathB))
		printWordSummary(d)
		bundleB, err := ooxml.OpenBundle(newBytes(dataB), int64(len(dataB)))
		if err != nil {
			return err
		}
		marked, err = docx.Mark(bundleB, d)
		if err != nil {
			return err
		}
		if reportName != "" {
			if err := writeReport(d, reportName); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("неподдерживаемый формат %q (нужен .xlsx, .xlsm или .docx)", ext)
	}

	if outName == "" {
		base := strings.TrimSuffix(pathB, filepath.Ext(pathB))
		outName = base + "_с_комментариями" + ext
	}
	if err := os.WriteFile(outName, marked, 0o644); err != nil {
		return err
	}
	fmt.Printf("\nФайл с комментариями сохранён: %s\n", outName)
	return nil
}

// writeReport сохраняет HTML-отчёт о сравнении.
func writeReport(d interface{}, name string) error {
	html, err := report.Generate(d)
	if err != nil {
		return err
	}
	if err := os.WriteFile(name, html, 0o644); err != nil {
		return err
	}
	fmt.Printf("HTML-отчёт сохранён: %s\n", name)
	return nil
}

type bytesAt struct{ data []byte }

func newBytes(data []byte) *bytesAt { return &bytesAt{data: data} }

func (b *bytesAt) ReadAt(p []byte, off int64) (int, error) {
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
	bundle, err := ooxml.OpenBundle(newBytes(data), int64(len(data)))
	if err != nil {
		return nil, nil, err
	}
	book, err := xlsx.Parse(bundle)
	if err != nil {
		return nil, nil, err
	}
	return book, bundle, nil
}

func parseDocx(data []byte) (*docx.Document, error) {
	bundle, err := ooxml.OpenBundle(newBytes(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	return docx.Parse(bundle)
}

func printExcelSummary(d *diff.ExcelDiff) {
	fmt.Printf("Сравнение таблиц: %s → %s\n", d.FileA, d.FileB)
	fmt.Printf("Изменено строк: %d, добавлено: %d, удалено: %d\n",
		d.Summary.Modified, d.Summary.Added, d.Summary.Deleted)
	for _, sh := range d.Sheets {
		switch sh.Status {
		case "same":
			fmt.Printf("  [%s] без изменений\n", sh.Name)
		case "onlyA":
			fmt.Printf("  [%s] лист только в оригинале\n", sh.Name)
		case "onlyB":
			fmt.Printf("  [%s] лист только в новом файле\n", sh.Name)
		default:
			fmt.Printf("  [%s] изменений: %d\n", sh.Name, len(sh.Changes))
			for _, ch := range sh.Changes {
				switch ch.Type {
				case diff.Added:
					fmt.Printf("    + строка %d добавлена\n", ch.RowB)
				case diff.Deleted:
					fmt.Printf("    - строка %d удалена\n", ch.RowA)
				case diff.Modified:
					fmt.Printf("    ~ строка %d изменена", ch.RowB)
					for _, c := range ch.Cells {
						switch {
						case c.FormulaOnly:
							fmt.Printf(" [%s: формула =%s → =%s]", c.Col, c.OldFormula, c.NewFormula)
						case c.OldFormula != c.NewFormula && (c.OldFormula != "" || c.NewFormula != ""):
							fmt.Printf(" [%s: %q→%q; формула =%s → =%s]", c.Col, c.Old, c.New, c.OldFormula, c.NewFormula)
						default:
							fmt.Printf(" [%s: %q→%q]", c.Col, c.Old, c.New)
						}
					}
					fmt.Println()
				}
			}
		}
	}
}

func printWordSummary(d *diff.WordDiff) {
	fmt.Printf("Сравнение документов: %s → %s\n", d.FileA, d.FileB)
	fmt.Printf("Изменено блоков: %d, добавлено: %d, удалено: %d\n",
		d.Summary.Modified, d.Summary.Added, d.Summary.Deleted)
	for _, ch := range d.Blocks {
		switch ch.Type {
		case diff.Added:
			fmt.Printf("  + добавлено: %.100s\n", ch.New)
		case diff.Deleted:
			fmt.Printf("  - удалено: %.100s\n", ch.Old)
		case diff.Modified:
			fmt.Printf("  ~ изменено (блок %d): %.80s… → %.80s…\n", ch.Index, ch.Old, ch.New)
		}
	}
}
