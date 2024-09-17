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
	Default      DefaultConfig
	Local        LocalConfig
	Registration RegistrationConfig
}

type DefaultConfig struct {
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

// ServerResponse は信号サーバに対するレスポンスを表します
type ServerResponse struct {
	PercentageProcessed int `json:"percentage_processed"`
}

// RegisterRequest は登録リクエストのペイロードを表します
type RegisterRequest struct {
	SystemURI string `json:"system_uri"`
	Port      int    `json:"port"`
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

// beaconsテーブルからすべてのUUIDとそのRSSIしきい値を取得
func getUUIDsAndThresholds(db *sql.DB) (map[string]int, error) {
	rows, err := db.Query("SELECT service_uuid, rssi FROM beacons")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// UUIDをRSSIしきい値にマッピング
	uuidThresholds := make(map[string]int)
	for rows.Next() {
		var uuid string
		var threshold int
		if err := rows.Scan(&uuid, &threshold); err != nil {
			return nil, err
		}
		uuid = strings.TrimSpace(uuid) // 空白を除去
		uuidThresholds[uuid] = threshold
		log.Printf("UUIDをロード: %s, RSSIしきい値: %d", uuid, threshold)
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
		// ユーザIDが提供されていない場合は匿名ユーザとします
		username = "anonymous"
	}
	return username
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

// /api/signals/submit エンドポイントの処理
func handleSignalsSubmit(w http.ResponseWriter, r *http.Request, proxyURL string, uuidThresholds map[string]int) {
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
	userID := getUserID(r)

	// 現在の日付を取得
	currentDate := time.Now().Format("2006-01-02") // YYYY-MM-DD

	// 保存先ディレクトリを構築
	baseDir := "./uploads"
	userDir := filepath.Join(baseDir, userID)
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

	for _, record := range bleRecords {
		if len(record) > 2 {
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

	if foundStrongSignal {
		// 強い信号が検出されたので、照会サーバに問い合わせる必要はない
		log.Println("強い信号でデバイスが存在します。")
	} else if foundWeakSignal {
		// 弱い信号が検出されたので、照会サーバに問い合わせる
		log.Println("弱い信号が検出されたため、照会サーバに問い合わせます。")
		err := forwardFilesToInquiry(wifiFile, bleFile, proxyURL)
		if err != nil {
			http.Error(w, fmt.Sprintf("照会サーバへのファイル転送エラー: %v", err), http.StatusInternalServerError)
			return
		}
	} else {
		// デバイスが見つからなかった場合、何もしない
		log.Println("BLEデータにデバイスが見つかりませんでした。何も行いません。")
	}

	response := UploadResponse{Message: "信号データを受信しました"}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// /api/signals/server エンドポイントの処理
func handleSignalsServer(w http.ResponseWriter, r *http.Request, proxyURL string, uuidThresholds map[string]int) {
	// handleSignalsSubmit と同じ処理を行う
	handleSignalsSubmit(w, r, proxyURL, uuidThresholds)
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
	mode := flag.String("mode", config.Mode, "アプリケーションの実行モード (default または local)")
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
		proxyURL = config.Default.ProxyURL
		dbConnStr = config.Default.DBConnStr
		skipRegistration = config.Default.SkipRegistration
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
	uuidThresholds, err := getUUIDsAndThresholds(db)
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

	http.HandleFunc("/api/signals/submit", func(w http.ResponseWriter, r *http.Request) {
		handleSignalsSubmit(w, r, proxyURL, uuidThresholds)
	})
	http.HandleFunc("/api/signals/server", func(w http.ResponseWriter, r *http.Request) {
		handleSignalsServer(w, r, proxyURL, uuidThresholds)
	})

	log.Printf("ポート %s でサーバを開始します。モード: %s", *port, *mode)
	if err := http.ListenAndServe(":"+*port, nil); err != nil {
		log.Fatalf("サーバを開始できませんでした: %s\n", err)
	}
}
