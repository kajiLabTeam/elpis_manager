package main

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	_ "github.com/lib/pq"
)

// Config 設定ファイルの構造体
type Config struct {
	Database struct {
		ConnStr string `toml:"conn_str"`
	} `toml:"database"`
	Server struct {
		Port int `toml:"port"`
	} `toml:"server"`
}

// RegisterRequest 連合登録リクエストの構造体
type RegisterRequest struct {
	Scheme string `json:"scheme"` // 必須: "http" または "https"
	Host   string `json:"host"`   // 必須: ホスト名またはIPアドレス
	Port   int    `json:"port"`   // オプション: ポート番号
}

// RegisterResponse 連合登録レスポンスの構造体
type RegisterResponse struct {
	Message string `json:"message"`
}

// InquiryRequest 照会リクエストの構造体
type InquiryRequest struct {
	WifiData           string  `json:"wifi_data"`
	BleData            string  `json:"ble_data"`
	PresenceConfidence float64 `json:"presence_confidence"`
}

// InquiryResponse 照会レスポンスの構造体
type InquiryResponse struct {
	ServerConfidence float64 `json:"server_confidence"`
	Success          bool    `json:"success"`
	Message          string  `json:"message,omitempty"`
}

var (
	db           *sql.DB
	client       = &http.Client{Timeout: 10 * time.Second}
	queryCounter int
	counterMutex = &sync.Mutex{}
)

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.SetOutput(os.Stdout)
}

func main() {
	var config Config
	if _, err := toml.DecodeFile("config.toml", &config); err != nil {
		log.Fatalf("[FATAL] 設定ファイルの読み込みエラー: %v", err)
	}

	var err error
	db, err = sql.Open("postgres", config.Database.ConnStr)
	if err != nil {
		log.Fatalf("[FATAL] データベースへの接続エラー: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("[FATAL] データベースに接続できません: %v", err)
	}
	log.Printf("[INFO] データベースに接続しました。")

	http.HandleFunc("/api/register", registerHandler)
	http.HandleFunc("/api/inquiry", inquiryHandler)

	go cleanupCache()

	address := fmt.Sprintf(":%d", config.Server.Port)
	log.Printf("[INFO] サーバーがポート %d で起動しました\n", config.Server.Port)
	if err := http.ListenAndServe(address, nil); err != nil {
		log.Fatalf("[FATAL] サーバーの起動に失敗しました: %v", err)
	}
}

// registerHandler /api/register エンドポイントのハンドラ
func registerHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		handleRegisterPost(w, r)
	case http.MethodGet:
		handleRegisterGet(w, r)
	default:
		log.Printf("[WARN] 許可されていないメソッド: %s, パス: %s", r.Method, r.URL.Path)
		http.Error(w, "許可されていないメソッドです", http.StatusMethodNotAllowed)
	}
}

// handleRegisterPost POST /api/register の処理
func handleRegisterPost(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[ERROR] JSONデコードエラー: %v", err)
		http.Error(w, "リクエストの形式が正しくありません", http.StatusBadRequest)
		return
	}

	// スキームのバリデーション
	if req.Scheme != "http" && req.Scheme != "https" {
		log.Printf("[ERROR] 不正なスキーム: %s", req.Scheme)
		http.Error(w, "スキームは 'http' または 'https' でなければなりません", http.StatusBadRequest)
		return
	}

	// 必須フィールドのチェック
	if req.Host == "" {
		log.Printf("[ERROR] ホストが指定されていません")
		http.Error(w, "ホストは必須です", http.StatusBadRequest)
		return
	}

	// system_uri の構築
	systemURI := fmt.Sprintf("%s://%s", req.Scheme, req.Host)
	if req.Port != 0 {
		systemURI = fmt.Sprintf("%s:%d", systemURI, req.Port)
	}

	log.Printf("[INFO] /api/register POST リクエスト - SystemURI: %s", systemURI)

	query := `
        INSERT INTO organizations (api_endpoint, port_number, last_updated)
        VALUES ($1, $2, CURRENT_TIMESTAMP)
        ON CONFLICT (api_endpoint)
        DO UPDATE SET port_number = EXCLUDED.port_number, last_updated = CURRENT_TIMESTAMP
    `
	_, err := db.Exec(query, req.Scheme+"://"+req.Host, req.Port)
	if err != nil {
		log.Printf("[ERROR] データベースエラー: %v", err)
		http.Error(w, "内部サーバーエラーが発生しました", http.StatusInternalServerError)
		return
	}

	resp := RegisterResponse{
		Message: "Success",
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("[ERROR] JSONエンコードエラー: %v", err)
		http.Error(w, "JSONエンコードエラー", http.StatusInternalServerError)
		return
	}

	log.Printf("[INFO] /api/register POST レスポンス - Message: %s", resp.Message)
}

// handleRegisterGet GET /api/register の処理
func handleRegisterGet(w http.ResponseWriter, _ *http.Request) {
	query := `SELECT api_endpoint, port_number, last_updated FROM organizations`
	rows, err := db.Query(query)
	if err != nil {
		log.Printf("[ERROR] データベースクエリエラー: %v", err)
		http.Error(w, fmt.Sprintf("データベースクエリエラー: %v", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type Organization struct {
		APIEndpoint string    `json:"api_endpoint"`
		PortNumber  int       `json:"port_number"`
		LastUpdated time.Time `json:"last_updated"`
	}

	var organizations []Organization
	for rows.Next() {
		var org Organization
		if err := rows.Scan(&org.APIEndpoint, &org.PortNumber, &org.LastUpdated); err != nil {
			log.Printf("[ERROR] 行のスキャンエラー: %v", err)
			http.Error(w, fmt.Sprintf("行のスキャンエラー: %v", err), http.StatusInternalServerError)
			return
		}
		organizations = append(organizations, org)
	}

	if err := rows.Err(); err != nil {
		log.Printf("[ERROR] 行のエラー: %v", err)
		http.Error(w, fmt.Sprintf("行のエラー: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(organizations); err != nil {
		log.Printf("[ERROR] JSONエンコードエラー: %v", err)
		http.Error(w, "JSONエンコードエラー", http.StatusInternalServerError)
		return
	}

	log.Printf("[INFO] /api/register GET レスポンス - 組織数: %d", len(organizations))
}

// inquiryHandler /api/inquiry エンドポイントのハンドラ
func inquiryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		log.Printf("[WARN] 許可されていないメソッド: %s, パス: %s", r.Method, r.URL.Path)
		http.Error(w, "許可されていないメソッドです", http.StatusMethodNotAllowed)
		return
	}

	var inquiryReq InquiryRequest
	if err := json.NewDecoder(r.Body).Decode(&inquiryReq); err != nil {
		log.Printf("[ERROR] JSONデコードエラー: %v", err)
		http.Error(w, "リクエストの形式が正しくありません", http.StatusBadRequest)
		return
	}

	wifiData, err := parseCSVFromString(inquiryReq.WifiData)
	if err != nil {
		log.Printf("[ERROR] WiFi CSV の解析エラー: %v", err)
		http.Error(w, "WiFi CSV の解析エラー", http.StatusBadRequest)
		return
	}

	bleData, err := parseCSVFromString(inquiryReq.BleData)
	if err != nil {
		log.Printf("[ERROR] BLE CSV の解析エラー: %v", err)
		http.Error(w, "BLE CSV の解析エラー", http.StatusBadRequest)
		return
	}

	log.Printf("[INFO] /api/inquiry POST リクエスト - WiFiレコード数: %d, BLEレコード数: %d", len(wifiData), len(bleData))

	query := `SELECT api_endpoint, port_number FROM organizations`
	rows, err := db.Query(query)
	if err != nil {
		log.Printf("[ERROR] データベースクエリエラー: %v", err)
		http.Error(w, fmt.Sprintf("データベースクエリエラー: %v", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type Organization struct {
		APIEndpoint string
		PortNumber  int
	}

	var organizations []Organization
	for rows.Next() {
		var org Organization
		if err := rows.Scan(&org.APIEndpoint, &org.PortNumber); err != nil {
			log.Printf("[ERROR] 行のスキャンエラー: %v", err)
			http.Error(w, fmt.Sprintf("行のスキャンエラー: %v", err), http.StatusInternalServerError)
			return
		}
		organizations = append(organizations, org)
	}

	if err := rows.Err(); err != nil {
		log.Printf("[ERROR] 行のエラー: %v", err)
		http.Error(w, fmt.Sprintf("行のエラー: %v", err), http.StatusInternalServerError)
		return
	}

	if len(organizations) == 0 {
		log.Printf("[WARN] 組織情報が存在しません")
		http.Error(w, "組織情報が存在しません", http.StatusNotFound)
		return
	}

	maxPercentage := 0
	var wg sync.WaitGroup
	responseChan := make(chan InquiryResponse, len(organizations))

	for _, org := range organizations {
		wg.Add(1)
		go func(systemURI string, port int) {
			defer wg.Done()
			percentage, err := querySystem(systemURI, port, wifiData, bleData)
			if err != nil {
				log.Printf("[ERROR] 組織 %s:%d へのクエリシステムエラー: %v", systemURI, port, err)
				responseChan <- InquiryResponse{
					ServerConfidence: 0,
					Success:          false,
					Message:          err.Error(),
				}
				return
			}
			responseChan <- InquiryResponse{
				ServerConfidence: float64(percentage),
				Success:          true,
			}
		}(org.APIEndpoint, org.PortNumber)
	}

	wg.Wait()
	close(responseChan)

	for resp := range responseChan {
		if resp.Success && int(resp.ServerConfidence) > maxPercentage {
			maxPercentage = int(resp.ServerConfidence)
		}
	}

	finalResp := InquiryResponse{
		ServerConfidence: float64(maxPercentage),
		Success:          true,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(finalResp); err != nil {
		log.Printf("[ERROR] JSONエンコードエラー: %v", err)
		http.Error(w, "JSONエンコードエラー", http.StatusInternalServerError)
		return
	}

	log.Printf("[INFO] /api/inquiry レスポンス - Success: %t, ServerConfidence: %.2f%%", finalResp.Success, finalResp.ServerConfidence)

	counterMutex.Lock()
	log.Printf("[DEBUG] querySystem の呼び出し回数: %d", queryCounter)
	counterMutex.Unlock()
}

// querySystem 他の照会サーバに問い合わせる関数
func querySystem(systemURI string, port int, wifiData, bleData [][]string) (int, error) {
	counterMutex.Lock()
	queryCounter++
	counterMutex.Unlock()

	url := fmt.Sprintf("%s:%d/api/signals/server", systemURI, port)

	wifiCSV := csvToString(wifiData)
	bleCSV := csvToString(bleData)

	inquiryReq := InquiryRequest{
		WifiData:           wifiCSV,
		BleData:            bleCSV,
		PresenceConfidence: 0,
	}

	reqBody, err := json.Marshal(inquiryReq)
	if err != nil {
		return 0, fmt.Errorf("照会リクエストのエンコードに失敗しました: %v", err)
	}

	resp, err := client.Post(url, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return 0, fmt.Errorf("リクエスト送信エラー: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("照会サーバからの応答が不正です。ステータスコード: %d", resp.StatusCode)
	}

	var signalResp InquiryResponse
	if err := json.NewDecoder(resp.Body).Decode(&signalResp); err != nil {
		return 0, fmt.Errorf("レスポンスのパースエラー: %v", err)
	}

	if !signalResp.Success {
		return 0, fmt.Errorf("照会サーバでの処理に失敗しました: %s", signalResp.Message)
	}

	return int(signalResp.ServerConfidence), nil
}

// csvToString 二次元スライスをCSV形式の文字列に変換する関数
func csvToString(data [][]string) string {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	for _, record := range data {
		writer.Write(record)
	}
	writer.Flush()
	return buf.String()
}

// parseCSVFromString 文字列からCSVをパースして二次元スライスに変換する関数
func parseCSVFromString(data string) ([][]string, error) {
	reader := csv.NewReader(strings.NewReader(data))
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	return records, nil
}

// cleanupCache 定期的にキャッシュをクリーンアップする関数
func cleanupCache() {
	for {
		time.Sleep(1 * time.Hour)
		query := `DELETE FROM organizations WHERE last_updated < NOW() - INTERVAL '24 hours' RETURNING api_endpoint`
		rows, err := db.Query(query)
		if err != nil {
			log.Printf("[ERROR] キャッシュのクリーンアップエラー: %v", err)
			continue
		}

		var deletedEndpoints []string
		for rows.Next() {
			var endpoint string
			if err := rows.Scan(&endpoint); err != nil {
				log.Printf("[ERROR] 削除されたエンドポイントのスキャンエラー: %v", err)
				continue
			}
			deletedEndpoints = append(deletedEndpoints, endpoint)
		}
		rows.Close()

		for _, endpoint := range deletedEndpoints {
			log.Printf("[INFO] 期限切れのキャッシュエントリを削除しました: %s", endpoint)
		}
	}
}
