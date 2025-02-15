# Chatcha
Patrice CHEN &amp; Elia Wu - ESIEE 2024/2025


## Description  
Chatcha est une application de chat temps réel avec support multi-nœuds.

## Fonctionnalités

- Système de messagerie instantanée avec plusieurs salles de discussion.
- Persistance des messages pour l’historique.
- Notification push pour les nouveaux messages

## Outils utilisés  

### 1. Redis  
Redis est utilisé pour la gestion des sessions et le stockage temporaire des messages pour assurer un échange rapide entre les utilisateurs.  

#### Installation et lancement  
```sh
sudo apt update  
sudo apt install redis-server  
sudo systemctl start redis  
sudo systemctl enable redis  
```
#### Vérification de l’installation  
```sh
redis-cli ping  
```
Si tout fonctionne correctement, la commande renverra **"PONG"**.  

### 2. JWT (JSON Web Token)  
JWT est utilisé pour sécuriser l’authentification et la gestion des sessions utilisateur. Il permet de vérifier l’identité des utilisateurs sans stocker de session côté serveur.  

### 3. SQLite3  
SQLite3 est utilisé comme base de données légère pour stocker les utilisateurs, les messages et l’historique des conversations.  

#### Installation  
```sh
sudo apt install sqlite3  
```

### 4. WebSocket  
WebSocket est utilisé pour permettre la communication en temps réel entre les clients. Il est essentiel pour assurer une messagerie instantanée fluide et réactive.  

## Lancement du projet  

1. Démarrer Redis  
```sh
sudo systemctl start redis  
```
2. Lancer le server
```sh
go run ./server/server.go
```
3. Lancer les clients
```sh
go run ./clients/clients.go
```