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

type User struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Pseudo   string `json:"pseudo"`
	Password string `json:"password"`
}

type InitialData struct {
	IDUser  int `json:"idUser"`
	IDSalon int `json:"idSalon"`
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
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS salons (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		nbUser INTEGER,
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
		PRIMARY KEY (idSalon, idUser)
	);

	INSERT OR IGNORE INTO salons (id, nbUser, nameSalon) VALUES (1, 0, 'salon_1');
	`
	_, err = db.Exec(query)
	if err != nil {
		log.Fatal("❌ Erreur lors de la création des tables :", err)
	}

	fmt.Println("✅ Base de données initialisée avec un salon par défaut.")
}

func initRedis() {
	rdb = redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   0,
	})
	fmt.Println("✅ Connexion à Redis établie.")
}

func validateJWT(authHeader string) (User, error) {
	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		return []byte(jwtSecret), nil
	})

	if err != nil {
		return User{}, err
	}

	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		userID := int(claims["user_id"].(float64))
		return User{ID: userID}, nil
	}
	return User{}, fmt.Errorf("invalid token")
}

func handleConnection(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("❌ Erreur WebSocket :", err)
		return
	}
	defer conn.Close()

	// 🔹 Lecture des credentials envoyés par le client
	_, p, err := conn.ReadMessage()
	if err != nil {
		log.Println("❌ Erreur réception credentials :", err)
		return
	}

	var userData map[string]string
	if err := json.Unmarshal(p, &userData); err != nil {
		log.Println("❌ Erreur parsing credentials :", err)
		return
	}

	username := userData["username"]
	pseudo := userData["pseudo"]
	password := userData["password"]
	var userID, salonID, nbUser int

	// 🔹 Vérification si l'utilisateur existe déjà
	err = db.QueryRow("SELECT id, (SELECT idSalon FROM listeUser WHERE idUser = users.id LIMIT 1) FROM users WHERE username = ?", username).Scan(&userID, &salonID)
	if err == sql.ErrNoRows {
		// 🔹 L'utilisateur n'existe pas, on le crée
		result, err := db.Exec("INSERT INTO users (username, pseudo, password) VALUES (?, ?, ?)", username, pseudo, password)
		if err != nil {
			log.Println("❌ Erreur insertion utilisateur :", err)
			return
		}
		userID64, _ := result.LastInsertId()
		userID = int(userID64)

		// 🔹 Recherche du dernier salon avec moins de 10 utilisateurs
		err = db.QueryRow("SELECT id, nbUser FROM salons ORDER BY id DESC LIMIT 1").Scan(&salonID, &nbUser)
		if err == sql.ErrNoRows || nbUser >= 10 {
			// 🔹 Aucun salon existant ou le dernier salon est plein, on en crée un nouveau
			result, err := db.Exec("INSERT INTO salons (nbUser, nameSalon) VALUES (?, ?)", 0, fmt.Sprintf("Salon_%d", salonID+1))
			if err != nil {
				log.Println("❌ Erreur création nouveau salon :", err)
				return
			}
			salonID64, _ := result.LastInsertId()
			salonID = int(salonID64)
		}

		// 🔹 Ajout de l'utilisateur au salon
		_, err = db.Exec("INSERT INTO listeUser (idSalon, idUser) VALUES (?, ?)", salonID, userID)
		if err != nil {
			log.Println("❌ Erreur ajout utilisateur dans salon :", err)
			return
		}

		// 🔹 Mise à jour du nombre d'utilisateurs dans le salon
		db.Exec("UPDATE salons SET nbUser = nbUser + 1 WHERE id = ?", salonID)
	} else {
		// 🔹 Si l'utilisateur existait déjà, on vérifie bien son salon
		err = db.QueryRow("SELECT idSalon FROM listeUser WHERE idUser = ?", userID).Scan(&salonID)
		if err != nil {
			log.Println("❌ Erreur récupération du salon de l'utilisateur :", err)
			return
		}
		fmt.Printf("🔄 Utilisateur %s déjà existant (ID: %d), réassigné au salon %d\n", username, userID, salonID)
	}

	// 🔹 Envoi des informations au client
	initialData := InitialData{IDUser: userID, IDSalon: salonID}
	initialDataJSON, _ := json.Marshal(initialData)
	conn.WriteMessage(websocket.TextMessage, initialDataJSON)

	fmt.Printf("✅ Utilisateur %s connecté avec ID %d dans le salon %d.\n", username, userID, salonID)

	// 🔹 Écoute des messages WebSocket
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
