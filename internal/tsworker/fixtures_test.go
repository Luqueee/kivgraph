package tsworker

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// updateFixtures regenerates the shared wire fixtures consumed by the
// TypeScript framing implementation in LUQUE-0603.
var updateFixtures = flag.Bool("update-fixtures", false, "rewrite the shared protocol fixtures")

const fixtureDirectory = "../../testdata/protocol/ts-worker-v1"

type fixtureCase struct {
	Name        string `json:"name"`
	File        string `json:"file"`
	Description string `json:"description"`
	Expect      string `json:"expect"`
	ErrorCode   string `json:"error_code,omitempty"`
	Fatal       bool   `json:"fatal"`
	Envelope    *struct {
		V    int    `json:"v"`
		ID   uint64 `json:"id"`
		Type string `json:"type"`
	} `json:"envelope,omitempty"`
	Canonical bool `json:"canonical_encoding"`
}

type fixtureManifest struct {
	Protocol      string        `json:"protocol"`
	Version       int           `json:"version"`
	ByteOrder     string        `json:"byte_order"`
	HeaderBytes   int           `json:"header_bytes"`
	MaxFrameBytes int           `json:"max_frame_bytes"`
	Cases         []fixtureCase `json:"cases"`
}

func rawFrame(body string) []byte {
	frame := make([]byte, frameHeaderBytes+len(body))
	binary.BigEndian.PutUint32(frame[:frameHeaderBytes], uint32(len(body)))
	copy(frame[frameHeaderBytes:], body)
	return frame
}

func encodedFrame(t *testing.T, id uint64, messageType string, payload any) []byte {
	t.Helper()
	sink := &bytes.Buffer{}
	if err := NewWriter(sink).WriteFrame(mustEnvelope(t, id, messageType, payload)); err != nil {
		t.Fatalf("WriteFrame() error = %v", err)
	}
	return sink.Bytes()
}

func buildFixtures(t *testing.T) (fixtureManifest, map[string][]byte) {
	t.Helper()
	bodies := map[string][]byte{}
	manifest := fixtureManifest{
		Protocol:      "luque-ts-worker",
		Version:       ProtocolVersion,
		ByteOrder:     "big-endian",
		HeaderBytes:   frameHeaderBytes,
		MaxFrameBytes: MaxFrameBytes,
	}

	appendCase := func(entry fixtureCase, frame []byte) {
		bodies[entry.File] = frame
		manifest.Cases = append(manifest.Cases, entry)
	}

	helloEnvelope := struct {
		V    int    `json:"v"`
		ID   uint64 `json:"id"`
		Type string `json:"type"`
	}{ProtocolVersion, 1, "HELLO"}
	appendCase(fixtureCase{
		Name:        "hello_request",
		File:        "hello_request.bin",
		Description: "Handshake request written by the supervisor.",
		Expect:      "ok",
		Envelope:    &helloEnvelope,
		Canonical:   true,
	}, encodedFrame(t, 1, "HELLO", map[string]any{
		"protocol_versions":  []int{ProtocolVersion},
		"supervisor_version": "0.1.0-dev",
	}))

	factsEnvelope := struct {
		V    int    `json:"v"`
		ID   uint64 `json:"id"`
		Type string `json:"type"`
	}{ProtocolVersion, 0, "FACTS"}
	appendCase(fixtureCase{
		Name:        "facts_event",
		File:        "facts_event.bin",
		Description: "Worker-initiated event; events use id zero.",
		Expect:      "ok",
		Envelope:    &factsEnvelope,
		Canonical:   true,
	}, encodedFrame(t, 0, "FACTS", map[string]any{
		"request_id": 4,
		"project_id": "repo-a:tsconfig.json",
		"file":       "src/index.ts",
		"facts":      []any{},
		"final":      true,
	}))

	errorEnvelope := struct {
		V    int    `json:"v"`
		ID   uint64 `json:"id"`
		Type string `json:"type"`
	}{ProtocolVersion, 4, "ERROR"}
	appendCase(fixtureCase{
		Name:        "error_response",
		File:        "error_response.bin",
		Description: "Classified error response carrying a protocol code.",
		Expect:      "ok",
		Envelope:    &errorEnvelope,
		Canonical:   true,
	}, encodedFrame(t, 4, "ERROR", map[string]any{
		"code":      "UNKNOWN_PROJECT",
		"message":   "project is not open",
		"retryable": false,
	}))

	appendCase(fixtureCase{
		Name:        "empty_body",
		File:        "empty_body.bin",
		Description: "Zero length prefix; the protocol forbids empty bodies.",
		Expect:      "error",
		ErrorCode:   string(FrameEmpty),
		Fatal:       true,
	}, make([]byte, frameHeaderBytes))

	oversized := make([]byte, frameHeaderBytes)
	binary.BigEndian.PutUint32(oversized, MaxFrameBytes+1)
	appendCase(fixtureCase{
		Name:        "oversized_length",
		File:        "oversized_length.bin",
		Description: "Length prefix above the 16 MiB limit; must be rejected before allocating.",
		Expect:      "error",
		ErrorCode:   string(FrameTooLarge),
		Fatal:       true,
	}, oversized)

	complete := encodedFrame(t, 2, "GET_STATUS", map[string]any{})
	appendCase(fixtureCase{
		Name:        "truncated_body",
		File:        "truncated_body.bin",
		Description: "Header announces more bytes than the stream provides.",
		Expect:      "error",
		ErrorCode:   string(FrameTruncated),
		Fatal:       true,
	}, complete[:len(complete)-2])

	appendCase(fixtureCase{
		Name:        "invalid_json",
		File:        "invalid_json.bin",
		Description: "Frame boundary is correct but the body is not valid JSON; recoverable.",
		Expect:      "error",
		ErrorCode:   string(InvalidPayload),
		Fatal:       false,
	}, rawFrame(`{"v":1,"id":3,"type":"HELLO"`))

	appendCase(fixtureCase{
		Name:        "foreign_version",
		File:        "foreign_version.bin",
		Description: "Envelope declares an unsupported protocol version.",
		Expect:      "error",
		ErrorCode:   string(VersionMismatch),
		Fatal:       true,
	}, rawFrame(`{"v":2,"id":3,"type":"HELLO","payload":{}}`))

	return manifest, bodies
}

func TestSharedFixturesMatchTheGoImplementation(t *testing.T) {
	manifest, bodies := buildFixtures(t)

	if *updateFixtures {
		if err := os.MkdirAll(fixtureDirectory, 0o750); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		for name, frame := range bodies {
			if err := os.WriteFile(filepath.Join(fixtureDirectory, name), frame, 0o600); err != nil {
				t.Fatalf("WriteFile(%s) error = %v", name, err)
			}
		}
		encoded, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			t.Fatalf("MarshalIndent() error = %v", err)
		}
		if err := os.WriteFile(filepath.Join(fixtureDirectory, "manifest.json"), append(encoded, '\n'), 0o600); err != nil {
			t.Fatalf("WriteFile(manifest) error = %v", err)
		}
	}

	stored, err := os.ReadFile(filepath.Join(fixtureDirectory, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest error = %v; regenerate with -update-fixtures", err)
	}
	var storedManifest fixtureManifest
	if err := json.Unmarshal(stored, &storedManifest); err != nil {
		t.Fatalf("decode manifest error = %v", err)
	}
	if storedManifest.Version != ProtocolVersion || storedManifest.ByteOrder != "big-endian" || storedManifest.MaxFrameBytes != MaxFrameBytes {
		t.Fatalf("stored manifest header = %#v", storedManifest)
	}
	if len(storedManifest.Cases) != len(manifest.Cases) {
		t.Fatalf("stored manifest has %d cases, want %d; regenerate with -update-fixtures", len(storedManifest.Cases), len(manifest.Cases))
	}

	for _, entry := range storedManifest.Cases {
		t.Run(entry.Name, func(t *testing.T) {
			frame, err := os.ReadFile(filepath.Join(fixtureDirectory, entry.File))
			if err != nil {
				t.Fatalf("read fixture error = %v", err)
			}
			// Bytes on disk are the contract shared with the TypeScript side.
			if expected, ok := bodies[entry.File]; ok && !bytes.Equal(frame, expected) {
				t.Fatalf("fixture %s drifted from the Go encoder", entry.File)
			}

			envelope, err := NewReader(bytes.NewReader(frame)).ReadFrame(context.Background())
			if entry.Expect == "ok" {
				if err != nil {
					t.Fatalf("ReadFrame() error = %v", err)
				}
				if entry.Envelope == nil || envelope.V != entry.Envelope.V || envelope.ID != entry.Envelope.ID || envelope.Type != entry.Envelope.Type {
					t.Fatalf("envelope = %#v, manifest = %#v", envelope, entry.Envelope)
				}
				if entry.Canonical {
					sink := &bytes.Buffer{}
					if err := NewWriter(sink).WriteFrame(envelope); err != nil {
						t.Fatalf("re-encode error = %v", err)
					}
					if !bytes.Equal(sink.Bytes(), frame) {
						t.Fatal("re-encoding a canonical fixture did not reproduce the same bytes")
					}
				}
				return
			}

			var framing *FramingError
			if !errors.As(err, &framing) {
				t.Fatalf("error %v is not a *FramingError", err)
			}
			if string(framing.Kind) != entry.ErrorCode {
				t.Fatalf("error code = %s, want %s", framing.Kind, entry.ErrorCode)
			}
			if framing.Fatal() != entry.Fatal {
				t.Fatalf("fatal = %v, want %v", framing.Fatal(), entry.Fatal)
			}
		})
	}
}
