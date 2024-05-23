package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

func saveCSVHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse the multipart form, if it's a multipart form request
	err := r.ParseMultipartForm(10 << 20) // max memory 10MB
	if err != nil {
		http.Error(w, "Unable to parse form", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Error retrieving the file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	os.MkdirAll("./csv", os.ModePerm)

	dst, err := os.Create(filepath.Join("./csv", "uploaded.csv"))
	if err != nil {
		http.Error(w, "Unable to create file on the server", http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	_, err = io.Copy(dst, file)
	if err != nil {
		http.Error(w, "Error saving the file", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "File uploaded successfully")
}

func main() {
	http.HandleFunc("/upload", saveCSVHandler)
	fmt.Println("Server started at :8080")
	http.ListenAndServe(":8080", nil)
}
