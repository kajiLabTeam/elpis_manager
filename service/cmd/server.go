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
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseMultipartForm(maxMemory); err != nil {
			writeAPIError(w, http.StatusBadRequest, "multipart parse error", err)
			return
		}

		lat, errLat := strconv.ParseFloat(r.FormValue("latitude"), 64)
		lon, errLon := strconv.ParseFloat(r.FormValue("longitude"), 64)
		_, errTime := time.Parse(time.RFC3339, r.FormValue("timestamp"))
		if errLat != nil || errLon != nil || errTime != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid lat/lon/timestamp", nil)
			return
		}

		// 必須ファイル存在チェック（内容は無視）
		if _, _, err := r.FormFile("wifi_data"); err != nil {
			writeAPIError(w, http.StatusBadRequest, "wifi_data missing", err)
			return
		}
		if _, _, err := r.FormFile("ble_data"); err != nil {
			writeAPIError(w, http.StatusBadRequest, "ble_data missing", err)
			return
		}

		// ── 近接パートナー検索
		rows, err := db.QueryContext(r.Context(),
			`SELECT id, inquiry_server_uri, latitude, longitude FROM inquiry_partners`)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "select partners failed", err)
			return
		}
		defer rows.Close()

		var (
			minDist = math.MaxFloat64
			bestID  int
			bestURI string
			bestLat float64
			bestLon float64
		)
		for rows.Next() {
			var id int
			var uri string
			var plat, plon float64
			if err := rows.Scan(&id, &uri, &plat, &plon); err != nil {
				writeAPIError(w, http.StatusInternalServerError, "row scan failed", err)
				return
			}
			d := haversineKm(lat, lon, plat, plon)
			if d < minDist {
				minDist, bestID, bestURI, bestLat, bestLon = d, id, uri, plat, plon
			}
		}
		if minDist < math.MaxFloat64 {
			log.Printf("nearest partner: id=%d uri=%s (%.3f km) target=(%.6f,%.6f)", bestID, bestURI, minDist, bestLat, bestLon)
		} else {
			log.Printf("nearest partner: none (no inquiry_partners rows)")
		}

		// ダミーレスポンス
		stub := json.RawMessage(`{
		  "type":"FeatureCollection",
		  "features":[{
		    "type":"Feature",
		    "geometry":{"type":"Polygon","coordinates":[[[649,938],[769,938],[769,1222],[649,1222],[649,938]]]},
		    "properties":{"id":"R010","name":"Room010","type":"room","area":33861}
		  }]
		}`)
		resp := queryResp{
			RoomID:    "R010",
			FloorMap:  stub,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

/*────────────── main ──────────────*/

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

	log.Printf("HTTP server listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
