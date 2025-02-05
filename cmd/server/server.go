package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"
)

type Salon struct {
	id        int
	nomSalon  string
	nbMaxUser int
}

type ListeUser struct {
	idSalon int
	idUser  int
}

type Message struct {
	id      int
	idUser  int
	idSalon int
	contenu string
	date    string
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var db *sql.DB

func handleConnection(w http.ResponseWriter, r *http.Request) {
	// Upgrade la connexion HTTP en WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Erreur lors de l'upgrade de la connexion : ", err)
		return
	}
	defer conn.Close()

	// Envoie un message d'accueil
	if err := conn.WriteMessage(websocket.TextMessage, []byte("Bienvenue sur le serveur de chat !")); err != nil {
		log.Println("Erreur lors de l'envoi du message : ", err)
		return
	}

	// Gère les messages entrants
	for {
		messageType, p, err := conn.ReadMessage()
		if err != nil {
			log.Println("Erreur lors de la lecture du message : ", err)
			return
		} else {
			insertMessage(string(p))
		}

		// Affiche le message et renvoie-le au client
		fmt.Printf("Message reçu : %s\n", p)

		if err := conn.WriteMessage(messageType, p); err != nil {
			log.Println("Erreur lors de l'envoi du message : ", err)
			return
		}
	}
}

func initDB() {
	var err error
	// Ouvrir ou créer la base de données
	db, err = sql.Open("sqlite3", "./bdd.db")
	if err != nil {
		log.Fatal("Erreur lors de l'ouverture de la base de données : ", err)
	}

	// Créer une table pour stocker les messages si elle n'existe pas déjà
	query := `
        CREATE TABLE IF NOT EXISTS messages (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            message TEXT,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        );
    `
	_, err = db.Exec(query)
	if err != nil {
		log.Fatal("Erreur lors de la création de la table : ", err)
	}
}

// Fonction pour insérer un message dans la base de données
func insertMessage(message string) {
	query := "INSERT INTO messages (message) VALUES (?)"
	_, err := db.Exec(query, message)
	if err != nil {
		log.Println("Erreur lors de l'insertion du message : ", err)
	}
}

func main() {
	initDB()
	defer db.Close()

	http.HandleFunc("/", handleConnection)
	fmt.Println("Serveur démarré sur :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
