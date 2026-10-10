package main

import (
	"encoding/json"
	"errors"
	"os"
)

const currentFormatVersion = 1

type Node struct {
	ID          int    `json:"id"`
	Type        string `json:"type"`
	Title       string `json:"title"`
	Address     string `json:"address,omitempty"`
	Description string `json:"description,omitempty"`
	X           int    `json:"x"`
	Y           int    `json:"y"`
}

type Link struct {
	From  int    `json:"from"`
	To    int    `json:"to"`
	Label string `json:"label,omitempty"`
}

type Diagram struct {
	Version int     `json:"version"`
	Title   string  `json:"title"`
	Nodes   []*Node `json:"nodes"`
	Links   []Link  `json:"links"`
	NextID  int     `json:"next_id"`
}

type Tool struct {
	Type, Title, Hint string
}

var tools = []Tool{
	{"exchange", "Обмен данными", "Шлюз интеграции и обмена данными"},
	{"1c", "1С сервер", "Сервер приложений 1С:Предприятие"},
	{"sql", "SQL Server", "Сервер баз данных Microsoft SQL Server"},
	{"file", "Файловый сервер", "Общие папки и файловые ресурсы"},
	{"mail", "Почтовый сервер", "Корпоративная электронная почта"},
	{"terminal", "Терминальный сервер", "Удалённые рабочие столы пользователей"},
}

func NewDiagram() *Diagram {
	return &Diagram{Version: currentFormatVersion, Title: "Схема серверов ЦОД", NextID: 1}
}

func SampleDiagram() *Diagram {
	d := NewDiagram()
	positions := [][2]int{{70, 80}, {310, 70}, {555, 80}, {70, 300}, {310, 310}, {555, 300}}
	for i, tool := range tools {
		n := d.AddNode(tool, positions[i][0], positions[i][1])
		n.Address = "10.0.0." + itoa(10+i)
		n.Description = tool.Hint + ". Выберите блок, чтобы изменить это описание."
	}
	d.Links = []Link{{1, 2, "HTTPS"}, {2, 3, "SQL"}, {4, 2, "файлы"}, {6, 2, "RDP"}, {5, 1, "SMTP"}}
	return d
}

func (d *Diagram) AddNode(tool Tool, x, y int) *Node {
	if d.NextID < 1 {
		d.NextID = 1
	}
	n := &Node{ID: d.NextID, Type: tool.Type, Title: tool.Title, Description: tool.Hint, X: x, Y: y}
	d.NextID++
	d.Nodes = append(d.Nodes, n)
	return n
}

func (d *Diagram) NodeByID(id int) *Node {
	for _, n := range d.Nodes {
		if n.ID == id {
			return n
		}
	}
	return nil
}

func (d *Diagram) AddLink(from, to int) bool {
	if from == to || d.NodeByID(from) == nil || d.NodeByID(to) == nil {
		return false
	}
	for _, l := range d.Links {
		if (l.From == from && l.To == to) || (l.From == to && l.To == from) {
			return false
		}
	}
	d.Links = append(d.Links, Link{From: from, To: to})
	return true
}

func (d *Diagram) DeleteNode(id int) {
	nodes := d.Nodes[:0]
	for _, n := range d.Nodes {
		if n.ID != id {
			nodes = append(nodes, n)
		}
	}
	d.Nodes = nodes
	links := d.Links[:0]
	for _, l := range d.Links {
		if l.From != id && l.To != id {
			links = append(links, l)
		}
	}
	d.Links = links
}

func (d *Diagram) Save(path string) error {
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func LoadDiagram(path string) (*Diagram, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var d Diagram
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, err
	}
	if d.Version != currentFormatVersion {
		return nil, errors.New("неподдерживаемая версия файла схемы")
	}
	maxID := 0
	for _, n := range d.Nodes {
		if n.ID > maxID {
			maxID = n.ID
		}
	}
	if d.NextID <= maxID {
		d.NextID = maxID + 1
	}
	return &d, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
