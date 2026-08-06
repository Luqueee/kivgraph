# ADR 0002: Núcleo de Ladygraph en Go

- **Estado:** aceptada
- **Fecha:** 2026-08-04

## Context

Ladygraph debe ejecutar un servidor MCP local, coordinar el indexado, gestionar el
almacenamiento canónico y responder consultas de baja latencia. El proceso
principal necesita buen control de concurrencia, distribución sencilla como
binario y acceso a las APIs oficiales de LadybugDB y `go/packages`.

## Decision

El núcleo de Ladygraph se implementa en Go. El binario principal será responsable
del ciclo de vida del servidor, la configuración, la persistencia, el índice
incremental, el HotSnapshot y la superficie MCP. El análisis semántico
TypeScript se delega al worker definido en ADR 0005.

Los paquetes internos se organizan por responsabilidad y se mantienen bajo
`internal/` hasta que exista una necesidad explícita de API pública.

## Alternatives

- **Node.js/TypeScript como proceso principal:** facilitaría reutilizar la API
  del compilador TypeScript, pero complica el control del almacenamiento nativo,
  la distribución del binario y el fast path de consultas.
- **Rust:** ofrece control de recursos y rendimiento, pero no es la tecnología
  fijada por el plan ni aporta una ventaja necesaria frente a las APIs Go
  requeridas.
- **Dos procesos principales simétricos:** aumentaría la complejidad de ciclo
  de vida y protocolo sin mejorar la autoridad semántica.

## Consequences

- El ejecutable principal puede distribuirse como binario nativo.
- La concurrencia, cancelación y límites de recursos se controlan desde un solo
  proceso coordinador.
- LadybugDB y `go/packages` se integran sin un adaptador de lenguaje adicional.
- El worker TypeScript requiere un protocolo explícito y supervisión desde Go.
- Las convenciones Go, `go test` y `go vet` forman parte del contrato de cada
  tarea.

## Risks

- Un error en el núcleo puede afectar simultáneamente al indexado y a las
  consultas; las capas deberán conservar límites de responsabilidad claros.
- El uso de APIs nativas debe probarse en cada plataforma soportada.
- La presión por reutilizar tipos internos como API pública puede introducir
  compatibilidad accidental.

## Status

Aceptada. Las decisiones de almacenamiento, worker y superficie MCP se
concretan en los ADR siguientes.
