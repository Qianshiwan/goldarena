package websocket

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type Client struct {
	ID       int64
	Hub      *Hub
	Conn     *websocket.Conn
	Send     chan []byte
	Subs     map[string]bool // subscribed channels: "quote", "kline:1m", ...
	mu       sync.RWMutex
}

func (c *Client) ReadPump() {
	defer func() {
		c.Hub.Unregister <- c
		c.Conn.Close()
	}()
	for {
		_, msg, err := c.Conn.ReadMessage()
		if err != nil {
			break
		}
		c.handleMessage(msg)
	}
}

func (c *Client) WritePump() {
	defer c.Conn.Close()
	for msg := range c.Send {
		if err := c.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			break
		}
	}
}

type wsMsg struct {
	Action  string `json:"action"`  // subscribe | unsubscribe | ping
	Channel string `json:"channel"` // quote | kline
	Symbol  string `json:"symbol"`  // e.g. XAU
	Period  string `json:"period"`  // 1m, 5m, 1h, 1d
}

func (c *Client) handleMessage(data []byte) {
	var msg wsMsg
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}
	switch msg.Action {
	case "subscribe":
		ch := msg.Channel
		if msg.Period != "" {
			ch = msg.Channel + ":" + msg.Symbol + ":" + msg.Period
		} else {
			ch = msg.Channel + ":" + msg.Symbol
		}
		c.mu.Lock()
		c.Subs[ch] = true
		c.mu.Unlock()
		// Register this client into the hub's channel routing table
		// (critical: without this, BroadcastToChannel never delivers)
		h := c.Hub
		h.mu.Lock()
		if h.Channels[ch] == nil {
			h.Channels[ch] = make(map[*Client]bool)
		}
		h.Channels[ch][c] = true
		h.mu.Unlock()
	case "unsubscribe":
		ch := msg.Channel
		if msg.Period != "" {
			ch = msg.Channel + ":" + msg.Symbol + ":" + msg.Period
		} else {
			ch = msg.Channel + ":" + msg.Symbol
		}
		c.mu.Lock()
		delete(c.Subs, ch)
		c.mu.Unlock()
		// Remove from hub channel routing table
		h := c.Hub
		h.mu.Lock()
		if subs, ok := h.Channels[ch]; ok {
			delete(subs, c)
			if len(subs) == 0 {
				delete(h.Channels, ch)
			}
		}
		h.mu.Unlock()
	case "ping":
		c.Send <- []byte(`{"type":"pong"}`)
	}
}

// Hub manages all WebSocket connections
type Hub struct {
	Clients    map[*Client]bool
	Broadcast  chan []byte
	Register   chan *Client
	Unregister chan *Client

	// Channel-based routing: channel -> set of clients
	Channels map[string]map[*Client]bool
	mu       sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{
		Clients:    make(map[*Client]bool),
		Broadcast:  make(chan []byte, 256),
		Register:   make(chan *Client),
		Unregister: make(chan *Client),
		Channels:   make(map[string]map[*Client]bool),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.Register:
			h.Clients[client] = true
			log.Printf("WebSocket client registered: %d", client.ID)
		case client := <-h.Unregister:
			if _, ok := h.Clients[client]; ok {
				delete(h.Clients, client)
				close(client.Send)
				h.mu.Lock()
				for _, subs := range h.Channels {
					delete(subs, client)
				}
				h.mu.Unlock()
				log.Printf("WebSocket client unregistered: %d", client.ID)
			}
		case msg := <-h.Broadcast:
			for client := range h.Clients {
				select {
				case client.Send <- msg:
				default:
					close(client.Send)
					delete(h.Clients, client)
				}
			}
		}
	}
}

// BroadcastToChannel sends a message to all clients subscribed to a specific channel
func (h *Hub) BroadcastToChannel(channel string, msg []byte) {
	h.mu.RLock()
	clients, ok := h.Channels[channel]
	h.mu.RUnlock()
	if !ok {
		return
	}
	for client := range clients {
		select {
		case client.Send <- msg:
		default:
		}
	}
}

// ServeWs handles WebSocket upgrade requests
func ServeWs(hub *Hub, userID int64, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	client := &Client{
		ID:   userID,
		Hub:  hub,
		Conn: conn,
		Send: make(chan []byte, 256),
		Subs: make(map[string]bool),
	}
	hub.Register <- client
	go client.WritePump()
	go client.ReadPump()
}
