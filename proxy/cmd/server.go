package main

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
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

// InquiryResponse は問い合わせレスポンスのJSON構造を表します
type InquiryResponse struct {
	Success             bool `json:"success"`
	PercentageProcessed int  `json:"percentage_processed"`
}

// SignalResponse はシグナルレスポンスのJSON構造を表します
type SignalResponse struct {
	PercentageProcessed int `json:"percentage_processed"`
}

var (
	db           *sql.DB
	client       = &http.Client{Timeout: 5 * time.Second}
	queryCounter int
	counterMutex = &sync.Mutex{}
)

// main 関数はサーバーを初期化し、設定を読み込み、データベースに接続し、HTTPハンドラーを設定してサーバーを起動します。
func main() {
	var config Config
	if _, err := toml.DecodeFile("config.toml", &config); err != nil {
		log.Fatalf("設定ファイルの読み込みエラー: %v", err)
	}

	var err error
	db, err = sql.Open("postgres", config.Database.ConnStr)
	if err != nil {
		log.Fatalf("データベースへの接続エラー: %v", err)
	}
	defer db.Close()

	http.HandleFunc("/api/register", registerHandler)
	http.HandleFunc("/api/inquiry", inquiryHandler)

	go cleanupCache()

	fmt.Printf("サーバーがポート :%d で起動しました\n", config.Server.Port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", config.Server.Port), nil))
}

// registerHandler は /api/register エンドポイントのリクエストを処理します。メソッドに応じてPOSTまたはGETのハンドラーに分岐します。
func registerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		handleRegisterPost(w, r)
	} else if r.Method == http.MethodGet {
		handleRegisterGet(w, r)
	} else {
		http.Error(w, "許可されていないメソッドです", http.StatusMethodNotAllowed)
	}
}

// handleRegisterPost 関数は登録用のPOSTリクエストを処理し、データベースに組織情報を挿入または更新します。
func handleRegisterPost(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("JSONデコードエラー: %v", err)
		http.Error(w, "リクエストの形式が正しくありません", http.StatusBadRequest)
		return
	}

	fmt.Println("受信した /api/register リクエスト:")
	fmt.Printf("SystemURI: %s, Port: %d\n", req.SystemURI, req.Port)

	query := `
        INSERT INTO organizations (api_endpoint, port_number)
        VALUES ($1, $2)
        ON CONFLICT (api_endpoint)
        DO UPDATE SET port_number = $2, last_updated = CURRENT_TIMESTAMP
    `
	_, err := db.Exec(query, req.SystemURI, req.Port)
	if err != nil {
		log.Printf("データベースエラー: %v", err)
		http.Error(w, "内部サーバーエラーが発生しました", http.StatusInternalServerError)
		return
	}

	resp := RegisterResponse{
		Message: "Success",
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)

	fmt.Println("送信した /api/register レスポンス:")
	fmt.Printf("Message: %s\n", resp.Message)
}

// handleRegisterGet 関数は登録用のGETリクエストを処理し、データベースから現在の組織一覧を取得して返します。
func handleRegisterGet(w http.ResponseWriter, _ *http.Request) {
	query := `SELECT api_endpoint, port_number, last_updated FROM organizations`
	rows, err := db.Query(query)
	if err != nil {
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
			http.Error(w, fmt.Sprintf("行のスキャンエラー: %v", err), http.StatusInternalServerError)
			return
		}
		organizations = append(organizations, org)
	}

	if err := rows.Err(); err != nil {
		http.Error(w, fmt.Sprintf("行のエラー: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(organizations)

	fmt.Println("送信した /api/register GET レスポンス（データベースからの現在の組織一覧）")
}

// inquiryHandler 関数は /api/inquiry エンドポイントのPOSTリクエストを処理し、受信したWiFiおよびBLEデータを各組織に問い合わせて結果を返します。
func inquiryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "許可されていないメソッドです", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseMultipartForm(10 << 20) // 10 MB 制限
	if err != nil {
		http.Error(w, "フォームの解析エラー", http.StatusBadRequest)
		return
	}

	wifiFile, _, err := r.FormFile("wifi_data")
	if err != nil {
		http.Error(w, "WiFiファイルの取得エラー", http.StatusBadRequest)
		return
	}
	defer wifiFile.Close()

	bleFile, _, err := r.FormFile("ble_data")
	if err != nil {
		http.Error(w, "BLEファイルの取得エラー", http.StatusBadRequest)
		return
	}
	defer bleFile.Close()

	wifiData, err := parseCSV(wifiFile)
	if err != nil {
		http.Error(w, "WiFi CSV の解析エラー", http.StatusBadRequest)
		return
	}

	bleData, err := parseCSV(bleFile)
	if err != nil {
		http.Error(w, "BLE CSV の解析エラー", http.StatusBadRequest)
		return
	}

	fmt.Println("受信した /api/inquiry リクエスト:")
	fmt.Println("WiFi Data:")
	for _, row := range wifiData {
		fmt.Println(row)
	}

	fmt.Println("BLE Data:")
	for _, row := range bleData {
		fmt.Println(row)
	}

	query := `SELECT api_endpoint, port_number FROM organizations`
	rows, err := db.Query(query)
	if err != nil {
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
			http.Error(w, fmt.Sprintf("行のスキャンエラー: %v", err), http.StatusInternalServerError)
			return
		}
		organizations = append(organizations, org)
	}

	if err := rows.Err(); err != nil {
		http.Error(w, fmt.Sprintf("行のエラー: %v", err), http.StatusInternalServerError)
		return
	}

	maxPercentage := 0
	var wg sync.WaitGroup
	responseChan := make(chan int, len(organizations))

	for _, org := range organizations {
		wg.Add(1)
		go func(systemURI string, port int) {
			defer wg.Done()
			percentage := querySystem(systemURI, port, wifiData, bleData)
			responseChan <- percentage
		}(org.APIEndpoint, org.PortNumber)
	}

	wg.Wait()
	close(responseChan)

	for percentage := range responseChan {
		if percentage > maxPercentage {
			maxPercentage = percentage
		}
	}

	resp := InquiryResponse{
		Success:             true,
		PercentageProcessed: maxPercentage,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)

	fmt.Println("送信した /api/inquiry レスポンス:")
	fmt.Printf("Success: %t, PercentageProcessed: %d\n", resp.Success, resp.PercentageProcessed)

	counterMutex.Lock()
	fmt.Printf("querySystem が %d 回呼び出されました\n", queryCounter)
	counterMutex.Unlock()
}

// querySystem 関数は指定された組織のAPIに対してWiFiおよびBLEデータを送信し、処理されたパーセンテージを取得します。
func querySystem(systemURI string, port int, wifiData, bleData [][]string) int {
	counterMutex.Lock()
	queryCounter++
	counterMutex.Unlock()

	url := fmt.Sprintf("http://%s:%d/api/signals/server", systemURI, port)
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	wifiPart, err := writer.CreateFormFile("wifi_data", "wifi_data.csv")
	if err != nil {
		fmt.Printf("WiFiフォームファイルの作成エラー（%s）: %v\n", systemURI, err)
		return 0
	}
	if err := writeCSV(wifiPart, wifiData); err != nil {
		fmt.Printf("WiFi CSV の書き込みエラー（%s）: %v\n", systemURI, err)
		return 0
	}

	blePart, err := writer.CreateFormFile("ble_data", "ble_data.csv")
	if err != nil {
		fmt.Printf("BLEフォームファイルの作成エラー（%s）: %v\n", systemURI, err)
		return 0
	}
	if err := writeCSV(blePart, bleData); err != nil {
		fmt.Printf("BLE CSV の書き込みエラー（%s）: %v\n", systemURI, err)
		return 0
	}

	writer.Close()

	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		fmt.Printf("リクエストの作成エラー（%s）: %v\n", systemURI, err)
		return 0
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("リクエスト送信エラー（%s）: %v\n", systemURI, err)
		return 0
	}
	defer resp.Body.Close()

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("レスポンス読み取りエラー（%s）: %v\n", systemURI, err)
		return 0
	}

	var signalResponse SignalResponse
	if err := json.Unmarshal(respBody, &signalResponse); err != nil {
		fmt.Printf("レスポンスのアンマーシャルエラー（%s）: %v\n", systemURI, err)
		return 0
	}

	return signalResponse.PercentageProcessed
}

// writeCSV 関数は与えられたデータをCSV形式で指定されたライターに書き込みます。
func writeCSV(writer io.Writer, data [][]string) error {
	csvWriter := csv.NewWriter(writer)
	for _, record := range data {
		if err := csvWriter.Write(record); err != nil {
			return err
		}
	}
	csvWriter.Flush()
	return csvWriter.Error()
}

// parseCSV 関数は指定されたリーダーからCSVデータを解析し、2次元スライスとして返します。
func parseCSV(file io.Reader) ([][]string, error) {
	reader := csv.NewReader(file)
	var data [][]string
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		data = append(data, record)
	}
	return data, nil
}

// cleanupCache 関数は定期的にデータベースから24時間以上更新されていない組織エントリを削除します。
func cleanupCache() {
	for {
		time.Sleep(1 * time.Hour)
		query := `DELETE FROM organizations WHERE last_updated < NOW() - INTERVAL '24 hours' RETURNING api_endpoint`
		rows, err := db.Query(query)
		if err != nil {
			fmt.Printf("キャッシュのクリーンアップエラー: %v\n", err)
			continue
		}

		var deletedEndpoints []string
		for rows.Next() {
			var endpoint string
			if err := rows.Scan(&endpoint); err != nil {
				fmt.Printf("削除されたエンドポイントのスキャンエラー: %v\n", err)
				continue
			}
			deletedEndpoints = append(deletedEndpoints, endpoint)
		}
		rows.Close()

		for _, endpoint := range deletedEndpoints {
			fmt.Printf("期限切れのキャッシュエントリを削除しました: %s\n", endpoint)
		}
	}
}
