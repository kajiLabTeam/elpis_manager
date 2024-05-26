package main

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
)

type UploadResponse struct {
	Message string `json:"message"`
}

type ServerResponse struct {
	PercentageProcessed int `json:"percentage_processed"`
}

func parseCSV(file multipart.File) ([][]string, error) {
	reader := csv.NewReader(file)
	var records [][]string
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func handleSignalsSubmit(w http.ResponseWriter, r *http.Request) {
	wifiFile, _, err := r.FormFile("wifi_data")
	if err != nil {
		http.Error(w, "Error reading WiFi data file", http.StatusBadRequest)
		return
	}
	defer wifiFile.Close()

	bleFile, _, err := r.FormFile("ble_data")
	if err != nil {
		http.Error(w, "Error reading BLE data file", http.StatusBadRequest)
		return
	}
	defer bleFile.Close()

	_, err = parseCSV(wifiFile)
	if err != nil {
		http.Error(w, "Error parsing WiFi CSV", http.StatusBadRequest)
		return
	}

	_, err = parseCSV(bleFile)
	if err != nil {
		http.Error(w, "Error parsing BLE CSV", http.StatusBadRequest)
		return
	}

	response := UploadResponse{Message: "Signal data received"}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func handleSignalsServer(w http.ResponseWriter, r *http.Request) {
	wifiFile, _, err := r.FormFile("wifi_data")
	if err != nil {
		http.Error(w, "Error reading WiFi data file", http.StatusBadRequest)
		return
	}
	defer wifiFile.Close()

	bleFile, _, err := r.FormFile("ble_data")
	if err != nil {
		http.Error(w, "Error reading BLE data file", http.StatusBadRequest)
		return
	}
	defer bleFile.Close()

	_, err = parseCSV(wifiFile)
	if err != nil {
		http.Error(w, "Error parsing WiFi CSV", http.StatusBadRequest)
		return
	}

	_, err = parseCSV(bleFile)
	if err != nil {
		http.Error(w, "Error parsing BLE CSV", http.StatusBadRequest)
		return
	}

	response := ServerResponse{PercentageProcessed: 100}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func main() {
	http.HandleFunc("/api/signals/submit", handleSignalsSubmit)
	http.HandleFunc("/api/signals/server", handleSignalsServer)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting server on port %s...", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Could not start server: %s\n", err)
	}
}
