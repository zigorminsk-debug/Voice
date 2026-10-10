//go:build !windows

package main

import "fmt"

func main() {
	fmt.Println("ЦОД is a native Windows application. Build it with: GOOS=windows GOARCH=amd64 go build -ldflags=\"-H windowsgui\" -o \"ЦОД.exe\" .")
}
