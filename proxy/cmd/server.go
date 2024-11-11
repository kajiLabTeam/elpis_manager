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

	"github.com/BurntSushi/toml" // TOML パーサー
	_ "github.com/lib/pq"        // PostgreSQL driver
)

// Config 構造体は config.toml の設定を保持します
type Config struct {
	Database struct {
		ConnStr string `toml:"conn_str"`
	} `toml:"database"`
	Server struct {
		Port int `toml:"port"`
	} `toml:"server"`
}

// RegisterRequest は登録リクエストのJSON構造を表します
type RegisterRequest struct {
	SystemURI string `json:"system_uri"`
	Port      int    `json:"port"`
}

// RegisterResponse は登録レスポンスのJSON構造を表します
type RegisterResponse struct {
	Message string `json:"message"`
}

// InquiryRequest は照会サーバーへのリクエストペイロードを表します
type InquiryRequest struct {
	WifiData           string  `json:"wifi_data"`           // WiFiデータの内容（CSVの文字列）
	BleData            string  `json:"ble_data"`            // BLEデータの内容（CSVの文字列）
	PresenceConfidence float64 `json:"presence_confidence"` // 在室確信度
}

// InquiryResponse は照会サーバーからのレスポンスを表します
type InquiryResponse struct {
	ServerConfidence float64 `json:"server_confidence"` // 照会サーバー側の確信度
	Success          bool    `json:"success"`           // 処理の成功可否
	Message          string  `json:"message,omitempty"` // エラーメッセージ（必要な場合）
}

var (
	db           *sql.DB
	client       = &http.Client{Timeout: 10 * time.Second} // タイムアウトを10秒に設定
	queryCounter int
	counterMutex = &sync.Mutex{}
)

func init() {
	// ログのフォーマットをカスタマイズ
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	// ログの出力先を標準出力に設定（必要に応じてファイルに変更可能）
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

	// データベースの接続確認
	if err := db.Ping(); err != nil {
		log.Fatalf("[FATAL] データベースに接続できません: %v", err)
	}
	log.Printf("[INFO] データベースに接続しました。")

	// ハンドラーの設定
	http.HandleFunc("/api/register", registerHandler)
	http.HandleFunc("/api/inquiry", inquiryHandler)

	// キャッシュクリーンアップのゴルーチン開始
	go cleanupCache()

	// サーバーの起動
	address := fmt.Sprintf(":%d", config.Server.Port)
	log.Printf("[INFO] サーバーがポート %d で起動しました\n", config.Server.Port)
	if err := http.ListenAndServe(address, nil); err != nil {
		log.Fatalf("[FATAL] サーバーの起動に失敗しました: %v", err)
	}
}

// registerHandler は /api/register エンドポイントのリクエストを処理します。メソッドに応じてPOSTまたはGETのハンドラーに分岐します。
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

// handleRegisterPost 関数は登録用のPOSTリクエストを処理し、データベースに組織情報を挿入または更新します。
func handleRegisterPost(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[ERROR] JSONデコードエラー: %v", err)
		http.Error(w, "リクエストの形式が正しくありません", http.StatusBadRequest)
		return
	}

	// リクエスト内容のログ出力（要約）
	log.Printf("[INFO] /api/register POST リクエスト - SystemURI: %s, Port: %d", req.SystemURI, req.Port)

	// 組織情報の挿入または更新
	query := `
        INSERT INTO organizations (api_endpoint, port_number)
        VALUES ($1, $2)
        ON CONFLICT (api_endpoint)
        DO UPDATE SET port_number = $2, last_updated = CURRENT_TIMESTAMP
    `
	_, err := db.Exec(query, req.SystemURI, req.Port)
	if err != nil {
		log.Printf("[ERROR] データベースエラー: %v", err)
		http.Error(w, "内部サーバーエラーが発生しました", http.StatusInternalServerError)
		return
	}

	// レスポンスの作成
	resp := RegisterResponse{
		Message: "Success",
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("[ERROR] JSONエンコードエラー: %v", err)
		http.Error(w, "JSONエンコードエラー", http.StatusInternalServerError)
		return
	}

	// レスポンス内容のログ出力
	log.Printf("[INFO] /api/register POST レスポンス - Message: %s", resp.Message)
}

// handleRegisterGet 関数は登録用のGETリクエストを処理し、データベースから現在の組織一覧を取得して返します。
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

// inquiryHandler 関数は /api/inquiry エンドポイントのPOSTリクエストを処理し、受信したWiFiおよびBLEデータを各組織に問い合わせて結果を返します。
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

	// WiFiおよびBLEデータの解析
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

	// 受信データのログ出力（要約情報）
	log.Printf("[INFO] /api/inquiry POST リクエスト - WiFiレコード数: %d, BLEレコード数: %d", len(wifiData), len(bleData))

	// データベースから組織情報を取得
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

	// 各組織に対してリクエストを送信し、確信度を収集
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

	// 確信度の最大値を取得
	for resp := range responseChan {
		if resp.Success && int(resp.ServerConfidence) > maxPercentage {
			maxPercentage = int(resp.ServerConfidence)
		}
	}

	// レスポンスを作成
	finalResp := InquiryResponse{
		ServerConfidence: float64(maxPercentage),
		Success:          true,
	}

	// レスポンスを送信
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(finalResp); err != nil {
		log.Printf("[ERROR] JSONエンコードエラー: %v", err)
		http.Error(w, "JSONエンコードエラー", http.StatusInternalServerError)
		return
	}

	// レスポンス内容のログ出力
	log.Printf("[INFO] /api/inquiry レスポンス - Success: %t, ServerConfidence: %.2f%%", finalResp.Success, finalResp.ServerConfidence)

	// クエリカウンタのログ出力
	counterMutex.Lock()
	log.Printf("[DEBUG] querySystem の呼び出し回数: %d", queryCounter)
	counterMutex.Unlock()
}

// querySystem 関数は指定された組織のAPIに対してWiFiおよびBLEデータを送信し、処理されたパーセンテージを取得します。
func querySystem(systemURI string, port int, wifiData, bleData [][]string) (int, error) {
	counterMutex.Lock()
	queryCounter++
	counterMutex.Unlock()

	url := fmt.Sprintf("http://%s:%d/api/signals/server", systemURI, port)

	// CSVデータを文字列に変換
	wifiCSV := csvToString(wifiData)
	bleCSV := csvToString(bleData)

	// リクエストペイロードを作成
	inquiryReq := InquiryRequest{
		WifiData:           wifiCSV,
		BleData:            bleCSV,
		PresenceConfidence: 0, // 必要に応じて設定
	}

	// JSONエンコード
	reqBody, err := json.Marshal(inquiryReq)
	if err != nil {
		return 0, fmt.Errorf("照会リクエストのエンコードに失敗しました: %v", err)
	}

	// POSTリクエストを送信
	resp, err := client.Post(url, "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		return 0, fmt.Errorf("リクエスト送信エラー: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("照会サーバからの応答が不正です。ステータスコード: %d", resp.StatusCode)
	}

	// レスポンスをパース
	var signalResp InquiryResponse
	if err := json.NewDecoder(resp.Body).Decode(&signalResp); err != nil {
		return 0, fmt.Errorf("レスポンスのパースエラー: %v", err)
	}

	if !signalResp.Success {
		return 0, fmt.Errorf("照会サーバでの処理に失敗しました: %s", signalResp.Message)
	}

	return int(signalResp.ServerConfidence), nil
}

// csvToString 関数は2次元スライスのCSVデータを文字列に変換します。
func csvToString(data [][]string) string {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	for _, record := range data {
		writer.Write(record)
	}
	writer.Flush()
	return buf.String()
}

// parseCSVFromString 関数は文字列からCSVデータを解析し、2次元スライスとして返します。
func parseCSVFromString(data string) ([][]string, error) {
	reader := csv.NewReader(strings.NewReader(data))
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	return records, nil
}

// cleanupCache 関数は定期的にデータベースから24時間以上更新されていない組織エントリを削除します。
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
