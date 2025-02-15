package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/go-redis/redis/v8"
	"github.com/gorilla/websocket"
)

var ctx = context.Background()
var rdb *redis.Client
var clients []*websocket.Conn
var clientsLock sync.Mutex
var stopChan = make(chan struct{})
var wg sync.WaitGroup

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

var initialData struct {
	IDUser  int `json:"idUser"`
	IDSalon int `json:"idSalon"`
}

// Générer un token JWT
func generateJWT(userID int) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": userID,
		"exp":     time.Now().Add(time.Hour * 24).Unix(),
	})
	tokenString, _ := token.SignedString([]byte(jwtSecret))
	return tokenString
}

// Initialiser Redis
func initRedis() {
	rdb = redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   0,
	})
}

// Se connecter au WebSocket avec tentative de reconnexion
func connectWebSocket(user User) (*websocket.Conn, error) {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+generateJWT(user.ID))

	var conn *websocket.Conn
	var err error
	for i := 0; i < 3; i++ {
		conn, _, err = websocket.DefaultDialer.Dial("ws://localhost:8080", headers)
		if err == nil {
			break
		}
		log.Printf("🔄 Tentative de reconnexion (%d/3) pour utilisateur %s...\n", i+1, user.Pseudo)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		return nil, fmt.Errorf("❌ Erreur connexion WebSocket : %v", err)
	}

	clientsLock.Lock()
	clients = append(clients, conn)
	clientsLock.Unlock()

	return conn, nil
}

func listenMessages(user User, salonID int) {
	defer wg.Done()

	salonChannel := fmt.Sprintf("salon_%d", salonID) // 🔹 Écoute uniquement son salon
	pubsub := rdb.Subscribe(ctx, salonChannel)
	defer pubsub.Close()

	ch := pubsub.Channel()
	for {
		select {
		case <-stopChan:
			fmt.Printf("🛑 Utilisateur %s arrête l'écoute de Redis.\n", user.Pseudo)
			return
		case msg, ok := <-ch:
			if !ok {
				fmt.Printf("🛑 Canal Redis fermé pour utilisateur %s.\n", user.Pseudo)
				return
			}

			var receivedMessage Message
			if err := json.Unmarshal([]byte(msg.Payload), &receivedMessage); err != nil {
				log.Println("❌ Erreur parsing message Redis :", err)
				continue
			}

			if receivedMessage.IDUser != user.ID {
				fmt.Printf("📩 Utilisateur %s a reçu dans salon %d : %s\n", user.Pseudo, salonID, receivedMessage.Contenu)
			}
		}
	}
}

func startClient(user User) {
	defer wg.Done()

	conn, err := connectWebSocket(user)
	if err != nil {
		log.Printf("❌ Utilisateur %s impossible de se connecter : %v\n", user.Pseudo, err)
		return
	}
	defer conn.Close()

	// 🔹 Envoi des informations d'inscription au serveur
	userData := map[string]string{
		"username": user.Username,
		"pseudo":   user.Pseudo,
		"password": user.Password,
	}
	userDataJSON, _ := json.Marshal(userData)
	if err := conn.WriteMessage(websocket.TextMessage, userDataJSON); err != nil {
		log.Printf("❌ Utilisateur %s erreur envoi credentials : %v\n", user.Pseudo, err)
		return
	}

	// 🔹 Réception de l'ID utilisateur et du salon assigné
	_, message, err := conn.ReadMessage()
	if err != nil {
		log.Printf("❌ Utilisateur %s erreur réception message : %v\n", user.Pseudo, err)
		return
	}

	if err := json.Unmarshal(message, &initialData); err != nil {
		log.Printf("❌ Utilisateur %s erreur parsing message initial : %v\n", user.Pseudo, err)
		return
	}

	user.ID = initialData.IDUser
	salonID := initialData.IDSalon

	fmt.Printf("✅ Utilisateur %s connecté avec ID %d dans le salon %d.\n", user.Pseudo, user.ID, salonID)

	wg.Add(1)
	go listenMessages(user, salonID) // Écoute uniquement son salon

	// 🔹 Simulation d'envoi de messages
	nbMessages := rand.Intn(10) + 1
	for i := 0; i < nbMessages; i++ {
		select {
		case <-stopChan:
			fmt.Printf("🛑 Utilisateur %s arrête l'envoi de messages.\n", user.Pseudo)
			return
		default:
			message := Message{
				IDUser:  user.ID,
				IDSalon: salonID, // 🔹 Envoi dans son salon
				Contenu: fmt.Sprintf("Message %d de l'utilisateur %s", i+1, user.Pseudo),
				Date:    time.Now().Format("2006-01-02 15:04:05"),
			}

			messageJSON, _ := json.Marshal(message)
			if err := conn.WriteMessage(websocket.TextMessage, messageJSON); err != nil {
				log.Printf("❌ Utilisateur %s erreur envoi message : %v\n", user.Pseudo, err)
				return
			}
			fmt.Printf("📤 Utilisateur %s a envoyé : %s\n", user.Pseudo, message.Contenu)

			time.Sleep(time.Duration(rand.Intn(2000)+500) * time.Millisecond)
		}
	}

	fmt.Printf("👋 Utilisateur %s se déconnecte.\n", user.Pseudo)
	conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "Bye !"))
}

// Gérer l'arrêt propre des clients
func shutdownClients() {
	fmt.Println("\n🛑 Arrêt en cours... Déconnexion des clients...")

	clientsLock.Lock()
	for _, conn := range clients {
		conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "Serveur arrêté"))
		conn.Close()
	}
	clients = nil
	clientsLock.Unlock()

	fmt.Println("✅ Tous les clients ont été déconnectés proprement.")
}

func main() {
	initRedis()
	rand.Seed(time.Now().UnixNano())

	clientID := 1

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)

	go func() {
		for {
			select {
			case <-stopChan:
				return
			default:
				wg.Add(1)
				user := User{
					ID:       0,
					Username: fmt.Sprintf("username%d", clientID),
					Pseudo:   fmt.Sprintf("pseudo%d", clientID),
					Password: fmt.Sprintf("password%d", clientID),
				}
				go startClient(user)
				clientID++
				time.Sleep(time.Duration(rand.Intn(3)) * time.Second)
			}
		}
	}()

	<-sigChan
	fmt.Println("\n🛑 Arrêt du client en cours...")

	close(stopChan)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		fmt.Println("✅ Toutes les goroutines se sont terminées.")
	case <-time.After(5 * time.Second):
		fmt.Println("⚠️ Temps d'arrêt dépassé, certaines goroutines n'ont pas terminé.")
	}

	shutdownClients()

	if err := rdb.Close(); err != nil {
		log.Println("❌ Erreur lors de la fermeture de Redis :", err)
	} else {
		fmt.Println("✅ Connexion Redis fermée.")
	}

	fmt.Println("👋 Client arrêté proprement.")
}
