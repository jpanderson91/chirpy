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

type parameters struct {
    Body string `json:"body"`
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
	// reset the counter 
    cfg.fileserverHits.Store(0)
} 

func handlerReadiness(w http.ResponseWriter, r *http.Request) {
    w.Header().Add("Content-Type", "text/plain; charset=utf-8")
    w.WriteHeader(http.StatusOK)
    w.Write([]byte(http.StatusText(http.StatusOK)))
}

func maskWords(input string, wordsToMask []string) string {
	// 1. split the input into pieces somehow
    words := strings.Split(input, " ")
	// 2. loop over the pieces
    for i, word := range words {
        // 3. for each piece, check if it matches (case-insensitively) any word in wordsToMask
        for _, wordToMask := range wordsToMask {
            if strings.EqualFold(word, wordToMask) {
                // 4. if it matches, replace it
                words[i] = "****"
            }
        }
    }	
	// 5. join everything back together
    input = strings.Join(words, " ")
	// 6. return the result
	return input
}

func validateChirp(w http.ResponseWriter, r *http.Request) {
    // decode the JSON request body into a parameters struct
    decoder := json.NewDecoder(r.Body)
    params := parameters{}
    err := decoder.Decode(&params)
    if err != nil {
        log.Print("Error decoding request body: ", err)
        w.WriteHeader(http.StatusBadRequest)
        return
    }
    // reject chirps that exceed the 140 character limit
    if len(params.Body) > 140 {
        w.Header().Set("Content-Type", "application/json")
        dat, err := json.Marshal(map[string]string{"error": "chirp is too long"})
        if err != nil {
            log.Print("Error encoding response body: ", err)
            w.WriteHeader(http.StatusInternalServerError)
            return
        }
        w.WriteHeader(http.StatusBadRequest)
        w.Write(dat)
        return
    }  
    // call maskWords function and pass in result as cleaned_body
    cleaned_body := maskWords(params.Body, []string{"kerfuffle", "sharbert", "fornax"})
    dat, err := json.Marshal(map[string]any{"cleaned_body": cleaned_body})
    if err != nil {
        log.Print("Error encoding response body: ", err)
        w.WriteHeader(http.StatusInternalServerError)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
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
    mux.HandleFunc("POST /api/validate_chirp", validateChirp)
    mux.HandleFunc("POST /api/users", apiCfg.handlerCreateUser)
    mux.HandleFunc("POST /api/chirps", )

    // Create a new http.Server struct
    server := &http.Server{
        Addr:    ":8080",
        Handler: mux,
    }

    log.Printf("Listening on %s", server.Addr)
    // Use the server's ListenAndServe method to start the server
    log.Fatal(server.ListenAndServe())
}

