package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/jpanderson91/chirpy/internal/database"
	_ "github.com/lib/pq"
)

type apiConfig struct {
	fileserverHits atomic.Int32
	db             *database.Queries
}

type Chirp struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Body      string    `json:"body"`
	UserID    uuid.UUID `json:"user_id"`
}

type User struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Email     string    `json:"email"`
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        cfg.fileserverHits.Add(1)
        next.ServeHTTP(w, r)
    })
}

func (cfg *apiConfig) numRequests(w http.ResponseWriter, r *http.Request) {
	count := cfg.fileserverHits.Load()
    msg := fmt.Sprintf(`<html>
  <body>
    <h1>Welcome, Chirpy Admin</h1>
    <p>Chirpy has been visited %d times!</p>
  </body>
</html>`, count)
    w.Header().Set("Content-Type", "text/html; charset=utf-8")
    w.Write([]byte(msg))
}

func (cfg *apiConfig) resetHandler(w http.ResponseWriter, r *http.Request) {
	cfg.fileserverHits.Store(0)
	if err := cfg.db.DeleteAllUsers(r.Context()); err != nil {
		log.Print("Error deleting users: ", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handlerReadiness(w http.ResponseWriter, r *http.Request) {
    w.Header().Add("Content-Type", "text/plain; charset=utf-8")
    w.WriteHeader(http.StatusOK)
    w.Write([]byte(http.StatusText(http.StatusOK)))
}

func maskWords(input string, wordsToMask []string) string {
	words := strings.Split(input, " ")
	for i, word := range words {
		for _, wordToMask := range wordsToMask {
			if strings.EqualFold(word, wordToMask) {
				words[i] = "****"
			}
		}
	}
	return strings.Join(words, " ")
}

func (cfg *apiConfig) handlerCreateChirp(w http.ResponseWriter, r *http.Request) {
	var params struct {
		Body   string    `json:"body"`
		UserID uuid.UUID `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		log.Print("Error decoding request body: ", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if len(params.Body) > 140 {
		w.Header().Set("Content-Type", "application/json")
		dat, _ := json.Marshal(map[string]string{"error": "chirp is too long"})
		w.WriteHeader(http.StatusBadRequest)
		w.Write(dat)
		return
	}
	dbChirp, err := cfg.db.CreateChirp(r.Context(), database.CreateChirpParams{
		Body:   maskWords(params.Body, []string{"kerfuffle", "sharbert", "fornax"}),
		UserID: params.UserID,
	})
	if err != nil {
		log.Print("Error creating chirp: ", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	dat, err := json.Marshal(Chirp{
		ID:        dbChirp.ID,
		CreatedAt: dbChirp.CreatedAt,
		UpdatedAt: dbChirp.UpdatedAt,
		Body:      dbChirp.Body,
		UserID:    dbChirp.UserID,
	})
	if err != nil {
		log.Print("Error encoding response body: ", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	w.Write(dat)
}

func (cfg *apiConfig) handlerCreateUser(w http.ResponseWriter, r *http.Request) {
    var params struct {
        Email string `json:"email"`
    }
    if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
        log.Print("Error decoding request body: ", err)
        w.WriteHeader(http.StatusBadRequest)
        return
    }
    dbUser, err := cfg.db.CreateUser(r.Context(), params.Email)
    if err != nil {
        log.Print("Error creating user: ", err)
        w.WriteHeader(http.StatusInternalServerError)
        return
    }
    response := User{
        ID:        dbUser.ID,
        CreatedAt: dbUser.CreatedAt,
        UpdatedAt: dbUser.UpdatedAt,
        Email:     dbUser.Email,
    }
    dat, err := json.Marshal(response)
    if err != nil {
        log.Print("Error encoding response body: ", err)
        w.WriteHeader(http.StatusInternalServerError)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusCreated)
    w.Write(dat)
}

func (cfg *apiConfig) handlerReturnChirp(w http.ResponseWriter, r *http.Request) {
	chirps, err := cfg.db.GetChirps(r.Context())
	if err != nil {
		log.Print("Error getting chirps: ", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	result := make([]Chirp, len(chirps))
	for i, c := range chirps {
		result[i] = Chirp{ID: c.ID, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt, Body: c.Body, UserID: c.UserID}
	}
	dat, err := json.Marshal(result)
	if err != nil {
		log.Print("Error encoding response body: ", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(dat)
}


func main() {
	godotenv.Load()
	dbURL := os.Getenv("DB_URL")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatal(err)
	}

	const filepathRoot = "."
    // Create a new http.ServeMux
    mux := http.NewServeMux()
    
    // Serve static files from current directory (will serve index.html)
    apiCfg := &apiConfig{db: database.New(db)}
    mux.Handle("/app/", apiCfg.middlewareMetricsInc(http.StripPrefix("/app", http.FileServer(http.Dir(filepathRoot)))))
    mux.HandleFunc("GET /api/healthz", handlerReadiness)
    mux.HandleFunc("GET /admin/metrics", apiCfg.numRequests)
    mux.HandleFunc("POST /admin/reset", apiCfg.resetHandler)
    mux.HandleFunc("POST /api/users", apiCfg.handlerCreateUser)
    mux.HandleFunc("POST /api/chirps", apiCfg.handlerCreateChirp)
	mux.HandleFunc("GET /api/chirps", apiCfg.handlerReturnChirp)

    // Create a new http.Server struct
    server := &http.Server{
        Addr:    ":8080",
        Handler: mux,
    }

    log.Printf("Listening on %s", server.Addr)
    // Use the server's ListenAndServe method to start the server
    log.Fatal(server.ListenAndServe())
}

