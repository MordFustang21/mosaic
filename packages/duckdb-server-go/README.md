# DuckDB Server (Go Implementation)

A Go-based server that runs a local DuckDB instance and supports queries over WebSockets or HTTP, returning data in either [Apache Arrow](https://arrow.apache.org/) or JSON format.

This is a Go implementation of the Python-based [duckdb-server](../duckdb-server) from the Mosaic project, providing the same API and functionality with improved performance and reduced dependencies. **Now updated to use go-duckdb v2.3.0 with native Arrow support for enhanced performance.**

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

- Go 1.24 or later
- CGO enabled (required for DuckDB)

### Build from Source

```bash
cd mosaic/duckdb-server-go
go mod tidy
# Build with Arrow support (recommended)
go build -tags="duckdb_arrow" -o duckdb-server
```

**Note:** Arrow support is opt-in starting with go-duckdb v2. Use the `-tags="duckdb_arrow"` flag to enable native Arrow format support for better performance.

### Dependencies

The server uses the following Go packages:
- `github.com/marcboeker/go-duckdb/v2` v2.3.0 - DuckDB Go driver with native Arrow support
- `github.com/gorilla/websocket` - WebSocket support
- `github.com/apache/arrow-go/v18` - Apache Arrow format (when using Arrow tags)
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
# With Arrow support (recommended)
go run -tags="duckdb_arrow" main.go

# Without Arrow support
go run main.go
```

### Testing

```bash
# Test with Arrow support
go test -tags="duckdb_arrow" -v

# Test without Arrow support
go test -v
```

### Benchmarking

Performance comparison between JSON and Arrow formats:

```bash
go test -tags="duckdb_arrow" -bench=Benchmark -run=^$ -benchtime=3s
```

Example results show Arrow format is ~1.8x faster than JSON for data retrieval.

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

## Differences from Python and Rust Versions

### Advantages over Python
- **Performance**: Compiled Go binary with significantly better performance
- **Dependencies**: Fewer runtime dependencies
- **Memory**: More efficient memory usage
- **Deployment**: Single binary deployment

### Comparison with Rust Version
- **Advantages**: Simpler codebase, faster compilation, good performance
- **Limitations**: Less sophisticated bundle operations, simpler error handling
- **Performance**: Arrow format ~1.8x faster than JSON, competitive with Rust implementation

### Current Limitations
- **Bundle Operations**: Simplified implementation (extensible)
- **Error Handling**: Simplified error responses compared to Rust version
- **Features**: Rust version has HTTPS/TLS, HTTP/2, and more advanced bundle operations

## Recent Updates (v2.3.0)

- ✅ **Updated to go-duckdb v2.3.0** with native Arrow support
- ✅ **Improved Arrow performance** using DuckDB's native Arrow interface
- ✅ **Better type safety** with updated Arrow library (v18)
- ✅ **Performance improvements** - Arrow queries ~1.8x faster than JSON
- ✅ **Comprehensive testing** including Arrow-specific test cases

## Future Enhancements

- [ ] Complete bundle create/load implementation (to match Rust version)
- [ ] Configuration file support
- [ ] SSL/TLS support (like Rust version)
- [ ] HTTP/2 support
- [ ] Enhanced error handling and categorization
- [ ] Metrics and monitoring
- [ ] Connection pooling improvements
- [ ] Query timeout handling
- [ ] Rate limiting

## License

This implementation follows the same license as the parent Mosaic project.