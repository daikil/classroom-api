package main

import (
	"fmt"
	"net/http"
)

func main() {
	fmt.Println("Starting server at :8000")
	if err := http.ListenAndServe(":8000", nil); err != nil {
		fmt.Println("Failed to start server:", err)
	}
}
