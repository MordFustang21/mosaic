# DuckDB Server (Go Implementation)

A Go-based server that runs a local DuckDB instance and supports queries over WebSockets or HTTP, returning data in either [Apache Arrow](https://arrow.apache.org/) or JSON format.

This is a Go implementation of the Python-based [duckdb-server](../duckdb-server) from the Mosaic project, providing the same API and functionality with improved performance and reduced dependencies.

## Features

- **HTTP API**: GET and POST endpoints for SQL queries
- **WebSocket Support**: Real-time query execution over WebSockets
- **Multiple Response Formats**: Apache Arrow (binary) and JSON
- **Query Caching**: In-memory caching with SHA-256 based keys
- **Bundle Support**: Create and load query/table bundles for offline use
- **CORS Enabled**: Cross-origin requests supported
- **Performance Logging**: Automatic logging of slow queries (>5s)

## Installation

### Prerequisites

- Go 1.21 or later
- CGO enabled (required for DuckDB)

### Build from Source

```bash
cd mosaic/duckdb-server-go
go mod tidy
go build -o duckdb-server
```

### Dependencies

The server uses the following Go packages:
- `github.com/marcboeker/go-duckdb` - DuckDB Go driver
- `github.com/gorilla/websocket` - WebSocket support
- `github.com/apache/arrow/go/v14` - Apache Arrow format
- `github.com/patrickmn/go-cache` - In-memory caching

## Usage

### Basic Usage

Start the server with an in-memory database:
```bash
./duckdb-server
```

Start the server with a persistent database file:
```bash
./duckdb-server /path/to/database.db
```

The server will start on port 3000 by default:
- HTTP: `http://localhost:3000`
- WebSocket: `ws://localhost:3000/ws`

### Example Queries

#### HTTP GET
```bash
curl "http://localhost:3000/?query={\"sql\":\"SELECT 1 as test\",\"type\":\"json\"}"
```

#### HTTP POST
```bash
curl -X POST http://localhost:3000/ \
  -H "Content-Type: application/json" \
  -d '{"sql":"SELECT * FROM generate_series(1,5) as t(i)", "type":"json"}'
```

#### WebSocket (JavaScript)
```javascript
const ws = new WebSocket('ws://localhost:3000/ws');
ws.onopen = () => {
  ws.send(JSON.stringify({
    sql: 'SELECT * FROM generate_series(1,5) as t(i)',
    type: 'json'
  }));
};
ws.onmessage = (event) => {
  console.log('Result:', JSON.parse(event.data));
};
```

## API Reference

The server accepts JSON objects with the following structure:

```json
{
  "sql": "SELECT * FROM table",
  "type": "json|arrow|exec",
  "persist": false,
  "name": "bundle-name",
  "queries": [...],
  "alias": "table-alias"
}
```

### Commands

#### `exec`
Executes SQL without returning results (useful for DDL/DML operations).

```json
{"sql": "CREATE TABLE test (id INTEGER)", "type": "exec"}
```

#### `json`
Executes SQL and returns results in JSON format.

```json
{"sql": "SELECT * FROM test", "type": "json"}
```

#### `arrow`
Executes SQL and returns results in Apache Arrow format (binary).

```json
{"sql": "SELECT * FROM test", "type": "arrow"}
```

#### `create-bundle`
Creates a bundle of cached queries and tables.

```json
{
  "type": "create-bundle",
  "name": "my-bundle",
  "queries": [
    {"sql": "SELECT * FROM table1", "alias": "cached_table1"},
    "SELECT COUNT(*) FROM table2"
  ]
}
```

#### `load-bundle`
Loads a previously created bundle.

```json
{"type": "load-bundle", "name": "my-bundle"}
```

### Query Options

- **`persist`**: Set to `true` to cache query results
- **`name`**: Bundle name for create/load operations
- **`alias`**: Table alias when creating bundles
- **`queries`**: Array of queries for bundle creation

## Performance

### Caching

The server implements query result caching using SHA-256 hashes of SQL queries. Cache keys are generated as `{hash}.{type}` where:
- `hash`: SHA-256 of the SQL query
- `type`: Query type (`json`, `arrow`)

### Logging

- All queries are logged with execution time
- Queries taking longer than 5 seconds are marked as "slow queries"
- Cache hits are logged for debugging

## Development

### Running in Development

```bash
go run main.go
```

### Testing

```bash
go test ./...
```

### Code Structure

```
main.go                 # Main server implementation
├── Query struct       # Request parsing
├── Server struct      # Core server logic  
├── Handler interface  # Response handling abstraction
├── HTTPHandler        # HTTP response handling
├── WebSocketHandler   # WebSocket response handling
└── Helper functions   # Arrow/JSON conversion, caching
```

## Differences from Python Version

### Advantages
- **Performance**: Compiled Go binary with better performance
- **Dependencies**: Fewer runtime dependencies
- **Memory**: More efficient memory usage
- **Deployment**: Single binary deployment

### Limitations
- **Bundle Operations**: Simplified implementation (extensible)
- **Arrow Support**: Basic type mapping (can be extended)
- **Error Handling**: Simplified error responses

## Future Enhancements

- [ ] Complete bundle create/load implementation
- [ ] Enhanced Arrow type mapping
- [ ] Configuration file support
- [ ] SSL/TLS support
- [ ] Metrics and monitoring
- [ ] Connection pooling
- [ ] Query timeout handling
- [ ] Rate limiting

## License

This implementation follows the same license as the parent Mosaic project.