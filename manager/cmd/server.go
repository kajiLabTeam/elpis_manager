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

// multipart.File からCSVファイルをパースする
func parseCSV(file multipart.File) ([][]string, error) {
	reader := csv.NewReader(file)

	// ヘッダー行を読み飛ばす
	if _, err := reader.Read(); err != nil {
		return nil, fmt.Errorf("ヘッダー行の読み込みエラー: %v", err)
	}

	// 残りのレコードを読み込む
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("CSVレコードの読み込みエラー: %v", err)
	}

	return records, nil
}

// BLEとWiFiのファイルを /api/inquiry エンドポイントに転送する
func forwardFilesToInquiry(wifiFile multipart.File, bleFile multipart.File, proxyURL string) error {
	// ファイルを先頭に戻す
	if _, err := wifiFile.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := bleFile.Seek(0, io.SeekStart); err != nil {
		return err
	}

	// リクエストボディを構築するためのmultipartライターを作成
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// WiFiファイルをフォームに追加
	wifiPart, err := writer.CreateFormFile("wifi_data", "wifi_data.csv")
	if err != nil {
		return err
	}
	if _, err := io.Copy(wifiPart, wifiFile); err != nil {
		return err
	}

	// BLEファイルをフォームに追加
	blePart, err := writer.CreateFormFile("ble_data", "ble_data.csv")
	if err != nil {
		return err
	}
	if _, err := io.Copy(blePart, bleFile); err != nil {
		return err
	}

	// multipartライターを閉じてフォームを完了
	writer.Close()

	// /api/inquiry エンドポイントにリクエストを送信
	resp, err := http.Post(fmt.Sprintf("%s/api/inquiry", proxyURL), writer.FormDataContentType(), body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ファイルの転送に失敗しました。ステータスコード: %d", resp.StatusCode)
	}

	return nil
}

// beaconsテーブルからすべてのUUIDとそのRSSIしきい値、room_idを取得
func getUUIDsAndThresholds(db *sql.DB) (map[string]int, map[string]int, error) {
	rows, err := db.Query("SELECT service_uuid, rssi, room_id FROM beacons")
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	// UUIDをRSSIしきい値にマッピング
	uuidThresholds := make(map[string]int)
	uuidRoomIDs := make(map[string]int)
	for rows.Next() {
		var uuid string
		var threshold int
		var roomID int
		if err := rows.Scan(&uuid, &threshold, &roomID); err != nil {
			return nil, nil, err
		}
		uuid = strings.TrimSpace(uuid) // 空白を除去
		uuidThresholds[uuid] = threshold
		uuidRoomIDs[uuid] = roomID
		log.Printf("UUIDをロード: %s, RSSIしきい値: %d, RoomID: %d", uuid, threshold, roomID)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	return uuidThresholds, uuidRoomIDs, nil
}

// ユーザIDを取得する関数（Basic認証を使用）
func getUserID(r *http.Request) string {
	username, _, ok := r.BasicAuth()
	if !ok || username == "" {
		// ユーザIDが提供されていない場合は匿名ユーザとします
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
	// ファイルを先頭に戻す
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

// /api/signals/submit エンドポイントの処理
func handleSignalsSubmit(w http.ResponseWriter, r *http.Request, db *sql.DB, proxyURL string, uuidThresholds map[string]int, uuidRoomIDs map[string]int) {
	// リクエストの最大メモリを設定（必要に応じて調整）
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

	// ユーザIDを取得
	username := getUserID(r)

	// データベースからユーザーIDを取得
	userID, err := getUserIDFromDB(db, username)
	if err != nil {
		http.Error(w, "ユーザーが見つかりません", http.StatusUnauthorized)
		return
	}

	// 現在の日付を取得
	currentDate := time.Now().Format("2006-01-02") // YYYY-MM-DD

	// 保存先ディレクトリを構築
	baseDir := "./uploads"
	userDir := filepath.Join(baseDir, username)
	dateDir := filepath.Join(userDir, currentDate)

	// ディレクトリが存在しない場合は作成
	if err := os.MkdirAll(dateDir, os.ModePerm); err != nil {
		http.Error(w, "ディレクトリの作成に失敗しました", http.StatusInternalServerError)
		return
	}

	// ファイル名にタイムスタンプを付加（必要に応じて）
	timeStamp := time.Now().Format("150405") // HHMMSS
	wifiFileName := fmt.Sprintf("wifi_data_%s.csv", timeStamp)
	bleFileName := fmt.Sprintf("ble_data_%s.csv", timeStamp)

	// ファイルを保存
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

	// ファイルポインタをリセット
	if _, err := wifiFile.Seek(0, io.SeekStart); err != nil {
		http.Error(w, "WiFiデータファイルのリセットに失敗しました", http.StatusInternalServerError)
		return
	}
	if _, err := bleFile.Seek(0, io.SeekStart); err != nil {
		http.Error(w, "BLEデータファイルのリセットに失敗しました", http.StatusInternalServerError)
		return
	}

	// WiFi CSVデータをパース（このロジックでは使用しないが、妥当性を確認するためにパース）
	_, err = parseCSV(wifiFile)
	if err != nil {
		http.Error(w, "WiFi CSVのパースエラー", http.StatusBadRequest)
		return
	}

	// BLE CSVデータをパース
	bleRecords, err := parseCSV(bleFile)
	if err != nil {
		http.Error(w, "BLE CSVのパースエラー", http.StatusBadRequest)
		return
	}

	foundStrongSignal := false
	foundWeakSignal := false
	var detectedRoomID int

	for _, record := range bleRecords {
		if len(record) > 2 {
			// 時刻, UUID, RSSI の順に想定
			uuid := strings.TrimSpace(record[1])
			rssiStr := strings.TrimSpace(record[2])
			rssiValue, err := strconv.Atoi(rssiStr)
			if err != nil {
				log.Printf("無効なRSSI値: %s", rssiStr)
				continue
			}

			if threshold, exists := uuidThresholds[uuid]; exists {
				if rssiValue > threshold {
					// RSSIがしきい値より大きい（信号が強い）; デバイスが存在すると判断
					foundStrongSignal = true
					detectedRoomID = uuidRoomIDs[uuid]
					log.Printf("強い信号を検出。UUID: %s, RSSI: %d (しきい値: %d)", uuid, rssiValue, threshold)
					break
				} else {
					// RSSIがしきい値以下（信号が弱い）
					foundWeakSignal = true
					log.Printf("弱い信号を検出。UUID: %s, RSSI: %d (しきい値: %d)", uuid, rssiValue, threshold)
					// 他のレコードをチェック
				}
			}
		}
	}

	currentTime := time.Now()

	if foundStrongSignal {
		// 強い信号が検出されたので、在室情報をデータベースに保存または更新
		log.Println("強い信号が検出されたため、在室情報を更新します。")
		err = updateUserPresence(db, userID, detectedRoomID, currentTime)
		if err != nil {
			log.Printf("在室情報の更新に失敗しました: %v", err)
		} else {
			log.Printf("ユーザーID %d の在室情報をRoomID %d に更新しました。", userID, detectedRoomID)
		}
	} else if foundWeakSignal {
		// 弱い信号が検出されたので、照会サーバに問い合わせる
		log.Println("弱い信号が検出されたため、照会サーバにファイルを転送します。")
		err := forwardFilesToInquiry(wifiFile, bleFile, proxyURL)
		if err != nil {
			http.Error(w, fmt.Sprintf("照会サーバへのファイル転送エラー: %v", err), http.StatusInternalServerError)
			return
		}
		log.Println("照会サーバへのファイル転送が完了しました。")
	} else {
		// デバイスが見つからなかった場合、セッションを終了
		log.Println("BLEデータにデバイスが見つからなかったため、セッションを終了します。")
		err = endUserSession(db, userID, currentTime)
		if err != nil {
			log.Printf("セッションの終了に失敗しました: %v", err)
		} else {
			log.Printf("ユーザーID %d のセッションを終了しました。", userID)
		}
	}

	// セッションのlast_seenを更新（強い信号があった場合）
	if foundStrongSignal {
		err = updateLastSeen(db, userID, currentTime)
		if err != nil {
			log.Printf("last_seenの更新に失敗しました: %v", err)
		}
	}

	response := UploadResponse{Message: "信号データを受信しました"}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// 在室情報を更新する関数
func updateUserPresence(db *sql.DB, userID int, roomID int, lastSeen time.Time) error {
	// 現在のセッションを取得
	var existingRoomID int
	err := db.QueryRow(`
		SELECT room_id FROM user_presence_sessions
		WHERE user_id = $1 AND end_time IS NULL
	`, userID).Scan(&existingRoomID)

	if err != nil {
		if err == sql.ErrNoRows {
			// セッションが存在しないので新規に開始
			err = startUserSession(db, userID, roomID, lastSeen)
			if err != nil {
				return fmt.Errorf("新規セッションの開始に失敗しました: %v", err)
			}
		} else {
			return fmt.Errorf("現在のセッションの取得に失敗しました: %v", err)
		}
	} else {
		if existingRoomID != roomID {
			// 部屋が変更されたので現在のセッションを終了し、新しいセッションを開始
			err = endUserSession(db, userID, lastSeen)
			if err != nil {
				return fmt.Errorf("既存セッションの終了に失敗しました: %v", err)
			}
			err = startUserSession(db, userID, roomID, lastSeen)
			if err != nil {
				return fmt.Errorf("新規セッションの開始に失敗しました: %v", err)
			}
		} else {
			// 同じ部屋の場合、last_seenを更新
			err = updateLastSeen(db, userID, lastSeen)
			if err != nil {
				return fmt.Errorf("last_seenの更新に失敗しました: %v", err)
			}
		}
	}

	return nil
}

// /api/signals/server エンドポイントの処理
func handleSignalsServer(w http.ResponseWriter, r *http.Request, db *sql.DB, proxyURL string, uuidThresholds map[string]int, uuidRoomIDs map[string]int) {
	// handleSignalsSubmit と同じ処理を行う
	handleSignalsSubmit(w, r, db, proxyURL, uuidThresholds, uuidRoomIDs)
}

// /api/presence_history エンドポイントの処理
func handlePresenceHistory(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	// クエリパラメータからユーザーIDを取得（必要に応じて認証を強化）
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

	// 1ヶ月前の日付を計算
	oneMonthAgo := time.Now().AddDate(0, -1, 0)

	// 1ヶ月分のセッションを取得
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

	// セッションを日付ごとにグループ化
	historyMap := make(map[string][]PresenceSession)
	for _, session := range sessions {
		date := session.StartTime.Format("2006-01-02")
		historyMap[date] = append(historyMap[date], session)
	}

	// マップをスライスに変換
	var history []UserPresenceDay
	for date, sessions := range historyMap {
		history = append(history, UserPresenceDay{
			Date:     date,
			Sessions: sessions,
		})
	}

	// 日付でソート（昇順）
	sort.Slice(history, func(i, j int) bool {
		return history[i].Date < history[j].Date
	})

	response := PresenceHistoryResponse{
		History: history,
	}

	// JSONとしてレスポンスを返す
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "JSONエンコードエラー", http.StatusInternalServerError)
		log.Printf("JSONエンコードエラー: %v", err)
		return
	}
}

// /api/current_occupants エンドポイントの処理
func handleCurrentOccupants(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	// 現在の在室者情報を取得するクエリ（user_presence_sessions から end_time が NULL のセッションを取得）
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

	// 部屋ごとに在室者をまとめるマップ
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

		// マップに部屋が存在しない場合は新規作成
		if _, exists := roomsMap[roomID]; !exists {
			roomsMap[roomID] = RoomOccupants{
				RoomID:    roomID,
				RoomName:  roomName,
				Occupants: []CurrentOccupant{},
			}
		}

		// 在室者が存在する場合のみ occupants に追加
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

	// マップをスライスに変換
	response := CurrentOccupantsResponse{
		Rooms: []RoomOccupants{},
	}
	for _, room := range roomsMap {
		response.Rooms = append(response.Rooms, room)
	}

	// JSONとしてレスポンスを返す
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "JSONエンコードエラー", http.StatusInternalServerError)
		log.Printf("JSONエンコードエラー: %v", err)
		return
	}
}

// クリーンアップ処理を行う関数
func cleanUpOldSessions(db *sql.DB, inactivityThreshold time.Duration) {
	ticker := time.NewTicker(1 * time.Minute) // 1分ごとにチェック
	defer ticker.Stop()

	for {
		<-ticker.C
		cutoffTime := time.Now().Add(-inactivityThreshold)

		// inactivityThreshold以上にlast_seenが古いセッションを終了
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
			// セッションを終了する際に、end_time を last_seen + inactivityThreshold に設定
			// これにより、セッション終了時刻が正確に反映されます
			endTime := lastSeen.Add(inactivityThreshold)
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
	// 設定ファイルのパスを指定（必要に応じて変更）
	configPath := "config.toml"

	// 設定を読み込む
	var config Config
	if _, err := toml.DecodeFile(configPath, &config); err != nil {
		log.Fatalf("設定ファイルの読み込みに失敗しました: %v\n", err)
	}

	// モードとポートのコマンドラインフラグを定義
	mode := flag.String("mode", config.Mode, "アプリケーションの実行モード (docker または local)")
	port := flag.String("port", config.ServerPort, "サーバを実行するポート")
	flag.Parse()

	var proxyURL, dbConnStr string
	var skipRegistration bool

	// モードに応じてURLを決定
	if *mode == "local" {
		proxyURL = config.Local.ProxyURL
		dbConnStr = config.Local.DBConnStr
		skipRegistration = config.Local.SkipRegistration
	} else {
		proxyURL = config.Docker.ProxyURL
		dbConnStr = config.Docker.DBConnStr
		skipRegistration = config.Docker.SkipRegistration
	}

	// 設定値を出力
	log.Printf("モード: %s", *mode)
	log.Printf("サーバポート: %s", *port)
	log.Printf("Proxy URL: %s", proxyURL)
	log.Printf("データベース接続文字列: %s", dbConnStr)
	log.Printf("skipRegistration: %v", skipRegistration)
	log.Printf("システムURI: %s", config.Registration.SystemURI)

	// データベースに接続
	db, err := sql.Open("postgres", dbConnStr)
	if err != nil {
		log.Fatalf("データベースに接続できませんでした: %v\n", err)
	}
	defer db.Close()

	// データベースからUUIDとRSSIしきい値を取得
	uuidThresholds, uuidRoomIDs, err := getUUIDsAndThresholds(db)
	if err != nil {
		log.Fatalf("UUIDとしきい値を取得できませんでした: %v\n", err)
	}

	if !skipRegistration {
		log.Println("skipRegistrationがfalseのため、サーバの登録を行います。")
		registerURL := fmt.Sprintf("%s/api/register", proxyURL)

		// サーバポートを整数に変換
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

	// クリーンアップ処理をバックグラウンドで開始（10分の閾値）
	go cleanUpOldSessions(db, 10*time.Minute)

	// エンドポイントのハンドラを設定
	http.HandleFunc("/api/signals/submit", func(w http.ResponseWriter, r *http.Request) {
		handleSignalsSubmit(w, r, db, proxyURL, uuidThresholds, uuidRoomIDs)
	})
	http.HandleFunc("/api/signals/server", func(w http.ResponseWriter, r *http.Request) {
		handleSignalsServer(w, r, db, proxyURL, uuidThresholds, uuidRoomIDs)
	})

	// 新しいエンドポイントのハンドラを設定
	http.HandleFunc("/api/presence_history", func(w http.ResponseWriter, r *http.Request) {
		handlePresenceHistory(w, r, db)
	})

	http.HandleFunc("/api/current_occupants", func(w http.ResponseWriter, r *http.Request) {
		handleCurrentOccupants(w, r, db)
	})

	log.Printf("ポート %s でサーバを開始します。モード: %s", *port, *mode)
	if err := http.ListenAndServe(":"+*port, nil); err != nil {
		log.Fatalf("サーバを開始できませんでした: %s\n", err)
	}
}
