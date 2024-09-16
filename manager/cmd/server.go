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
	"strings"

	_ "github.com/lib/pq"
)

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

// /api/signals/submit エンドポイントの処理
func handleSignalsSubmit(w http.ResponseWriter, r *http.Request, proxyURL string, uuidThresholds map[string]int) {
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

	response := ServerResponse{PercentageProcessed: 100}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func main() {
	// モードとポートのコマンドラインフラグを定義
	mode := flag.String("mode", "docker", "アプリケーションの実行モード (docker または local)")
	port := flag.String("port", "8010", "サーバを実行するポート")
	flag.Parse()

	var proxyURL, managerURL, dbConnStr string

	// モードに応じてURLを決定
	if *mode == "local" {
		proxyURL = "http://localhost:8080"
		managerURL = "http://localhost"
		dbConnStr = "postgres://myuser:mypassword@localhost:5433/managerdb?sslmode=disable"
	} else {
		proxyURL = "http://proxy:8080"
		managerURL = "http://manager"
		dbConnStr = "postgres://myuser:mypassword@postgres_manager:5432/managerdb?sslmode=disable"
	}

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
