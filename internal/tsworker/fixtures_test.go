package tsworker

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
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
		Protocol:      "ladygraph-ts-worker",
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
		Description: "Petición de handshake escrita por el supervisor.",
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
		Description: "Evento iniciado por el worker; los eventos usan id cero.",
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
		Description: "Respuesta de error clasificada, con código de protocolo.",
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
		Description: "Prefijo de longitud cero; el protocolo prohíbe cuerpos vacíos.",
		Expect:      "error",
		ErrorCode:   string(FrameEmpty),
		Fatal:       true,
	}, make([]byte, frameHeaderBytes))

	oversized := make([]byte, frameHeaderBytes)
	binary.BigEndian.PutUint32(oversized, MaxFrameBytes+1)
	appendCase(fixtureCase{
		Name:        "oversized_length",
		File:        "oversized_length.bin",
		Description: "Prefijo por encima del límite de 16 MiB; debe rechazarse antes de asignar memoria.",
		Expect:      "error",
		ErrorCode:   string(FrameTooLarge),
		Fatal:       true,
	}, oversized)

	complete := encodedFrame(t, 2, "GET_STATUS", map[string]any{})
	appendCase(fixtureCase{
		Name:        "truncated_body",
		File:        "truncated_body.bin",
		Description: "La cabecera anuncia más bytes de los que entrega el flujo.",
		Expect:      "error",
		ErrorCode:   string(FrameTruncated),
		Fatal:       true,
	}, complete[:len(complete)-2])

	appendCase(fixtureCase{
		Name:        "invalid_json",
		File:        "invalid_json.bin",
		Description: "El frame está bien delimitado pero el cuerpo no es JSON válido; recuperable.",
		Expect:      "error",
		ErrorCode:   string(InvalidPayload),
		Fatal:       false,
	}, rawFrame(`{"v":1,"id":3,"type":"HELLO"`))

	appendCase(fixtureCase{
		Name:        "foreign_version",
		File:        "foreign_version.bin",
		Description: "El sobre declara una versión de protocolo no soportada.",
		Expect:      "error",
		ErrorCode:   string(VersionMismatch),
		Fatal:       true,
	}, rawFrame(`{"v":2,"id":3,"type":"HELLO","payload":{}}`))

	return manifest, bodies
}

// buildFixtureReadme renders a human-readable view of the wire fixtures so the
// TypeScript implementation can be written without a hex viewer.
func buildFixtureReadme(manifest fixtureManifest, bodies map[string][]byte) string {
	var out strings.Builder
	out.WriteString("# Fixtures del protocolo Go–TypeScript, versión 1\n\n")
	out.WriteString("Archivo generado. No se edita a mano: lo produce\n")
	out.WriteString("`go test ./internal/tsworker -args -update-fixtures` y un test falla si diverge.\n\n")
	out.WriteString("Cada `.bin` es un frame literal del cable, tal y como viaja por el pipe.\n")
	out.WriteString("Los primeros bytes son el prefijo de longitud y no son texto imprimible,\n")
	out.WriteString("por lo que un editor los trata como binarios.\n\n")
	fmt.Fprintf(&out, "- Protocolo: `%s`, versión `%d`\n", manifest.Protocol, manifest.Version)
	fmt.Fprintf(&out, "- Prefijo: %d bytes, %s, cuenta solo el cuerpo\n", manifest.HeaderBytes, manifest.ByteOrder)
	fmt.Fprintf(&out, "- Cuerpo máximo: %d bytes\n", manifest.MaxFrameBytes)
	out.WriteString("- Especificación: `docs/protocol/ts-worker-v1.md`\n\n")

	out.WriteString("## Resumen\n\n")
	out.WriteString("| Archivo | Resultado | Código | Fatal |\n")
	out.WriteString("| --- | --- | --- | --- |\n")
	for _, entry := range manifest.Cases {
		code := entry.ErrorCode
		if code == "" {
			code = "-"
		}
		fmt.Fprintf(&out, "| `%s` | %s | `%s` | %t |\n", entry.File, entry.Expect, code, entry.Fatal)
	}

	for _, entry := range manifest.Cases {
		frame := bodies[entry.File]
		fmt.Fprintf(&out, "\n## %s\n\n", entry.File)
		fmt.Fprintf(&out, "%s\n\n", entry.Description)
		declared := "sin prefijo completo"
		if len(frame) >= frameHeaderBytes {
			declared = fmt.Sprintf("%d", binary.BigEndian.Uint32(frame[:frameHeaderBytes]))
		}
		fmt.Fprintf(&out, "- Tamaño del archivo: %d bytes\n", len(frame))
		fmt.Fprintf(&out, "- Longitud declarada: %s\n", declared)
		fmt.Fprintf(&out, "- Bytes de cuerpo presentes: %d\n", max(0, len(frame)-frameHeaderBytes))
		if entry.Expect == "ok" {
			out.WriteString("- Esperado: el lector decodifica el sobre\n")
		} else {
			fmt.Fprintf(&out, "- Esperado: error `%s`, sesión %s\n", entry.ErrorCode, sessionOutcome(entry.Fatal))
		}
		out.WriteString("\n```text\n")
		out.WriteString(hexDump(frame))
		out.WriteString("```\n")
		if body := frame[min(len(frame), frameHeaderBytes):]; len(body) != 0 && utf8.Valid(body) {
			out.WriteString("\nCuerpo como texto:\n\n```text\n")
			out.Write(body)
			out.WriteString("\n```\n")
		}
	}
	return out.String()
}

func sessionOutcome(fatal bool) string {
	if fatal {
		return "terminada"
	}
	return "conservada"
}

// hexDump renders offset, hexadecimal bytes and printable ASCII per line.
func hexDump(frame []byte) string {
	var out strings.Builder
	for offset := 0; offset < len(frame); offset += 16 {
		end := min(offset+16, len(frame))
		chunk := frame[offset:end]
		fmt.Fprintf(&out, "%08x  ", offset)
		for index := 0; index < 16; index++ {
			if index < len(chunk) {
				fmt.Fprintf(&out, "%02x ", chunk[index])
			} else {
				out.WriteString("   ")
			}
			if index == 7 {
				out.WriteByte(' ')
			}
		}
		out.WriteString(" |")
		for _, value := range chunk {
			if value >= 0x20 && value < 0x7f {
				out.WriteByte(value)
			} else {
				out.WriteByte('.')
			}
		}
		out.WriteString("|\n")
	}
	return out.String()
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
		if err := os.WriteFile(filepath.Join(fixtureDirectory, "README.md"), []byte(buildFixtureReadme(manifest, bodies)), 0o600); err != nil {
			t.Fatalf("WriteFile(README) error = %v", err)
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

	storedReadme, err := os.ReadFile(filepath.Join(fixtureDirectory, "README.md"))
	if err != nil {
		t.Fatalf("read README error = %v; regenerate with -update-fixtures", err)
	}
	if string(storedReadme) != buildFixtureReadme(manifest, bodies) {
		t.Fatal("README.md drifted from the fixtures; regenerate with -update-fixtures")
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
