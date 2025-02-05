package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"

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
	idUser  int    `json:"id"`
	idSalon int    `json:"idSalon"`
	contenu string `json:"contenu"`
	date    string `json:"date"`	
}

type User struct {
	id       int
	username string
	pseudo   string
	password string
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
			// Vérifie si l'erreur est liée à la fermeture normale de la connexion
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				log.Println("La connexion a été fermée proprement.")
			} else {
				log.Println("Erreur lors de la lecture du message : ", err)
			}
			return
		} 
		//else {
		// var msg Message
			// err := json.Unmarshal(p, &msg)
			// if err != nil {
			// 	insertMessage((p.contenu))
			// }
		//}

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
            username TEXT,
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
		log.Fatal("Erreur lors de la création de la table : ", err)
	}
}

// Fonction pour insérer un message dans la base de données
func insertMessage(message Message) {
	query := "INSERT INTO messages (idUser,idSalon,contenu) VALUES (?,?,?)"
	_, err := db.Exec(query, message.idUser, message.idSalon, message.contenu)
	if err != nil {
		log.Println("Erreur lors de l'insertion du message : ", err)
	}
}

func createUser(i int) {
	query := "INSERT INTO users (username,pseudo,password) VALUES(?,?,?)"
	_, err := db.Exec(query, "user"+strconv.Itoa(i), "pseudo"+strconv.Itoa(i), "password"+strconv.Itoa(i))
	if err != nil {
		log.Println("Erreur lors de la création du user")
	}
}

func createSalon(i int) {
	query := "INSERT INTO salons (nameSalon, nbUserMax) VALUES(?,?)"
	_, err := db.Exec(query, "Salon"+strconv.Itoa(i), 100)
	if err != nil {
		log.Println("Erreur lors de la création du salon")
	}
}

func main() {
	initDB()

	i := 0
	for i < 10 {
		createUser(i)
		i++
	}

	j := 0
	for j < 2 {
		createSalon(j)
		j++
	}

	defer db.Close()

	http.HandleFunc("/", handleConnection)
	fmt.Println("Serveur démarré sur :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
