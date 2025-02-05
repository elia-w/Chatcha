package main

import (
	"fmt"
	"log"

	"github.com/gorilla/websocket"
)

type User struct {
	id       int
	username string
	pseudo   string
	password string
}

func main() {
	// Connexion au serveur WebSocket
	url := "ws://localhost:8080"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		log.Fatalf("Erreur de connexion au serveur WebSocket : %v", err)
	}
	defer conn.Close()

	// Lire le message d'accueil
	_, message, err := conn.ReadMessage()
	if err != nil {
		log.Fatalf("Erreur lors de la lecture du message : %v", err)
	}
	fmt.Printf("Message du serveur : %s\n", message)

	// Envoi d'un message au serveur
	messageToSend := "Salut serveur !"
	if err := conn.WriteMessage(websocket.TextMessage, []byte(messageToSend)); err != nil {
		log.Fatalf("Erreur lors de l'envoi du message : %v", err)
	}
	fmt.Printf("Message envoyé : %s\n", messageToSend)

	// Attente de la réponse du serveur
	_, response, err := conn.ReadMessage()
	if err != nil {
		log.Fatalf("Erreur lors de la lecture de la réponse : %v", err)
	}
	fmt.Printf("Réponse du serveur : %s\n", response)

	// Envoi d'un message de fermeture au serveur (facultatif, mais recommandé)
	if err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")); err != nil {
		log.Fatalf("Erreur lors de l'envoi du message de fermeture : %v", err)
	}
	fmt.Println("Message de fermeture envoyé.")

	// Fermeture propre de la connexion
	if err := conn.Close(); err != nil {
		log.Fatalf("Erreur lors de la fermeture de la connexion : %v", err)
	}
	fmt.Println("Connexion fermée proprement.")
}
