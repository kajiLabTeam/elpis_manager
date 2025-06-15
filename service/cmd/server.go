package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	_ "github.com/lib/pq"
	"github.com/rs/zerolog/log"
)

type RegisterRequest struct {
	ManagementServerURL string             `json:"management_server_url"`
	ProxyServerURL      string             `json:"proxy_server_url"`
	Mapping             []RoomMappingInput `json:"mapping"`
}

type RoomMappingInput struct {
	Floor    string `json:"floor"`
	RoomID   string `json:"room_id"`
	RoomName string `json:"room_name"`
}

type RegisterResponse struct {
	Message string `json:"message"`
}

func handleRegister(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		ctx := r.Context()
		var req RegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Ctx(ctx).Error().Err(err).Msg("failed to decode register request")
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			log.Ctx(ctx).Error().Err(err).Msg("failed to begin tx")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()

		// 1) registrations テーブルに挿入
		var regID int
		err = tx.QueryRowContext(ctx, `
            INSERT INTO registrations (management_server_url, proxy_server_url, created_at)
            VALUES ($1, $2, $3)
            RETURNING id
        `, req.ManagementServerURL, req.ProxyServerURL, time.Now()).Scan(&regID)
		if err != nil {
			log.Ctx(ctx).Error().Err(err).Msg("failed to insert registration")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		// 2) room_mappings テーブルに複数レコードを挿入
		stmt, err := tx.PrepareContext(ctx, `
            INSERT INTO room_mappings (registration_id, floor, room_id, room_name)
            VALUES ($1, $2, $3, $4)
        `)
		if err != nil {
			log.Ctx(ctx).Error().Err(err).Msg("failed to prepare mapping insert")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		defer stmt.Close()

		for _, m := range req.Mapping {
			if _, err := stmt.ExecContext(ctx, regID, m.Floor, m.RoomID, m.RoomName); err != nil {
				log.Ctx(ctx).Error().
					Int("regID", regID).
					Str("floor", m.Floor).
					Str("room_id", m.RoomID).
					Str("room_name", m.RoomName).
					Err(err).
					Msg("failed to insert room mapping")
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
		}

		if err := tx.Commit(); err != nil {
			log.Ctx(ctx).Error().Err(err).Msg("failed to commit tx")
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		resp := RegisterResponse{Message: "Registration successful"}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Ctx(ctx).Error().Err(err).Msg("failed to write response")
		}
	}
}

func main() {
	dsn := "postgres://myuser:mypassword@postgres_service:5432/servicedb?sslmode=disable"
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to open db")
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatal().Err(err).Msg("failed to ping db")
	}
	log.Info().Msg("DB connection succeeded")

	mux := http.NewServeMux()
	mux.Handle("/api/register", handleRegister(db))

	log.Info().Msg("starting service on :8012")
	if err := http.ListenAndServe(":8012", mux); err != nil {
		log.Fatal().Err(err).Msg("server error")
	}
}
