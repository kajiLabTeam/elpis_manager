package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
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

type RegisterRequest struct {
	SystemURI string `json:"system_uri"`
	Port      int    `json:"port"`
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

	bleRecords, err := parseCSV(bleFile)
	if err != nil {
		http.Error(w, "Error parsing BLE CSV", http.StatusBadRequest)
		return
	}

	for _, record := range bleRecords {
		if len(record) > 1 && record[1] == "2E-3C-A8-03-7C-0A" {
			fmt.Println("Found target MAC address: 2E-3C-A8-03-7C-0A")
		}
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

	bleRecords, err := parseCSV(bleFile)
	if err != nil {
		http.Error(w, "Error parsing BLE CSV", http.StatusBadRequest)
		return
	}

	for _, record := range bleRecords {
		if len(record) > 1 && record[1] == "2E-3C-A8-03-7C-0A" {
			fmt.Println("Found target MAC address: 2E-3C-A8-03-7C-0A")
		}
	}

	response := ServerResponse{PercentageProcessed: 100}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func main() {
	// Register the server by sending a POST request to /api/register
	registerURL := "http://localhost:8080/api/register"
	registerData := RegisterRequest{
		SystemURI: "http://localhost",
		Port:      8010,
	}

	registerBody, err := json.Marshal(registerData)
	if err != nil {
		log.Fatalf("Error encoding register request: %s\n", err)
	}

	resp, err := http.Post(registerURL, "application/json", bytes.NewBuffer(registerBody))
	if err != nil {
		log.Fatalf("Error registering server: %s\n", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Failed to register server, status code: %d\n", resp.StatusCode)
	}

	// Start the server
	http.HandleFunc("/api/signals/submit", handleSignalsSubmit)
	http.HandleFunc("/api/signals/server", handleSignalsServer)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8010"
	}

	log.Printf("Starting server on port %s...", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Could not start server: %s\n", err)
	}
}
