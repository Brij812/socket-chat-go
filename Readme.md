# SocketChat-Go – Concurrent TCP Chat Server (Go)

SocketChat-Go is a lightweight, concurrent TCP chat server built purely in Go using the standard library.
It enables multiple users to connect, log in with a username, and chat with each other in real time over TCP.
The project demonstrates clean concurrency patterns, robust user management, and socket-based communication.

---

## Table of Contents

1. Overview  
2. Architecture  
3. Features  
4. Tech Stack  
5. Folder Structure  
6. Connection Flow  
7. Message Commands  
8. Timeout & Heartbeat  
9. Running Locally  
10. Example Interaction  
11. Logging     

---

## 1. Overview

SocketChat-Go provides a real-time TCP chat environment where clients connect directly to the server via sockets.
Each user logs in with a unique username and can send messages, direct messages, or check active users.

The system supports at least 5–10 concurrent clients, handles disconnects gracefully, and includes idle timeouts and heartbeat responses.

---

## 2. Architecture

The system follows a simple but scalable architecture:

- **main.go** — entry point that sets up TCP listener and accepts clients  
- **Hub struct** — manages all connected users and broadcasts messages  
- **Client struct** — encapsulates each connection and outbound channel  
- **handleConn** — handles login, message parsing, and dispatch  
- **clientWriter** — asynchronous writer for each client  
- **Utility functions** — normalize inputs, handle errors safely, and manage timers  

Key design principles:

- Non-blocking broadcast using buffered channels  
- Mutex-protected global state (`Hub`)  
- Graceful cleanup on disconnect  
- Safe idle timeout handling with proper timer reset pattern  
- Structured logs for easy debugging

---

## 3. Features

**Completed:**

- Concurrent connection handling with goroutines  
- LOGIN command for unique usernames  
- MSG command for public messages  
- DM command for private messages  
- WHO command to list all active users  
- PING → PONG heartbeat response  
- Idle timeout (auto-disconnect after 60s inactivity)  
- Graceful shutdown via Ctrl+C  
- Structured logging and error-safe writes

**Pending (Future Enhancements):**

- Room-based chat support  
- Encrypted connections (TLS)  
- Authentication via tokens or password system  
- Message persistence (to files or DB)  
- WebSocket adapter for browser clients

---

## 4. Tech Stack

- **Language:** Go 1.22+  
- **Networking:** net + bufio (standard library only)  
- **Concurrency:** Goroutines + Channels  
- **Logging:** log package  
- **CLI Support:** Flags & environment variables  

---

## 5. Folder Structure

```
.
├── main.go
├── README.txt
```

---

## 6. Connection Flow

1. Server listens on port 4000 (or from ENV/flag).  
2. Each client connects via TCP (e.g., using ncat or telnet).  
3. On connection, user must log in using:  
   `LOGIN <username>`  
4. If username is unique → server replies `OK`, else `ERR username-taken`.  
5. Once logged in, users can send messages or commands.  
6. On disconnect, all users are notified with:  
   `INFO <username> disconnected`.

---

## 7. Message Commands

| Command | Description | Example |
|----------|-------------|----------|
| LOGIN <username> | Log in with unique name | LOGIN Naman |
| MSG <text> | Send public message | MSG Hello everyone! |
| DM <username> <text> | Send private message | DM Yudi hey there! |
| WHO | List active users | WHO |
| PING | Heartbeat | PING → PONG |

---

## 8. Timeout & Heartbeat

- **Idle Timeout:** Users are disconnected after 60s of inactivity.  
  The server sends:  
  `INFO disconnected due to inactivity`
- **Heartbeat:**  
  Clients can periodically send `PING`, and the server responds `PONG`.

---

## 9. Running Locally

### Prerequisites:
- Go 1.22+ installed  
- ncat or telnet for client connections  

### Start Server:
```
go run .
```

By default, the server runs on **localhost:4000**.

### Connect Clients:
In two separate terminals:
```
ncat localhost 4000
LOGIN Naman
MSG hi everyone!
```
```
ncat localhost 4000
LOGIN Yudi
DM Naman hey this is private!
```

---

## 10. Example Interaction

**Client 1 (Naman):**
```
LOGIN Naman
OK
MSG hi everyone!
MSG Yudi hello Naman!
DM Yudi hey this is private!
```

**Client 2 (Yudi):**
```
LOGIN Yudi
OK
MSG hello Naman!
```

**Server Output:**
```
[CONNECT] new connection from 127.0.0.1:55342
[LOGIN] user=Naman
[MSG] from=Naman text="hi everyone!"
[DM] from=Yudi to=Naman text="hey this is private!"
[DISCONNECT] Naman disconnected
```

---

## 11. Logging

All major actions are logged in the server terminal with structured context:
- `[CONNECT]`, `[LOGIN]`, `[MSG]`, `[DM]`, `[DISCONNECT]`, `[ERROR]`  
This helps debug or audit chat interactions easily.

---




