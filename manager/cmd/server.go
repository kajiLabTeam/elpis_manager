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
	SystemURI string `json:"system_uri"`
	Port      int    `json:"port"`
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

type InquiryRequest struct {
	WifiData           string  `json:"wifi_data"`
	BleData            string  `json:"ble_data"`
	PresenceConfidence float64 `json:"presence_confidence"`
}

type InquiryResponse struct {
	ServerConfidence float64 `json:"server_confidence"`
}

func parseCSV(file multipart.File) ([][]string, error) {
	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	return records, nil
}

func forwardFilesToEstimationServer(bleFilePath string, estimationURL string) (float64, error) {
	file, err := os.Open(bleFilePath)
	if err != nil {
		return 0, fmt.Errorf("BLEデータファイルのオープンに失敗しました: %v", err)
	}
	defer file.Close()

	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)
	part, err := writer.CreateFormFile("file", filepath.Base(bleFilePath))
	if err != nil {
		return 0, fmt.Errorf("マルチパートフォームの作成に失敗しました: %v", err)
	}
	_, err = io.Copy(part, file)
	if err != nil {
		return 0, fmt.Errorf("ファイルのコピーに失敗しました: %v", err)
	}
	writer.Close()

	req, err := http.NewRequest("POST", estimationURL, &requestBody)
	if err != nil {
		return 0, fmt.Errorf("推定サーバへのリクエスト作成に失敗しました: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("推定サーバへのリクエスト送信に失敗しました: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("推定サーバからの応答が不正です。ステータスコード: %d", resp.StatusCode)
	}

	var predictionResp PredictionResponse
	if err := json.NewDecoder(resp.Body).Decode(&predictionResp); err != nil {
		return 0, fmt.Errorf("推定サーバのレスポンスパースに失敗しました: %v", err)
	}

	percentageStr := strings.TrimSuffix(predictionResp.PredictedPercentage, "%")
	percentage, err := strconv.ParseFloat(percentageStr, 64)
	if err != nil {
		return 0, fmt.Errorf("予測パーセンテージの解析に失敗しました: %v", err)
	}

	return percentage, nil
}

func forwardFilesToInquiryServer(wifiFilePath string, bleFilePath string, inquiryURL string, confidence float64) (float64, error) {
	wifiData, err := os.ReadFile(wifiFilePath)
	if err != nil {
		return 0, fmt.Errorf("WiFiデータの読み込みに失敗しました: %v", err)
	}

	bleData, err := os.ReadFile(bleFilePath)
	if err != nil {
		return 0, fmt.Errorf("BLEデータの読み込みに失敗しました: %v", err)
	}

	inquiryReq := InquiryRequest{
		WifiData:           string(wifiData),
		BleData:            string(bleData),
		PresenceConfidence: confidence,
	}

	reqBody, err := json.Marshal(inquiryReq)
	if err != nil {
		return 0, fmt.Errorf("照会リクエストのエンコードに失敗しました: %v", err)
	}

	resp, err := http.Post(inquiryURL, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return 0, fmt.Errorf("照会サーバへのリクエスト送信に失敗しました: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("照会サーバからの応答が不正です。ステータスコード: %d", resp.StatusCode)
	}

	var inquiryResp InquiryResponse
	if err := json.NewDecoder(resp.Body).Decode(&inquiryResp); err != nil {
		return 0, fmt.Errorf("照会サーバのレスポンスパースに失敗しました: %v", err)
	}

	return inquiryResp.ServerConfidence, nil
}

func getUserID(r *http.Request) string {
	username, _, ok := r.BasicAuth()
	if ok && username != "" {
		log.Printf("authentication passed: %s", username)
		return username
	} else {
		log.Println("authentication failed")
		return "anonymous"
	}
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
	log.Printf("ユーザーID %d のセッションを開始しました。RoomID: %d, StartTime: %s", userID, roomID, startTime)
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
	if rowsAffected == 0 {
		log.Printf("ユーザーID %d の現在のセッションが見つかりませんでした。", userID)
	} else {
		log.Printf("ユーザーID %d のセッションを終了しました。EndTime: %s", userID, endTime)
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
	if rowsAffected == 0 {
		log.Printf("ユーザーID %d のセッションが見つかりませんでした。", userID)
	} else {
		log.Printf("ユーザーID %d のlast_seenを更新しました。", userID)
	}

	return nil
}

func updateUserPresence(db *sql.DB, userID int, estimationConfidence float64, inquiryConfidence float64, lastSeen time.Time) error {
	if inquiryConfidence > estimationConfidence {
		err := endUserSession(db, userID, lastSeen)
		if err != nil {
			return fmt.Errorf("セッションの終了に失敗しました: %v", err)
		}
		log.Printf("ユーザーID %d は在室していないと判定しました。照会サーバ確信度: %.2f%%, 推定サーバ確信度: %.2f%%", userID, inquiryConfidence, estimationConfidence)
	} else {
		var existingRoomID int
		err := db.QueryRow(`
            SELECT room_id FROM user_presence_sessions
            WHERE user_id = $1 AND end_time IS NULL
        `, userID).Scan(&existingRoomID)

		if err != nil {
			if err == sql.ErrNoRows {
				err = startUserSession(db, userID, 1, lastSeen)
				if err != nil {
					return fmt.Errorf("新規セッションの開始に失敗しました: %v", err)
				}
			} else {
				return fmt.Errorf("現在のセッションの取得に失敗しました: %v", err)
			}
		} else {
			err = updateLastSeen(db, userID, lastSeen)
			if err != nil {
				return fmt.Errorf("last_seenの更新に失敗しました: %v", err)
			}
		}
		log.Printf("ユーザーID %d は在室していると判定しました。推定サーバ確信度: %.2f%%", userID, estimationConfidence)
	}
	return nil
}

func handleSignalsSubmit(w http.ResponseWriter, r *http.Request, db *sql.DB, estimationURL string, inquiryURL string) {
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

	estimationConfidence, err := forwardFilesToEstimationServer(bleFilePath, estimationURL)
	if err != nil {
		http.Error(w, fmt.Sprintf("推定サーバへのファイル転送エラー: %v", err), http.StatusInternalServerError)
		return
	}
	log.Printf("推定サーバからの確信度: %.2f%%", estimationConfidence)

	currentTime := time.Now()

	if estimationConfidence >= 20.0 && estimationConfidence <= 70.0 {
		inquiryConfidence, err := forwardFilesToInquiryServer(wifiFilePath, bleFilePath, inquiryURL, estimationConfidence)
		if err != nil {
			http.Error(w, fmt.Sprintf("照会サーバへのファイル転送エラー: %v", err), http.StatusInternalServerError)
			return
		}
		log.Printf("照会サーバからの確信度: %.2f%%", inquiryConfidence)

		err = updateUserPresence(db, userID, estimationConfidence, inquiryConfidence, currentTime)
		if err != nil {
			log.Printf("在室情報の更新に失敗しました: %v", err)
		}
	} else {
		if estimationConfidence > 70.0 {
			err = updateUserPresence(db, userID, estimationConfidence, 0, currentTime)
			if err != nil {
				log.Printf("在室情報の更新に失敗しました: %v", err)
			}
		} else {
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

func handleSignalsServer(w http.ResponseWriter, r *http.Request, db *sql.DB, estimationURL string, inquiryURL string) {
	handleSignalsSubmit(w, r, db, estimationURL, inquiryURL)
}

func handlePresenceHistory(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	dateStr := r.URL.Query().Get("date")
	var since time.Time
	var err error

	if dateStr != "" {
		since, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			http.Error(w, "無効な date パラメータです。フォーマットは YYYY-MM-DD です。", http.StatusBadRequest)
			return
		}
		since = time.Date(since.Year(), since.Month(), since.Day(), 0, 0, 0, 0, since.Location())
	} else {
		since = time.Now().AddDate(0, -1, 0)
	}

	sessions, err := fetchAllSessions(db, since)
	if err != nil {
		http.Error(w, "在室履歴の取得に失敗しました", http.StatusInternalServerError)
		log.Printf("在室履歴取得クエリエラー: %v", err)
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
		http.Error(w, "JSONエンコードエラー", http.StatusInternalServerError)
		log.Printf("JSONエンコードエラー: %v", err)
		return
	}
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
		return nil, err
	}

	return sessions, nil
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
		return nil, err
	}

	return sessions, nil
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

	log.Printf("モード: %s", *mode)
	log.Printf("サーバポート: %s", *port)
	log.Printf("Proxy URL: %s", proxyURL)
	log.Printf("Estimation URL: %s", estimationURL)
	log.Printf("Inquiry URL: %s", inquiryURL)
	log.Printf("データベース接続文字列: %s", dbConnStr)
	log.Printf("skipRegistration: %v", skipRegistration)
	log.Printf("システムURI: %s", config.Registration.SystemURI)

	db, err := sql.Open("postgres", dbConnStr)
	if err != nil {
		log.Fatalf("データベースに接続できませんでした: %v\n", err)
	}
	defer db.Close()

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

	http.HandleFunc("/api/users/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) == 4 && parts[0] == "api" && parts[1] == "users" && parts[3] == "presence_history" && r.Method == http.MethodGet {
			userIDStr := parts[2]
			userID, err := strconv.Atoi(userIDStr)
			if err != nil {
				http.Error(w, "無効な user_id です", http.StatusBadRequest)
				return
			}
			handleUserPresenceHistory(w, r, db, userID)
			return
		}
		http.NotFound(w, r)
	})

	http.HandleFunc("/api/presence_history", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "メソッドが許可されていません", http.StatusMethodNotAllowed)
			return
		}
		handlePresenceHistory(w, r, db)
	})

	http.HandleFunc("/api/current_occupants", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "メソッドが許可されていません", http.StatusMethodNotAllowed)
			return
		}
		handleCurrentOccupants(w, r, db)
	})

	http.HandleFunc("/api/signals/submit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "メソッドが許可されていません", http.StatusMethodNotAllowed)
			return
		}
		handleSignalsSubmit(w, r, db, estimationURL, inquiryURL)
	})
	http.HandleFunc("/api/signals/server", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "メソッドが許可されていません", http.StatusMethodNotAllowed)
			return
		}
		handleSignalsServer(w, r, db, estimationURL, inquiryURL)
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

func handleUserPresenceHistory(w http.ResponseWriter, r *http.Request, db *sql.DB, userID int) {
	dateStr := r.URL.Query().Get("date")
	var since time.Time
	var err error

	if dateStr != "" {
		since, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			http.Error(w, "無効な date パラメータです。フォーマットは YYYY-MM-DD です。", http.StatusBadRequest)
			return
		}
		since = time.Date(since.Year(), since.Month(), since.Day(), 0, 0, 0, 0, since.Location())
	} else {
		since = time.Now().AddDate(0, -1, 0)
	}

	sessions, err := fetchUserSessions(db, userID, since)
	if err != nil {
		http.Error(w, "在室履歴の取得に失敗しました", http.StatusInternalServerError)
		log.Printf("在室履歴取得クエリエラー: %v", err)
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
		http.Error(w, "JSONエンコードエラー", http.StatusInternalServerError)
		log.Printf("JSONエンコードエラー: %v", err)
		return
	}
}
