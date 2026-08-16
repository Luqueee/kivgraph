package webapi

import (
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

func TestHandlerTilesBinaryPayloadAndSnapshotValidation(t *testing.T) {
	handler := NewHandler(hotsnapshot.NewSnapshotStore(testSnapshot(t)))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/tiles?min_x=0&min_y=0&max_x=100000&max_y=100000&lod=3&max_nodes=100&snapshot_id=7&format_version=2", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("tiles status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	header := decodeViewerHeader(t, response.Body.Bytes())
	if header.kind != viewerPayloadTiles || header.snapshotID != 7 || header.level != 3 || header.nodeCount != 6 || header.edgeCount != 2 {
		t.Fatalf("tiles header = %#v", header)
	}
	if response.Header().Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("tiles content type = %q", response.Header().Get("Content-Type"))
	}
	if response.Header().Get("Content-Length") != stringValueForTest(len(response.Body.Bytes())) {
		t.Fatalf("tiles content length = %q, want %d", response.Header().Get("Content-Length"), len(response.Body.Bytes()))
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/tiles?min_x=0&min_y=0&max_x=100000&max_y=100000&lod=3&snapshot_id=8", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusConflict, "SNAPSHOT_MISMATCH")

	request = httptest.NewRequest(http.MethodGet, "/api/v1/tiles?min_x=0&min_y=0&max_x=100000&max_y=100000&format_version=1", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusBadRequest, "UNSUPPORTED_VERSION")
}

// A tile must name what it renders. Dense IDs are meaningless to a reader, so
// every node carries the repository name, package name, file path or
// qualified symbol name it stands for.
func TestHandlerTilesCarryReadableNodeLabels(t *testing.T) {
	handler := NewHandler(hotsnapshot.NewSnapshotStore(testSnapshot(t)))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/tiles?min_x=0&min_y=0&max_x=100000&max_y=100000&lod=3&max_nodes=100", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("tiles status = %d; body=%s", response.Code, response.Body.String())
	}

	header := decodeViewerHeader(t, response.Body.Bytes())
	if len(header.labels) != int(header.nodeCount) {
		t.Fatalf("labels = %d, want one per node (%d)", len(header.labels), header.nodeCount)
	}
	labels := make(map[string]bool, len(header.labels))
	for _, label := range header.labels {
		labels[label] = true
	}
	for _, want := range []string{"repo", "example", "src/index.go", "example.Load", "example.Loader", "example.Other"} {
		if !labels[want] {
			t.Fatalf("label %q missing from %v", want, header.labels)
		}
	}
}

func TestHandlerNeighborhoodBinaryIsInducedSubgraph(t *testing.T) {
	handler := NewHandler(hotsnapshot.NewSnapshotStore(testSnapshot(t)))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/neighborhood?stable_key=symbol-b&depth=1&direction=both&format=bin&format_version=2", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("binary neighborhood status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	header := decodeViewerHeader(t, response.Body.Bytes())
	if header.kind != viewerPayloadNeighborhood || header.snapshotID != 7 || header.nodeCount != 3 || header.edgeCount != 2 || header.flags != 0 {
		t.Fatalf("binary neighborhood header = %#v", header)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/neighborhood?stable_key=symbol-b&depth=1&direction=both", nil)
	request.Header.Set("Accept", "application/octet-stream")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("accept negotiation status/content-type = %d/%q", response.Code, response.Header().Get("Content-Type"))
	}
	decodeViewerHeader(t, response.Body.Bytes())
}

func TestHandlerRejectsOriginAndRequestSize(t *testing.T) {
	handler := NewHandler(hotsnapshot.NewSnapshotStore(testSnapshot(t)))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil)
	request.Header.Set("Origin", "http://evil.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusForbidden, "ORIGIN_NOT_ALLOWED")

	request = httptest.NewRequest(http.MethodGet, "/api/v1/meta?value="+strings.Repeat("x", maxRequestURIBytes), nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusRequestURITooLong, "REQUEST_TOO_LARGE")
}

type viewerHeaderTest struct {
	version     uint16
	kind        byte
	flags       byte
	snapshotID  uint64
	nodeCount   uint32
	edgeCount   uint32
	nodeOffset  uint32
	nodeBytes   uint32
	edgeOffset  uint32
	edgeBytes   uint32
	level       byte
	totalBytes  uint32
	snapshotVer uint32
	schemaVer   uint32
	labelOffset uint32
	labelBytes  uint32
	labels      []string
}

func TestHandlerRejectsViewerBoundsAndBodies(t *testing.T) {
	handler := NewHandler(hotsnapshot.NewSnapshotStore(testSnapshot(t)))

	request := httptest.NewRequest(http.MethodGet, "/api/v1/tiles?min_x=0&min_y=0&max_x=10&max_y=10&max_nodes=0", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusBadRequest, "INVALID_ARGUMENT")

	request = httptest.NewRequest(http.MethodGet, "/api/v1/neighborhood?stable_key=symbol-b&depth=6", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusBadRequest, "INVALID_ARGUMENT")

	request = httptest.NewRequest(http.MethodGet, "/healthz", strings.NewReader("body"))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusBadRequest, "REQUEST_BODY_NOT_ALLOWED")
}

func decodeViewerHeader(t *testing.T, payload []byte) viewerHeaderTest {
	t.Helper()
	if len(payload) < viewerBinaryHeaderSize || string(payload[:4]) != "LGVB" {
		t.Fatalf("binary payload header = %q, length=%d", payload[:minTestInt(len(payload), 4)], len(payload))
	}
	header := viewerHeaderTest{
		version:     binary.LittleEndian.Uint16(payload[4:6]),
		kind:        payload[6],
		flags:       payload[7],
		snapshotID:  binary.LittleEndian.Uint64(payload[8:16]),
		nodeCount:   binary.LittleEndian.Uint32(payload[16:20]),
		edgeCount:   binary.LittleEndian.Uint32(payload[20:24]),
		nodeOffset:  binary.LittleEndian.Uint32(payload[24:28]),
		nodeBytes:   binary.LittleEndian.Uint32(payload[28:32]),
		edgeOffset:  binary.LittleEndian.Uint32(payload[32:36]),
		edgeBytes:   binary.LittleEndian.Uint32(payload[36:40]),
		level:       payload[40],
		totalBytes:  binary.LittleEndian.Uint32(payload[44:48]),
		snapshotVer: binary.LittleEndian.Uint32(payload[48:52]),
		schemaVer:   binary.LittleEndian.Uint32(payload[52:56]),
		labelOffset: binary.LittleEndian.Uint32(payload[56:60]),
		labelBytes:  binary.LittleEndian.Uint32(payload[60:64]),
	}
	if header.version != viewerBinaryVersion || int(header.totalBytes) != len(payload) {
		t.Fatalf("binary header version/length = %d/%d, want %d/%d", header.version, header.totalBytes, viewerBinaryVersion, len(payload))
	}
	if header.nodeOffset != viewerBinaryHeaderSize || header.nodeBytes != header.nodeCount*viewerBinaryNodeSize ||
		header.edgeOffset != header.nodeOffset+header.nodeBytes || header.edgeBytes != header.edgeCount*viewerBinaryEdgeSize ||
		header.labelOffset != header.edgeOffset+header.edgeBytes {
		t.Fatalf("binary sections = %#v", header)
	}
	if uint64(header.labelOffset)+uint64(header.labelBytes) != uint64(len(payload)) {
		t.Fatalf("binary section end = %d, payload length=%d", uint64(header.labelOffset)+uint64(header.labelBytes), len(payload))
	}
	// Every node carries exactly one label, in node order.
	cursor := int(header.labelOffset)
	for index := uint32(0); index < header.nodeCount; index++ {
		if cursor+2 > len(payload) {
			t.Fatalf("label %d is truncated at %d", index, cursor)
		}
		size := int(binary.LittleEndian.Uint16(payload[cursor : cursor+2]))
		cursor += 2
		if cursor+size > len(payload) {
			t.Fatalf("label %d claims %d bytes past the payload", index, size)
		}
		header.labels = append(header.labels, string(payload[cursor:cursor+size]))
		cursor += size
	}
	if cursor != len(payload) {
		t.Fatalf("label section ends at %d, payload length=%d", cursor, len(payload))
	}
	return header
}

func stringValueForTest(value int) string {
	return strconv.Itoa(value)
}

func minTestInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
