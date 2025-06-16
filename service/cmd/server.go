// elpis_server.go – /api/register, /api/partners/register, /api/query (dummy)

package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

/*────────────── Config ──────────────*/

const (
	addr      = ":8012"
	dbDSN     = "postgres://myuser:mypassword@postgres_service:5432/servicedb?sslmode=disable"
	dbTimeout = 5 * time.Second
	maxMemory = 10 << 20 // multipart parse size (10 MiB)
)

/*────────────── Helpers ──────────────*/

type apiError struct {
	Error string `json:"error"`
}

func writeAPIError(w http.ResponseWriter, status int, msg string, err error) {
	log.Printf("[ERROR] %s: %v", msg, err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiError{Error: msg})
}

/*────────────── BasicAuth ──────────────*/

func requireBasicAuth(next http.Handler) http.Handler {
	user := os.Getenv("BASIC_AUTH_USER")
	if user == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const p = "Basic "
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, p) {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			writeAPIError(w, http.StatusUnauthorized, "auth required", nil)
			return
		}
		b, _ := base64.StdEncoding.DecodeString(h[len(p):])
		parts := strings.SplitN(string(b), ":", 2)
		if len(parts) != 2 || parts[0] != user {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			writeAPIError(w, http.StatusUnauthorized, "invalid user", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

/*────────────── /api/register ──────────────*/

type registerReq struct {
	ManagementServerURL string           `json:"management_server_url"`
	ProxyServerURL      string           `json:"proxy_server_url"`
	Mapping             []roomMappingReq `json:"mapping"`
}
type roomMappingReq struct {
	Floor, RoomID, RoomName string `json:"floor" json:"room_id" json:"room_name"`
}

func handleRegister(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}

		var req registerReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid JSON", err)
			return
		}
		if req.ManagementServerURL == "" || len(req.Mapping) == 0 {
			writeAPIError(w, http.StatusBadRequest, "missing required fields", nil)
			return
		}

		ctx := r.Context()
		tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "begin tx failed", err)
			return
		}
		defer tx.Rollback()

		var regID int
		err = tx.QueryRowContext(ctx,
			`INSERT INTO registrations (management_server_url, proxy_server_url, created_at)
			 VALUES ($1, $2, $3) RETURNING id`,
			req.ManagementServerURL, req.ProxyServerURL, time.Now()).
			Scan(&regID)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "insert registrations failed", err)
			return
		}

		stmt, err := tx.PrepareContext(ctx,
			`INSERT INTO room_mappings (registration_id, floor, room_id, room_name)
			 VALUES ($1, $2, $3, $4)`)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "prepare room_mappings failed", err)
			return
		}
		defer stmt.Close()

		for _, m := range req.Mapping {
			if _, err := stmt.ExecContext(ctx, regID, m.Floor, m.RoomID, m.RoomName); err != nil {
				writeAPIError(w, http.StatusInternalServerError, "insert room_mappings failed", err)
				return
			}
		}

		if err := tx.Commit(); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "commit failed", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Registration successful"})
	}
}

/*────────────── /api/partners/register ──────────────*/

type partnerReq struct {
	InquiryServerURI string  `json:"inquiry_server_uri"`
	Port             int     `json:"port"`
	Latitude         float64 `json:"latitude"`
	Longitude        float64 `json:"longitude"`
	Description      string  `json:"description"`
}

func handlePartnerRegister(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}

		var req partnerReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid JSON", err)
			return
		}
		if req.InquiryServerURI == "" || req.Port == 0 {
			writeAPIError(w, http.StatusBadRequest, "missing required fields", nil)
			return
		}

		ctx := r.Context()
		var id int
		if err := db.QueryRowContext(ctx,
			`INSERT INTO inquiry_partners (inquiry_server_uri, port, latitude, longitude, description, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			req.InquiryServerURI, req.Port, req.Latitude, req.Longitude, req.Description, time.Now()).
			Scan(&id); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "insert inquiry_partners failed", err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"message":    "Inquiry server registered successfully",
			"partner_id": fmt.Sprintf("inq-%d", id),
		})
	}
}

/*────────────── /api/query ──────────────*/

type queryResp struct {
	RoomID    string          `json:"room_id"`
	FloorMap  json.RawMessage `json:"floor_map"`
	Timestamp string          `json:"timestamp"`
}

func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371.0 // km
	φ1, φ2 := lat1*math.Pi/180, lat2*math.Pi/180
	dφ := (lat2 - lat1) * math.Pi / 180
	dλ := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dφ/2)*math.Sin(dφ/2) +
		math.Cos(φ1)*math.Cos(φ2)*math.Sin(dλ/2)*math.Sin(dλ/2)
	return R * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func handleQuery(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		/* ────── 1. メソッド & マルチパート解析 ────── */
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseMultipartForm(maxMemory); err != nil {
			writeAPIError(w, http.StatusBadRequest, "multipart parse error", err)
			return
		}

		/* ────── 2. パラメータ検証 ────── */
		lat, errLat := strconv.ParseFloat(r.FormValue("latitude"), 64)
		lon, errLon := strconv.ParseFloat(r.FormValue("longitude"), 64)
		if errLat != nil || errLon != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid latitude/longitude", nil)
			return
		}
		if _, err := time.Parse(time.RFC3339, r.FormValue("timestamp")); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid timestamp", err)
			return
		}

		// 必須ファイル（内容は保管しない）存在チェック
		if f, _, err := r.FormFile("wifi_data"); err != nil {
			writeAPIError(w, http.StatusBadRequest, "wifi_data missing", err)
			return
		} else {
			f.Close()
		}
		if f, _, err := r.FormFile("ble_data"); err != nil {
			writeAPIError(w, http.StatusBadRequest, "ble_data missing", err)
			return
		} else {
			f.Close()
		}

		/* ────── 3. 最近傍照会サーバを DB から検索 ────── */
		var (
			bestID   int
			bestURI  string
			bestDist = math.MaxFloat64
		)
		rows, err := db.QueryContext(r.Context(),
			`SELECT id, inquiry_server_uri, latitude, longitude FROM inquiry_partners`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id int
				var uri string
				var plat, plon float64
				if err := rows.Scan(&id, &uri, &plat, &plon); err == nil {
					d := haversineKm(lat, lon, plat, plon)
					if d < bestDist {
						bestDist, bestID, bestURI = d, id, uri
					}
				}
			}
		}
		if bestDist < math.MaxFloat64 {
			log.Printf("nearest partner: id=%d uri=%s (%.3f km)", bestID, bestURI, bestDist)
		} else {
			log.Printf("nearest partner: none")
		}

		/* ────── 4. ダミーレスポンス生成 ────── */
		const floorMapJSON = `{
		  "type":"FeatureCollection",
		  "crs":{"type":"name","properties":{"name":"CRS:PIXEL"}},
		  "features":[
		    {"type":"Feature","geometry":{"type":"Polygon","coordinates":[[[781,91],[777,380],[84,389],[84,448],[364,448],[366,837],[84,837],[84,929],[776,932],[776,1431],[839,1431],[841,91]]]},"properties":{"id":"R073","name":"Room073","type":"room","area":354190}},
		    {"type":"Feature","geometry":{"type":"Polygon","coordinates":[[[846,1289],[1088,1289],[1088,1436],[846,1436],[846,1289]]]},"properties":{"id":"R004","name":"Room004","type":"room","area":34869}},
		    {"type":"Feature","geometry":{"type":"Polygon","coordinates":[[[846,989],[1092,989],[1092,1283],[846,1283],[846,989]]]},"properties":{"id":"R008","name":"Room008","type":"room","area":71467}},
		    {"type":"Feature","geometry":{"type":"Polygon","coordinates":[[[649,938],[769,938],[769,1222],[649,1222],[649,938]]]},"properties":{"id":"R010","name":"Room010","type":"room","area":33861}},
		    {"type":"Feature","geometry":{"type":"Polygon","coordinates":[[[459,938],[643,938],[643,1219],[459,1219],[459,938]]]},"properties":{"id":"R011","name":"Room011","type":"room","area":51433}},
		    {"type":"Feature","geometry":{"type":"Polygon","coordinates":[[[209,938],[453,938],[453,1217],[209,1217],[209,938]]]},"properties":{"id":"R012","name":"Room012","type":"room","area":67544}},
		    {"type":"Feature","geometry":{"type":"Polygon","coordinates":[[[84,936],[203,936],[203,1215],[84,1215],[84,936]]]},"properties":{"id":"R016","name":"Room016","type":"room","area":33388}},
		    {"type":"Feature","geometry":{"type":"Polygon","coordinates":[[[846,692],[1093,692],[1093,985],[846,985],[846,692]]]},"properties":{"id":"R022","name":"Room022","type":"room","area":72050}},
		    {"type":"Feature","geometry":{"type":"Polygon","coordinates":[[[847,541],[1094,541],[1094,685],[847,685],[847,541]]]},"properties":{"id":"R050","name":"Room050","type":"room","area":35691}},
		    {"type":"Feature","geometry":{"type":"Polygon","coordinates":[[[427,455],[776,455],[776,871],[427,871],[427,455]]]},"properties":{"id":"R056","name":"Room056","type":"room","area":94347}},
		    {"type":"Feature","geometry":{"type":"Polygon","coordinates":[[[84,580],[358,580],[358,830],[84,830],[84,580]]]},"properties":{"id":"R058","name":"Room057","type":"room","area":73746}},
		    {"type":"Feature","geometry":{"type":"Polygon","coordinates":[[[84,454],[358,454],[358,574],[84,574],[84,454]]]},"properties":{"id":"R058","name":"Room058","type":"room","area":73746}},
		    {"type":"Feature","geometry":{"type":"Polygon","coordinates":[[[848,390],[1093,390],[1093,534],[848,534],[848,390]]]},"properties":{"id":"R060","name":"Room060","type":"room","area":35243}},
		    {"type":"Feature","geometry":{"type":"Polygon","coordinates":[[[84,101],[205,101],[205,380],[84,380],[84,101]]]},"properties":{"id":"R063","name":"Room063","type":"room","area":33763}},
		    {"type":"Feature","geometry":{"type":"Polygon","coordinates":[[[211,97],[453,97],[453,379],[211,379],[211,97]]]},"properties":{"id":"R066","name":"Room066","type":"room","area":67497}},
		    {"type":"Feature","geometry":{"type":"Polygon","coordinates":[[[460,94],[645,94],[645,377],[460,377],[460,94]]]},"properties":{"id":"R069","name":"Room069","type":"room","area":51855}},
		    {"type":"Feature","geometry":{"type":"Polygon","coordinates":[[[652,91],[772,91],[772,374],[652,374],[652,91]]]},"properties":{"id":"R074","name":"Room074","type":"room","area":33729,"highlight":true}},
		    {"type":"Feature","geometry":{"type":"Polygon","coordinates":[[[849,87],[1092,87],[1092,385],[849,385],[849,87]]]},"properties":{"id":"R075","name":"Room075","type":"room","area":72292}}
		  ]
		}`

		resp := queryResp{
			RoomID:    "R074",
			FloorMap:  json.RawMessage(floorMapJSON),
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}

		/* ────── 5. 返却 ────── */
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

/*────────────── main ──────────────*/

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ここでは開発用に全許可。必要に応じて Origin を絞る
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")

		// プリフライト (OPTIONS) なら 204 だけ返す
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func main() {
	db, err := sql.Open("postgres", dbDSN)
	if err != nil {
		log.Fatalf("failed to open DB: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("database unreachable: %v", err)
	}
	log.Println("✅ connected to Postgres")

	mux := http.NewServeMux()
	mux.HandleFunc("/api/register", handleRegister(db))
	mux.HandleFunc("/api/partners/register", handlePartnerRegister(db))
	mux.Handle("/api/query", requireBasicAuth(http.HandlerFunc(handleQuery(db))))
	handler := withCORS(mux)

	log.Printf("HTTP server listening on %s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
