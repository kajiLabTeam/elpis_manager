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
	"strconv"
)

// UploadResponse represents the response for signal data upload.
type UploadResponse struct {
	Message string `json:"message"`
}

// ServerResponse represents the response for the signals server.
type ServerResponse struct {
	PercentageProcessed int `json:"percentage_processed"`
}

// RegisterRequest represents the registration request payload.
type RegisterRequest struct {
	SystemURI string `json:"system_uri"`
	Port      int    `json:"port"`
}

// parseCSV parses a CSV file from a multipart.File.
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

// forwardFilesToInquiry forwards the BLE and WiFi files to the /api/inquiry endpoint.
func forwardFilesToInquiry(wifiFile multipart.File, bleFile multipart.File) error {
	// Rewind the files to read from the beginning
	if _, err := wifiFile.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := bleFile.Seek(0, io.SeekStart); err != nil {
		return err
	}

	// Create a new multipart writer to build the request body
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add the WiFi file to the multipart form
	wifiPart, err := writer.CreateFormFile("wifi_data", "wifi_data.csv")
	if err != nil {
		return err
	}
	if _, err := io.Copy(wifiPart, wifiFile); err != nil {
		return err
	}

	// Add the BLE file to the multipart form
	blePart, err := writer.CreateFormFile("ble_data", "ble_data.csv")
	if err != nil {
		return err
	}
	if _, err := io.Copy(blePart, bleFile); err != nil {
		return err
	}

	// Close the multipart writer to finalize the form
	writer.Close()

	// Send the request to the /api/inquiry endpoint
	resp, err := http.Post("http://localhost:8080/api/inquiry", writer.FormDataContentType(), body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to forward files, status code: %d", resp.StatusCode)
	}

	return nil
}

// handleSignalsSubmit handles the /api/signals/submit endpoint.
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

	foundTargetMAC := false
	for _, record := range bleRecords {
		if len(record) > 1 && record[1] == "2E-3C-A8-03-7C-0A" {
			foundTargetMAC = true
			fmt.Println("Found target MAC address: 2E-3C-A8-03-7C-0A")
			break
		}
	}

	if !foundTargetMAC {
		err := forwardFilesToInquiry(wifiFile, bleFile)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error forwarding files to inquiry: %v", err), http.StatusInternalServerError)
			return
		}
	}

	response := UploadResponse{Message: "Signal data received"}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleSignalsServer handles the /api/signals/server endpoint.
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

	foundTargetMAC := false
	for _, record := range bleRecords {
		if len(record) > 1 && record[1] == "2E-3C-A8-03-7C-0A" {
			foundTargetMAC = true
			fmt.Println("Found target MAC address: 2E-3C-A8-03-7C-0A")
			break
		}
	}

	if !foundTargetMAC {
		err := forwardFilesToInquiry(wifiFile, bleFile)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error forwarding files to inquiry: %v", err), http.StatusInternalServerError)
			return
		}
	}

	response := ServerResponse{PercentageProcessed: 100}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func main() {
	// Check if registration should be skipped
	skipRegistration := true
	if val, exists := os.LookupEnv("SKIP_REGISTRATION"); exists {
		skipRegistration, _ = strconv.ParseBool(val)
	}

	if !skipRegistration {
		registerURL := "http://proxy:8080/api/register"
		registerData := RegisterRequest{
			SystemURI: "http://manager",
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
	}

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
