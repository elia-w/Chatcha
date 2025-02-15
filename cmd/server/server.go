package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/dgrijalva/jwt-go"
	"github.com/go-redis/redis/v8"
	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"
)

var ctx = context.Background()
var rdb *redis.Client
var db *sql.DB
var clients = make(map[*websocket.Conn]bool)
var clientsLock sync.Mutex
var stop = make(chan os.Signal, 1)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

const jwtSecret = "secret_key"

type Message struct {
	IDUser  int    `json:"idUser"`
	IDSalon int    `json:"idSalon"`
	Contenu string `json:"contenu"`
	Date    string `json:"date"`
}

func initDB() {
	var err error
	db, err = sql.Open("sqlite3", "./bdd.db")
	if err != nil {
		log.Fatal("❌ Erreur d'ouverture de la BDD :", err)
	}

	query := `
	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		idUser INTEGER,
		idSalon INTEGER,
		contenu TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (idUser) REFERENCES users(id),
		FOREIGN KEY (idSalon) REFERENCES salons(id)
	);

	CREATE TABLE IF NOT EXISTS salons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		nbUserMax INTEGER,
		nameSalon TEXT
	);

	CREATE TABLE IF NOT EXISTS users(
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE,
		pseudo TEXT,
		password TEXT
	);

	CREATE TABLE IF NOT EXISTS listeUser(
		idSalon INTEGER,
		idUser INTEGER,
		PRIMARY KEY (idSalon, idUser),
		FOREIGN KEY (idUser) REFERENCES users(id),
		FOREIGN KEY (idSalon) REFERENCES salons(id)
	);
	`
	_, err = db.Exec(query)
	if err != nil {
		log.Fatal("❌ Erreur lors de la création des tables :", err)
	}

	insertSampleData()
	fmt.Println("✅ Base de données initialisée.")
}

func insertSampleData() {
	db.Exec("INSERT INTO salons (nbUserMax, nameSalon) VALUES (10, 'Golang Fans'), (20, 'Redis Experts')")
	db.Exec("INSERT INTO users (username, pseudo, password) VALUES ('user1', 'GoMaster', 'pass1'), ('user2', 'RedisKing', 'pass2')")
	db.Exec("INSERT INTO listeUser (idSalon, idUser) VALUES (1, 1), (2, 2)")
}

func initRedis() {
	rdb = redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   0,
	})
	fmt.Println("✅ Connexion à Redis établie.")
}

func validateJWT(tokenString string) (int, error) {
	tokenString = strings.TrimPrefix(tokenString, "Bearer ")
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		return []byte(jwtSecret), nil
	})
	if err != nil || !token.Valid {
		return 0, fmt.Errorf("❌ Token invalide")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return 0, fmt.Errorf("❌ Claims invalides")
	}
	userID := int(claims["user_id"].(float64))
	return userID, nil
}

func handleConnection(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	userID, err := validateJWT(authHeader)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("❌ Erreur WebSocket :", err)
		return
	}
	defer conn.Close()

	clientsLock.Lock()
	clients[conn] = true
	clientsLock.Unlock()

	fmt.Printf("✅ Utilisateur %d connecté.\n", userID)

	for {
		_, p, err := conn.ReadMessage()
		if err != nil {
			clientsLock.Lock()
			delete(clients, conn)
			clientsLock.Unlock()
			fmt.Printf("❌ Utilisateur %d déconnecté.\n", userID)
			return
		}

		var msg Message
		if err := json.Unmarshal(p, &msg); err != nil {
			log.Println("❌ Erreur parsing message :", err)
			continue
		}

		if msg.IDUser != userID {
			log.Println("⚠️ Tentative de spoofing détectée !")
			continue
		}

		messageJSON, _ := json.Marshal(msg)
		rdb.Publish(ctx, fmt.Sprintf("salon_%d", msg.IDSalon), string(messageJSON))
		_, err = db.Exec("INSERT INTO messages (idUser, idSalon, contenu) VALUES (?, ?, ?)", msg.IDUser, msg.IDSalon, msg.Contenu)
		if err != nil {
			log.Println("❌ Erreur insertion message :", err)
		}
	}
}

func shutdownServer() {
	fmt.Println("\n🛑 Arrêt du serveur en cours...")
	clientsLock.Lock()
	for conn := range clients {
		conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "Serveur arrêté"))
		conn.Close()
	}
	clients = make(map[*websocket.Conn]bool)
	clientsLock.Unlock()
	rdb.Close()
	db.Close()
	fmt.Println("✅ Serveur arrêté proprement.")
}

func main() {
	initDB()
	initRedis()

	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	http.HandleFunc("/", handleConnection)

	server := &http.Server{Addr: ":8080", Handler: http.DefaultServeMux}
	go func() {
		fmt.Println("🚀 Serveur WebSocket lancé sur :8080")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("❌ Erreur serveur : %v", err)
		}
	}()

	<-stop
	shutdownServer()
}
