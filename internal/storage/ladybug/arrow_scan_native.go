//go:build ladybug && cgo

package ladybug

/*
#cgo linux,amd64 LDFLAGS: -llbug
#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>

typedef struct ArrowSchema {
	const char* format;
	const char* name;
	const char* metadata;
	int64_t flags;
	int64_t n_children;
	struct ArrowSchema** children;
	struct ArrowSchema* dictionary;
	void (*release)(struct ArrowSchema*);
	void* private_data;
} ArrowSchema;

typedef struct ArrowArray {
	int64_t length;
	int64_t null_count;
	int64_t offset;
	int64_t n_buffers;
	int64_t n_children;
	const void** buffers;
	struct ArrowArray** children;
	struct ArrowArray* dictionary;
	void (*release)(struct ArrowArray*);
	void* private_data;
} ArrowArray;

typedef struct {
	uint64_t buffer_pool_size;
	uint64_t max_num_threads;
	bool enable_compression;
	bool read_only;
	uint64_t max_db_size;
	bool auto_checkpoint;
	uint64_t checkpoint_threshold;
	bool throw_on_wal_replay_failure;
	bool enable_checksums;
	bool enable_multi_writes;
	bool enable_default_hash_index;
} lbug_system_config;

typedef struct { void* _database; } lbug_database;
typedef struct { void* _connection; } lbug_connection;
typedef struct { void* _query_result; bool _is_owned_by_cpp; } lbug_query_result;
typedef int lbug_state;

lbug_system_config lbug_default_system_config(void);
lbug_state lbug_database_init(const char*, lbug_system_config, lbug_database*);
void lbug_database_destroy(lbug_database*);
lbug_state lbug_connection_init(lbug_database*, lbug_connection*);
void lbug_connection_destroy(lbug_connection*);
void lbug_connection_interrupt(lbug_connection*);
lbug_state lbug_connection_query(lbug_connection*, const char*, lbug_query_result*);
void lbug_query_result_destroy(lbug_query_result*);
char* lbug_query_result_get_error_message(lbug_query_result*);
bool lbug_query_result_has_next(lbug_query_result*);
lbug_state lbug_query_result_get_arrow_schema(lbug_query_result*, ArrowSchema*);
lbug_state lbug_query_result_get_next_arrow_chunk(lbug_query_result*, int64_t, ArrowArray*);
void lbug_destroy_string(char*);

static ArrowSchema* kivgraph_schema_child(ArrowSchema* schema, int64_t index) {
	return schema->children[index];
}

static ArrowArray* kivgraph_array_child(ArrowArray* array, int64_t index) {
	return array->children[index];
}

static const void* kivgraph_array_buffer(ArrowArray* array, int64_t index) {
	return array->buffers[index];
}

static void kivgraph_release_schema(ArrowSchema* schema) {
	if (schema->release != NULL) {
		schema->release(schema);
	}
}

static void kivgraph_release_array(ArrowArray* array) {
	if (array->release != NULL) {
		array->release(array);
	}
}
*/
import "C"

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"unsafe"
)

const arrowScanChunkSize = 2_000_000

type arrowColumn struct {
	format string
	array  *C.ArrowArray
}

// scanArrowAll keeps database scans streaming and restores the deterministic key order in Go.

func (reader *reader) scanArrowAll(ctx context.Context) (ScanRows, error) {
	if err := ctx.Err(); err != nil {
		return ScanRows{}, err
	}

	path := C.CString(reader.parent.path)
	defer C.free(unsafe.Pointer(path))
	config := C.lbug_default_system_config()
	config.enable_compression = C.bool(true)
	config.read_only = C.bool(true)
	config.buffer_pool_size = C.uint64_t(scanBufferPoolBytes(reader.parent.path))

	var database C.lbug_database
	if status := C.lbug_database_init(path, config, &database); status != 0 {
		return ScanRows{}, fmt.Errorf("open Arrow scan database: status %d", status)
	}
	defer C.lbug_database_destroy(&database)

	var connections [4]C.lbug_connection
	for index := range connections {
		if status := C.lbug_connection_init(&database, &connections[index]); status != 0 {
			for closeIndex := 0; closeIndex < index; closeIndex++ {
				C.lbug_connection_destroy(&connections[closeIndex])
			}
			return ScanRows{}, fmt.Errorf("open Arrow scan connection %d: status %d", index, status)
		}
	}
	defer func() {
		for index := range connections {
			C.lbug_connection_destroy(&connections[index])
		}
	}()

	scanCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stops := make([]func(), len(connections))
	for index := range connections {
		stops[index] = arrowInterruptGuard(scanCtx, &connections[index])
	}
	defer func() {
		for index := len(stops) - 1; index >= 0; index-- {
			stops[index]()
		}
	}()

	jobs := [...]arrowScanJob{
		{query: scanRepositoriesQuery, formats: []string{"u", "u", "u", "u"}, operation: "scan repositories", decode: decodeArrowRepositories},
		{query: scanFilesQuery, formats: []string{"u", "u", "u", "u", "u"}, operation: "scan files", decode: decodeArrowFiles},
		{query: scanSymbolsQuery, formats: []string{"u", "u", "u", "u", "u", "u", "u", "l", "l"}, operation: "scan symbols", decode: decodeArrowSymbols},
		{query: scanEdgesQuery, formats: []string{"u", "u", "u", "u", "u", "u"}, operation: "scan edges", decode: decodeArrowEdges},
	}
	var (
		results [len(jobs)]ScanRows
		errs    [len(jobs)]error
		wait    sync.WaitGroup
	)
	wait.Add(len(jobs))
	for index := range jobs {
		go func(index int) {
			defer wait.Done()
			errs[index] = scanArrowQuery(scanCtx, &connections[index], jobs[index].query, jobs[index].formats, func(columns []arrowColumn, rowCount int64) error {
				return jobs[index].decode(&results[index], columns, rowCount)
			})
			if errs[index] != nil {
				cancel()
			}
		}(index)
	}
	wait.Wait()
	for index := range errs {
		if errs[index] != nil {
			return ScanRows{}, &Error{Op: jobs[index].operation, Err: errs[index]}
		}
	}
	if err := ctx.Err(); err != nil {
		return ScanRows{}, err
	}
	for index := range results {
		sortArrowRows(&results[index])
	}
	return ScanRows{
		Repositories: results[0].Repositories,
		Files:        results[1].Files,
		Symbols:      results[2].Symbols,
		Edges:        results[3].Edges,
	}, nil
}

func sortArrowRows(rows *ScanRows) {
	if !sort.SliceIsSorted(rows.Repositories, func(i, j int) bool {
		return rows.Repositories[i].StableKey < rows.Repositories[j].StableKey
	}) {
		sort.Slice(rows.Repositories, func(i, j int) bool {
			return rows.Repositories[i].StableKey < rows.Repositories[j].StableKey
		})
	}
	if !sort.SliceIsSorted(rows.Files, func(i, j int) bool {
		return rows.Files[i].StableKey < rows.Files[j].StableKey
	}) {
		sort.Slice(rows.Files, func(i, j int) bool {
			return rows.Files[i].StableKey < rows.Files[j].StableKey
		})
	}
	if !sort.SliceIsSorted(rows.Symbols, func(i, j int) bool {
		return rows.Symbols[i].StableKey < rows.Symbols[j].StableKey
	}) {
		sort.Slice(rows.Symbols, func(i, j int) bool {
			return rows.Symbols[i].StableKey < rows.Symbols[j].StableKey
		})
	}
	if !sort.SliceIsSorted(rows.Edges, func(i, j int) bool {
		return scanEdgeLess(rows.Edges[i], rows.Edges[j])
	}) {
		sort.Slice(rows.Edges, func(i, j int) bool {
			return scanEdgeLess(rows.Edges[i], rows.Edges[j])
		})
	}
}

func scanEdgeLess(left, right ScanEdge) bool {
	if left.SourceKey != right.SourceKey {
		return left.SourceKey < right.SourceKey
	}
	if left.TargetKey != right.TargetKey {
		return left.TargetKey < right.TargetKey
	}
	if left.Kind != right.Kind {
		return left.Kind < right.Kind
	}
	if left.EvidenceKind != right.EvidenceKind {
		return left.EvidenceKind < right.EvidenceKind
	}
	if left.SourceFileKey != right.SourceFileKey {
		return left.SourceFileKey < right.SourceFileKey
	}
	return left.TargetFileKey < right.TargetFileKey
}

type arrowScanJob struct {
	query     string
	formats   []string
	operation string
	decode    func(*ScanRows, []arrowColumn, int64) error
}

type arrowArenaColumn struct {
	column      arrowColumn
	offsets     unsafe.Pointer
	data        unsafe.Pointer
	validity    unsafe.Pointer
	sourceStart int64
	sourceEnd   int64
	destination int
}

func newArrowArenaColumn(column arrowColumn) (arrowArenaColumn, error) {
	array := column.array
	if array == nil {
		return arrowArenaColumn{}, fmt.Errorf("nil Arrow string column")
	}
	if array.n_buffers < 3 {
		return arrowArenaColumn{}, fmt.Errorf("unexpected Arrow string column with %d buffers, want at least 3", array.n_buffers)
	}
	offsets := C.kivgraph_array_buffer(array, 1)
	data := C.kivgraph_array_buffer(array, 2)
	if offsets == nil || data == nil {
		return arrowArenaColumn{}, fmt.Errorf("nil offsets or data buffer in the Arrow string column")
	}
	var validity unsafe.Pointer
	if array.null_count != 0 {
		validity = C.kivgraph_array_buffer(array, 0)
		if validity == nil {
			return arrowArenaColumn{}, fmt.Errorf("missing validity buffer for the Arrow string column with null count %d", array.null_count)
		}
	}
	return arrowArenaColumn{column: column, offsets: offsets, data: data, validity: validity}, nil
}

// offsetsAt answers the byte range of a row and rejects a NULL: every column
// of the old schema's four queries is written unconditionally.
func (column *arrowArenaColumn) offsetsAt(row int64) (int64, int64, error) {
	start, end, null, err := column.offsetsOrNullAt(row)
	if err != nil {
		return 0, 0, err
	}
	if null {
		return 0, 0, fmt.Errorf("null in the Arrow column at row %d", row)
	}
	return start, end, nil
}

// offsetsOrNullAt answers the byte range of a row and whether it is NULL.
// The canonical schema round trips an empty string as a NULL, so its reader
// needs the distinction rather than an error.
func (column *arrowArenaColumn) offsetsOrNullAt(row int64) (int64, int64, bool, error) {
	array := column.column.array
	if row < 0 || row >= int64(array.length) {
		return 0, 0, false, fmt.Errorf("out-of-range Arrow row %d, length %d", row, array.length)
	}
	index := int64(array.offset) + row
	if column.validity != nil {
		bit := *(*byte)(unsafe.Add(column.validity, index/8))
		if bit&(byte(1)<<uint(index%8)) == 0 {
			return 0, 0, true, nil
		}
	}
	var start, end int64
	switch column.column.format {
	case "u":
		size := int64(unsafe.Sizeof(C.int32_t(0)))
		start = int64(*(*C.int32_t)(unsafe.Add(column.offsets, index*size)))
		end = int64(*(*C.int32_t)(unsafe.Add(column.offsets, (index+1)*size)))
	case "U":
		size := int64(unsafe.Sizeof(C.int64_t(0)))
		start = int64(*(*C.int64_t)(unsafe.Add(column.offsets, index*size)))
		end = int64(*(*C.int64_t)(unsafe.Add(column.offsets, (index+1)*size)))
	default:
		return 0, 0, false, fmt.Errorf("unsupported Arrow string format %q", column.column.format)
	}
	if start < 0 || end < start {
		return 0, 0, false, fmt.Errorf("invalid Arrow string offsets %d..%d", start, end)
	}
	return start, end, false, nil
}

// dataRange answers the byte range this chunk occupies in the column's data
// buffer.
//
// It is the widest range over the rows, not the span from the first row's
// start to the last row's end: the engine writes a NULL as the offsets
// `0..0`, so a column whose last row is NULL would otherwise report a range
// that excludes every value in it.
func (column *arrowArenaColumn) dataRange(rowCount int64) (int64, int64, error) {
	var start, end int64
	found := false
	for row := int64(0); row < rowCount; row++ {
		rowStart, rowEnd, null, err := column.offsetsOrNullAt(row)
		if err != nil {
			return 0, 0, err
		}
		if null || rowEnd == rowStart {
			continue
		}
		if !found {
			start, end, found = rowStart, rowEnd, true
			continue
		}
		if rowStart < start {
			start = rowStart
		}
		if rowEnd > end {
			end = rowEnd
		}
	}
	return start, end, nil
}

func decodeArrowStringRows(rows *ScanRows, columns []arrowColumn, stringCount int, rowCount int64, consume func(int64, []string) error) error {
	if stringCount < 0 || stringCount > len(columns) {
		return fmt.Errorf("unexpected Arrow string column count = %d, columns = %d", stringCount, len(columns))
	}
	arenaColumns := make([]arrowArenaColumn, stringCount)
	var total int64
	for column := 0; column < stringCount; column++ {
		item, err := newArrowArenaColumn(columns[column])
		if err != nil {
			return err
		}
		if rowCount > 0 {
			item.sourceStart, _, err = item.offsetsAt(0)
			if err != nil {
				return err
			}
			_, item.sourceEnd, err = item.offsetsAt(rowCount - 1)
			if err != nil {
				return err
			}
			if item.sourceEnd < item.sourceStart || item.sourceEnd-item.sourceStart > int64(^uint(0)>>1)-total {
				return fmt.Errorf("the Arrow string data exceeds addressable memory")
			}
			item.destination = int(total)
			total += item.sourceEnd - item.sourceStart
		}
		arenaColumns[column] = item
	}
	arena := make([]byte, int(total))
	for column := range arenaColumns {
		item := &arenaColumns[column]
		if item.sourceEnd == item.sourceStart {
			continue
		}
		length := int(item.sourceEnd - item.sourceStart)
		copy(arena[item.destination:item.destination+length], unsafe.Slice((*byte)(unsafe.Add(item.data, item.sourceStart)), length))
	}
	// Strings point into arena; their data pointers keep the backing allocation alive.
	values := make([]string, stringCount)
	for row := int64(0); row < rowCount; row++ {
		for column := 0; column < stringCount; column++ {
			item := &arenaColumns[column]
			start, end, err := item.offsetsAt(row)
			if err != nil {
				return err
			}
			if start < item.sourceStart || end < start || end > item.sourceEnd {
				return fmt.Errorf("out-of-range Arrow string offsets %d..%d, chunk range %d..%d", start, end, item.sourceStart, item.sourceEnd)
			}
			length := int(end - start)
			if length == 0 {
				values[column] = ""
				continue
			}
			offset := item.destination + int(start-item.sourceStart)
			values[column] = unsafe.String(unsafe.SliceData(arena[offset:offset+length]), length)
		}
		if err := consume(row, values); err != nil {
			return err
		}
	}
	return nil
}

func decodeArrowRepositories(rows *ScanRows, columns []arrowColumn, rowCount int64) error {
	return decodeArrowStringRows(rows, columns, 4, rowCount, func(_ int64, values []string) error {
		rows.Repositories = append(rows.Repositories, RepositoryRecord{
			StableKey: values[0], Name: values[1], Path: values[2], Language: values[3],
		})
		return nil
	})
}

func decodeArrowFiles(rows *ScanRows, columns []arrowColumn, rowCount int64) error {
	return decodeArrowStringRows(rows, columns, 5, rowCount, func(_ int64, values []string) error {
		rows.Files = append(rows.Files, FileRecord{
			StableKey: values[0], RepositoryKey: values[1], Path: values[2], ContentHash: values[3], Language: values[4],
		})
		return nil
	})
}

func decodeArrowSymbols(rows *ScanRows, columns []arrowColumn, rowCount int64) error {
	startColumn, err := newArrowInt64Column(columns[7])
	if err != nil {
		return err
	}
	endColumn, err := newArrowInt64Column(columns[8])
	if err != nil {
		return err
	}
	return decodeArrowStringRows(rows, columns, 7, rowCount, func(row int64, values []string) error {
		startLine, err := startColumn.valueAt(row)
		if err != nil {
			return err
		}
		endLine, err := endColumn.valueAt(row)
		if err != nil {
			return err
		}
		rows.Symbols = append(rows.Symbols, Symbol{
			StableKey: values[0], RepositoryKey: values[1], FileKey: values[2], Name: values[3],
			QualifiedName: values[4], Kind: values[5], Signature: values[6], StartLine: startLine, EndLine: endLine,
		})
		return nil
	})
}

type arrowInt64Column struct {
	column   arrowColumn
	values   unsafe.Pointer
	validity unsafe.Pointer
}

func newArrowInt64Column(column arrowColumn) (arrowInt64Column, error) {
	array := column.array
	if array == nil {
		return arrowInt64Column{}, fmt.Errorf("nil Arrow int64 column")
	}
	if column.format != "l" {
		return arrowInt64Column{}, fmt.Errorf("unsupported Arrow int64 format %q", column.format)
	}
	if array.n_buffers < 2 {
		return arrowInt64Column{}, fmt.Errorf("unexpected Arrow int64 column with %d buffers, want at least 2", array.n_buffers)
	}
	values := C.kivgraph_array_buffer(array, 1)
	if values == nil {
		return arrowInt64Column{}, fmt.Errorf("nil values buffer in the Arrow int64 column")
	}
	var validity unsafe.Pointer
	if array.null_count != 0 {
		validity = C.kivgraph_array_buffer(array, 0)
		if validity == nil {
			return arrowInt64Column{}, fmt.Errorf("missing validity buffer for the Arrow int64 column with null count %d", array.null_count)
		}
	}
	return arrowInt64Column{column: column, values: values, validity: validity}, nil
}

func (column *arrowInt64Column) valueAt(row int64) (int64, error) {
	array := column.column.array
	if row < 0 || row >= int64(array.length) {
		return 0, fmt.Errorf("out-of-range Arrow row %d, length %d", row, array.length)
	}
	index := int64(array.offset) + row
	if column.validity != nil {
		bit := *(*byte)(unsafe.Add(column.validity, index/8))
		if bit&(byte(1)<<uint(index%8)) == 0 {
			return 0, fmt.Errorf("null in the Arrow column at row %d", row)
		}
	}
	value := *(*C.int64_t)(unsafe.Add(column.values, index*int64(unsafe.Sizeof(C.int64_t(0)))))
	return int64(value), nil
}

func decodeArrowEdges(rows *ScanRows, columns []arrowColumn, rowCount int64) error {
	return decodeArrowStringRows(rows, columns, 6, rowCount, func(_ int64, values []string) error {
		rows.Edges = append(rows.Edges, ScanEdge{
			SourceKey: values[0], TargetKey: values[1], Kind: values[2], EvidenceKind: values[3],
			SourceFileKey: values[4], TargetFileKey: values[5],
		})
		return nil
	})
}

func scanArrowQuery(ctx context.Context, connection *C.lbug_connection, query string, expectedFormats []string, consume func([]arrowColumn, int64) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cquery := C.CString(query)
	defer C.free(unsafe.Pointer(cquery))
	var result C.lbug_query_result
	if status := C.lbug_connection_query(connection, cquery, &result); status != 0 {
		defer C.lbug_query_result_destroy(&result)
		return arrowQueryError(&result, "query")
	}
	defer C.lbug_query_result_destroy(&result)

	var schema C.ArrowSchema
	if status := C.lbug_query_result_get_arrow_schema(&result, &schema); status != 0 {
		return arrowQueryError(&result, "get Arrow schema")
	}
	formats, err := arrowSchemaFormats(&schema)
	C.kivgraph_release_schema(&schema)
	if err != nil {
		return err
	}
	if len(formats) != len(expectedFormats) {
		return fmt.Errorf("unexpected Arrow schema columns = %d, want %d", len(formats), len(expectedFormats))
	}
	for index := range formats {
		if formats[index] != expectedFormats[index] {
			return fmt.Errorf("unexpected Arrow schema column %d format = %q, want %q", index, formats[index], expectedFormats[index])
		}
	}

	for C.lbug_query_result_has_next(&result) {
		if err := ctx.Err(); err != nil {
			return err
		}
		var array C.ArrowArray
		if status := C.lbug_query_result_get_next_arrow_chunk(&result, arrowScanChunkSize, &array); status != 0 {
			return arrowQueryError(&result, "get Arrow chunk")
		}
		chunkErr := func() error {
			defer C.kivgraph_release_array(&array)
			if array.n_children != C.int64_t(len(formats)) {
				return fmt.Errorf("unexpected Arrow chunk columns = %d, want %d", array.n_children, len(formats))
			}
			columns := make([]arrowColumn, len(formats))
			for index, format := range formats {
				columns[index] = arrowColumn{format: format, array: C.kivgraph_array_child(&array, C.int64_t(index))}
				if columns[index].array == nil {
					return fmt.Errorf("nil Arrow chunk column %d", index)
				}
			}
			return consume(columns, int64(array.length))
		}()
		if chunkErr != nil {
			return chunkErr
		}
	}
	return ctx.Err()
}

func arrowSchemaFormats(schema *C.ArrowSchema) ([]string, error) {
	if schema.n_children < 0 {
		return nil, fmt.Errorf("negative child count %d in the Arrow schema", schema.n_children)
	}
	formats := make([]string, int(schema.n_children))
	for index := range formats {
		child := C.kivgraph_schema_child(schema, C.int64_t(index))
		if child == nil || child.format == nil {
			return nil, fmt.Errorf("no format for Arrow schema column %d", index)
		}
		formats[index] = C.GoString(child.format)
	}
	return formats, nil
}

func arrowQueryError(result *C.lbug_query_result, operation string) error {
	message := C.lbug_query_result_get_error_message(result)
	if message == nil {
		return fmt.Errorf("%s failed", operation)
	}
	text := C.GoString(message)
	C.lbug_destroy_string(message)
	return fmt.Errorf("%s failed: %s", operation, text)
}

func arrowInterruptGuard(ctx context.Context, connection *C.lbug_connection) func() {
	finished := make(chan struct{})
	stopped := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(stopped)
		select {
		case <-ctx.Done():
			C.lbug_connection_interrupt(connection)
		case <-finished:
		}
	}()
	return func() {
		once.Do(func() {
			close(finished)
			<-stopped
		})
	}
}

// canonicalArrowScan opens the graph and runs every canonical query on one
// connection.
//
// The database and connection handles are locals, never fields of a Go struct
// that also holds Go pointers: cgo refuses a pointer into an allocation that
// contains unpinned Go pointers, and a session object would smuggle the
// cancel function into the same block.
func canonicalArrowScan(ctx context.Context, path string, run func(query arrowQueryFunc) error) error {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	config := C.lbug_default_system_config()
	config.enable_compression = C.bool(true)
	config.read_only = C.bool(true)
	config.buffer_pool_size = C.uint64_t(scanBufferPoolBytes(path))

	var database C.lbug_database
	if status := C.lbug_database_init(cpath, config, &database); status != 0 {
		return fmt.Errorf("open Arrow scan database: status %d", status)
	}
	defer C.lbug_database_destroy(&database)

	var connection C.lbug_connection
	if status := C.lbug_connection_init(&database, &connection); status != 0 {
		return fmt.Errorf("open Arrow scan connection: status %d", status)
	}
	defer C.lbug_connection_destroy(&connection)

	scanCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := arrowInterruptGuard(scanCtx, &connection)
	defer stop()

	return run(func(query string, formats []string, consume func([]arrowColumn, int64) error) error {
		return scanArrowQuery(scanCtx, &connection, query, formats, consume)
	})
}

// arrowBoolColumn reads a BOOL column, which Arrow packs one bit per row
// rather than one byte.
type arrowBoolColumn struct {
	column   arrowColumn
	values   unsafe.Pointer
	validity unsafe.Pointer
}

func newArrowBoolColumn(column arrowColumn) (arrowBoolColumn, error) {
	array := column.array
	if array == nil {
		return arrowBoolColumn{}, fmt.Errorf("nil Arrow bool column")
	}
	if column.format != arrowBool {
		return arrowBoolColumn{}, fmt.Errorf("unsupported Arrow bool format %q", column.format)
	}
	if array.n_buffers < 2 {
		return arrowBoolColumn{}, fmt.Errorf("unexpected Arrow bool column with %d buffers, want at least 2", array.n_buffers)
	}
	values := C.kivgraph_array_buffer(array, 1)
	if values == nil {
		return arrowBoolColumn{}, fmt.Errorf("nil values buffer in the Arrow bool column")
	}
	var validity unsafe.Pointer
	if array.null_count != 0 {
		validity = C.kivgraph_array_buffer(array, 0)
		if validity == nil {
			return arrowBoolColumn{}, fmt.Errorf("missing validity buffer for the Arrow bool column with null count %d", array.null_count)
		}
	}
	return arrowBoolColumn{column: column, values: values, validity: validity}, nil
}

// valueAt answers one row. A NULL is false, which is what the value round
// trips from: canonical_load.go renders every BOOL unconditionally, so a
// stored NULL can only come from a row nothing wrote.
func (column *arrowBoolColumn) valueAt(row int64) (bool, error) {
	array := column.column.array
	if row < 0 || row >= int64(array.length) {
		return false, fmt.Errorf("out-of-range Arrow row %d, length %d", row, array.length)
	}
	index := int64(array.offset) + row
	if column.validity != nil && !arrowBitSet(column.validity, index) {
		return false, nil
	}
	return arrowBitSet(column.values, index), nil
}
