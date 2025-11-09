package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
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

// addUser registers a user if username free. returns error if taken.
func (h *Hub) addUser(c *Client) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.users[c.username]; exists {
		return fmt.Errorf("username taken")
	}
	h.users[c.username] = c
	return nil
}

// removeUser removes a user by username
func (h *Hub) removeUser(username string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.users, username)
}

// broadcast sends a line to all connected clients except optional sender
func (h *Hub) broadcast(sender, line string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, c := range h.users {
		if sender != "" && c.username == sender {
			continue // skip sender so they don't see their own message
		}
		select {
		case c.out <- line:
		default:
			// drop if client's writer is slow
		}
	}
}

func main() {
	var (
		flagPort = flag.Int("port", 4000, "Port to listen on")
	)
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

	reader := bufio.NewScanner(conn)
	// Raise the default token size to handle long messages (up to ~64KB)
	buf := make([]byte, 0, 64*1024)
	reader.Buffer(buf, 64*1024)

	// 1) LOGIN flow
	if !reader.Scan() {
		return
	}
	line := cleanLine(reader.Text())
	if !strings.HasPrefix(strings.ToUpper(line), "LOGIN ") {
		fmt.Fprintln(conn, "ERR expected 'LOGIN <username>'")
		return
	}
	username := strings.TrimSpace(line[len("LOGIN "):])
	if username == "" || strings.Contains(username, " ") {
		fmt.Fprintln(conn, "ERR invalid-username")
		return
	}

	client := &Client{
		username: username,
		conn:     conn,
		out:      make(chan string, 32),
	}
	if err := hub.addUser(client); err != nil {
		fmt.Fprintln(conn, "ERR username-taken")
		return
	}
	defer func() {
		hub.removeUser(client.username)
		hub.broadcast("", fmt.Sprintf("INFO %s disconnected", client.username))
	}()

	// Confirm login
	fmt.Fprintln(conn, "OK")

	// Start writer goroutine so broadcasts don't block reader
	done := make(chan struct{})
	go clientWriter(client, done)

	// 2) Read commands from this client
	for reader.Scan() {
		line := cleanLine(reader.Text())
		if line == "" {
			continue
		}
		upper := strings.ToUpper(line)

		switch {
		case strings.HasPrefix(upper, "MSG "):
			// broadcast message
			text := strings.TrimSpace(line[len("MSG "):])
			if text == "" {
				continue
			}
			msg := fmt.Sprintf("MSG %s %s", client.username, text)
			hub.broadcast(client.username, msg)

		case upper == "WHO":
			hub.mu.RLock()
			for _, c := range hub.users {
				fmt.Fprintf(conn, "USER %s\n", c.username)
			}
			hub.mu.RUnlock()

		case upper == "PING":
			fmt.Fprintln(conn, "PONG")

		case strings.HasPrefix(upper, "DM "):
			// Split into "DM", "<target>", "<text>"
			parts := strings.SplitN(line, " ", 3)
			if len(parts) < 3 {
				fmt.Fprintln(conn, "ERR usage: DM <username> <text>")
				continue
			}
			targetName := strings.TrimSpace(parts[1])
			messageText := strings.TrimSpace(parts[2])

			if targetName == "" || messageText == "" {
				fmt.Fprintln(conn, "ERR usage: DM <username> <text>")
				continue
			}

			hub.mu.RLock()
			target, ok := hub.users[targetName]
			hub.mu.RUnlock()

			if !ok {
				fmt.Fprintln(conn, "ERR user-not-found")
				continue
			}

			// Send the DM only to the target
			target.out <- fmt.Sprintf("DM %s %s", client.username, messageText)

			// (Optional) echo confirmation back to sender
			fmt.Fprintf(conn, "DM to %s: %s\n", targetName, messageText)

		default:
			// unknown command
			fmt.Fprintln(conn, "ERR unknown-cmd")
		}
	}
	// On scanner error or EOF, return -> deferred cleanup runs
}

func clientWriter(c *Client, done chan struct{}) {
	w := bufio.NewWriter(c.conn)
	for {
		select {
		case line, ok := <-c.out:
			if !ok {
				return
			}
			// Each broadcast already a full line; ensure newline
			if !strings.HasSuffix(line, "\n") {
				line += "\n"
			}
			if _, err := w.WriteString(line); err != nil {
				return
			}
			if err := w.Flush(); err != nil {
				return
			}
		case <-done:
			return
		}
	}
}

// cleanLine normalizes whitespace: trims and collapses multiple spaces to singles around command tokens.
func cleanLine(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.TrimSpace(s)
	// Don't collapse spaces inside message text; only normalize leading command spacing
	// Split once by first space to separate command and rest
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
