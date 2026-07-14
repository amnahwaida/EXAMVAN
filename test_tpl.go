package main
import (
	"html/template"
	"fmt"
)
func main() {
	_, err := template.ParseFiles("webui/templates/public/download.html")
	if err != nil {
		fmt.Println("Error:", err)
	} else {
		fmt.Println("OK")
	}
}
