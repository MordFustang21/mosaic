package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/gorilla/websocket"
	"github.com/marcboeker/go-duckdb/v2"
	"github.com/patrickmn/go-cache"
)

const (
	SlowQueryThreshold = 5000 * time.Millisecond
	BundleDir         = ".mosaic/bundle"
)

type Query struct {
	SQL     string `json:"sql"`
	Type    string `json:"type"`
	Persist bool   `json:"persist,omitempty"`
	Name    string `json:"name,omitempty"`
	Queries []any  `json:"queries,omitempty"`
	Alias   string `json:"alias,omitempty"`
}

type Server struct {
	db    *sql.DB
	cache *cache.Cache
}

type ErrorResponse struct {
	Error string `json:"error"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for development
	},
}

func main() {
	dbPath := ""
	if len(os.Args) >= 2 {
		dbPath = os.Args[1]
	}

	if dbPath == "" {
		log.Printf("Using DuckDB in-memory database")
	} else {
		log.Printf("Using DuckDB %s", dbPath)
	}

	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		log.Fatal("Failed to open DuckDB:", err)
	}
	defer db.Close()

	// Test connection
	if err := db.Ping(); err != nil {
		log.Fatal("Failed to ping DuckDB:", err)
	}

	// Create cache with 5 minute default expiration and 10 minute cleanup interval
	queryCache := cache.New(5*time.Minute, 10*time.Minute)

	server := &Server{
		db:    db,
		cache: queryCache,
	}

	log.Printf("Caching enabled")

	http.HandleFunc("/", server.handleHTTP)
	http.HandleFunc("/ws", server.handleWebSocket)

	port := 3000
	log.Printf("DuckDB Server listening at ws://localhost:%d/ws and http://localhost:%d", port, port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}

func (s *Server) handleHTTP(w http.ResponseWriter, r *http.Request) {
	// Set CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Request-Method", "*")
	w.Header().Set("Access-Control-Allow-Methods", "OPTIONS, POST, GET")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Access-Control-Max-Age", "2592000")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	var query Query
	var err error

	if r.Method == "GET" {
		queryParam := r.URL.Query().Get("query")
		if queryParam == "" {
			http.Error(w, "Missing query parameter", http.StatusBadRequest)
			return
		}
		err = json.Unmarshal([]byte(queryParam), &query)
	} else if r.Method == "POST" {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}
		err = json.Unmarshal(body, &query)
	} else {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	s.handleQuery(NewHTTPHandler(w), query)
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			log.Printf("WebSocket read error: %v", err)
			break
		}

		if messageType != websocket.TextMessage {
			continue
		}

		var query Query
		if err := json.Unmarshal(message, &query); err != nil {
			s.sendWebSocketError(conn, fmt.Sprintf("Invalid JSON: %v", err))
			continue
		}

		s.handleQuery(NewWebSocketHandler(conn), query)
	}
}

func (s *Server) handleQuery(handler Handler, query Query) {
	log.Printf("Query: %+v", query)
	start := time.Now()

	defer func() {
		duration := time.Since(start)
		if duration > SlowQueryThreshold {
			log.Printf("DONE. Slow query took %d ms.\n%s", duration.Milliseconds(), query.SQL)
		} else {
			log.Printf("DONE. Query took %d ms.\n%s", duration.Milliseconds(), query.SQL)
		}
	}()

	switch query.Type {
	case "exec":
		s.handleExec(handler, query)
	case "arrow":
		s.handleArrow(handler, query)
	case "json":
		s.handleJSON(handler, query)
	case "create-bundle":
		s.handleCreateBundle(handler, query)
	case "load-bundle":
		s.handleLoadBundle(handler, query)
	default:
		handler.Error(fmt.Sprintf("Unknown command: %s", query.Type))
	}
}

func (s *Server) handleExec(handler Handler, query Query) {
	if _, err := s.db.Exec(query.SQL); err != nil {
		handler.Error(fmt.Sprintf("Query execution failed: %v", err))
		return
	}
	handler.Done()
}

func (s *Server) handleArrow(handler Handler, query Query) {
	cacheKey := s.getCacheKey(query.SQL, "arrow")
	
	if cached, found := s.cache.Get(cacheKey); found {
		log.Printf("Cache hit")
		handler.Arrow(cached.([]byte))
		return
	}

	arrowData, err := s.queryToArrowNative(query.SQL)
	if err != nil {
		handler.Error(fmt.Sprintf("Arrow conversion failed: %v", err))
		return
	}

	if query.Persist {
		s.cache.Set(cacheKey, arrowData, cache.DefaultExpiration)
	}

	handler.Arrow(arrowData)
}

func (s *Server) handleJSON(handler Handler, query Query) {
	cacheKey := s.getCacheKey(query.SQL, "json")
	
	if cached, found := s.cache.Get(cacheKey); found {
		log.Printf("Cache hit")
		handler.JSON(cached.(string))
		return
	}

	jsonData, err := s.queryToJSON(query.SQL)
	if err != nil {
		handler.Error(fmt.Sprintf("JSON conversion failed: %v", err))
		return
	}

	if query.Persist {
		s.cache.Set(cacheKey, jsonData, cache.DefaultExpiration)
	}

	handler.JSON(jsonData)
}

func (s *Server) handleCreateBundle(handler Handler, query Query) {
	// Simplified bundle creation - full implementation would be more complex
	bundleDir := filepath.Join(BundleDir, query.Name)
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		handler.Error(fmt.Sprintf("Failed to create bundle directory: %v", err))
		return
	}

	manifest := map[string]interface{}{
		"tables":  []string{},
		"queries": []string{},
	}

	// Save manifest
	manifestData, _ := json.MarshalIndent(manifest, "", "  ")
	manifestPath := filepath.Join(bundleDir, "bundle.json")
	if err := os.WriteFile(manifestPath, manifestData, 0644); err != nil {
		handler.Error(fmt.Sprintf("Failed to save manifest: %v", err))
		return
	}

	handler.Done()
}

func (s *Server) handleLoadBundle(handler Handler, query Query) {
	// Simplified bundle loading - full implementation would be more complex
	bundleDir := filepath.Join(BundleDir, query.Name)
	manifestPath := filepath.Join(bundleDir, "bundle.json")
	
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		handler.Error(fmt.Sprintf("Bundle not found: %s", query.Name))
		return
	}

	handler.Done()
}

func (s *Server) queryToArrowNative(sql string) ([]byte, error) {
	// Use the native Arrow interface from go-duckdb v2
	conn, err := s.db.Conn(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %v", err)
	}
	defer conn.Close()

	var arrowData []byte
	err = conn.Raw(func(driverConn any) error {
		dConn, ok := driverConn.(driver.Conn)
		if !ok {
			return fmt.Errorf("could not cast to driver.Conn")
		}

		// Get Arrow interface
		arrow, err := duckdb.NewArrowFromConn(dConn)
		if err != nil {
			return fmt.Errorf("failed to create Arrow interface: %v", err)
		}

		// Execute query using Arrow interface
		reader, err := arrow.QueryContext(context.Background(), sql)
		if err != nil {
			return fmt.Errorf("failed to execute Arrow query: %v", err)
		}
		defer reader.Release()

		// Convert Arrow records to proper IPC format
		var buf strings.Builder
		mem := memory.NewGoAllocator()
		
		// Get the schema from the first record
		if reader.Next() {
			record := reader.Record()
			schema := record.Schema()
			
			// Create IPC writer
			writer := ipc.NewWriter(&buf, ipc.WithSchema(schema), ipc.WithAllocator(mem))
			defer writer.Close()
			
			// Write the first record
			if err := writer.Write(record); err != nil {
				return fmt.Errorf("failed to write Arrow record: %v", err)
			}
			
			// Write remaining records
			for reader.Next() {
				record := reader.Record()
				if err := writer.Write(record); err != nil {
					return fmt.Errorf("failed to write Arrow record: %v", err)
				}
			}
		}
		
		arrowData = []byte(buf.String())
		return nil
	})

	if err != nil {
		return nil, err
	}

	return arrowData, nil
}

func (s *Server) queryToJSON(sql string) (string, error) {
	rows, err := s.db.Query(sql)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return "", err
	}

	var results []map[string]interface{}

	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return "", err
		}

		row := make(map[string]interface{})
		for i, col := range columns {
			val := values[i]
			if b, ok := val.([]byte); ok {
				val = string(b)
			}
			row[col] = val
		}
		results = append(results, row)
	}

	jsonData, err := json.Marshal(results)
	if err != nil {
		return "", err
	}

	return string(jsonData), nil
}

func (s *Server) getCacheKey(sql, command string) string {
	hash := sha256.Sum256([]byte(sql))
	return hex.EncodeToString(hash[:]) + "." + command
}

func (s *Server) sendWebSocketError(conn *websocket.Conn, message string) {
	errorMsg := ErrorResponse{Error: message}
	if err := conn.WriteJSON(errorMsg); err != nil {
		log.Printf("Failed to send WebSocket error: %v", err)
	}
}

// Handler interface for different response types
type Handler interface {
	Done()
	Arrow([]byte)
	JSON(string)
	Error(string)
}

// HTTP Handler
type HTTPHandler struct {
	w http.ResponseWriter
}

func NewHTTPHandler(w http.ResponseWriter) *HTTPHandler {
	return &HTTPHandler{w: w}
}

func (h *HTTPHandler) Done() {
	h.w.WriteHeader(http.StatusOK)
}

func (h *HTTPHandler) Arrow(data []byte) {
	h.w.Header().Set("Content-Type", "application/octet-stream")
	h.w.Write(data)
}

func (h *HTTPHandler) JSON(data string) {
	h.w.Header().Set("Content-Type", "application/json")
	h.w.WriteHeader(http.StatusOK)
	h.w.Write([]byte(data))
}

func (h *HTTPHandler) Error(message string) {
	http.Error(h.w, message, http.StatusInternalServerError)
}

// WebSocket Handler
type WebSocketHandler struct {
	conn *websocket.Conn
}

func NewWebSocketHandler(conn *websocket.Conn) *WebSocketHandler {
	return &WebSocketHandler{conn: conn}
}

func (h *WebSocketHandler) Done() {
	h.conn.WriteMessage(websocket.TextMessage, []byte("{}"))
}

func (h *WebSocketHandler) Arrow(data []byte) {
	h.conn.WriteMessage(websocket.BinaryMessage, data)
}

func (h *WebSocketHandler) JSON(data string) {
	h.conn.WriteMessage(websocket.TextMessage, []byte(data))
}

func (h *WebSocketHandler) Error(message string) {
	errorMsg := ErrorResponse{Error: message}
	h.conn.WriteJSON(errorMsg)
}