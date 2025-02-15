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
var stopChan = make(chan struct{}) // Canal global pour arrêter proprement
var wg sync.WaitGroup              // Attendre la fin des goroutines

const jwtSecret = "secret_key" // ⚠️ Change cette clé en production !

type Message struct {
	IDUser  int    `json:"idUser"`
	IDSalon int    `json:"idSalon"`
	Contenu string `json:"contenu"`
	Date    string `json:"date"`
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

// Se connecter au WebSocket
func connectWebSocket(userID int) (*websocket.Conn, error) {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+generateJWT(userID))

	conn, _, err := websocket.DefaultDialer.Dial("ws://localhost:8080", headers)
	if err != nil {
		return nil, fmt.Errorf("Erreur connexion WebSocket : %v", err)
	}

	clientsLock.Lock()
	clients = append(clients, conn) // Stocker la connexion pour une fermeture propre
	clientsLock.Unlock()

	return conn, nil
}

// Écouter les messages de Redis
func listenMessages(userID int) {
	defer wg.Done()

	pubsub := rdb.Subscribe(ctx, "salon_1")
	defer pubsub.Close()

	ch := pubsub.Channel() // ✅ Utiliser le canal au lieu de `ReceiveMessage()`

	for {
		select {
		case <-stopChan: // Arrêt demandé
			fmt.Printf("🛑 Utilisateur %d arrête l'écoute de Redis.\n", userID)
			return
		case msg, ok := <-ch:
			if !ok {
				fmt.Printf("🛑 Canal Redis fermé pour utilisateur %d.\n", userID)
				return
			}

			var receivedMessage Message
			if err := json.Unmarshal([]byte(msg.Payload), &receivedMessage); err != nil {
				log.Println("❌ Erreur parsing message Redis :", err)
				continue
			}

			if receivedMessage.IDUser != userID {
				fmt.Printf("📩 Utilisateur %d a reçu : %s\n", userID, receivedMessage.Contenu)
			}
		}
	}
}

// Simuler un client
func startClient(userID int) {
	defer wg.Done()

	conn, err := connectWebSocket(userID)
	if err != nil {
		log.Printf("❌ Utilisateur %d impossible de se connecter : %v\n", userID, err)
		return
	}
	defer conn.Close()

	fmt.Printf("✅ Utilisateur %d connecté.\n", userID)

	wg.Add(1)
	go listenMessages(userID)

	// Simulation de messages envoyés
	nbMessages := rand.Intn(10) + 1
	for i := 0; i < nbMessages; i++ {
		select {
		case <-stopChan:
			fmt.Printf("🛑 Utilisateur %d arrête l'envoi de messages.\n", userID)
			return
		default:
			message := Message{
				IDUser:  userID,
				IDSalon: 1,
				Contenu: fmt.Sprintf("Message %d de l'utilisateur %d", i+1, userID),
				Date:    time.Now().Format("2006-01-02 15:04:05"),
			}

			messageJSON, _ := json.Marshal(message)
			if err := conn.WriteMessage(websocket.TextMessage, messageJSON); err != nil {
				log.Printf("❌ Utilisateur %d erreur envoi message : %v\n", userID, err)
				return
			}
			fmt.Printf("📤 Utilisateur %d a envoyé : %s\n", userID, message.Contenu)

			time.Sleep(time.Duration(rand.Intn(2000)+500) * time.Millisecond)
		}
	}

	fmt.Printf("👋 Utilisateur %d se déconnecte.\n", userID)
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

	// Capture Ctrl+C pour arrêter proprement
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)

	// Démarrer les clients dans une goroutine
	go func() {
		for {
			select {
			case <-stopChan:
				return
			default:
				wg.Add(1)
				go startClient(clientID)
				clientID++
				time.Sleep(time.Duration(rand.Intn(3)) * time.Second)
			}
		}
	}()

	// Attendre Ctrl+C
	<-sigChan
	fmt.Println("\n🛑 Arrêt du client en cours...")

	// Fermer stopChan pour signaler l'arrêt aux goroutines
	close(stopChan)

	// ✅ Attendre avec timeout pour éviter blocage
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

	// Fermer proprement les WebSockets
	shutdownClients()

	// Fermer Redis proprement
	if err := rdb.Close(); err != nil {
		log.Println("❌ Erreur lors de la fermeture de Redis :", err)
	} else {
		fmt.Println("✅ Connexion Redis fermée.")
	}

	fmt.Println("👋 Client arrêté proprement.")
}
