# Fixtures del protocolo Go–TypeScript, versión 1

Archivo generado. No se edita a mano: lo produce
`go test ./internal/tsworker -args -update-fixtures` y un test falla si diverge.

Cada `.bin` es un frame literal del cable, tal y como viaja por el pipe.
Los primeros bytes son el prefijo de longitud y no son texto imprimible,
por lo que un editor los trata como binarios.

- Protocolo: `ladygraph-ts-worker`, versión `1`
- Prefijo: 4 bytes, big-endian, cuenta solo el cuerpo
- Cuerpo máximo: 16777216 bytes
- Especificación: `docs/protocol/ts-worker-v1.md`

## Resumen

| Archivo | Resultado | Código | Fatal |
| --- | --- | --- | --- |
| `hello_request.bin` | ok | `-` | false |
| `facts_event.bin` | ok | `-` | false |
| `error_response.bin` | ok | `-` | false |
| `empty_body.bin` | error | `FRAME_EMPTY` | true |
| `oversized_length.bin` | error | `FRAME_TOO_LARGE` | true |
| `truncated_body.bin` | error | `FRAME_TRUNCATED` | true |
| `invalid_json.bin` | error | `INVALID_PAYLOAD` | false |
| `foreign_version.bin` | error | `VERSION_MISMATCH` | true |

## hello_request.bin

Petición de handshake escrita por el supervisor.

- Tamaño del archivo: 102 bytes
- Longitud declarada: 98
- Bytes de cuerpo presentes: 98
- Esperado: el lector decodifica el sobre

```text
00000000  00 00 00 62 7b 22 76 22  3a 31 2c 22 69 64 22 3a  |...b{"v":1,"id":|
00000010  31 2c 22 74 79 70 65 22  3a 22 48 45 4c 4c 4f 22  |1,"type":"HELLO"|
00000020  2c 22 70 61 79 6c 6f 61  64 22 3a 7b 22 70 72 6f  |,"payload":{"pro|
00000030  74 6f 63 6f 6c 5f 76 65  72 73 69 6f 6e 73 22 3a  |tocol_versions":|
00000040  5b 31 5d 2c 22 73 75 70  65 72 76 69 73 6f 72 5f  |[1],"supervisor_|
00000050  76 65 72 73 69 6f 6e 22  3a 22 30 2e 31 2e 30 2d  |version":"0.1.0-|
00000060  64 65 76 22 7d 7d                                 |dev"}}|
```

Cuerpo como texto:

```text
{"v":1,"id":1,"type":"HELLO","payload":{"protocol_versions":[1],"supervisor_version":"0.1.0-dev"}}
```

## facts_event.bin

Evento iniciado por el worker; los eventos usan id cero.

- Tamaño del archivo: 142 bytes
- Longitud declarada: 138
- Bytes de cuerpo presentes: 138
- Esperado: el lector decodifica el sobre

```text
00000000  00 00 00 8a 7b 22 76 22  3a 31 2c 22 69 64 22 3a  |....{"v":1,"id":|
00000010  30 2c 22 74 79 70 65 22  3a 22 46 41 43 54 53 22  |0,"type":"FACTS"|
00000020  2c 22 70 61 79 6c 6f 61  64 22 3a 7b 22 66 61 63  |,"payload":{"fac|
00000030  74 73 22 3a 5b 5d 2c 22  66 69 6c 65 22 3a 22 73  |ts":[],"file":"s|
00000040  72 63 2f 69 6e 64 65 78  2e 74 73 22 2c 22 66 69  |rc/index.ts","fi|
00000050  6e 61 6c 22 3a 74 72 75  65 2c 22 70 72 6f 6a 65  |nal":true,"proje|
00000060  63 74 5f 69 64 22 3a 22  72 65 70 6f 2d 61 3a 74  |ct_id":"repo-a:t|
00000070  73 63 6f 6e 66 69 67 2e  6a 73 6f 6e 22 2c 22 72  |sconfig.json","r|
00000080  65 71 75 65 73 74 5f 69  64 22 3a 34 7d 7d        |equest_id":4}}|
```

Cuerpo como texto:

```text
{"v":1,"id":0,"type":"FACTS","payload":{"facts":[],"file":"src/index.ts","final":true,"project_id":"repo-a:tsconfig.json","request_id":4}}
```

## error_response.bin

Respuesta de error clasificada, con código de protocolo.

- Tamaño del archivo: 120 bytes
- Longitud declarada: 116
- Bytes de cuerpo presentes: 116
- Esperado: el lector decodifica el sobre

```text
00000000  00 00 00 74 7b 22 76 22  3a 31 2c 22 69 64 22 3a  |...t{"v":1,"id":|
00000010  34 2c 22 74 79 70 65 22  3a 22 45 52 52 4f 52 22  |4,"type":"ERROR"|
00000020  2c 22 70 61 79 6c 6f 61  64 22 3a 7b 22 63 6f 64  |,"payload":{"cod|
00000030  65 22 3a 22 55 4e 4b 4e  4f 57 4e 5f 50 52 4f 4a  |e":"UNKNOWN_PROJ|
00000040  45 43 54 22 2c 22 6d 65  73 73 61 67 65 22 3a 22  |ECT","message":"|
00000050  70 72 6f 6a 65 63 74 20  69 73 20 6e 6f 74 20 6f  |project is not o|
00000060  70 65 6e 22 2c 22 72 65  74 72 79 61 62 6c 65 22  |pen","retryable"|
00000070  3a 66 61 6c 73 65 7d 7d                           |:false}}|
```

Cuerpo como texto:

```text
{"v":1,"id":4,"type":"ERROR","payload":{"code":"UNKNOWN_PROJECT","message":"project is not open","retryable":false}}
```

## empty_body.bin

Prefijo de longitud cero; el protocolo prohíbe cuerpos vacíos.

- Tamaño del archivo: 4 bytes
- Longitud declarada: 0
- Bytes de cuerpo presentes: 0
- Esperado: error `FRAME_EMPTY`, sesión terminada

```text
00000000  00 00 00 00                                       |....|
```

## oversized_length.bin

Prefijo por encima del límite de 16 MiB; debe rechazarse antes de asignar memoria.

- Tamaño del archivo: 4 bytes
- Longitud declarada: 16777217
- Bytes de cuerpo presentes: 0
- Esperado: error `FRAME_TOO_LARGE`, sesión terminada

```text
00000000  01 00 00 01                                       |....|
```

## truncated_body.bin

La cabecera anuncia más bytes de los que entrega el flujo.

- Tamaño del archivo: 49 bytes
- Longitud declarada: 47
- Bytes de cuerpo presentes: 45
- Esperado: error `FRAME_TRUNCATED`, sesión terminada

```text
00000000  00 00 00 2f 7b 22 76 22  3a 31 2c 22 69 64 22 3a  |.../{"v":1,"id":|
00000010  32 2c 22 74 79 70 65 22  3a 22 47 45 54 5f 53 54  |2,"type":"GET_ST|
00000020  41 54 55 53 22 2c 22 70  61 79 6c 6f 61 64 22 3a  |ATUS","payload":|
00000030  7b                                                |{|
```

Cuerpo como texto:

```text
{"v":1,"id":2,"type":"GET_STATUS","payload":{
```

## invalid_json.bin

El frame está bien delimitado pero el cuerpo no es JSON válido; recuperable.

- Tamaño del archivo: 32 bytes
- Longitud declarada: 28
- Bytes de cuerpo presentes: 28
- Esperado: error `INVALID_PAYLOAD`, sesión conservada

```text
00000000  00 00 00 1c 7b 22 76 22  3a 31 2c 22 69 64 22 3a  |....{"v":1,"id":|
00000010  33 2c 22 74 79 70 65 22  3a 22 48 45 4c 4c 4f 22  |3,"type":"HELLO"|
```

Cuerpo como texto:

```text
{"v":1,"id":3,"type":"HELLO"
```

## foreign_version.bin

El sobre declara una versión de protocolo no soportada.

- Tamaño del archivo: 46 bytes
- Longitud declarada: 42
- Bytes de cuerpo presentes: 42
- Esperado: error `VERSION_MISMATCH`, sesión terminada

```text
00000000  00 00 00 2a 7b 22 76 22  3a 32 2c 22 69 64 22 3a  |...*{"v":2,"id":|
00000010  33 2c 22 74 79 70 65 22  3a 22 48 45 4c 4c 4f 22  |3,"type":"HELLO"|
00000020  2c 22 70 61 79 6c 6f 61  64 22 3a 7b 7d 7d        |,"payload":{}}|
```

Cuerpo como texto:

```text
{"v":2,"id":3,"type":"HELLO","payload":{}}
```
