package main

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"

	_ "github.com/lib/pq"
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
func forwardFilesToInquiry(wifiFile multipart.File, bleFile multipart.File, proxyURL string) error {
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
	resp, err := http.Post(fmt.Sprintf("%s/api/inquiry", proxyURL), writer.FormDataContentType(), body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to forward files, status code: %d", resp.StatusCode)
	}

	return nil
}

// getUUIDs fetches all UUIDs from the beacons table.
func getUUIDs(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query("SELECT service_uuid FROM beacons")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	uuids := make(map[string]bool)
	for rows.Next() {
		var uuid string
		if err := rows.Scan(&uuid); err != nil {
			return nil, err
		}
		uuids[uuid] = true
		log.Printf("Loaded UUID: %s", uuid)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return uuids, nil
}

// handleSignalsSubmit handles the /api/signals/submit endpoint.
func handleSignalsSubmit(w http.ResponseWriter, r *http.Request, proxyURL string, uuids map[string]bool) {
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

	foundTarget := false
	for _, record := range bleRecords {
		if len(record) > 1 {
			uuid := record[1] // UUID

			log.Printf("Processing BLE record UUID: %s\n", uuid) // Add this log to check the record being processed
			for targetUUID := range uuids {
				if uuid == targetUUID {
					foundTarget = true
					log.Printf("Found target UUID: %s\n", uuid)
					break
				}
			}
			if foundTarget {
				break
			}
		}
	}

	if foundTarget {
		log.Println("Target UUID found in BLE data.")
	} else {
		log.Println("Target UUID not found in BLE data.")
		err := forwardFilesToInquiry(wifiFile, bleFile, proxyURL)
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
func handleSignalsServer(w http.ResponseWriter, r *http.Request, proxyURL string, uuids map[string]bool) {
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

	foundTarget := false
	for _, record := range bleRecords {
		if len(record) > 1 {
			uuid := record[1]

			log.Printf("Processing BLE record UUID: %s\n", uuid)
			for targetUUID := range uuids {
				if uuid == targetUUID {
					foundTarget = true
					log.Printf("Found target UUID: %s\n", uuid)
					break
				}
			}
			if foundTarget {
				break
			}
		}
	}

	if foundTarget {
		log.Println("Target UUID found in BLE data.")
	} else {
		log.Println("Target UUID not found in BLE data.")
		err := forwardFilesToInquiry(wifiFile, bleFile, proxyURL)
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
	// Define command-line flags for mode and port
	mode := flag.String("mode", "docker", "Mode to run the application in (docker or local)")
	port := flag.String("port", "8010", "Port to run the server on")
	flag.Parse()

	var proxyURL, managerURL, dbConnStr string

	// Determine URLs based on the mode
	if *mode == "local" {
		proxyURL = "http://localhost:8080"
		managerURL = "http://localhost"
		dbConnStr = "postgres://myuser:mypassword@localhost:5433/managerdb?sslmode=disable"
	} else {
		proxyURL = "http://proxy:8080"
		managerURL = "http://manager"
		dbConnStr = "postgres://myuser:mypassword@postgres_manager:5432/managerdb?sslmode=disable"
	}

	// Connect to the database
	db, err := sql.Open("postgres", dbConnStr)
	if err != nil {
		log.Fatalf("Could not connect to the database: %v\n", err)
	}
	defer db.Close()

	// Fetch UUIDs from the database
	uuids, err := getUUIDs(db)
	if err != nil {
		log.Fatalf("Could not fetch UUIDs: %v\n", err)
	}

	skipRegistration := true
	if val, exists := os.LookupEnv("SKIP_REGISTRATION"); exists {
		skipRegistration, _ = strconv.ParseBool(val)
	}

	if !skipRegistration {
		registerURL := fmt.Sprintf("%s/api/register", proxyURL)
		registerData := RegisterRequest{
			SystemURI: managerURL,
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

	http.HandleFunc("/api/signals/submit", func(w http.ResponseWriter, r *http.Request) {
		handleSignalsSubmit(w, r, proxyURL, uuids)
	})
	http.HandleFunc("/api/signals/server", func(w http.ResponseWriter, r *http.Request) {
		handleSignalsServer(w, r, proxyURL, uuids)
	})

	log.Printf("Starting server on port %s in %s mode...", *port, *mode)
	if err := http.ListenAndServe(":"+*port, nil); err != nil {
		log.Fatalf("Could not start server: %s\n", err)
	}
}
