package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"
)

// Client represents a connected user
type Client struct {
	username string
	conn     net.Conn
	out      chan string // outbound messages
}

// Hub keeps track of active users and broadcasting
type Hub struct {
	mu    sync.RWMutex
	users map[string]*Client // username -> client
}

func NewHub() *Hub {
	return &Hub{users: make(map[string]*Client)}
}

func (h *Hub) addUser(c *Client) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.users[c.username]; exists {
		return fmt.Errorf("username taken")
	}
	h.users[c.username] = c
	return nil
}

func (h *Hub) removeUser(username string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.users, username)
}

func (h *Hub) broadcast(sender, line string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.users {
		if sender != "" && c.username == sender {
			continue
		}
		select {
		case c.out <- line:
		default:
		}
	}
}

func main() {
	var flagPort = flag.Int("port", 4000, "Port to listen on")
	flag.Parse()

	port := *flagPort
	if env := os.Getenv("PORT"); env != "" {
		if p, err := parsePort(env); err == nil {
			port = p
		}
	}

	addr := fmt.Sprintf(":%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", addr, err)
	}
	defer ln.Close()
	log.Printf("chat server listening on %s", addr)

	hub := NewHub()

	// Graceful shutdown
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		<-sig
		log.Println("[SHUTDOWN] server shutting down gracefully...")
		ln.Close()
		os.Exit(0)
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("accept error: %v", err)
			continue
		}
		go handleConn(hub, conn)
	}
}

func handleConn(hub *Hub, conn net.Conn) {
	defer conn.Close()
	log.Printf("[CONNECT] new connection from %s", conn.RemoteAddr())

	reader := bufio.NewScanner(conn)
	buf := make([]byte, 0, 64*1024)
	reader.Buffer(buf, 64*1024)

	if !reader.Scan() {
		return
	}
	line := cleanLine(reader.Text())
	if !strings.HasPrefix(strings.ToUpper(line), "LOGIN ") {
		writeSafe(conn, "ERR expected 'LOGIN <username>'")
		return
	}
	username := strings.TrimSpace(line[len("LOGIN "):])
	if username == "" || strings.Contains(username, " ") {
		writeSafe(conn, "ERR invalid-username")
		return
	}

	client := &Client{
		username: username,
		conn:     conn,
		out:      make(chan string, 32),
	}
	if err := hub.addUser(client); err != nil {
		writeSafe(conn, "ERR username-taken")
		return
	}
	log.Printf("[LOGIN] user=%s", username)

	defer func() {
		hub.removeUser(client.username)
		hub.broadcast("", fmt.Sprintf("INFO %s disconnected", client.username))
		log.Printf("[DISCONNECT] %s disconnected", client.username)
	}()

	writeSafe(conn, "OK")
	done := make(chan struct{})
	go clientWriter(client, done)

	idleTimer := time.NewTimer(60 * time.Second)
	resetTimer := func(d time.Duration) {
		if !idleTimer.Stop() {
			select {
			case <-idleTimer.C:
			default:
			}
		}
		idleTimer.Reset(d)
	}
	go func() {
		<-idleTimer.C
		writeSafe(conn, "INFO disconnected due to inactivity")
		conn.Close()
	}()

	for reader.Scan() {
		line := cleanLine(reader.Text())
		if line == "" {
			continue
		}
		resetTimer(60 * time.Second)

		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "MSG "):
			text := strings.TrimSpace(line[len("MSG "):])
			if text == "" {
				continue
			}
			msg := fmt.Sprintf("MSG %s %s", client.username, text)
			hub.broadcast(client.username, msg)
			log.Printf("[MSG] from=%s text=%q", client.username, text)

		case upper == "WHO":
			hub.mu.RLock()
			for _, c := range hub.users {
				writeSafe(conn, fmt.Sprintf("USER %s", c.username))
			}
			hub.mu.RUnlock()

		case upper == "PING":
			writeSafe(conn, "PONG")

		case strings.HasPrefix(upper, "DM "):
			parts := strings.SplitN(line, " ", 3)
			if len(parts) < 3 {
				writeSafe(conn, "ERR usage: DM <username> <text>")
				continue
			}
			targetName := strings.TrimSpace(parts[1])
			messageText := strings.TrimSpace(parts[2])

			if targetName == "" || messageText == "" {
				writeSafe(conn, "ERR usage: DM <username> <text>")
				continue
			}

			hub.mu.RLock()
			target, ok := hub.users[targetName]
			hub.mu.RUnlock()

			if !ok {
				writeSafe(conn, "ERR user-not-found")
				continue
			}

			// Send the DM only to the target
			target.out <- fmt.Sprintf("DM %s %s", client.username, messageText)

			// Log the DM server-side but don't show it to the sender
			log.Printf("[DM] from=%s to=%s text=%q", client.username, targetName, messageText)

		default:
			writeSafe(conn, "ERR unknown-cmd")
		}
	}
}

func clientWriter(c *Client, done chan struct{}) {
	w := bufio.NewWriter(c.conn)
	for {
		select {
		case line, ok := <-c.out:
			if !ok {
				return
			}
			if !strings.HasSuffix(line, "\n") {
				line += "\n"
			}
			if _, err := w.WriteString(line); err != nil {
				log.Printf("[ERROR] write to %s failed: %v", c.username, err)
				return
			}
			if err := w.Flush(); err != nil {
				log.Printf("[ERROR] flush to %s failed: %v", c.username, err)
				return
			}
		case <-done:
			return
		}
	}
}

func writeSafe(conn net.Conn, msg string) {
	if _, err := fmt.Fprintln(conn, msg); err != nil {
		log.Printf("[ERROR] write failed to %v: %v", conn.RemoteAddr(), err)
	}
}

func cleanLine(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	parts := strings.SplitN(s, " ", 2)
	cmd := strings.TrimSpace(parts[0])
	if len(parts) == 1 {
		return cmd
	}
	rest := strings.TrimSpace(parts[1])
	return cmd + " " + rest
}

func parsePort(s string) (int, error) {
	var p int
	_, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &p)
	if err != nil {
		return 0, err
	}
	if p <= 0 || p > 65535 {
		return 0, fmt.Errorf("invalid port")
	}
	return p, nil
}
