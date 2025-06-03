package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	_ "github.com/marcboeker/go-duckdb"
	"github.com/patrickmn/go-cache"
)

func setupTestServer() *Server {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		panic(err)
	}
	
	queryCache := cache.New(5*time.Minute, 10*time.Minute)
	
	return &Server{
		db:    db,
		cache: queryCache,
	}
}

func TestHandleExec(t *testing.T) {
	server := setupTestServer()
	defer server.db.Close()

	// Create a test table
	query := Query{
		SQL:  "CREATE TABLE test_table (id INTEGER, name VARCHAR)",
		Type: "exec",
	}

	recorder := httptest.NewRecorder()
	handler := NewHTTPHandler(recorder)
	
	server.handleExec(handler, query)
	
	if recorder.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", recorder.Code)
	}

	// Verify table was created by inserting data
	insertQuery := Query{
		SQL:  "INSERT INTO test_table VALUES (1, 'test')",
		Type: "exec",
	}
	
	recorder2 := httptest.NewRecorder()
	handler2 := NewHTTPHandler(recorder2)
	
	server.handleExec(handler2, insertQuery)
	
	if recorder2.Code != http.StatusOK {
		t.Errorf("Expected status OK for insert, got %d", recorder2.Code)
	}
}

func TestHandleJSON(t *testing.T) {
	server := setupTestServer()
	defer server.db.Close()

	// Create and populate test table
	server.db.Exec("CREATE TABLE test_table (id INTEGER, name VARCHAR)")
	server.db.Exec("INSERT INTO test_table VALUES (1, 'Alice'), (2, 'Bob')")

	query := Query{
		SQL:  "SELECT * FROM test_table ORDER BY id",
		Type: "json",
	}

	recorder := httptest.NewRecorder()
	handler := NewHTTPHandler(recorder)
	
	server.handleJSON(handler, query)
	
	if recorder.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", recorder.Code)
	}

	var result []map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to unmarshal JSON response: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("Expected 2 rows, got %d", len(result))
	}

	if result[0]["name"] != "Alice" {
		t.Errorf("Expected first row name to be 'Alice', got %v", result[0]["name"])
	}
}

func TestHTTPEndpoint(t *testing.T) {
	server := setupTestServer()
	defer server.db.Close()

	// Test POST request
	query := Query{
		SQL:  "SELECT 42 as answer, 'Hello' as greeting",
		Type: "json",
	}
	
	queryBytes, _ := json.Marshal(query)
	req := httptest.NewRequest("POST", "/", bytes.NewReader(queryBytes))
	req.Header.Set("Content-Type", "application/json")
	
	recorder := httptest.NewRecorder()
	server.handleHTTP(recorder, req)
	
	if recorder.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", recorder.Code)
	}

	var result []map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to unmarshal JSON response: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("Expected 1 row, got %d", len(result))
	}

	if result[0]["answer"] != float64(42) {
		t.Errorf("Expected answer to be 42, got %v", result[0]["answer"])
	}
}

func TestCacheKey(t *testing.T) {
	server := setupTestServer()
	defer server.db.Close()

	key1 := server.getCacheKey("SELECT 1", "json")
	key2 := server.getCacheKey("SELECT 1", "json")
	key3 := server.getCacheKey("SELECT 2", "json")
	key4 := server.getCacheKey("SELECT 1", "arrow")

	if key1 != key2 {
		t.Error("Same SQL and type should produce same cache key")
	}

	if key1 == key3 {
		t.Error("Different SQL should produce different cache keys")
	}

	if key1 == key4 {
		t.Error("Different type should produce different cache keys")
	}

	// Check key format
	if len(key1) != 69 { // 64 hex chars + 1 dot + 4 chars for "json"
		t.Errorf("Cache key should be 69 characters long, got %d", len(key1))
	}
}

func TestQueryCaching(t *testing.T) {
	server := setupTestServer()
	defer server.db.Close()

	// Create test data
	server.db.Exec("CREATE TABLE cache_test (id INTEGER)")
	server.db.Exec("INSERT INTO cache_test VALUES (1), (2), (3)")

	query := Query{
		SQL:     "SELECT COUNT(*) as count FROM cache_test",
		Type:    "json",
		Persist: true,
	}

	// First request - should hit database
	recorder1 := httptest.NewRecorder()
	handler1 := NewHTTPHandler(recorder1)
	server.handleJSON(handler1, query)

	// Second request - should hit cache
	recorder2 := httptest.NewRecorder()
	handler2 := NewHTTPHandler(recorder2)
	server.handleJSON(handler2, query)

	// Both should return same result
	if recorder1.Body.String() != recorder2.Body.String() {
		t.Error("Cached and non-cached results should be identical")
	}

	// Verify cache was used by checking cache directly
	cacheKey := server.getCacheKey(query.SQL, query.Type)
	if _, found := server.cache.Get(cacheKey); !found {
		t.Error("Result should be cached")
	}
}

func TestCORSHeaders(t *testing.T) {
	server := setupTestServer()
	defer server.db.Close()

	req := httptest.NewRequest("OPTIONS", "/", nil)
	recorder := httptest.NewRecorder()
	
	server.handleHTTP(recorder, req)
	
	if recorder.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("CORS header Access-Control-Allow-Origin should be set to *")
	}
	
	if recorder.Code != http.StatusOK {
		t.Errorf("OPTIONS request should return 200, got %d", recorder.Code)
	}
}

func TestInvalidJSON(t *testing.T) {
	server := setupTestServer()
	defer server.db.Close()

	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("invalid json")))
	recorder := httptest.NewRecorder()
	
	server.handleHTTP(recorder, req)
	
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("Invalid JSON should return 400, got %d", recorder.Code)
	}
}

func TestUnsupportedMethod(t *testing.T) {
	server := setupTestServer()
	defer server.db.Close()

	req := httptest.NewRequest("DELETE", "/", nil)
	recorder := httptest.NewRecorder()
	
	server.handleHTTP(recorder, req)
	
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Errorf("Unsupported method should return 405, got %d", recorder.Code)
	}
}

func TestUnknownCommand(t *testing.T) {
	server := setupTestServer()
	defer server.db.Close()

	query := Query{
		SQL:  "SELECT 1",
		Type: "unknown",
	}

	recorder := httptest.NewRecorder()
	handler := NewHTTPHandler(recorder)
	
	server.handleQuery(handler, query)
	
	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("Unknown command should return 500, got %d", recorder.Code)
	}
}

func BenchmarkJSONQuery(b *testing.B) {
	server := setupTestServer()
	defer server.db.Close()

	// Setup test data
	server.db.Exec("CREATE TABLE bench_table (id INTEGER, value DOUBLE)")
	server.db.Exec("INSERT INTO bench_table SELECT i, random() FROM generate_series(1, 1000) as t(i)")

	query := Query{
		SQL:  "SELECT * FROM bench_table WHERE id <= 100",
		Type: "json",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		recorder := httptest.NewRecorder()
		handler := NewHTTPHandler(recorder)
		server.handleJSON(handler, query)
	}
}