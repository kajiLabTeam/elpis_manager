package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/BurntSushi/toml"
	_ "github.com/lib/pq"
	"github.com/rs/cors"
)

var requestID uint64
var logger *slog.Logger

type contextKey string

const requestIDKey = contextKey("requestID")

type ResponseCapture struct {
	http.ResponseWriter
	StatusCode int
	Body       bytes.Buffer
}

func (r *ResponseCapture) WriteHeader(statusCode int) {
	r.StatusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *ResponseCapture) Write(b []byte) (int, error) {
	r.Body.Write(b)
	return r.ResponseWriter.Write(b)
}

type Config struct {
	Mode         string
	ServerPort   string `toml:"server_port"`
	Docker       DockerConfig
	Local        LocalConfig
	Registration RegistrationConfig
}

type DockerConfig struct {
	ProxyURL         string `toml:"proxy_url"`
	EstimationURL    string `toml:"estimation_url"`
	InquiryURL       string `toml:"inquiry_url"`
	DBConnStr        string `toml:"db_conn_str"`
	SkipRegistration bool   `toml:"skip_registration"`
}

type LocalConfig struct {
	ProxyURL         string `toml:"proxy_url"`
	EstimationURL    string `toml:"estimation_url"`
	InquiryURL       string `toml:"inquiry_url"`
	DBConnStr        string `toml:"db_conn_str"`
	SkipRegistration bool   `toml:"skip_registration"`
}

type RegistrationConfig struct {
	SystemURI string `toml:"system_uri"`
}

type UploadResponse struct {
	Message string `json:"message"`
}

type RegisterRequest struct {
	Scheme string `json:"scheme"`
	Host   string `json:"host"`
	Port   int    `json:"port,omitempty"`
}

type PresenceSession struct {
	SessionID int        `json:"session_id"`
	UserID    int        `json:"user_id"`
	RoomID    int        `json:"room_id"`
	StartTime time.Time  `json:"start_time"`
	EndTime   *time.Time `json:"end_time"`
	LastSeen  time.Time  `json:"last_seen"`
}

type UserPresenceDay struct {
	Date     string            `json:"date"`
	Sessions []PresenceSession `json:"sessions"`
}

type AllUsersPresenceDay struct {
	Date  string               `json:"date"`
	Users []UserPresenceDetail `json:"users"`
}

type UserPresenceDetail struct {
	UserID   int               `json:"user_id"`
	Sessions []PresenceSession `json:"sessions"`
}

type PresenceHistoryResponse struct {
	AllHistory []AllUsersPresenceDay `json:"all_history,omitempty"`
}

type UserPresenceResponse struct {
	UserID  int               `json:"user_id"`
	History []UserPresenceDay `json:"history"`
}

type CurrentOccupant struct {
	UserID   string    `json:"user_id"`
	LastSeen time.Time `json:"last_seen"`
}

type RoomOccupants struct {
	RoomID    int               `json:"room_id"`
	RoomName  string            `json:"room_name"`
	Occupants []CurrentOccupant `json:"occupants"`
}

type CurrentOccupantsResponse struct {
	Rooms []RoomOccupants `json:"rooms"`
}

type HealthCheckResponse struct {
	Status    string `json:"status"`
	Database  string `json:"database"`
	Timestamp string `json:"timestamp"`
}

type PredictionResponse struct {
	PredictedPercentage int `json:"predicted_percentage"`
}

type EstimationServerResponse struct {
	PercentageProcessed int `json:"percentage_processed"`
}

type InquiryRequest struct {
	WifiData string `json:"wifi_data"`
	BleData  string `json:"ble_data"`
}

type InquiryResponse struct {
	ServerConfidence int `json:"percentage_processed"`
}

type BeaconSignal struct {
	UUID  string
	BSSID string
	RSSI  float64
}

type WiFiSignal struct {
	SSID  string
	BSSID string
	RSSI  float64
}

func logConfig(ctx context.Context, msg string, args ...interface{}) {
	id, _ := ctx.Value(requestIDKey).(uint64)
	logger.Info(fmt.Sprintf(msg, args...), "request_id", id)
}

func logRequest(ctx context.Context, msg string, args ...interface{}) {
	id, _ := ctx.Value(requestIDKey).(uint64)
	logger.Info(fmt.Sprintf(msg, args...), "request_id", id)
}

func logError(ctx context.Context, msg string, args ...interface{}) {
	id, _ := ctx.Value(requestIDKey).(uint64)
	logger.Error(fmt.Sprintf(msg, args...), "request_id", id)
}

func logInfo(ctx context.Context, msg string, args ...interface{}) {
	id, _ := ctx.Value(requestIDKey).(uint64)
	logger.Info(fmt.Sprintf(msg, args...), "request_id", id)
}

func forwardFilesToEstimationServer(ctx context.Context, bleFilePath string, wifiFilePath string, estimationURL string) (int, error) {
	combinedFilePath := filepath.Join(os.TempDir(), fmt.Sprintf("combined_data_%d.csv", time.Now().Unix()))
	defer os.Remove(combinedFilePath)

	bleFile, err := os.Open(bleFilePath)
	if err != nil {
		logError(ctx, "Failed to open BLE file: %v", err)
		return 0, fmt.Errorf("failed to open BLE file: %v", err)
	}
	defer bleFile.Close()

	wifiFile, err := os.Open(wifiFilePath)
	if err != nil {
		logError(ctx, "Failed to open WiFi file: %v", err)
		return 0, fmt.Errorf("failed to open WiFi file: %v", err)
	}
	defer wifiFile.Close()

	bleReader := csv.NewReader(bleFile)
	wifiReader := csv.NewReader(wifiFile)

	bleRecords, err := bleReader.ReadAll()
	if err != nil {
		logError(ctx, "Failed to read BLE CSV: %v", err)
		return 0, fmt.Errorf("failed to read BLE CSV: %v", err)
	}

	wifiRecords, err := wifiReader.ReadAll()
	if err != nil {
		logError(ctx, "Failed to read WiFi CSV: %v", err)
		return 0, fmt.Errorf("failed to read WiFi CSV: %v", err)
	}

	combinedRecords := append(bleRecords, wifiRecords...)

	combinedFile, err := os.Create(combinedFilePath)
	if err != nil {
		logError(ctx, "Failed to create combined CSV file: %v", err)
		return 0, fmt.Errorf("failed to create combined CSV file: %v", err)
	}
	defer combinedFile.Close()

	writer := csv.NewWriter(combinedFile)
	if err := writer.WriteAll(combinedRecords); err != nil {
		logError(ctx, "Failed to write combined CSV: %v", err)
		return 0, fmt.Errorf("failed to write combined CSV: %v", err)
	}
	writer.Flush()

	var requestBody bytes.Buffer
	writerMultipart := multipart.NewWriter(&requestBody)
	filePart, err := writerMultipart.CreateFormFile("file", filepath.Base(combinedFilePath))
	if err != nil {
		logError(ctx, "Failed to create form file: %v", err)
		return 0, fmt.Errorf("failed to create form file: %v", err)
	}

	combinedData, err := os.Open(combinedFilePath)
	if err != nil {
		logError(ctx, "Failed to open combined CSV file: %v", err)
		return 0, fmt.Errorf("failed to open combined CSV file: %v", err)
	}
	defer combinedData.Close()

	_, err = io.Copy(filePart, combinedData)
	if err != nil {
		logError(ctx, "Failed to copy combined CSV data: %v", err)
		return 0, fmt.Errorf("failed to copy combined CSV data: %v", err)
	}

	writerMultipart.Close()

	req, err := http.NewRequest("POST", estimationURL, &requestBody)
	if err != nil {
		logError(ctx, "Failed to create request to estimation server: %v", err)
		return 0, fmt.Errorf("failed to create request to estimation server: %v", err)
	}
	req.Header.Set("Content-Type", writerMultipart.FormDataContentType())

	logInfo(ctx, "Sending request to estimation server")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logError(ctx, "Failed to send request to estimation server: %v", err)
		return 0, fmt.Errorf("failed to send request to estimation server: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logError(ctx, "Invalid response from estimation server. Status code: %d", resp.StatusCode)
		return 0, fmt.Errorf("invalid response from estimation server. Status code: %d", resp.StatusCode)
	}

	var predictionResp PredictionResponse
	if err := json.NewDecoder(resp.Body).Decode(&predictionResp); err != nil {
		logError(ctx, "Failed to decode response from estimation server: %v", err)
		return 0, fmt.Errorf("failed to decode response from estimation server: %v", err)
	}

	logInfo(ctx, "Received response from estimation server: %+v", predictionResp)
	percentage := int(predictionResp.PredictedPercentage)

	logInfo(ctx, "Received estimation confidence: %d", percentage)

	return percentage, nil
}

func handleSignalsServerSubmit(w http.ResponseWriter, r *http.Request, ctx context.Context, estimationURL string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed. Please use POST.", http.StatusMethodNotAllowed)
		return
	}

	logRequest(ctx, "Received POST /api/signals/server request")

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		logError(ctx, "Failed to parse multipart/form-data: %v", err)
		http.Error(w, "Failed to parse multipart/form-data", http.StatusBadRequest)
		return
	}

	bleFile, _, err := r.FormFile("ble_data")
	if err != nil {
		logError(ctx, "Failed to get ble_data file: %v", err)
		http.Error(w, "Failed to get ble_data file", http.StatusBadRequest)
		return
	}
	defer bleFile.Close()

	wifiFile, _, err := r.FormFile("wifi_data")
	if err != nil {
		logError(ctx, "Failed to get wifi_data file: %v", err)
		http.Error(w, "Failed to get wifi_data file", http.StatusBadRequest)
		return
	}
	defer wifiFile.Close()

	tempBleFilePath := filepath.Join(os.TempDir(), fmt.Sprintf("ble_data_%d.csv", time.Now().Unix()))
	if err := saveUploadedFile(ctx, bleFile, tempBleFilePath); err != nil {
		logError(ctx, "Failed to save ble_data file: %v", err)
		http.Error(w, "Failed to save ble_data file", http.StatusInternalServerError)
		return
	}
	defer os.Remove(tempBleFilePath)

	tempWifiFilePath := filepath.Join(os.TempDir(), fmt.Sprintf("wifi_data_%d.csv", time.Now().Unix()))
	if err := saveUploadedFile(ctx, wifiFile, tempWifiFilePath); err != nil {
		logError(ctx, "Failed to save wifi_data file: %v", err)
		http.Error(w, "Failed to save wifi_data file", http.StatusInternalServerError)
		return
	}
	defer os.Remove(tempWifiFilePath)

	percentage, err := forwardFilesToEstimationServer(ctx, tempBleFilePath, tempWifiFilePath, estimationURL)
	if err != nil {
		logError(ctx, "Failed to forward to estimation server: %v", err)
		http.Error(w, fmt.Sprintf("Failed to forward to estimation server: %v", err), http.StatusInternalServerError)
		return
	}

	percentageInt := percentage

	response := EstimationServerResponse{
		PercentageProcessed: percentageInt,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logError(ctx, "Failed to encode JSON response: %v", err)
		http.Error(w, "Failed to encode JSON response", http.StatusInternalServerError)
		return
	}

	logRequest(ctx, "Completed processing POST /api/signals/server request")
}

func parseBLECSV(ctx context.Context, filePath string) ([]BeaconSignal, error) {
	file, err := os.Open(filePath)
	if err != nil {
		logError(ctx, "Failed to open BLE CSV file: %v", err)
		return nil, fmt.Errorf("failed to open BLE CSV file: %v", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		logError(ctx, "Failed to read BLE CSV: %v", err)
		return nil, fmt.Errorf("failed to read BLE CSV: %v", err)
	}

	var signals []BeaconSignal
	for _, record := range records {
		if len(record) < 3 {
			continue
		}
		rssi, err := strconv.ParseFloat(strings.TrimSpace(record[2]), 64)
		if err != nil {
			continue
		}
		signal := BeaconSignal{
			UUID:  strings.TrimSpace(record[1]),
			BSSID: "",
			RSSI:  rssi,
		}
		signals = append(signals, signal)
	}

	return signals, nil
}

func parseWifiCSV(ctx context.Context, filePath string) ([]WiFiSignal, error) {
	file, err := os.Open(filePath)
	if err != nil {
		logError(ctx, "Failed to open WiFi CSV file: %v", err)
		return nil, fmt.Errorf("failed to open WiFi CSV file: %v", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		logError(ctx, "Failed to read WiFi CSV: %v", err)
		return nil, fmt.Errorf("failed to read WiFi CSV: %v", err)
	}

	var signals []WiFiSignal
	for _, record := range records {
		if len(record) < 3 {
			continue
		}
		rssi, err := strconv.ParseFloat(strings.TrimSpace(record[2]), 64)
		if err != nil {
			continue
		}
		signal := WiFiSignal{
			SSID:  strings.TrimSpace(record[0]),
			BSSID: strings.TrimSpace(record[1]),
			RSSI:  rssi,
		}
		signals = append(signals, signal)
	}

	return signals, nil
}

func getRoomIDByBeacon(ctx context.Context, db *sql.DB, beacon BeaconSignal) (int, error) {
	var roomID int
	query := `
        SELECT room_id FROM beacons 
        WHERE UPPER(service_uuid) = UPPER($1)
        LIMIT 1
    `
	err := db.QueryRow(query, beacon.UUID).Scan(&roomID)
	if err != nil {
		return 0, err
	}
	logInfo(ctx, "Found room ID=%d for beacon UUID=%s (RSSI=%.2f)", roomID, beacon.UUID, beacon.RSSI)
	return roomID, nil
}

func getRoomIDByWifi(ctx context.Context, db *sql.DB, wifi WiFiSignal) (int, error) {
	var roomID int
	query := `
        SELECT room_id FROM wifi_access_points 
        WHERE LOWER(bssid) = LOWER($1)
        LIMIT 1
    `
	err := db.QueryRow(query, wifi.BSSID).Scan(&roomID)
	if err != nil {
		return 0, err
	}
	logInfo(ctx, "Found room ID=%d for WiFi BSSID=%s (RSSI=%.2f)", roomID, wifi.BSSID, wifi.RSSI)
	return roomID, nil
}

func determineRoomID(ctx context.Context, db *sql.DB, bleFilePath string, wifiFilePath string) (int, error) {
	bleSignals, err := parseBLECSV(ctx, bleFilePath)
	if err != nil {
		return 0, err
	}

	wifiSignals, err := parseWifiCSV(ctx, wifiFilePath)
	if err != nil {
		return 0, err
	}

	if len(bleSignals) == 0 && len(wifiSignals) == 0 {
		logError(ctx, "No BLE and WiFi signals found")
		return 0, fmt.Errorf("no BLE and WiFi signals found")
	}

	var bleRoomID int
	for _, beacon := range bleSignals {
		roomID, err := getRoomIDByBeacon(ctx, db, beacon)
		if err != nil {
			continue
		}
		bleRoomID = roomID
		break
	}

	var wifiRoomID int
	for _, wifi := range wifiSignals {
		roomID, err := getRoomIDByWifi(ctx, db, wifi)
		if err != nil {
			continue
		}
		wifiRoomID = roomID
		break
	}

	if bleRoomID != 0 {
		return bleRoomID, nil
	} else if wifiRoomID != 0 {
		return wifiRoomID, nil
	} else {
		logError(ctx, "No valid BLE or WiFi access points found")
		return 0, fmt.Errorf("no valid BLE or WiFi access points found")
	}
}

func forwardFilesToInquiryServer(ctx context.Context, wifiFilePath string, bleFilePath string, inquiryURL string, confidence int) (int, error) {
	wifiData, err := os.ReadFile(wifiFilePath)
	if err != nil {
		logError(ctx, "Failed to read WiFi data: %v", err)
		return 0, fmt.Errorf("failed to read WiFi data: %v", err)
	}

	bleData, err := os.ReadFile(bleFilePath)
	if err != nil {
		logError(ctx, "Failed to read BLE data: %v", err)
		return 0, fmt.Errorf("failed to read BLE data: %v", err)
	}

	inquiryReq := InquiryRequest{
		WifiData: string(wifiData),
		BleData:  string(bleData),
	}

	reqBody, err := json.Marshal(inquiryReq)
	if err != nil {
		logError(ctx, "Failed to encode inquiry request: %v", err)
		return 0, fmt.Errorf("failed to encode inquiry request: %v", err)
	}

	logInfo(ctx, "Sending request to inquiry server")

	resp, err := http.Post(inquiryURL, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		logError(ctx, "Failed to send request to inquiry server: %v", err)
		return 0, fmt.Errorf("failed to send request to inquiry server: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logError(ctx, "Invalid response from inquiry server. Status code: %d", resp.StatusCode)
		return 0, fmt.Errorf("invalid response from inquiry server. Status code: %d", resp.StatusCode)
	}

	var inquiryResp InquiryResponse
	if err := json.NewDecoder(resp.Body).Decode(&inquiryResp); err != nil {
		logError(ctx, "Failed to decode response from inquiry server: %v", err)
		return 0, fmt.Errorf("failed to decode response from inquiry server: %v", err)
	}

	logInfo(ctx, "Received response from inquiry server: %+v", inquiryResp)
	logInfo(ctx, "Received inquiry confidence: %d", inquiryResp.ServerConfidence)

	return inquiryResp.ServerConfidence, nil
}

func getUserID(r *http.Request) string {
	username, _, ok := r.BasicAuth()
	if ok && username != "" {
		return username
	}
	return "anonymous"
}

func getUserIDFromDB(ctx context.Context, db *sql.DB, username string) (int, error) {
	var userID int
	err := db.QueryRowContext(ctx, "SELECT id FROM users WHERE user_id = $1", username).Scan(&userID)
	if err != nil {
		logError(ctx, "Failed to get user ID: %v", err)
		return 0, err
	}
	return userID, nil
}

func saveUploadedFile(ctx context.Context, file multipart.File, path string) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		logError(ctx, "Failed to seek file: %v", err)
		return err
	}

	outFile, err := os.Create(path)
	if err != nil {
		logError(ctx, "Failed to create file: %v", err)
		return err
	}
	defer outFile.Close()

	if _, err := io.Copy(outFile, file); err != nil {
		logError(ctx, "Failed to copy file: %v", err)
		return err
	}
	return nil
}

func startUserSession(ctx context.Context, db *sql.DB, userID int, roomID int, startTime time.Time) error {
	_, err := db.ExecContext(ctx, `
        INSERT INTO user_presence_sessions (user_id, room_id, start_time, last_seen)
        VALUES ($1, $2, $3, $3)
    `, userID, roomID, startTime)
	if err != nil {
		logError(ctx, "Failed to start session: %v", err)
		return fmt.Errorf("failed to start session: %v", err)
	}
	return nil
}

func endUserSession(ctx context.Context, db *sql.DB, userID int, endTime time.Time) error {
	result, err := db.ExecContext(ctx, `
        UPDATE user_presence_sessions
        SET end_time = $1
        WHERE user_id = $2 AND end_time IS NULL
    `, endTime, userID)
	if err != nil {
		logError(ctx, "Failed to end session: %v", err)
		return fmt.Errorf("failed to end session: %v", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		logError(ctx, "Failed to get RowsAffected: %v", err)
		return fmt.Errorf("failed to get RowsAffected: %v", err)
	}
	if rowsAffected > 0 {
		logInfo(ctx, "Ended session for user ID %d at %s", userID, endTime)
	}
	return nil
}

func updateLastSeen(ctx context.Context, db *sql.DB, userID int, lastSeen time.Time) error {
	result, err := db.ExecContext(ctx, `
        UPDATE user_presence_sessions
        SET last_seen = $1
        WHERE user_id = $2 AND end_time IS NULL
    `, lastSeen, userID)
	if err != nil {
		logError(ctx, "Failed to update last_seen: %v", err)
		return fmt.Errorf("failed to update last_seen: %v", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		logError(ctx, "Failed to get RowsAffected: %v", err)
		return fmt.Errorf("failed to get RowsAffected: %v", err)
	}
	if rowsAffected > 0 {
		logInfo(ctx, "Updated last_seen for user ID %d", userID)
	}
	return nil
}

func updateUserPresence(ctx context.Context, db *sql.DB, userID int, estimationConfidence int, inquiryConfidence int, lastSeen time.Time, roomID int) error {
	if inquiryConfidence > estimationConfidence {
		err := endUserSession(ctx, db, userID, lastSeen)
		if err != nil {
			return fmt.Errorf("failed to end session: %v", err)
		}
	} else {
		var existingRoomID int
		err := db.QueryRowContext(ctx, `
            SELECT room_id FROM user_presence_sessions
            WHERE user_id = $1 AND end_time IS NULL
        `, userID).Scan(&existingRoomID)

		if err != nil {
			if err == sql.ErrNoRows {
				err = startUserSession(ctx, db, userID, roomID, lastSeen)
				if err != nil {
					return fmt.Errorf("failed to start new session: %v", err)
				}
				logInfo(ctx, "Started new session for user ID %d in room ID %d", userID, roomID)
			} else {
				return fmt.Errorf("failed to get current session: %v", err)
			}
		} else {
			err = updateLastSeen(ctx, db, userID, lastSeen)
			if err != nil {
				return fmt.Errorf("failed to update last_seen: %v", err)
			}
		}
	}
	return nil
}

func handleSignalsSubmit(w http.ResponseWriter, r *http.Request, ctx context.Context, db *sql.DB, estimationURL string, inquiryURL string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed. Please use POST.", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		logError(ctx, "Failed to parse request: %v", err)
		http.Error(w, "Failed to parse request", http.StatusBadRequest)
		return
	}

	wifiFile, _, err := r.FormFile("wifi_data")
	if err != nil {
		logError(ctx, "Failed to read WiFi data file: %v", err)
		http.Error(w, "Failed to read WiFi data file", http.StatusBadRequest)
		return
	}
	defer wifiFile.Close()

	bleFile, _, err := r.FormFile("ble_data")
	if err != nil {
		logError(ctx, "Failed to read BLE data file: %v", err)
		http.Error(w, "Failed to read BLE data file", http.StatusBadRequest)
		return
	}
	defer bleFile.Close()

	username := getUserID(r)
	userID, err := getUserIDFromDB(ctx, db, username)
	if err != nil {
		logError(ctx, "User not found: %v", err)
		http.Error(w, "User not found", http.StatusUnauthorized)
		return
	}

	currentDate := time.Now().Format("2006-01-02")
	baseDir := "./uploads"
	dateDir := filepath.Join(baseDir, currentDate)
	userDir := filepath.Join(dateDir, username)

	if err := os.MkdirAll(userDir, os.ModePerm); err != nil {
		logError(ctx, "Failed to create directory: %v", err)
		http.Error(w, "Failed to create directory", http.StatusInternalServerError)
		return
	}

	currentTime := time.Now()
	unixTime := currentTime.Unix()
	wifiFileName := fmt.Sprintf("wifi_data_%d.csv", unixTime)
	bleFileName := fmt.Sprintf("ble_data_%d.csv", unixTime)

	wifiFilePath := filepath.Join(userDir, wifiFileName)
	bleFilePath := filepath.Join(userDir, bleFileName)

	if err := saveUploadedFile(ctx, wifiFile, wifiFilePath); err != nil {
		logError(ctx, "Failed to save WiFi data: %v", err)
		http.Error(w, "Failed to save WiFi data", http.StatusInternalServerError)
		return
	}
	if err := saveUploadedFile(ctx, bleFile, bleFilePath); err != nil {
		logError(ctx, "Failed to save BLE data: %v", err)
		http.Error(w, "Failed to save BLE data", http.StatusInternalServerError)
		return
	}

	wifiFileInfo, err := os.Stat(wifiFilePath)
	if err != nil {
		logError(ctx, "Failed to verify WiFi data: %v", err)
		http.Error(w, "Failed to verify WiFi data", http.StatusInternalServerError)
		return
	}

	bleFileInfo, err := os.Stat(bleFilePath)
	if err != nil {
		logError(ctx, "Failed to verify BLE data: %v", err)
		http.Error(w, "Failed to verify BLE data", http.StatusInternalServerError)
		return
	}

	var emptyFiles []string
	if wifiFileInfo.Size() == 0 {
		emptyFiles = append(emptyFiles, "WiFi data file is empty")
	}
	if bleFileInfo.Size() == 0 {
		emptyFiles = append(emptyFiles, "BLE data file is empty")
	}

	if len(emptyFiles) > 0 {
		errorMessage := strings.Join(emptyFiles, "; ")
		logError(ctx, "User ID %d uploaded empty files", userID)
		http.Error(w, errorMessage, http.StatusBadRequest)
		return
	}

	estimationConfidence, err := forwardFilesToEstimationServer(ctx, bleFilePath, wifiFilePath, estimationURL)
	if err != nil {
		logError(ctx, "Failed to forward to estimation server: %v", err)
		http.Error(w, fmt.Sprintf("Failed to forward to estimation server: %v", err), http.StatusInternalServerError)
		return
	}

	var roomID int
	if estimationConfidence >= 20 && estimationConfidence <= 70 {
		inquiryConfidence, err := forwardFilesToInquiryServer(ctx, wifiFilePath, bleFilePath, inquiryURL, estimationConfidence)
		if err != nil {
			logError(ctx, "Failed to forward to inquiry server: %v", err)
			http.Error(w, fmt.Sprintf("Failed to forward to inquiry server: %v", err), http.StatusInternalServerError)
			return
		}

		if estimationConfidence >= inquiryConfidence {
			roomID, err = determineRoomID(ctx, db, bleFilePath, wifiFilePath)
			if err != nil {
				logError(ctx, "Failed to determine room ID: %v", err)
				http.Error(w, fmt.Sprintf("Failed to determine room ID: %v", err), http.StatusInternalServerError)
				return
			}
			logInfo(ctx, "Determined room ID %d for user ID %d", roomID, userID)

			err = updateUserPresence(ctx, db, userID, estimationConfidence, inquiryConfidence, currentTime, roomID)
			if err != nil {
				logError(ctx, "Failed to update presence for user ID %d: %v", userID, err)
			}
		} else {
			err = endUserSession(ctx, db, userID, currentTime)
			if err != nil {
				logError(ctx, "Failed to end session for user ID %d: %v", userID, err)
			} else {
				logInfo(ctx, "Ended session for user ID %d", userID)
			}
		}
	} else {
		if estimationConfidence > 70 {
			roomID, err = determineRoomID(ctx, db, bleFilePath, wifiFilePath)
			if err != nil {
				logError(ctx, "Failed to determine room ID: %v", err)
				http.Error(w, fmt.Sprintf("Failed to determine room ID: %v", err), http.StatusInternalServerError)
				return
			}
			logInfo(ctx, "Determined room ID %d for user ID %d", roomID, userID)

			err = updateUserPresence(ctx, db, userID, estimationConfidence, 0, currentTime, roomID)
			if err != nil {
				logError(ctx, "Failed to update presence for user ID %d: %v", userID, err)
			}
		} else {
			err = endUserSession(ctx, db, userID, currentTime)
			if err != nil {
				logError(ctx, "Failed to end session for user ID %d: %v", userID, err)
			} else {
				logInfo(ctx, "Ended session for user ID %d", userID)
			}
		}
	}

	response := UploadResponse{Message: "Received signal data"}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logError(ctx, "Failed to encode JSON response: %v", err)
		http.Error(w, "Failed to encode JSON response", http.StatusInternalServerError)
		return
	}
}

func handleSignalsServer(w http.ResponseWriter, r *http.Request, ctx context.Context, db *sql.DB, estimationURL string, inquiryURL string) {
	handleSignalsServerSubmit(w, r, ctx, estimationURL)
}

func handlePresenceHistory(w http.ResponseWriter, r *http.Request, ctx context.Context, db *sql.DB) {
	dateStr := r.URL.Query().Get("date")
	var since time.Time
	var err error

	if dateStr != "" {
		since, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			logError(ctx, "Invalid date parameter: %v", err)
			http.Error(w, "Invalid date parameter. Format should be YYYY-MM-DD.", http.StatusBadRequest)
			return
		}
		since = time.Date(since.Year(), since.Month(), since.Day(), 0, 0, 0, 0, since.Location())
	} else {
		since = time.Now().AddDate(0, -1, 0)
	}

	sessions, err := fetchAllSessions(ctx, db, since)
	if err != nil {
		logError(ctx, "Failed to fetch presence history: %v", err)
		http.Error(w, "Failed to fetch presence history", http.StatusInternalServerError)
		return
	}

	dayUserMap := make(map[string]map[int][]PresenceSession)
	for _, session := range sessions {
		date := session.StartTime.Format("2006-01-02")
		if _, exists := dayUserMap[date]; !exists {
			dayUserMap[date] = make(map[int][]PresenceSession)
		}
		dayUserMap[date][session.UserID] = append(dayUserMap[date][session.UserID], session)
	}

	var allHistory []AllUsersPresenceDay
	for date, usersMap := range dayUserMap {
		var users []UserPresenceDetail
		for userID, userSessions := range usersMap {
			users = append(users, UserPresenceDetail{
				UserID:   userID,
				Sessions: userSessions,
			})
		}
		allHistory = append(allHistory, AllUsersPresenceDay{
			Date:  date,
			Users: users,
		})
	}

	sort.Slice(allHistory, func(i, j int) bool {
		return allHistory[i].Date < allHistory[j].Date
	})

	response := PresenceHistoryResponse{
		AllHistory: allHistory,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logError(ctx, "Failed to encode JSON response: %v", err)
		http.Error(w, "Failed to encode JSON response", http.StatusInternalServerError)
	}
}

func fetchAllSessions(ctx context.Context, db *sql.DB, since time.Time) ([]PresenceSession, error) {
	rows, err := db.QueryContext(ctx, `
        SELECT session_id, user_id, room_id, start_time, end_time, last_seen
        FROM user_presence_sessions
        WHERE start_time >= $1
        ORDER BY start_time
    `, since)
	if err != nil {
		logError(ctx, "Failed to query sessions: %v", err)
		return nil, err
	}
	defer rows.Close()

	var sessions []PresenceSession
	for rows.Next() {
		var session PresenceSession
		var endTime sql.NullTime
		if err := rows.Scan(&session.SessionID, &session.UserID, &session.RoomID, &session.StartTime, &endTime, &session.LastSeen); err != nil {
			continue
		}
		if endTime.Valid {
			session.EndTime = &endTime.Time
		} else {
			session.EndTime = nil
		}
		sessions = append(sessions, session)
	}

	if err := rows.Err(); err != nil {
		logError(ctx, "Error reading sessions: %v", err)
		return nil, err
	}

	return sessions, nil
}

func fetchUserSessions(ctx context.Context, db *sql.DB, userID int, since time.Time) ([]PresenceSession, error) {
	rows, err := db.QueryContext(ctx, `
        SELECT session_id, user_id, room_id, start_time, end_time, last_seen
        FROM user_presence_sessions
        WHERE user_id = $1 AND start_time >= $2
        ORDER BY start_time
    `, userID, since)
	if err != nil {
		logError(ctx, "Failed to query user sessions: %v", err)
		return nil, err
	}
	defer rows.Close()

	var sessions []PresenceSession
	for rows.Next() {
		var session PresenceSession
		var endTime sql.NullTime
		if err := rows.Scan(&session.SessionID, &session.UserID, &session.RoomID, &session.StartTime, &endTime, &session.LastSeen); err != nil {
			continue
		}
		if endTime.Valid {
			session.EndTime = &endTime.Time
		} else {
			session.EndTime = nil
		}
		sessions = append(sessions, session)
	}

	if err := rows.Err(); err != nil {
		logError(ctx, "Error reading user sessions: %v", err)
		return nil, err
	}

	return sessions, nil
}

func handleUserPresenceHistory(w http.ResponseWriter, r *http.Request, ctx context.Context, db *sql.DB, userID int) {
	dateStr := r.URL.Query().Get("date")
	var since time.Time
	var err error

	if dateStr != "" {
		since, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			logError(ctx, "Invalid date parameter: %v", err)
			http.Error(w, "Invalid date parameter. Format should be YYYY-MM-DD.", http.StatusBadRequest)
			return
		}
		since = time.Date(since.Year(), since.Month(), since.Day(), 0, 0, 0, 0, since.Location())
	} else {
		since = time.Now().AddDate(0, -1, 0)
	}

	sessions, err := fetchUserSessions(ctx, db, userID, since)
	if err != nil {
		logError(ctx, "Failed to fetch user presence history: %v", err)
		http.Error(w, "Failed to fetch user presence history", http.StatusInternalServerError)
		return
	}

	historyMap := make(map[string][]PresenceSession)
	for _, session := range sessions {
		date := session.StartTime.Format("2006-01-02")
		historyMap[date] = append(historyMap[date], session)
	}

	var userHistory []UserPresenceDay
	for date, sessions := range historyMap {
		userHistory = append(userHistory, UserPresenceDay{
			Date:     date,
			Sessions: sessions,
		})
	}

	sort.Slice(userHistory, func(i, j int) bool {
		return userHistory[i].Date < userHistory[j].Date
	})

	response := UserPresenceResponse{
		UserID:  userID,
		History: userHistory,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logError(ctx, "Failed to encode JSON response: %v", err)
		http.Error(w, "Failed to encode JSON response", http.StatusInternalServerError)
	}
}

func handleCurrentOccupants(w http.ResponseWriter, r *http.Request, ctx context.Context, db *sql.DB) {
	query := `
        SELECT 
            rooms.room_id, 
            rooms.room_name, 
            users.user_id, 
            user_presence_sessions.last_seen
        FROM 
            rooms
        LEFT JOIN 
            user_presence_sessions ON rooms.room_id = user_presence_sessions.room_id AND user_presence_sessions.end_time IS NULL
        LEFT JOIN 
            users ON user_presence_sessions.user_id = users.id
        ORDER BY 
            rooms.room_id, users.user_id
    `

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		logError(ctx, "Failed to get current occupants: %v", err)
		http.Error(w, "Failed to get current occupants", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	roomsMap := make(map[int]RoomOccupants)

	for rows.Next() {
		var roomID int
		var roomName string
		var userID sql.NullString
		var lastSeen sql.NullTime

		if err := rows.Scan(&roomID, &roomName, &userID, &lastSeen); err != nil {
			continue
		}

		if _, exists := roomsMap[roomID]; !exists {
			roomsMap[roomID] = RoomOccupants{
				RoomID:    roomID,
				RoomName:  roomName,
				Occupants: []CurrentOccupant{},
			}
		}

		if userID.Valid {
			occupant := CurrentOccupant{
				UserID:   userID.String,
				LastSeen: lastSeen.Time,
			}
			room := roomsMap[roomID]
			room.Occupants = append(room.Occupants, occupant)
			roomsMap[roomID] = room
		}
	}

	if err := rows.Err(); err != nil {
		logError(ctx, "Error reading current occupants: %v", err)
		http.Error(w, "Error reading current occupants", http.StatusInternalServerError)
		return
	}

	response := CurrentOccupantsResponse{
		Rooms: []RoomOccupants{},
	}
	for _, room := range roomsMap {
		response.Rooms = append(response.Rooms, room)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logError(ctx, "Failed to encode JSON response: %v", err)
		http.Error(w, "Failed to encode JSON response", http.StatusInternalServerError)
	}
}

func handleHealthCheck(w http.ResponseWriter, r *http.Request, ctx context.Context, db *sql.DB) {
	response := HealthCheckResponse{
		Status:    "ok",
		Timestamp: time.Now().Format(time.RFC3339),
	}

	if err := db.PingContext(ctx); err != nil {
		response.Status = "error"
		response.Database = "Unavailable"
	} else {
		response.Database = "Available"
	}

	w.Header().Set("Content-Type", "application/json")
	if response.Status == "ok" {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logError(ctx, "Failed to encode HealthCheck JSON response: %v", err)
	}
}

func cleanUpOldSessions(ctx context.Context, db *sql.DB, inactivityThreshold time.Duration) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		<-ticker.C
		cutoffTime := time.Now().Add(-inactivityThreshold)

		rows, err := db.QueryContext(ctx, `
            SELECT user_id, last_seen
            FROM user_presence_sessions
            WHERE end_time IS NULL AND last_seen < $1
        `, cutoffTime)
		if err != nil {
			logError(ctx, "Failed to query old sessions: %v", err)
			continue
		}

		var userID int
		var lastSeen time.Time
		var usersToEnd []int

		for rows.Next() {
			if err := rows.Scan(&userID, &lastSeen); err != nil {
				continue
			}
			usersToEnd = append(usersToEnd, userID)
		}
		rows.Close()

		for _, uid := range usersToEnd {
			endTime := time.Now()
			err := endUserSession(ctx, db, uid, endTime)
			if err == nil {
				logInfo(ctx, "Ended session for user ID %d", uid)
			} else {
				logError(ctx, "Failed to end session for user ID %d: %v", uid, err)
			}
		}
	}
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		id := atomic.AddUint64(&requestID, 1)

		unixTime := time.Now().Unix()

		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}

		userAgent := r.Header.Get("User-Agent")

		excludedPaths := map[string]bool{
			"/api/signals/server":      true,
			"/api/signals/submit":      true,
			"/api/fingerprint/collect": true,
		}

		excludeBody := excludedPaths[r.URL.Path]

		var requestBody string

		if r.Body != nil && !excludeBody {
			const maxBodySize = 10 * 1024 * 1024
			body, err := io.ReadAll(io.LimitReader(r.Body, maxBodySize))
			if err != nil {
				logger.Error("Failed to read request body", "request_id", id, "error", err)
			} else {
				requestBody = string(body)
				r.Body = io.NopCloser(bytes.NewBuffer(body))
			}
		}

		capture := &ResponseCapture{
			ResponseWriter: w,
			StatusCode:     http.StatusOK,
		}

		ctx := context.WithValue(r.Context(), requestIDKey, id)

		logRequest(ctx, "IP: %s | User-Agent: %s | Time: %d | Method: %s | URI: %s", ip, userAgent, unixTime, r.Method, r.RequestURI)

		if !excludeBody && requestBody != "" {
			logRequest(ctx, "Content: %s", sanitizeString(requestBody))
		}

		next.ServeHTTP(capture, r.WithContext(ctx))

		responseBody := capture.Body.String()
		responseLog := fmt.Sprintf("Status code: %d", capture.StatusCode)

		if responseBody != "" {
			responseLog += fmt.Sprintf(" | Response body: %s", sanitizeString(responseBody))
		}

		logRequest(ctx, responseLog)
	})
}

func sanitizeString(s string) string {
	const maxLength = 1000
	if len(s) > maxLength {
		return s[:maxLength] + "...(truncated)"
	}

	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func handleFingerprintCollect(w http.ResponseWriter, r *http.Request, ctx context.Context) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed. Please use POST.", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		logError(ctx, "Failed to parse multipart/form-data: %v", err)
		http.Error(w, "Failed to parse multipart/form-data", http.StatusBadRequest)
		return
	}

	roomIDStr := r.FormValue("room_id")
	if roomIDStr == "" {
		logError(ctx, "room_id is not specified")
		http.Error(w, "Please specify room_id.", http.StatusBadRequest)
		return
	}

	roomID, err := strconv.Atoi(roomIDStr)
	if err != nil {
		logError(ctx, "Invalid room_id: %v", err)
		http.Error(w, "room_id must be an integer.", http.StatusBadRequest)
		return
	}

	var sampleType string
	if roomID == 0 {
		sampleType = "negative"
	} else {
		sampleType = "positive"
	}

	wifiFile, _, err := r.FormFile("wifi_data")
	if err != nil {
		logError(ctx, "Failed to retrieve wifi_data file: %v", err)
		http.Error(w, "Failed to retrieve wifi_data file.", http.StatusBadRequest)
		return
	}
	defer wifiFile.Close()

	bleFile, _, err := r.FormFile("ble_data")
	if err != nil {
		logError(ctx, "Failed to retrieve ble_data file: %v", err)
		http.Error(w, "Failed to retrieve ble_data file.", http.StatusBadRequest)
		return
	}
	defer bleFile.Close()

	baseDir := "./estimation"
	sanitizedRoomID := filepath.Base(roomIDStr)
	var saveDir string
	if sampleType == "positive" {
		saveDir = filepath.Join(baseDir, "positive_samples", sanitizedRoomID)
	} else {
		saveDir = filepath.Join(baseDir, "negative_samples", sanitizedRoomID)
	}

	if err := os.MkdirAll(saveDir, os.ModePerm); err != nil {
		logError(ctx, "Failed to create save directory: %v", err)
		http.Error(w, "Failed to create save directory.", http.StatusInternalServerError)
		return
	}

	timestamp := time.Now().Unix()
	wifiFileName := fmt.Sprintf("wifi_data_%d.csv", timestamp)
	bleFileName := fmt.Sprintf("ble_data_%d.csv", timestamp)

	wifiFilePath := filepath.Join(saveDir, wifiFileName)
	bleFilePath := filepath.Join(saveDir, bleFileName)

	if err := saveUploadedFile(ctx, wifiFile, wifiFilePath); err != nil {
		logError(ctx, "Failed to save wifi_data: %v", err)
		http.Error(w, "Failed to save wifi_data.", http.StatusInternalServerError)
		return
	}

	if err := saveUploadedFile(ctx, bleFile, bleFilePath); err != nil {
		logError(ctx, "Failed to save ble_data: %v", err)
		http.Error(w, "Failed to save ble_data.", http.StatusInternalServerError)
		return
	}

	response := UploadResponse{Message: "Fingerprint data received successfully"}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logError(ctx, "Failed to encode JSON response: %v", err)
		http.Error(w, "Failed to create response.", http.StatusInternalServerError)
		return
	}

	logInfo(ctx, "Fingerprint data received successfully. Sample type: %s, RoomID: %s", sampleType, roomIDStr)
}

func main() {
	configPath := "config.toml"

	var config Config
	if _, err := toml.DecodeFile(configPath, &config); err != nil {
		// Initialize logger here before using it
		logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelError,
		}))
		logger.Error("Failed to read config file", "error", err)
		os.Exit(1)
	}

	mode := flag.String("mode", config.Mode, "Application mode (docker or local)")
	port := flag.String("port", config.ServerPort, "Server port")
	flag.Parse()

	var proxyURL, estimationURL, inquiryURL, dbConnStr string
	var skipRegistration bool

	if *mode == "local" {
		proxyURL = config.Local.ProxyURL
		estimationURL = config.Local.EstimationURL
		inquiryURL = config.Local.InquiryURL
		dbConnStr = config.Local.DBConnStr
		skipRegistration = config.Local.SkipRegistration
	} else {
		proxyURL = config.Docker.ProxyURL
		estimationURL = config.Docker.EstimationURL
		inquiryURL = config.Docker.InquiryURL
		dbConnStr = config.Docker.DBConnStr
		skipRegistration = config.Docker.SkipRegistration
	}

	// Initialize the logger with desired handler and options
	logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	logConfig(context.Background(), `
===========================================
		Server Configuration
-------------------------------------------
	Mode               : %s
	Server Port        : %s
	Proxy URL          : %s
	Estimation URL     : %s
	Inquiry URL        : %s
	Database ConnStr    : %s
	Skip Registration  : %v
	System URI         : %s
===========================================
`, *mode, *port, proxyURL, estimationURL, inquiryURL, dbConnStr, skipRegistration, config.Registration.SystemURI)

	db, err := sql.Open("postgres", dbConnStr)
	if err != nil {
		logError(context.Background(), "Failed to connect to database: %v", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		logError(context.Background(), "Failed to ping database: %v", err)
		os.Exit(1)
	}
	logInfo(context.Background(), "Successfully connected to database")

	if !skipRegistration {
		go func() {
			serverPortInt, err := strconv.Atoi(*port)
			if err != nil {
				logError(context.Background(), "Failed to convert port number: %v", err)
				os.Exit(1)
			}

			registerData := RegisterRequest{
				Scheme: "http",
				Host:   config.Registration.SystemURI,
				Port:   serverPortInt,
			}

			for {
				registerBody, err := json.Marshal(registerData)
				if err != nil {
					logError(context.Background(), "Failed to encode registration request: %v", err)
					logInfo(context.Background(), "Retrying registration...")
					time.Sleep(5 * time.Second)
					continue
				}

				resp, err := http.Post(proxyURL, "application/json", bytes.NewBuffer(registerBody))
				if err != nil {
					logError(context.Background(), "Registration error: %v", err)
					logInfo(context.Background(), "Retrying registration...")
					time.Sleep(5 * time.Second)
					continue
				}

				if resp.StatusCode != http.StatusOK {
					logError(context.Background(), "Failed to register server. Status code: %d", resp.StatusCode)
					resp.Body.Close()
					logInfo(context.Background(), "Retrying registration...")
					time.Sleep(5 * time.Second)
					continue
				}

				resp.Body.Close()
				logInfo(context.Background(), "Server registration completed.")
				break
			}
		}()
	}

	go cleanUpOldSessions(context.Background(), db, 10*time.Minute)

	mux := http.NewServeMux()

	mux.HandleFunc("/api/users/", func(w http.ResponseWriter, r *http.Request) {
		id := atomic.AddUint64(&requestID, 1)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) == 4 && parts[0] == "api" && parts[1] == "users" && parts[3] == "presence_history" && r.Method == http.MethodGet {
			userIDStr := parts[2]
			userID, err := strconv.Atoi(userIDStr)
			if err != nil {
				logError(ctx, "Invalid user ID: %v", err)
				http.Error(w, "Invalid user ID", http.StatusBadRequest)
				return
			}
			handleUserPresenceHistory(w, r, ctx, db, userID)
			return
		}
		http.NotFound(w, r)
	})

	mux.HandleFunc("/api/presence_history", func(w http.ResponseWriter, r *http.Request) {
		id := atomic.AddUint64(&requestID, 1)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		if r.Method != http.MethodGet {
			logError(ctx, "Method not allowed: %s", r.Method)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handlePresenceHistory(w, r, ctx, db)
	})

	mux.HandleFunc("/api/current_occupants", func(w http.ResponseWriter, r *http.Request) {
		id := atomic.AddUint64(&requestID, 1)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		if r.Method != http.MethodGet {
			logError(ctx, "Method not allowed: %s", r.Method)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handleCurrentOccupants(w, r, ctx, db)
	})

	mux.HandleFunc("/api/signals/submit", func(w http.ResponseWriter, r *http.Request) {
		id := atomic.AddUint64(&requestID, 1)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		handleSignalsSubmit(w, r, ctx, db, estimationURL, inquiryURL)
	})

	mux.HandleFunc("/api/signals/server", func(w http.ResponseWriter, r *http.Request) {
		id := atomic.AddUint64(&requestID, 1)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		handleSignalsServer(w, r, ctx, db, estimationURL, inquiryURL)
	})

	mux.HandleFunc("/api/fingerprint/collect", func(w http.ResponseWriter, r *http.Request) {
		id := atomic.AddUint64(&requestID, 1)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		handleFingerprintCollect(w, r, ctx)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		id := atomic.AddUint64(&requestID, 1)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		handleHealthCheck(w, r, ctx, db)
	})

	loggedMux := loggingMiddleware(mux)

	corsHandler := cors.New(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173", "https://elpis.kajilab.dev", "https://elpis-a.kajilab.dev", "https://elpis-b.kajilab.dev"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
	})

	finalHandler := corsHandler.Handler(loggedMux)

	logInfo(context.Background(), "Starting server on port %s. Mode: %s", *port, *mode)
	if err := http.ListenAndServe(":"+*port, finalHandler); err != nil {
		logError(context.Background(), "Failed to start server: %v", err)
		os.Exit(1)
	}
}
