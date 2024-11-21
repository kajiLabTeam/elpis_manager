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

// ResponseCapture はレスポンスをキャプチャするためのカスタムレスポンスライターです。
type ResponseCapture struct {
	http.ResponseWriter
	StatusCode int
	Body       bytes.Buffer
}

// WriteHeader キャプチャされたステータスコードを保存します。
func (r *ResponseCapture) WriteHeader(statusCode int) {
	r.StatusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

// Write キャプチャされたボディを保存します。
func (r *ResponseCapture) Write(b []byte) (int, error) {
	r.Body.Write(b) // レスポンスボディをバッファに保存
	return r.ResponseWriter.Write(b)
}

// Config は設定ファイルの構造体です。
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

// 各種レスポンスおよびリクエストの構造体
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
	PredictedPercentage string `json:"predicted_percentage"`
}

type EstimationServerResponse struct {
	PercentageProcessed float64 `json:"percentage_processed"`
}

type InquiryRequest struct {
	WifiData           string  `json:"wifi_data"`
	BleData            string  `json:"ble_data"`
	PresenceConfidence float64 `json:"presence_confidence"`
}

type InquiryResponse struct {
	ServerConfidence float64 `json:"server_confidence"`
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

// ログヘルパー関数
func logConfig(format string, v ...interface{}) {
	log.Printf("[CONFIG] "+format, v...)
}

func logRequest(format string, v ...interface{}) {
	log.Printf("[REQUEST] "+format, v...)
}

func logError(format string, v ...interface{}) {
	log.Printf("[ERROR] "+format, v...)
}

func logInfo(format string, v ...interface{}) {
	log.Printf("[INFO] "+format, v...)
}

// forwardFilesToEstimationServer はBLEおよびWiFiデータファイルを推定サーバーに転送し、信頼度を受信します。
func forwardFilesToEstimationServer(bleFilePath string, wifiFilePath string, estimationURL string) (float64, error) {
	combinedFilePath := filepath.Join(os.TempDir(), fmt.Sprintf("combined_data_%d.csv", time.Now().Unix()))
	defer os.Remove(combinedFilePath)

	bleFile, err := os.Open(bleFilePath)
	if err != nil {
		return 0, fmt.Errorf("BLEファイルを開くのに失敗しました: %v", err)
	}
	defer bleFile.Close()

	wifiFile, err := os.Open(wifiFilePath)
	if err != nil {
		return 0, fmt.Errorf("WiFiファイルを開くのに失敗しました: %v", err)
	}
	defer wifiFile.Close()

	bleReader := csv.NewReader(bleFile)
	wifiReader := csv.NewReader(wifiFile)

	bleRecords, err := bleReader.ReadAll()
	if err != nil {
		return 0, fmt.Errorf("BLE CSVの読み取りに失敗しました: %v", err)
	}

	wifiRecords, err := wifiReader.ReadAll()
	if err != nil {
		return 0, fmt.Errorf("WiFi CSVの読み取りに失敗しました: %v", err)
	}

	combinedRecords := append(bleRecords, wifiRecords...)

	combinedFile, err := os.Create(combinedFilePath)
	if err != nil {
		return 0, fmt.Errorf("結合されたCSVファイルの作成に失敗しました: %v", err)
	}
	defer combinedFile.Close()

	writer := csv.NewWriter(combinedFile)
	if err := writer.WriteAll(combinedRecords); err != nil {
		return 0, fmt.Errorf("結合されたCSVの書き込みに失敗しました: %v", err)
	}
	writer.Flush()

	var requestBody bytes.Buffer
	writerMultipart := multipart.NewWriter(&requestBody)
	filePart, err := writerMultipart.CreateFormFile("file", filepath.Base(combinedFilePath))
	if err != nil {
		return 0, fmt.Errorf("フォームファイルの作成に失敗しました: %v", err)
	}

	combinedData, err := os.Open(combinedFilePath)
	if err != nil {
		return 0, fmt.Errorf("結合されたCSVファイルの開封に失敗しました: %v", err)
	}
	defer combinedData.Close()

	_, err = io.Copy(filePart, combinedData)
	if err != nil {
		return 0, fmt.Errorf("結合されたCSVデータのコピーに失敗しました: %v", err)
	}

	writerMultipart.Close()

	req, err := http.NewRequest("POST", estimationURL, &requestBody)
	if err != nil {
		return 0, fmt.Errorf("推定サーバーへのリクエストの作成に失敗しました: %v", err)
	}
	req.Header.Set("Content-Type", writerMultipart.FormDataContentType())

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("推定サーバーへのリクエストの送信に失敗しました: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("推定サーバーからの無効なレスポンス。ステータスコード: %d", resp.StatusCode)
	}

	var predictionResp PredictionResponse
	if err := json.NewDecoder(resp.Body).Decode(&predictionResp); err != nil {
		return 0, fmt.Errorf("推定サーバーのレスポンスの解析に失敗しました: %v", err)
	}

	percentageStr := strings.TrimSpace(strings.TrimSuffix(predictionResp.PredictedPercentage, "%"))
	percentage, err := strconv.ParseFloat(percentageStr, 64)
	if err != nil {
		return 0, fmt.Errorf("予測された割合の解析に失敗しました: %v", err)
	}

	logInfo("推定信頼度を受信しました: %.2f%%", percentage)

	return percentage, nil
}

// handleSignalsServerSubmit は /api/signals/server エンドポイントのハンドラーです。
func handleSignalsServerSubmit(w http.ResponseWriter, r *http.Request, estimationURL string) {
	if r.Method != http.MethodPost {
		http.Error(w, "メソッドが許可されていません。POSTを使用してください。", http.StatusMethodNotAllowed)
		return
	}

	logRequest("POST /api/signals/server リクエストを受信しました")

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		logError("multipart/form-dataの解析に失敗しました: %v", err)
		http.Error(w, "multipart/form-dataの解析に失敗しました", http.StatusBadRequest)
		return
	}

	bleFile, _, err := r.FormFile("ble_data")
	if err != nil {
		logError("ble_dataファイルの取得に失敗しました: %v", err)
		http.Error(w, "ble_dataファイルの取得に失敗しました", http.StatusBadRequest)
		return
	}
	defer bleFile.Close()

	wifiFile, _, err := r.FormFile("wifi_data")
	if err != nil {
		logError("wifi_dataファイルの取得に失敗しました: %v", err)
		http.Error(w, "wifi_dataファイルの取得に失敗しました", http.StatusBadRequest)
		return
	}
	defer wifiFile.Close()

	tempBleFilePath := filepath.Join(os.TempDir(), fmt.Sprintf("ble_data_%d.csv", time.Now().Unix()))
	if err := saveUploadedFile(bleFile, tempBleFilePath); err != nil {
		logError("ble_dataファイルの保存に失敗しました: %v", err)
		http.Error(w, "ble_dataファイルの保存に失敗しました", http.StatusInternalServerError)
		return
	}
	defer os.Remove(tempBleFilePath)

	tempWifiFilePath := filepath.Join(os.TempDir(), fmt.Sprintf("wifi_data_%d.csv", time.Now().Unix()))
	if err := saveUploadedFile(wifiFile, tempWifiFilePath); err != nil {
		logError("wifi_dataファイルの保存に失敗しました: %v", err)
		http.Error(w, "wifi_dataファイルの保存に失敗しました", http.StatusInternalServerError)
		return
	}
	defer os.Remove(tempWifiFilePath)

	percentage, err := forwardFilesToEstimationServer(tempBleFilePath, tempWifiFilePath, estimationURL)
	if err != nil {
		logError("推定サーバーへのファイル転送に失敗しました: %v", err)
		http.Error(w, fmt.Sprintf("推定サーバーエラー: %v", err), http.StatusInternalServerError)
		return
	}

	response := EstimationServerResponse{
		PercentageProcessed: percentage,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logError("JSONレスポンスのエンコードに失敗しました: %v", err)
		http.Error(w, "JSONレスポンスのエンコードに失敗しました", http.StatusInternalServerError)
		return
	}

	logRequest("POST /api/signals/server リクエストの処理が完了しました")
}

// 以下、他のハンドラー関数やユーティリティ関数も同様にログヘルパーを活用して整理します。

func parseBLECSV(filePath string) ([]BeaconSignal, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("BLE CSVファイルの開封に失敗しました: %v", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("BLE CSVの読み取りに失敗しました: %v", err)
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

func parseWifiCSV(filePath string) ([]WiFiSignal, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("WiFi CSVファイルの開封に失敗しました: %v", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("WiFi CSVの読み取りに失敗しました: %v", err)
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

func getRoomIDByBeacon(db *sql.DB, beacon BeaconSignal) (int, error) {
	var roomID int
	query := `
        SELECT room_id FROM beacons 
        WHERE UPPER(service_uuid) = UPPER($1)
        LIMIT 1
    `
	err := db.QueryRow(query, beacon.UUID).Scan(&roomID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("ビーコンが見つかりません: UUID=%s", beacon.UUID)
		}
		return 0, err
	}
	logInfo("ビーコン UUID=%s (RSSI=%.2f) に対して room ID=%d を見つけました", beacon.UUID, beacon.RSSI, roomID)
	return roomID, nil
}

func getRoomIDByWifi(db *sql.DB, wifi WiFiSignal) (int, error) {
	var roomID int
	query := `
        SELECT room_id FROM wifi_access_points 
        WHERE LOWER(bssid) = LOWER($1)
        LIMIT 1
    `
	err := db.QueryRow(query, wifi.BSSID).Scan(&roomID)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("WiFiアクセスポイントが見つかりません: BSSID=%s", wifi.BSSID)
		}
		return 0, err
	}
	logInfo("WiFi BSSID=%s (RSSI=%.2f) に対して room ID=%d を見つけました", wifi.BSSID, wifi.RSSI, roomID)
	return roomID, nil
}

func determineRoomID(db *sql.DB, bleFilePath string, wifiFilePath string) (int, error) {
	bleSignals, err := parseBLECSV(bleFilePath)
	if err != nil {
		return 0, err
	}

	wifiSignals, err := parseWifiCSV(wifiFilePath)
	if err != nil {
		return 0, err
	}

	if len(bleSignals) == 0 && len(wifiSignals) == 0 {
		return 0, fmt.Errorf("BLEおよびWiFi信号が見つかりません")
	}

	var bleRoomID int
	for _, beacon := range bleSignals {
		roomID, err := getRoomIDByBeacon(db, beacon)
		if err != nil {
			continue
		}
		bleRoomID = roomID
		break
	}

	var wifiRoomID int
	for _, wifi := range wifiSignals {
		roomID, err := getRoomIDByWifi(db, wifi)
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
		return 0, fmt.Errorf("有効なBLEまたはWiFiアクセスポイントが見つかりません")
	}
}

func forwardFilesToInquiryServer(wifiFilePath string, bleFilePath string, inquiryURL string, confidence float64) (float64, error) {
	wifiData, err := os.ReadFile(wifiFilePath)
	if err != nil {
		return 0, fmt.Errorf("WiFiデータの読み取りに失敗しました: %v", err)
	}

	bleData, err := os.ReadFile(bleFilePath)
	if err != nil {
		return 0, fmt.Errorf("BLEデータの読み取りに失敗しました: %v", err)
	}

	inquiryReq := InquiryRequest{
		WifiData:           string(wifiData),
		BleData:            string(bleData),
		PresenceConfidence: confidence,
	}

	reqBody, err := json.Marshal(inquiryReq)
	if err != nil {
		return 0, fmt.Errorf("問い合わせリクエストのエンコードに失敗しました: %v", err)
	}

	resp, err := http.Post(inquiryURL, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return 0, fmt.Errorf("問い合わせサーバーへのリクエストの送信に失敗しました: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("問い合わせサーバーからの無効なレスポンス。ステータスコード: %d", resp.StatusCode)
	}

	var inquiryResp InquiryResponse
	if err := json.NewDecoder(resp.Body).Decode(&inquiryResp); err != nil {
		return 0, fmt.Errorf("問い合わせサーバーのレスポンスの解析に失敗しました: %v", err)
	}

	logInfo("問い合わせ信頼度を受信しました: %.2f", inquiryResp.ServerConfidence)

	return inquiryResp.ServerConfidence, nil
}

func getUserID(r *http.Request) string {
	username, _, ok := r.BasicAuth()
	if ok && username != "" {
		return username
	}
	return "anonymous"
}

func getUserIDFromDB(db *sql.DB, username string) (int, error) {
	var userID int
	err := db.QueryRow("SELECT id FROM users WHERE user_id = $1", username).Scan(&userID)
	if err != nil {
		return 0, err
	}
	return userID, nil
}

func saveUploadedFile(file multipart.File, path string) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}

	outFile, err := os.Create(path)
	if err != nil {
		return err
	}
	defer outFile.Close()

	if _, err := io.Copy(outFile, file); err != nil {
		return err
	}
	return nil
}

func startUserSession(db *sql.DB, userID int, roomID int, startTime time.Time) error {
	_, err := db.Exec(`
        INSERT INTO user_presence_sessions (user_id, room_id, start_time, last_seen)
        VALUES ($1, $2, $3, $3)
    `, userID, roomID, startTime)
	if err != nil {
		return fmt.Errorf("セッションの開始に失敗しました: %v", err)
	}
	return nil
}

func endUserSession(db *sql.DB, userID int, endTime time.Time) error {
	result, err := db.Exec(`
        UPDATE user_presence_sessions
        SET end_time = $1
        WHERE user_id = $2 AND end_time IS NULL
    `, endTime, userID)
	if err != nil {
		return fmt.Errorf("セッションの終了に失敗しました: %v", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("RowsAffectedの取得に失敗しました: %v", err)
	}
	if rowsAffected > 0 {
		logInfo("ユーザーID %d のセッションを %s に終了しました", userID, endTime)
	}
	return nil
}

func updateLastSeen(db *sql.DB, userID int, lastSeen time.Time) error {
	result, err := db.Exec(`
        UPDATE user_presence_sessions
        SET last_seen = $1
        WHERE user_id = $2 AND end_time IS NULL
    `, lastSeen, userID)
	if err != nil {
		return fmt.Errorf("last_seenの更新に失敗しました: %v", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("RowsAffectedの取得に失敗しました: %v", err)
	}
	if rowsAffected > 0 {
		logInfo("ユーザーID %d のlast_seenを更新しました", userID)
	}
	return nil
}

func updateUserPresence(db *sql.DB, userID int, estimationConfidence float64, inquiryConfidence float64, lastSeen time.Time, roomID int) error {
	if inquiryConfidence > estimationConfidence {
		err := endUserSession(db, userID, lastSeen)
		if err != nil {
			return fmt.Errorf("セッションの終了に失敗しました: %v", err)
		}
	} else {
		var existingRoomID int
		err := db.QueryRow(`
            SELECT room_id FROM user_presence_sessions
            WHERE user_id = $1 AND end_time IS NULL
        `, userID).Scan(&existingRoomID)

		if err != nil {
			if err == sql.ErrNoRows {
				err = startUserSession(db, userID, roomID, lastSeen)
				if err != nil {
					return fmt.Errorf("新しいセッションの開始に失敗しました: %v", err)
				}
				logInfo("ユーザーID %d の新しいセッションを room ID %d で開始しました", userID, roomID)
			} else {
				return fmt.Errorf("現在のセッションの取得に失敗しました: %v", err)
			}
		} else {
			err = updateLastSeen(db, userID, lastSeen)
			if err != nil {
				return fmt.Errorf("last_seenの更新に失敗しました: %v", err)
			}
		}
	}
	return nil
}

// handleSignalsSubmit は /api/signals/submit エンドポイントのハンドラーです。
func handleSignalsSubmit(w http.ResponseWriter, r *http.Request, db *sql.DB, estimationURL string, inquiryURL string) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "リクエストの解析に失敗しました", http.StatusBadRequest)
		return
	}

	wifiFile, _, err := r.FormFile("wifi_data")
	if err != nil {
		http.Error(w, "WiFiデータファイルの読み取りに失敗しました", http.StatusBadRequest)
		return
	}
	defer wifiFile.Close()

	bleFile, _, err := r.FormFile("ble_data")
	if err != nil {
		http.Error(w, "BLEデータファイルの読み取りに失敗しました", http.StatusBadRequest)
		return
	}
	defer bleFile.Close()

	username := getUserID(r)
	userID, err := getUserIDFromDB(db, username)
	if err != nil {
		http.Error(w, "ユーザーが見つかりません", http.StatusUnauthorized)
		return
	}

	currentDate := time.Now().Format("2006-01-02")
	baseDir := "./uploads"
	dateDir := filepath.Join(baseDir, currentDate)
	userDir := filepath.Join(dateDir, username)

	if err := os.MkdirAll(userDir, os.ModePerm); err != nil {
		http.Error(w, "ディレクトリの作成に失敗しました", http.StatusInternalServerError)
		return
	}

	currentTime := time.Now()
	unixTime := currentTime.Unix()
	wifiFileName := fmt.Sprintf("wifi_data_%d.csv", unixTime)
	bleFileName := fmt.Sprintf("ble_data_%d.csv", unixTime)

	wifiFilePath := filepath.Join(userDir, wifiFileName)
	bleFilePath := filepath.Join(userDir, bleFileName)

	if err := saveUploadedFile(wifiFile, wifiFilePath); err != nil {
		http.Error(w, "WiFiデータの保存に失敗しました", http.StatusInternalServerError)
		return
	}
	if err := saveUploadedFile(bleFile, bleFilePath); err != nil {
		http.Error(w, "BLEデータの保存に失敗しました", http.StatusInternalServerError)
		return
	}

	wifiFileInfo, err := os.Stat(wifiFilePath)
	if err != nil {
		http.Error(w, "WiFiデータの検証に失敗しました", http.StatusInternalServerError)
		return
	}

	bleFileInfo, err := os.Stat(bleFilePath)
	if err != nil {
		http.Error(w, "BLEデータの検証に失敗しました", http.StatusInternalServerError)
		return
	}

	var emptyFiles []string
	if wifiFileInfo.Size() == 0 {
		emptyFiles = append(emptyFiles, "WiFiデータファイルが空です")
	}
	if bleFileInfo.Size() == 0 {
		emptyFiles = append(emptyFiles, "BLEデータファイルが空です")
	}

	if len(emptyFiles) > 0 {
		errorMessage := strings.Join(emptyFiles, "; ")
		http.Error(w, errorMessage, http.StatusBadRequest)
		logError("ユーザーID %d が空のファイルをアップロードしました", userID)
		return
	}

	estimationConfidence, err := forwardFilesToEstimationServer(bleFilePath, wifiFilePath, estimationURL)
	if err != nil {
		http.Error(w, fmt.Sprintf("推定サーバーへの転送エラー: %v", err), http.StatusInternalServerError)
		return
	}

	var roomID int
	if estimationConfidence >= 20.0 && estimationConfidence <= 70.0 {
		inquiryConfidence, err := forwardFilesToInquiryServer(wifiFilePath, bleFilePath, inquiryURL, estimationConfidence)
		if err != nil {
			http.Error(w, fmt.Sprintf("問い合わせサーバーへの転送エラー: %v", err), http.StatusInternalServerError)
			return
		}

		if estimationConfidence > inquiryConfidence {
			roomID, err = determineRoomID(db, bleFilePath, wifiFilePath)
			if err != nil {
				http.Error(w, fmt.Sprintf("部屋IDの決定に失敗しました: %v", err), http.StatusInternalServerError)
				return
			}
			logInfo("ユーザーID %d のために部屋ID %d を決定しました", userID, roomID)

			err = updateUserPresence(db, userID, estimationConfidence, inquiryConfidence, currentTime, roomID)
			if err != nil {
				logError("ユーザーID %d のプレゼンス更新に失敗しました: %v", userID, err)
			}
		} else {
			err = endUserSession(db, userID, currentTime)
			if err != nil {
				logError("ユーザーID %d のセッション終了に失敗しました: %v", userID, err)
			} else {
				logInfo("ユーザーID %d のセッションを終了しました", userID)
			}
		}
	} else {
		if estimationConfidence > 70.0 {
			roomID, err = determineRoomID(db, bleFilePath, wifiFilePath)
			if err != nil {
				http.Error(w, fmt.Sprintf("部屋IDの決定に失敗しました: %v", err), http.StatusInternalServerError)
				return
			}
			logInfo("ユーザーID %d のために部屋ID %d を決定しました", userID, roomID)

			err = updateUserPresence(db, userID, estimationConfidence, 0, currentTime, roomID)
			if err != nil {
				logError("ユーザーID %d のプレゼンス更新に失敗しました: %v", userID, err)
			}
		} else {
			err = endUserSession(db, userID, currentTime)
			if err != nil {
				logError("ユーザーID %d のセッション終了に失敗しました: %v", userID, err)
			} else {
				logInfo("ユーザーID %d のセッションを終了しました", userID)
			}
		}
	}

	response := UploadResponse{Message: "シグナルデータを受信しました"}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logError("JSONレスポンスのエンコードに失敗しました: %v", err)
		http.Error(w, "JSONレスポンスのエンコードに失敗しました", http.StatusInternalServerError)
	}
}

// handleSignalsServer は /api/signals/server エンドポイントのハンドラーです。
func handleSignalsServer(w http.ResponseWriter, r *http.Request, db *sql.DB, estimationURL string, inquiryURL string) {
	handleSignalsServerSubmit(w, r, estimationURL)
}

func handlePresenceHistory(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	dateStr := r.URL.Query().Get("date")
	var since time.Time
	var err error

	if dateStr != "" {
		since, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			http.Error(w, "日付パラメータが無効です。フォーマットはYYYY-MM-DDです。", http.StatusBadRequest)
			return
		}
		since = time.Date(since.Year(), since.Month(), since.Day(), 0, 0, 0, 0, since.Location())
	} else {
		since = time.Now().AddDate(0, -1, 0)
	}

	sessions, err := fetchAllSessions(db, since)
	if err != nil {
		http.Error(w, "プレゼンス履歴の取得に失敗しました", http.StatusInternalServerError)
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
		logError("JSONレスポンスのエンコードに失敗しました: %v", err)
		http.Error(w, "JSONレスポンスのエンコードに失敗しました", http.StatusInternalServerError)
	}
}

func fetchAllSessions(db *sql.DB, since time.Time) ([]PresenceSession, error) {
	rows, err := db.Query(`
        SELECT session_id, user_id, room_id, start_time, end_time, last_seen
        FROM user_presence_sessions
        WHERE start_time >= $1
        ORDER BY start_time
    `, since)
	if err != nil {
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
		return nil, err
	}

	return sessions, nil
}

func fetchUserSessions(db *sql.DB, userID int, since time.Time) ([]PresenceSession, error) {
	rows, err := db.Query(`
        SELECT session_id, user_id, room_id, start_time, end_time, last_seen
        FROM user_presence_sessions
        WHERE user_id = $1 AND start_time >= $2
        ORDER BY start_time
    `, userID, since)
	if err != nil {
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
		return nil, err
	}

	return sessions, nil
}

func handleUserPresenceHistory(w http.ResponseWriter, r *http.Request, db *sql.DB, userID int) {
	dateStr := r.URL.Query().Get("date")
	var since time.Time
	var err error

	if dateStr != "" {
		since, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			http.Error(w, "日付パラメータが無効です。フォーマットはYYYY-MM-DDです。", http.StatusBadRequest)
			return
		}
		since = time.Date(since.Year(), since.Month(), since.Day(), 0, 0, 0, 0, since.Location())
	} else {
		since = time.Now().AddDate(0, -1, 0)
	}

	sessions, err := fetchUserSessions(db, userID, since)
	if err != nil {
		http.Error(w, "ユーザープレゼンス履歴の取得に失敗しました", http.StatusInternalServerError)
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
		logError("JSONレスポンスのエンコードに失敗しました: %v", err)
		http.Error(w, "JSONレスポンスのエンコードに失敗しました", http.StatusInternalServerError)
	}
}

func handleCurrentOccupants(w http.ResponseWriter, r *http.Request, db *sql.DB) {
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

	rows, err := db.Query(query)
	if err != nil {
		http.Error(w, "現在の占有者の取得に失敗しました", http.StatusInternalServerError)
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
		http.Error(w, "現在の占有者の読み取り中にエラーが発生しました", http.StatusInternalServerError)
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
		logError("JSONレスポンスのエンコードに失敗しました: %v", err)
		http.Error(w, "JSONレスポンスのエンコードに失敗しました", http.StatusInternalServerError)
	}
}

func handleHealthCheck(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	response := HealthCheckResponse{
		Status:    "ok",
		Timestamp: time.Now().Format(time.RFC3339),
	}

	if err := db.Ping(); err != nil {
		response.Status = "error"
		response.Database = "接続不可"
	} else {
		response.Database = "接続可能"
	}

	w.Header().Set("Content-Type", "application/json")
	if response.Status == "ok" {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logError("HealthCheck JSONレスポンスのエンコードに失敗しました: %v", err)
	}
}

func cleanUpOldSessions(db *sql.DB, inactivityThreshold time.Duration) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		<-ticker.C
		cutoffTime := time.Now().Add(-inactivityThreshold)

		rows, err := db.Query(`
            SELECT user_id, last_seen
            FROM user_presence_sessions
            WHERE end_time IS NULL AND last_seen < $1
        `, cutoffTime)
		if err != nil {
			logError("古いセッションのクエリに失敗しました: %v", err)
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
			err := endUserSession(db, uid, endTime)
			if err == nil {
				logInfo("ユーザーID %d のセッションを終了しました", uid)
			} else {
				logError("ユーザーID %d のセッション終了に失敗しました: %v", uid, err)
			}
		}
	}
}

// loggingMiddleware はリクエストとレスポンスをログに記録します。
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
				logError("リクエストID %d: リクエストボディの読み取り中にエラーが発生しました: %v", id, err)
			} else {
				requestBody = string(body)
				r.Body = io.NopCloser(bytes.NewBuffer(body))
			}
		}

		// カスタムResponseWriterの作成
		capture := &ResponseCapture{
			ResponseWriter: w,
			StatusCode:     http.StatusOK, // デフォルトステータスコード
		}

		// リクエストログの作成
		logLine := fmt.Sprintf("リクエストID: %d | IP: %s | User-Agent: %s | 時間: %d | メソッド: %s | URI: %s",
			id, ip, userAgent, unixTime, r.Method, r.RequestURI)

		if !excludeBody && requestBody != "" {
			logLine += fmt.Sprintf(" | コンテンツ: %s", sanitizeString(requestBody))
		}

		logRequest(logLine)

		// 次のハンドラーを呼び出す
		next.ServeHTTP(capture, r)

		// レスポンスログの作成
		responseBody := capture.Body.String()
		responseLog := fmt.Sprintf("リクエストID: %d | ステータスコード: %d", id, capture.StatusCode)

		// レスポンスボディが空でなければログに追加
		if responseBody != "" {
			responseLog += fmt.Sprintf(" | レスポンスボディ: %s", sanitizeString(responseBody))
		}

		logRequest(responseLog)
	})
}

// sanitizeString はログに記録する前に文字列をサニタイズします。
func sanitizeString(s string) string {
	const maxLength = 1000
	if len(s) > maxLength {
		return s[:maxLength] + "...(省略)"
	}

	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.Join(strings.Fields(s), " ")
	return s
}

// handleFingerprintCollect は /api/fingerprint/collect エンドポイントのハンドラーです。
func handleFingerprintCollect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "メソッドが許可されていません。POSTを使用してください。", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "リクエストの解析に失敗しました", http.StatusBadRequest)
		return
	}

	sampleType := r.FormValue("sample_type")
	roomID := r.FormValue("room_id")

	if sampleType != "positive" && sampleType != "negative" {
		http.Error(w, "無効なsample_typeです。'positive' または 'negative' を使用してください。", http.StatusBadRequest)
		return
	}

	if roomID == "" {
		http.Error(w, "room_idを指定してください。", http.StatusBadRequest)
		return
	}

	wifiFile, _, err := r.FormFile("wifi_data")
	if err != nil {
		http.Error(w, "wifi_dataファイルの取得に失敗しました。", http.StatusBadRequest)
		return
	}
	defer wifiFile.Close()

	bleFile, _, err := r.FormFile("ble_data")
	if err != nil {
		http.Error(w, "ble_dataファイルの取得に失敗しました。", http.StatusBadRequest)
		return
	}
	defer bleFile.Close()

	baseDir := "./estimation"
	sanitizedRoomID := filepath.Base(roomID)
	var saveDir string
	if sampleType == "positive" {
		saveDir = filepath.Join(baseDir, "positive_samples", sanitizedRoomID)
	} else {
		saveDir = filepath.Join(baseDir, "negative_samples", sanitizedRoomID)
	}

	if err := os.MkdirAll(saveDir, os.ModePerm); err != nil {
		http.Error(w, "保存ディレクトリの作成に失敗しました。", http.StatusInternalServerError)
		return
	}

	timestamp := time.Now().Unix()
	wifiFileName := fmt.Sprintf("wifi_data_%d.csv", timestamp)
	bleFileName := fmt.Sprintf("ble_data_%d.csv", timestamp)

	wifiFilePath := filepath.Join(saveDir, wifiFileName)
	bleFilePath := filepath.Join(saveDir, bleFileName)

	if err := saveUploadedFile(wifiFile, wifiFilePath); err != nil {
		http.Error(w, "wifi_dataの保存に失敗しました。", http.StatusInternalServerError)
		return
	}

	if err := saveUploadedFile(bleFile, bleFilePath); err != nil {
		http.Error(w, "ble_dataの保存に失敗しました。", http.StatusInternalServerError)
		return
	}

	response := UploadResponse{Message: "フィンガープリントデータを正常に受信しました"}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		logError("フィンガープリント収集JSONレスポンスのエンコードに失敗しました: %v", err)
		http.Error(w, "レスポンスの作成に失敗しました。", http.StatusInternalServerError)
		return
	}

	logInfo("フィンガープリントデータを正常に受信しました。サンプルタイプ: %s, RoomID: %s", sampleType, roomID)
}

func main() {
	configPath := "config.toml"

	var config Config
	if _, err := toml.DecodeFile(configPath, &config); err != nil {
		log.Fatalf("[CONFIG] 設定ファイルの読み取りに失敗しました: %v\n", err)
	}

	mode := flag.String("mode", config.Mode, "アプリケーションモード (docker または local)")
	port := flag.String("port", config.ServerPort, "サーバーポート")
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

	logConfig(`
	===========================================
			  サーバー設定情報
	-------------------------------------------
	モード               : %s
	サーバーポート       : %s
	プロキシURL          : %s
	推定URL             : %s
	問い合わせURL       : %s
	データベース接続文字列 : %s
	登録をスキップするか : %v
	システムURI         : %s
	===========================================
	`, *mode, *port, proxyURL, estimationURL, inquiryURL, dbConnStr, skipRegistration, config.Registration.SystemURI)

	db, err := sql.Open("postgres", dbConnStr)
	if err != nil {
		log.Fatalf("[CONFIG] データベースへの接続に失敗しました: %v\n", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("[CONFIG] データベースへのPingに失敗しました: %v\n", err)
	}
	logInfo("データベースへの接続に成功しました。")

	if !skipRegistration {
		go func() {
			serverPortInt, err := strconv.Atoi(*port)
			if err != nil {
				log.Fatalf("[CONFIG] ポート番号の変換に失敗しました: %v\n", err)
			}

			registerData := RegisterRequest{
				Scheme: "http",
				Host:   config.Registration.SystemURI,
				Port:   serverPortInt,
			}

			for {
				registerBody, err := json.Marshal(registerData)
				if err != nil {
					logError("登録リクエストのエンコードに失敗しました: %v", err)
					logInfo("登録を再試行しています...")
					time.Sleep(5 * time.Second)
					continue
				}

				resp, err := http.Post(proxyURL, "application/json", bytes.NewBuffer(registerBody))
				if err != nil {
					logError("サーバー登録エラー: %v", err)
					logInfo("登録を再試行しています...")
					time.Sleep(5 * time.Second)
					continue
				}

				if resp.StatusCode != http.StatusOK {
					logError("サーバーの登録に失敗しました。ステータスコード: %d", resp.StatusCode)
					resp.Body.Close()
					logInfo("登録を再試行しています...")
					time.Sleep(5 * time.Second)
					continue
				}

				resp.Body.Close()
				logInfo("サーバーの登録が完了しました。")
				break
			}
		}()
	}

	go cleanUpOldSessions(db, 10*time.Minute)

	mux := http.NewServeMux()

	// ユーザーのプレゼンス履歴取得エンドポイント
	mux.HandleFunc("/api/users/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) == 4 && parts[0] == "api" && parts[1] == "users" && parts[3] == "presence_history" && r.Method == http.MethodGet {
			userIDStr := parts[2]
			userID, err := strconv.Atoi(userIDStr)
			if err != nil {
				http.Error(w, "無効なユーザーID", http.StatusBadRequest)
				return
			}
			handleUserPresenceHistory(w, r, db, userID)
			return
		}
		http.NotFound(w, r)
	})

	// 全ユーザーのプレゼンス履歴取得エンドポイント
	mux.HandleFunc("/api/presence_history", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "メソッドが許可されていません", http.StatusMethodNotAllowed)
			return
		}
		handlePresenceHistory(w, r, db)
	})

	// 現在の占有者取得エンドポイント
	mux.HandleFunc("/api/current_occupants", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "メソッドが許可されていません", http.StatusMethodNotAllowed)
			return
		}
		handleCurrentOccupants(w, r, db)
	})

	// シグナル送信エンドポイント
	mux.HandleFunc("/api/signals/submit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "メソッドが許可されていません。POSTを使用してください。", http.StatusMethodNotAllowed)
			return
		}
		handleSignalsSubmit(w, r, db, estimationURL, inquiryURL)
	})

	// シグナルサーバー送信エンドポイント
	mux.HandleFunc("/api/signals/server", func(w http.ResponseWriter, r *http.Request) {
		handleSignalsServer(w, r, db, estimationURL, inquiryURL)
	})

	// フィンガープリント収集エンドポイント
	mux.HandleFunc("/api/fingerprint/collect", handleFingerprintCollect)

	// ヘルスチェックエンドポイント
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handleHealthCheck(w, r, db)
	})

	// ログミドルウェアの適用
	loggedMux := loggingMiddleware(mux)

	// CORS設定の適用
	corsHandler := cors.New(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173", "https://elpis.kajilab.dev"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
	})

	finalHandler := corsHandler.Handler(loggedMux)

	logInfo("ポート %s でサーバーを起動します。モード: %s", *port, *mode)
	if err := http.ListenAndServe(":"+*port, finalHandler); err != nil {
		log.Fatalf("[ERROR] サーバーの起動に失敗しました: %v\n", err)
	}
}
