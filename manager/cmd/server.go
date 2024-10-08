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
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	_ "github.com/lib/pq"
	"github.com/rs/cors"
)

// Config は設定ファイルの構造を表します
type Config struct {
	Mode         string
	ServerPort   string `toml:"server_port"`
	Docker       DockerConfig
	Local        LocalConfig
	Registration RegistrationConfig
}

type DockerConfig struct {
	ProxyURL         string `toml:"proxy_url"`
	DBConnStr        string `toml:"db_conn_str"`
	SkipRegistration bool   `toml:"skip_registration"`
}

type LocalConfig struct {
	ProxyURL         string `toml:"proxy_url"`
	DBConnStr        string `toml:"db_conn_str"`
	SkipRegistration bool   `toml:"skip_registration"`
}

type RegistrationConfig struct {
	SystemURI string `toml:"system_uri"`
}

// UploadResponse は信号データのアップロードに対するレスポンスを表します
type UploadResponse struct {
	Message string `json:"message"`
}

// RegisterRequest は登録リクエストのペイロードを表します
type RegisterRequest struct {
	SystemURI string `json:"system_uri"`
	Port      int    `json:"port"`
}

// PresenceSession はユーザーの在室セッションを表す構造体
type PresenceSession struct {
	SessionID int        `json:"session_id"`
	UserID    int        `json:"user_id"`
	RoomID    int        `json:"room_id"`
	StartTime time.Time  `json:"start_time"`
	EndTime   *time.Time `json:"end_time"`
	LastSeen  time.Time  `json:"last_seen"`
}

// UserPresenceDay は1日ごとのユーザーの在室情報を表す構造体
type UserPresenceDay struct {
	Date     string            `json:"date"`
	Sessions []PresenceSession `json:"sessions"`
}

// PresenceHistoryResponse は在室履歴のレスポンス構造体
type PresenceHistoryResponse struct {
	History []UserPresenceDay `json:"history"`
}

// CurrentOccupant は現在の在室者情報を表す構造体
type CurrentOccupant struct {
	UserID   string    `json:"user_id"`
	LastSeen time.Time `json:"last_seen"`
}

// RoomOccupants は部屋ごとの在室者情報を表す構造体
type RoomOccupants struct {
	RoomID    int               `json:"room_id"`
	RoomName  string            `json:"room_name"`
	Occupants []CurrentOccupant `json:"occupants"`
}

// CurrentOccupantsResponse は現在の在室者情報のレスポンス構造体
type CurrentOccupantsResponse struct {
	Rooms []RoomOccupants `json:"rooms"`
}

// HealthCheckResponse は健康チェックのレスポンス構造体
type HealthCheckResponse struct {
	Status    string `json:"status"`
	Database  string `json:"database"`
	Timestamp string `json:"timestamp"`
}

// BeaconThreshold はUUIDに対応するしきい値と部屋IDを保持する構造体
type BeaconThreshold struct {
	Threshold int
	RoomID    int
}

// InquiryRequest は照会サーバへのリクエストペイロードを表します
type InquiryRequest struct {
	WifiData           string  `json:"wifi_data"`           // WiFiデータの内容
	BleData            string  `json:"ble_data"`            // BLEデータの内容
	PresenceConfidence float64 `json:"presence_confidence"` // 在室確信度
}

// InquiryResponse は照会サーバからのレスポンスを表します
type InquiryResponse struct {
	ServerConfidence float64 `json:"server_confidence"`
}

// multipart.File からCSVファイルをパースする
func parseCSV(file multipart.File) ([][]string, error) {
	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	return records, nil
}

// 照会サーバへの送信関数
func forwardFilesToInquiry(wifiFilePath string, bleFilePath string, proxyURL string, confidence float64) (float64, error) {
	// ファイルを読み込む
	wifiData, err := os.ReadFile(wifiFilePath)
	if err != nil {
		return 0, fmt.Errorf("WiFiデータの読み込みに失敗しました: %v", err)
	}

	bleData, err := os.ReadFile(bleFilePath)
	if err != nil {
		return 0, fmt.Errorf("BLEデータの読み込みに失敗しました: %v", err)
	}

	// リクエストペイロードを作成
	inquiryReq := InquiryRequest{
		WifiData:           string(wifiData),
		BleData:            string(bleData),
		PresenceConfidence: confidence,
	}

	// JSONエンコード
	reqBody, err := json.Marshal(inquiryReq)
	if err != nil {
		return 0, fmt.Errorf("照会リクエストのエンコードに失敗しました: %v", err)
	}

	// POSTリクエストを送信
	resp, err := http.Post(fmt.Sprintf("%s/api/inquiry", proxyURL), "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return 0, fmt.Errorf("照会サーバへのリクエスト送信に失敗しました: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("照会サーバからの応答が不正です。ステータスコード: %d", resp.StatusCode)
	}

	// レスポンスをパース
	var inquiryResp InquiryResponse
	if err := json.NewDecoder(resp.Body).Decode(&inquiryResp); err != nil {
		return 0, fmt.Errorf("照会サーバのレスポンスパースに失敗しました: %v", err)
	}

	return inquiryResp.ServerConfidence, nil
}

// beaconsテーブルからすべてのUUIDとそのRSSIしきい値、room_idを取得
func getUUIDsAndThresholds(db *sql.DB) (map[string][]BeaconThreshold, error) {
	rows, err := db.Query("SELECT service_uuid, rssi, room_id FROM beacons")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	uuidThresholds := make(map[string][]BeaconThreshold)
	for rows.Next() {
		var uuid string
		var threshold int
		var roomID int
		if err := rows.Scan(&uuid, &threshold, &roomID); err != nil {
			return nil, err
		}
		uuid = strings.TrimSpace(uuid)
		uuidThresholds[uuid] = append(uuidThresholds[uuid], BeaconThreshold{
			Threshold: threshold,
			RoomID:    roomID,
		})
		log.Printf("UUIDをロード: %s, RSSIしきい値: %d, RoomID: %d", uuid, threshold, roomID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return uuidThresholds, nil
}

// ユーザIDを取得する関数（Basic認証を使用）
func getUserID(r *http.Request) string {
	username, _, ok := r.BasicAuth()
	if !ok || username == "" {
		username = "anonymous"
	}
	return username
}

// ユーザーIDからデータベースのユーザーIDを取得
func getUserIDFromDB(db *sql.DB, username string) (int, error) {
	var userID int
	err := db.QueryRow("SELECT id FROM users WHERE user_id = $1", username).Scan(&userID)
	if err != nil {
		return 0, err
	}
	return userID, nil
}

// ファイルを保存するヘルパー関数
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

// セッションを開始する関数
func startUserSession(db *sql.DB, userID int, roomID int, startTime time.Time) error {
	_, err := db.Exec(`
        INSERT INTO user_presence_sessions (user_id, room_id, start_time, last_seen)
        VALUES ($1, $2, $3, $3)
    `, userID, roomID, startTime)
	if err != nil {
		return fmt.Errorf("セッションの開始に失敗しました: %v", err)
	}
	log.Printf("ユーザーID %d のセッションを開始しました。RoomID: %d, StartTime: %s", userID, roomID, startTime)
	return nil
}

// セッションを終了する関数
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
	if rowsAffected == 0 {
		log.Printf("ユーザーID %d の現在のセッションが見つかりませんでした。", userID)
	} else {
		log.Printf("ユーザーID %d のセッションを終了しました。EndTime: %s", userID, endTime)
	}

	return nil
}

// セッションのlast_seenを更新する関数
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
	if rowsAffected == 0 {
		log.Printf("ユーザーID %d のセッションが見つかりませんでした。", userID)
	} else {
		log.Printf("ユーザーID %d のlast_seenを更新しました。", userID)
	}

	return nil
}

// 在室確信度を計算する関数
func calculatePresenceConfidence(foundStrongSignal bool, totalSignals int, strongSignals int) float64 {
	if totalSignals == 0 {
		return 0.0
	}
	// 強い信号の割合をパーセンテージで返す
	confidence := (float64(strongSignals) / float64(totalSignals)) * 100
	if foundStrongSignal {
		// 強い信号がある場合、確信度を高く設定
		return confidence
	}
	// 強い信号がない場合、確信度を低く設定
	return confidence * 0.5
}

// 在室情報を更新する関数
func updateUserPresence(db *sql.DB, userID int, roomID int, lastSeen time.Time) error {
	var existingRoomID int
	err := db.QueryRow(`
        SELECT room_id FROM user_presence_sessions
        WHERE user_id = $1 AND end_time IS NULL
    `, userID).Scan(&existingRoomID)

	if err != nil {
		if err == sql.ErrNoRows {
			err = startUserSession(db, userID, roomID, lastSeen)
			if err != nil {
				return fmt.Errorf("新規セッションの開始に失敗しました: %v", err)
			}
		} else {
			return fmt.Errorf("現在のセッションの取得に失敗しました: %v", err)
		}
	} else {
		if existingRoomID != roomID {
			err = endUserSession(db, userID, lastSeen)
			if err != nil {
				return fmt.Errorf("既存セッションの終了に失敗しました: %v", err)
			}
			err = startUserSession(db, userID, roomID, lastSeen)
			if err != nil {
				return fmt.Errorf("新規セッションの開始に失敗しました: %v", err)
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

// /api/signals/submit エンドポイントの処理
func handleSignalsSubmit(w http.ResponseWriter, r *http.Request, db *sql.DB, proxyURL string, uuidThresholds map[string][]BeaconThreshold) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "リクエストの解析に失敗しました", http.StatusBadRequest)
		return
	}

	wifiFile, _, err := r.FormFile("wifi_data")
	if err != nil {
		http.Error(w, "WiFiデータファイルの読み込みエラー", http.StatusBadRequest)
		return
	}
	defer wifiFile.Close()

	bleFile, _, err := r.FormFile("ble_data")
	if err != nil {
		http.Error(w, "BLEデータファイルの読み込みエラー", http.StatusBadRequest)
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
	userDir := filepath.Join(baseDir, username)
	dateDir := filepath.Join(userDir, currentDate)

	if err := os.MkdirAll(dateDir, os.ModePerm); err != nil {
		http.Error(w, "ディレクトリの作成に失敗しました", http.StatusInternalServerError)
		return
	}

	timeStamp := time.Now().Format("150405")
	wifiFileName := fmt.Sprintf("wifi_data_%s.csv", timeStamp)
	bleFileName := fmt.Sprintf("ble_data_%s.csv", timeStamp)

	wifiFilePath := filepath.Join(dateDir, wifiFileName)
	bleFilePath := filepath.Join(dateDir, bleFileName)

	if err := saveUploadedFile(wifiFile, wifiFilePath); err != nil {
		http.Error(w, "WiFiデータの保存に失敗しました", http.StatusInternalServerError)
		return
	}
	if err := saveUploadedFile(bleFile, bleFilePath); err != nil {
		http.Error(w, "BLEデータの保存に失敗しました", http.StatusInternalServerError)
		return
	}

	if _, err := wifiFile.Seek(0, io.SeekStart); err != nil {
		http.Error(w, "WiFiデータファイルのリセットに失敗しました", http.StatusInternalServerError)
		return
	}
	if _, err := bleFile.Seek(0, io.SeekStart); err != nil {
		http.Error(w, "BLEデータファイルのリセットに失敗しました", http.StatusInternalServerError)
		return
	}

	_, err = parseCSV(wifiFile)
	if err != nil {
		http.Error(w, "WiFi CSVのパースエラー", http.StatusBadRequest)
		return
	}

	bleRecords, err := parseCSV(bleFile)
	if err != nil {
		http.Error(w, "BLE CSVのパースエラー", http.StatusBadRequest)
		return
	}

	foundStrongSignal := false
	foundWeakSignal := false
	var detectedRoomID int
	var maxRSSI int = -1000

	totalSignals := 0
	strongSignals := 0

	for _, record := range bleRecords {
		if len(record) > 2 {
			uuid := strings.TrimSpace(record[1])
			rssiStr := strings.TrimSpace(record[2])
			rssiValue, err := strconv.Atoi(rssiStr)
			if err != nil {
				log.Printf("無効なRSSI値: %s", rssiStr)
				continue
			}

			if thresholds, exists := uuidThresholds[uuid]; exists {
				for _, bt := range thresholds {
					totalSignals++
					if rssiValue > bt.Threshold {
						strongSignals++
						foundStrongSignal = true
						if rssiValue > maxRSSI {
							maxRSSI = rssiValue
							detectedRoomID = bt.RoomID
							log.Printf("強い信号を検出。UUID: %s, RSSI: %d (しきい値: %d), RoomID: %d", uuid, rssiValue, bt.Threshold, bt.RoomID)
						}
					} else {
						foundWeakSignal = true
						log.Printf("弱い信号を検出。UUID: %s, RSSI: %d (しきい値: %d)", uuid, rssiValue, bt.Threshold)
					}
				}
			}
		}
	}

	// 在室確信度を計算
	confidence := calculatePresenceConfidence(foundStrongSignal, totalSignals, strongSignals)
	log.Printf("在室確信度: %.2f%%", confidence)

	currentTime := time.Now()

	var serverConfidence float64

	if foundStrongSignal {
		log.Println("強い信号が検出されたため、在室情報を更新します。")
		err = updateUserPresence(db, userID, detectedRoomID, currentTime)
		if err != nil {
			log.Printf("在室情報の更新に失敗しました: %v", err)
		} else {
			log.Printf("ユーザーID %d の在室情報をRoomID %d に更新しました。", userID, detectedRoomID)
		}
	}

	if foundWeakSignal {
		log.Println("弱い信号が検出されたため、照会サーバにファイルを転送します。")
		serverConfidence, err = forwardFilesToInquiry(wifiFilePath, bleFilePath, proxyURL, confidence)
		if err != nil {
			http.Error(w, fmt.Sprintf("照会サーバへのファイル転送エラー: %v", err), http.StatusInternalServerError)
			return
		}
		log.Printf("照会サーバからの確信度: %.2f%%", serverConfidence)
	}

	// 照会サーバとの確信度比較
	if foundWeakSignal {
		if serverConfidence > confidence {
			log.Println("照会サーバの確信度が高いため、ユーザーは在室していないと判定します。")
			err = endUserSession(db, userID, currentTime)
			if err != nil {
				log.Printf("セッションの終了に失敗しました: %v", err)
			} else {
				log.Printf("ユーザーID %d のセッションを終了しました。", userID)
			}
		} else {
			log.Println("照会サーバの確信度が低いため、ユーザーは在室していると判定します。")
			err = updateLastSeen(db, userID, currentTime)
			if err != nil {
				log.Printf("last_seenの更新に失敗しました: %v", err)
			}
		}
	} else {
		if !foundStrongSignal && !foundWeakSignal {
			log.Println("BLEデータにデバイスが見つからなかったため、セッションを終了します。")
			err = endUserSession(db, userID, currentTime)
			if err != nil {
				log.Printf("セッションの終了に失敗しました: %v", err)
			} else {
				log.Printf("ユーザーID %d のセッションを終了しました。", userID)
			}
		}
	}

	response := UploadResponse{Message: "信号データを受信しました"}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// /api/signals/server エンドポイントの処理
func handleSignalsServer(w http.ResponseWriter, r *http.Request, db *sql.DB, proxyURL string, uuidThresholds map[string][]BeaconThreshold) {
	handleSignalsSubmit(w, r, db, proxyURL, uuidThresholds)
}

// /api/presence_history エンドポイントの処理
func handlePresenceHistory(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	userIDStr := r.URL.Query().Get("user_id")
	if userIDStr == "" {
		http.Error(w, "user_id パラメータが必要です", http.StatusBadRequest)
		return
	}

	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		http.Error(w, "無効な user_id パラメータです", http.StatusBadRequest)
		return
	}

	oneMonthAgo := time.Now().AddDate(0, -1, 0)

	rows, err := db.Query(`
        SELECT session_id, user_id, room_id, start_time, end_time, last_seen
        FROM user_presence_sessions
        WHERE user_id = $1 AND start_time >= $2
        ORDER BY start_time
    `, userID, oneMonthAgo)
	if err != nil {
		http.Error(w, "在室履歴の取得に失敗しました", http.StatusInternalServerError)
		log.Printf("在室履歴取得クエリエラー: %v", err)
		return
	}
	defer rows.Close()

	var sessions []PresenceSession
	for rows.Next() {
		var session PresenceSession
		var endTime sql.NullTime
		if err := rows.Scan(&session.SessionID, &session.UserID, &session.RoomID, &session.StartTime, &endTime, &session.LastSeen); err != nil {
			log.Printf("在室履歴のスキャンエラー: %v", err)
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
		http.Error(w, "在室履歴の読み取り中にエラーが発生しました", http.StatusInternalServerError)
		log.Printf("rows.Err(): %v", err)
		return
	}

	historyMap := make(map[string][]PresenceSession)
	for _, session := range sessions {
		date := session.StartTime.Format("2006-01-02")
		historyMap[date] = append(historyMap[date], session)
	}

	var history []UserPresenceDay
	for date, sessions := range historyMap {
		history = append(history, UserPresenceDay{
			Date:     date,
			Sessions: sessions,
		})
	}

	sort.Slice(history, func(i, j int) bool {
		return history[i].Date < history[j].Date
	})

	response := PresenceHistoryResponse{
		History: history,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "JSONエンコードエラー", http.StatusInternalServerError)
		log.Printf("JSONエンコードエラー: %v", err)
		return
	}
}

// /api/current_occupants エンドポイントの処理
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
		http.Error(w, "在室者情報の取得に失敗しました", http.StatusInternalServerError)
		log.Printf("在室者情報取得クエリエラー: %v", err)
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
			log.Printf("行のスキャンエラー: %v", err)
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
		http.Error(w, "在室者情報の読み取り中にエラーが発生しました", http.StatusInternalServerError)
		log.Printf("rows.Err(): %v", err)
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
		http.Error(w, "JSONエンコードエラー", http.StatusInternalServerError)
		log.Printf("JSONエンコードエラー: %v", err)
		return
	}
}

// /health エンドポイントの処理
func handleHealthCheck(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	response := HealthCheckResponse{
		Status:    "ok",
		Timestamp: time.Now().Format(time.RFC3339),
	}

	if err := db.Ping(); err != nil {
		response.Status = "error"
		response.Database = "unreachable"
	} else {
		response.Database = "reachable"
	}

	w.Header().Set("Content-Type", "application/json")
	if response.Status == "ok" {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	json.NewEncoder(w).Encode(response)
}

// クリーンアップ処理を行う関数
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
			log.Printf("クリーンアップ処理中のクエリエラー: %v", err)
			continue
		}

		var userID int
		var lastSeen time.Time
		var usersToEnd []int

		for rows.Next() {
			if err := rows.Scan(&userID, &lastSeen); err != nil {
				log.Printf("クリーンアップ処理中のスキャンエラー: %v", err)
				continue
			}
			usersToEnd = append(usersToEnd, userID)
			log.Printf("ユーザーID %d のセッションを終了対象として検出 (LastSeen: %s)", userID, lastSeen)
		}
		rows.Close()

		for _, uid := range usersToEnd {
			endTime := time.Now()
			err := endUserSession(db, uid, endTime)
			if err != nil {
				log.Printf("ユーザーID %d のセッション終了エラー: %v", uid, err)
			} else {
				log.Printf("ユーザーID %d のセッションを終了しました。", uid)
			}
		}
	}
}

func main() {
	configPath := "config.toml"

	var config Config
	if _, err := toml.DecodeFile(configPath, &config); err != nil {
		log.Fatalf("設定ファイルの読み込みに失敗しました: %v\n", err)
	}

	mode := flag.String("mode", config.Mode, "アプリケーションの実行モード (docker または local)")
	port := flag.String("port", config.ServerPort, "サーバを実行するポート")
	flag.Parse()

	var proxyURL, dbConnStr string
	var skipRegistration bool

	if *mode == "local" {
		proxyURL = config.Local.ProxyURL
		dbConnStr = config.Local.DBConnStr
		skipRegistration = config.Local.SkipRegistration
	} else {
		proxyURL = config.Docker.ProxyURL
		dbConnStr = config.Docker.DBConnStr
		skipRegistration = config.Docker.SkipRegistration
	}

	log.Printf("モード: %s", *mode)
	log.Printf("サーバポート: %s", *port)
	log.Printf("Proxy URL: %s", proxyURL)
	log.Printf("データベース接続文字列: %s", dbConnStr)
	log.Printf("skipRegistration: %v", skipRegistration)
	log.Printf("システムURI: %s", config.Registration.SystemURI)

	db, err := sql.Open("postgres", dbConnStr)
	if err != nil {
		log.Fatalf("データベースに接続できませんでした: %v\n", err)
	}
	defer db.Close()

	uuidThresholds, err := getUUIDsAndThresholds(db)
	if err != nil {
		log.Fatalf("UUIDとしきい値を取得できませんでした: %v\n", err)
	}

	if !skipRegistration {
		log.Println("skipRegistrationがfalseのため、サーバの登録を行います。")
		registerURL := fmt.Sprintf("%s/api/register", proxyURL)

		serverPortInt, err := strconv.Atoi(*port)
		if err != nil {
			log.Fatalf("ポート番号の変換に失敗しました: %v\n", err)
		}

		registerData := RegisterRequest{
			SystemURI: config.Registration.SystemURI,
			Port:      serverPortInt,
		}

		registerBody, err := json.Marshal(registerData)
		if err != nil {
			log.Fatalf("登録リクエストのエンコードエラー: %s\n", err)
		}

		resp, err := http.Post(registerURL, "application/json", bytes.NewBuffer(registerBody))
		if err != nil {
			log.Fatalf("サーバの登録エラー: %s\n", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			log.Fatalf("サーバの登録に失敗しました。ステータスコード: %d\n", resp.StatusCode)
		}

		log.Println("サーバの登録が完了しました。")
	} else {
		log.Println("skipRegistrationがtrueのため、サーバの登録をスキップします。")
	}

	go cleanUpOldSessions(db, 10*time.Minute)

	// ハンドラーの設定
	http.HandleFunc("/api/signals/submit", func(w http.ResponseWriter, r *http.Request) {
		handleSignalsSubmit(w, r, db, proxyURL, uuidThresholds)
	})
	http.HandleFunc("/api/signals/server", func(w http.ResponseWriter, r *http.Request) {
		handleSignalsServer(w, r, db, proxyURL, uuidThresholds)
	})
	http.HandleFunc("/api/presence_history", func(w http.ResponseWriter, r *http.Request) {
		handlePresenceHistory(w, r, db)
	})
	http.HandleFunc("/api/current_occupants", func(w http.ResponseWriter, r *http.Request) {
		handleCurrentOccupants(w, r, db)
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handleHealthCheck(w, r, db)
	})

	corsHandler := cors.New(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173", "https://elpis.kajilab.dev"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
	})

	handler := corsHandler.Handler(http.DefaultServeMux)

	log.Printf("ポート %s でサーバを開始します。モード: %s", *port, *mode)
	if err := http.ListenAndServe(":"+*port, handler); err != nil {
		log.Fatalf("サーバを開始できませんでした: %s\n", err)
	}
}
