//go:build windows

package main

import (
	"fmt"
	"path/filepath"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

const (
	wmCreate           = 0x0001
	wmDestroy          = 0x0002
	wmSize             = 0x0005
	wmPaint            = 0x000F
	wmCommand          = 0x0111
	wmLButtonDown      = 0x0201
	wmLButtonUp        = 0x0202
	wmMouseMove        = 0x0200
	wsOverlappedWindow = 0x00CF0000
	wsVisible          = 0x10000000
	wsChild            = 0x40000000
	wsBorder           = 0x00800000
	wsVScroll          = 0x00200000
	esMultiline        = 0x0004
	esAutoVScroll      = 0x0040
	esWantReturn       = 0x1000
	bsPushButton       = 0
	ssLeft             = 0
	csHRedraw          = 0x0002
	csVRedraw          = 0x0001
	swShow             = 5
	colorWindow        = 5
	idcArrow           = 32512
	dtCenter           = 1
	dtVCenter          = 4
	dtSingleLine       = 0x20
	dtWordBreak        = 0x10
	dtEndEllipsis      = 0x8000
	psSolid            = 0
	transparent        = 1
	gwlpUserdata       = -21
	mbOK               = 0
	mbIconError        = 0x10
	mbIconInformation  = 0x40
	mbYesNo            = 4
	idYes              = 6
	ofExplorer         = 0x00080000
	ofPathMustExist    = 0x00000800
	ofFileMustExist    = 0x00001000
	ofOverwritePrompt  = 0x00000002
	maxPath            = 260
	nodeW              = 190
	nodeH              = 112
)

const (
	idAddBase     = 1000
	idLink        = 1010
	idDelete      = 1011
	idNew         = 1012
	idOpen        = 1013
	idSave        = 1014
	idTitle       = 1020
	idAddress     = 1021
	idDescription = 1022
	idApply       = 1023
)

type point struct{ X, Y int32 }
type rect struct{ Left, Top, Right, Bottom int32 }
type paintStruct struct {
	Hdc                uintptr
	Erase              int32
	RcPaint            rect
	Restore, IncUpdate int32
	Reserved           [32]byte
}
type wndClassEx struct {
	Size                               uint32
	Style                              uint32
	WndProc                            uintptr
	ClsExtra, WndExtra                 int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
	IconSm                             uintptr
}
type openFileName struct {
	Size                         uint32
	Owner, Instance              uintptr
	Filter, CustomFilter         *uint16
	MaxCustomFilter, FilterIndex uint32
	File                         *uint16
	MaxFile                      uint32
	FileTitle                    *uint16
	MaxFileTitle                 uint32
	InitialDir, Title            *uint16
	Flags                        uint32
	FileOffset, FileExtension    uint16
	DefExt                       *uint16
	CustData, Hook               uintptr
	TemplateName                 *uint16
	ReservedPtr                  uintptr
	Reserved                     uint32
	FlagsEx                      uint32
}

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	gdi32                = syscall.NewLazyDLL("gdi32.dll")
	comdlg32             = syscall.NewLazyDLL("comdlg32.dll")
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	pRegisterClassEx     = user32.NewProc("RegisterClassExW")
	pCreateWindowEx      = user32.NewProc("CreateWindowExW")
	pDefWindowProc       = user32.NewProc("DefWindowProcW")
	pShowWindow          = user32.NewProc("ShowWindow")
	pUpdateWindow        = user32.NewProc("UpdateWindow")
	pGetMessage          = user32.NewProc("GetMessageW")
	pTranslateMessage    = user32.NewProc("TranslateMessage")
	pDispatchMessage     = user32.NewProc("DispatchMessageW")
	pPostQuitMessage     = user32.NewProc("PostQuitMessage")
	pBeginPaint          = user32.NewProc("BeginPaint")
	pEndPaint            = user32.NewProc("EndPaint")
	pInvalidateRect      = user32.NewProc("InvalidateRect")
	pMoveWindow          = user32.NewProc("MoveWindow")
	pGetClientRect       = user32.NewProc("GetClientRect")
	pLoadCursor          = user32.NewProc("LoadCursorW")
	pMessageBox          = user32.NewProc("MessageBoxW")
	pSetCapture          = user32.NewProc("SetCapture")
	pReleaseCapture      = user32.NewProc("ReleaseCapture")
	pSetWindowText       = user32.NewProc("SetWindowTextW")
	pGetWindowText       = user32.NewProc("GetWindowTextW")
	pGetWindowTextLength = user32.NewProc("GetWindowTextLengthW")
	pEnableWindow        = user32.NewProc("EnableWindow")
	pSendMessage         = user32.NewProc("SendMessageW")
	pGetModuleHandle     = kernel32.NewProc("GetModuleHandleW")
	pCreateSolidBrush    = gdi32.NewProc("CreateSolidBrush")
	pCreatePen           = gdi32.NewProc("CreatePen")
	pSelectObject        = gdi32.NewProc("SelectObject")
	pDeleteObject        = gdi32.NewProc("DeleteObject")
	pRoundRect           = gdi32.NewProc("RoundRect")
	pMoveToEx            = gdi32.NewProc("MoveToEx")
	pLineTo              = gdi32.NewProc("LineTo")
	pSetBkMode           = gdi32.NewProc("SetBkMode")
	pSetTextColor        = gdi32.NewProc("SetTextColor")
	pCreateFont          = gdi32.NewProc("CreateFontW")
	pDrawText            = user32.NewProc("DrawTextW")
	pFillRect            = user32.NewProc("FillRect")
	pGetOpenFileName     = comdlg32.NewProc("GetOpenFileNameW")
	pGetSaveFileName     = comdlg32.NewProc("GetSaveFileNameW")
)

var app struct {
	instance, main, canvas                                       uintptr
	buttons, labels                                              []uintptr
	titleEdit, addressEdit, descriptionEdit, applyButton, status uintptr
	diagram                                                      *Diagram
	selected, linkStart                                          int
	linkMode, dragging, dirty                                    bool
	dragDX, dragDY                                               int
	filePath                                                     string
	font, fontBold                                               uintptr
}

func ptr(s string) *uint16     { p, _ := syscall.UTF16PtrFromString(s); return p }
func rgb(r, g, b byte) uintptr { return uintptr(r) | uintptr(g)<<8 | uintptr(b)<<16 }
func signed(v int32) uintptr   { return uintptr(v) }
func lo(v uintptr) int         { return int(int16(v & 0xffff)) }
func hi(v uintptr) int         { return int(int16((v >> 16) & 0xffff)) }

func main() {
	app.diagram = SampleDiagram()
	app.instance, _, _ = pGetModuleHandle.Call(0)
	app.font, _, _ = pCreateFont.Call(signed(-16), 0, 0, 0, 400, 0, 0, 0, 204, 0, 0, 5, 0, uintptr(unsafe.Pointer(ptr("Segoe UI"))))
	app.fontBold, _, _ = pCreateFont.Call(signed(-17), 0, 0, 0, 600, 0, 0, 0, 204, 0, 0, 5, 0, uintptr(unsafe.Pointer(ptr("Segoe UI Semibold"))))
	registerClass("DCDesignerMain", syscall.NewCallback(mainProc), uintptr(colorWindow+1))
	registerClass("DCDesignerCanvas", syscall.NewCallback(canvasProc), uintptr(colorWindow+1))
	app.main = createWindow(0, "DCDesignerMain", "Схема связей ЦОД", wsOverlappedWindow|wsVisible, 0x80000000, 0x80000000, 1280, 800, 0, 0)
	if app.main == 0 {
		panic("не удалось создать окно")
	}
	var client rect
	pGetClientRect.Call(app.main, uintptr(unsafe.Pointer(&client)))
	layout(int(client.Right), int(client.Bottom))
	pShowWindow.Call(app.main, swShow)
	pUpdateWindow.Call(app.main)
	var msg [7]uintptr
	for {
		r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg[0])))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&msg[0])))
	}
}

func registerClass(name string, proc, background uintptr) {
	cursor, _, _ := pLoadCursor.Call(0, idcArrow)
	wc := wndClassEx{Size: uint32(unsafe.Sizeof(wndClassEx{})), Style: csHRedraw | csVRedraw, WndProc: proc, Instance: app.instance, Cursor: cursor, Background: background, ClassName: ptr(name)}
	pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
}

func createWindow(ex uintptr, class, title string, style uintptr, x, y, w, h int, parent uintptr, id int) uintptr {
	r, _, _ := pCreateWindowEx.Call(ex, uintptr(unsafe.Pointer(ptr(class))), uintptr(unsafe.Pointer(ptr(title))), style, uintptr(x), uintptr(y), uintptr(w), uintptr(h), parent, uintptr(id), app.instance, 0)
	return r
}

func mainProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	switch msg {
	case wmCreate:
		createControls(hwnd)
		return 0
	case wmSize:
		layout(lo(lparam), hi(lparam))
		return 0
	case wmCommand:
		handleCommand(int(wparam & 0xffff))
		return 0
	case wmDestroy:
		if app.font != 0 {
			pDeleteObject.Call(app.font)
		}
		if app.fontBold != 0 {
			pDeleteObject.Call(app.fontBold)
		}
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, msg, wparam, lparam)
	return r
}

func control(class, text string, style uintptr, parent uintptr, id int) uintptr {
	h := createWindow(0, class, text, wsChild|wsVisible|style, 0, 0, 100, 30, parent, id)
	pSendMessage.Call(h, 0x0030, app.font, 1)
	return h
}

func createControls(hwnd uintptr) {
	app.labels = append(app.labels, control("STATIC", "ИНСТРУМЕНТЫ", ssLeft, hwnd, 0))
	for i, t := range tools {
		app.buttons = append(app.buttons, control("BUTTON", "＋  "+t.Title, bsPushButton, hwnd, idAddBase+i))
	}
	app.buttons = append(app.buttons, control("BUTTON", "⛓  Создать связь", bsPushButton, hwnd, idLink))
	app.buttons = append(app.buttons, control("BUTTON", "Удалить выбранное", bsPushButton, hwnd, idDelete))
	app.buttons = append(app.buttons, control("BUTTON", "Новая схема", bsPushButton, hwnd, idNew))
	app.buttons = append(app.buttons, control("BUTTON", "Открыть…", bsPushButton, hwnd, idOpen))
	app.buttons = append(app.buttons, control("BUTTON", "Сохранить как…", bsPushButton, hwnd, idSave))
	app.canvas = createWindow(0, "DCDesignerCanvas", "", wsChild|wsVisible|wsBorder, 220, 0, 700, 700, hwnd, 2000)
	app.labels = append(app.labels, control("STATIC", "СВОЙСТВА ОБЪЕКТА", ssLeft, hwnd, 0))
	app.labels = append(app.labels, control("STATIC", "Название", ssLeft, hwnd, 0))
	app.titleEdit = control("EDIT", "", wsBorder, hwnd, idTitle)
	app.labels = append(app.labels, control("STATIC", "IP / имя хоста", ssLeft, hwnd, 0))
	app.addressEdit = control("EDIT", "", wsBorder, hwnd, idAddress)
	app.labels = append(app.labels, control("STATIC", "Описание", ssLeft, hwnd, 0))
	app.descriptionEdit = control("EDIT", "", wsBorder|wsVScroll|esMultiline|esAutoVScroll|esWantReturn, hwnd, idDescription)
	app.applyButton = control("BUTTON", "Применить изменения", bsPushButton, hwnd, idApply)
	app.status = control("STATIC", "Перетаскивайте блоки мышью. Выберите инструмент слева, чтобы добавить сервер.", ssLeft, hwnd, 0)
	setEditorEnabled(false)
}

func layout(w, h int) {
	if w < 850 {
		w = 850
	}
	if h < 600 {
		h = 600
	}
	left, right, bottom := 220, 300, 42
	canvasW := w - left - right
	// Position the tool palette, canvas and property editor.
	for i, b := range app.buttons {
		y := 48 + i*40
		if i >= 8 {
			y = 48 + i*40
		}
		pMoveWindow.Call(b, 12, uintptr(y), 196, 32, 1)
	}
	pMoveWindow.Call(app.canvas, uintptr(left), 8, uintptr(canvasW), uintptr(h-bottom-16), 1)
	rx := w - right + 16
	pMoveWindow.Call(app.titleEdit, uintptr(rx), 82, uintptr(right-32), 28, 1)
	pMoveWindow.Call(app.addressEdit, uintptr(rx), 142, uintptr(right-32), 28, 1)
	pMoveWindow.Call(app.descriptionEdit, uintptr(rx), 202, uintptr(right-32), uintptr(h-310), 1)
	pMoveWindow.Call(app.applyButton, uintptr(rx), uintptr(h-96), uintptr(right-32), 34, 1)
	pMoveWindow.Call(app.status, 12, uintptr(h-bottom+10), uintptr(w-24), 24, 1)
	if len(app.labels) == 5 {
		pMoveWindow.Call(app.labels[0], 12, 14, 196, 26, 1)
		pMoveWindow.Call(app.labels[1], uintptr(rx), 14, uintptr(right-32), 28, 1)
		pMoveWindow.Call(app.labels[2], uintptr(rx), 58, uintptr(right-32), 22, 1)
		pMoveWindow.Call(app.labels[3], uintptr(rx), 118, uintptr(right-32), 22, 1)
		pMoveWindow.Call(app.labels[4], uintptr(rx), 178, uintptr(right-32), 22, 1)
	}
}

func handleCommand(id int) {
	switch {
	case id >= idAddBase && id < idAddBase+len(tools):
		x, y := 40+len(app.diagram.Nodes)%3*215, 40+(len(app.diagram.Nodes)/3)%4*145
		n := app.diagram.AddNode(tools[id-idAddBase], x, y)
		app.selected = n.ID
		app.dirty = true
		updateEditor()
		setStatus("Добавлен объект «" + n.Title + "». Перетащите его в нужное место.")
		redraw()
	case id == idLink:
		app.linkMode = true
		app.linkStart = 0
		setStatus("Режим связи: щёлкните по первому, затем по второму объекту.")
	case id == idDelete:
		if app.selected != 0 {
			app.diagram.DeleteNode(app.selected)
			app.selected = 0
			app.dirty = true
			updateEditor()
			redraw()
			setStatus("Объект и его связи удалены.")
		}
	case id == idNew:
		if confirm("Очистить схему и начать заново?") {
			app.diagram = NewDiagram()
			app.selected = 0
			app.filePath = ""
			app.dirty = false
			updateEditor()
			redraw()
			setStatus("Создана пустая схема.")
		}
	case id == idOpen:
		openDiagram()
	case id == idSave:
		saveDiagram()
	case id == idApply:
		applyEditor()
	}
}

func canvasProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	x, y := lo(lparam), hi(lparam)
	switch msg {
	case wmPaint:
		paintCanvas(hwnd)
		return 0
	case wmLButtonDown:
		id := hitNode(x, y)
		if app.linkMode {
			if id == 0 {
				return 0
			}
			if app.linkStart == 0 {
				app.linkStart = id
				app.selected = id
				updateEditor()
				setStatus("Первый объект выбран. Теперь выберите второй.")
			} else {
				if app.diagram.AddLink(app.linkStart, id) {
					app.dirty = true
					setStatus("Связь создана. Для подписи связи отредактируйте JSON-файл.")
				} else {
					setStatus("Такую связь создать нельзя.")
				}
				app.linkMode = false
				app.linkStart = 0
			}
			redraw()
			return 0
		}
		app.selected = id
		updateEditor()
		if n := app.diagram.NodeByID(id); n != nil {
			app.dragging = true
			app.dragDX = x - n.X
			app.dragDY = y - n.Y
			pSetCapture.Call(hwnd)
		}
		redraw()
		return 0
	case wmMouseMove:
		if app.dragging {
			if n := app.diagram.NodeByID(app.selected); n != nil {
				n.X = x - app.dragDX
				n.Y = y - app.dragDY
				if n.X < 5 {
					n.X = 5
				}
				if n.Y < 5 {
					n.Y = 5
				}
				app.dirty = true
				redraw()
			}
		}
		return 0
	case wmLButtonUp:
		if app.dragging {
			app.dragging = false
			pReleaseCapture.Call()
		}
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, msg, wparam, lparam)
	return r
}

func paintCanvas(hwnd uintptr) {
	var ps paintStruct
	hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	var rc rect
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	bg, _, _ := pCreateSolidBrush.Call(rgb(247, 249, 252))
	pFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), bg)
	pDeleteObject.Call(bg)
	pSetBkMode.Call(hdc, transparent)
	pSelectObject.Call(hdc, app.font)
	// subtle grid
	gridPen, _, _ := pCreatePen.Call(psSolid, 1, rgb(225, 231, 239))
	old, _, _ := pSelectObject.Call(hdc, gridPen)
	for xx := 20; xx < int(rc.Right); xx += 24 {
		pMoveToEx.Call(hdc, uintptr(xx), 0, 0)
		pLineTo.Call(hdc, uintptr(xx), uintptr(rc.Bottom))
	}
	for yy := 20; yy < int(rc.Bottom); yy += 24 {
		pMoveToEx.Call(hdc, 0, uintptr(yy), 0)
		pLineTo.Call(hdc, uintptr(rc.Right), uintptr(yy))
	}
	pSelectObject.Call(hdc, old)
	pDeleteObject.Call(gridPen)
	// links behind nodes
	linkPen, _, _ := pCreatePen.Call(psSolid, 3, rgb(74, 102, 138))
	old, _, _ = pSelectObject.Call(hdc, linkPen)
	for _, l := range app.diagram.Links {
		a, b := app.diagram.NodeByID(l.From), app.diagram.NodeByID(l.To)
		if a == nil || b == nil {
			continue
		}
		x1, y1 := a.X+nodeW/2, a.Y+nodeH/2
		x2, y2 := b.X+nodeW/2, b.Y+nodeH/2
		pMoveToEx.Call(hdc, uintptr(x1), uintptr(y1), 0)
		pLineTo.Call(hdc, uintptr(x2), uintptr(y2))
		if l.Label != "" {
			drawText(hdc, l.Label, rect{int32((x1+x2)/2 - 45), int32((y1+y2)/2 - 12), int32((x1+x2)/2 + 45), int32((y1+y2)/2 + 12)}, dtCenter|dtVCenter|dtSingleLine, rgb(53, 73, 99))
		}
	}
	pSelectObject.Call(hdc, old)
	pDeleteObject.Call(linkPen)
	for _, n := range app.diagram.Nodes {
		drawNode(hdc, n, n.ID == app.selected)
	}
}

func drawNode(hdc uintptr, n *Node, selected bool) {
	accent := typeColor(n.Type)
	border := accent
	width := 2
	if selected {
		border = rgb(255, 153, 0)
		width = 4
	}
	brush, _, _ := pCreateSolidBrush.Call(rgb(255, 255, 255))
	pen, _, _ := pCreatePen.Call(psSolid, uintptr(width), border)
	oldB, _, _ := pSelectObject.Call(hdc, brush)
	oldP, _, _ := pSelectObject.Call(hdc, pen)
	pRoundRect.Call(hdc, uintptr(n.X), uintptr(n.Y), uintptr(n.X+nodeW), uintptr(n.Y+nodeH), 14, 14)
	pSelectObject.Call(hdc, oldB)
	pSelectObject.Call(hdc, oldP)
	pDeleteObject.Call(brush)
	pDeleteObject.Call(pen)
	bar, _, _ := pCreateSolidBrush.Call(accent)
	r := rect{int32(n.X), int32(n.Y), int32(n.X + 8), int32(n.Y + nodeH)}
	pFillRect.Call(hdc, uintptr(unsafe.Pointer(&r)), bar)
	pDeleteObject.Call(bar)
	pSelectObject.Call(hdc, app.fontBold)
	drawText(hdc, n.Title, rect{int32(n.X + 18), int32(n.Y + 12), int32(n.X + nodeW - 10), int32(n.Y + 38)}, dtSingleLine|dtEndEllipsis, rgb(27, 42, 61))
	pSelectObject.Call(hdc, app.font)
	addr := n.Address
	if addr == "" {
		addr = "IP / имя не указано"
	}
	drawText(hdc, addr, rect{int32(n.X + 18), int32(n.Y + 43), int32(n.X + nodeW - 10), int32(n.Y + 67)}, dtSingleLine|dtEndEllipsis, rgb(60, 112, 160))
	desc := n.Description
	if desc == "" {
		desc = "Добавьте описание справа"
	}
	drawText(hdc, desc, rect{int32(n.X + 18), int32(n.Y + 70), int32(n.X + nodeW - 10), int32(n.Y + 103)}, dtWordBreak|dtEndEllipsis, rgb(91, 104, 120))
}

func typeColor(t string) uintptr {
	switch t {
	case "1c":
		return rgb(206, 36, 53)
	case "sql":
		return rgb(0, 120, 170)
	case "file":
		return rgb(93, 76, 169)
	case "mail":
		return rgb(28, 151, 115)
	case "terminal":
		return rgb(220, 117, 25)
	default:
		return rgb(51, 105, 178)
	}
}
func drawText(hdc uintptr, s string, r rect, flags uintptr, color uintptr) {
	pSetTextColor.Call(hdc, color)
	u := ptr(s)
	pDrawText.Call(hdc, uintptr(unsafe.Pointer(u)), signed(-1), uintptr(unsafe.Pointer(&r)), flags)
}
func hitNode(x, y int) int {
	for i := len(app.diagram.Nodes) - 1; i >= 0; i-- {
		n := app.diagram.Nodes[i]
		if x >= n.X && x <= n.X+nodeW && y >= n.Y && y <= n.Y+nodeH {
			return n.ID
		}
	}
	return 0
}
func redraw() {
	if app.canvas != 0 {
		pInvalidateRect.Call(app.canvas, 0, 1)
	}
}

func updateEditor() {
	n := app.diagram.NodeByID(app.selected)
	setEditorEnabled(n != nil)
	if n == nil {
		setText(app.titleEdit, "")
		setText(app.addressEdit, "")
		setText(app.descriptionEdit, "")
		return
	}
	setText(app.titleEdit, n.Title)
	setText(app.addressEdit, n.Address)
	setText(app.descriptionEdit, n.Description)
}
func setEditorEnabled(v bool) {
	x := uintptr(0)
	if v {
		x = 1
	}
	for _, h := range []uintptr{app.titleEdit, app.addressEdit, app.descriptionEdit, app.applyButton} {
		if h != 0 {
			pEnableWindow.Call(h, x)
		}
	}
}
func applyEditor() {
	n := app.diagram.NodeByID(app.selected)
	if n == nil {
		return
	}
	n.Title = windowText(app.titleEdit)
	n.Address = windowText(app.addressEdit)
	n.Description = windowText(app.descriptionEdit)
	app.dirty = true
	redraw()
	setStatus("Изменения объекта применены.")
}
func setText(h uintptr, s string) { pSetWindowText.Call(h, uintptr(unsafe.Pointer(ptr(s)))) }
func windowText(h uintptr) string {
	n, _, _ := pGetWindowTextLength.Call(h)
	b := make([]uint16, n+1)
	pGetWindowText.Call(h, uintptr(unsafe.Pointer(&b[0])), n+1)
	return syscall.UTF16ToString(b)
}
func setStatus(s string) { setText(app.status, s) }
func message(text, title string, flags uintptr) {
	pMessageBox.Call(app.main, uintptr(unsafe.Pointer(ptr(text))), uintptr(unsafe.Pointer(ptr(title))), flags)
}
func confirm(text string) bool {
	r, _, _ := pMessageBox.Call(app.main, uintptr(unsafe.Pointer(ptr(text))), uintptr(unsafe.Pointer(ptr("Схема связей ЦОД"))), mbYesNo|0x20)
	return r == idYes
}

func chooseFile(save bool) string {
	var buf [maxPath]uint16
	if app.filePath != "" {
		copy(buf[:], utf16.Encode([]rune(app.filePath)))
	} else if save {
		copy(buf[:], utf16.Encode([]rune("scheme.json")))
	}
	filter := utf16.Encode([]rune("Схемы ЦОД (*.json)\x00*.json\x00Все файлы (*.*)\x00*.*\x00\x00"))
	ofn := openFileName{Size: uint32(unsafe.Sizeof(openFileName{})), Owner: app.main, Filter: &filter[0], FilterIndex: 1, File: &buf[0], MaxFile: maxPath, DefExt: ptr("json")}
	var r uintptr
	if save {
		ofn.Title = ptr("Сохранить схему")
		ofn.Flags = ofExplorer | ofOverwritePrompt | ofPathMustExist
		r, _, _ = pGetSaveFileName.Call(uintptr(unsafe.Pointer(&ofn)))
	} else {
		ofn.Title = ptr("Открыть схему")
		ofn.Flags = ofExplorer | ofFileMustExist | ofPathMustExist
		r, _, _ = pGetOpenFileName.Call(uintptr(unsafe.Pointer(&ofn)))
	}
	if r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:])
}
func saveDiagram() {
	path := chooseFile(true)
	if path == "" {
		return
	}
	if filepath.Ext(path) == "" {
		path += ".json"
	}
	if err := app.diagram.Save(path); err != nil {
		message("Не удалось сохранить файл:\n"+err.Error(), "Ошибка", mbOK|mbIconError)
		return
	}
	app.filePath = path
	app.dirty = false
	setStatus("Схема сохранена: " + path)
	message("Схема успешно сохранена.", "Готово", mbOK|mbIconInformation)
}
func openDiagram() {
	path := chooseFile(false)
	if path == "" {
		return
	}
	d, err := LoadDiagram(path)
	if err != nil {
		message("Не удалось открыть файл:\n"+err.Error(), "Ошибка", mbOK|mbIconError)
		return
	}
	app.diagram = d
	app.filePath = path
	app.selected = 0
	app.dirty = false
	updateEditor()
	redraw()
	setStatus("Открыта схема: " + path)
}

// References keep fmt available for useful panic diagnostics in GUI-subsystem builds.
var _ = fmt.Sprintf
