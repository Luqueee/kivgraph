# ADR 0005: Worker TypeScript persistente

- **Estado:** aceptada
- **Fecha:** 2026-08-04

## Context

La identidad semántica de TypeScript debe resolverse con la TypeScript
Compiler API y respetando la versión del compilador de cada proyecto. Crear un
programa, un `TypeChecker` o resolver módulos desde cada petición MCP sería
costoso y rompería el fast path. El núcleo Go sigue siendo el coordinador y la
superficie MCP.

## Decision

El análisis TypeScript se ejecutará en un worker Node.js persistente,
supervisado por Go. El worker cargará los proyectos y sus versiones de
TypeScript, conservará el estado semántico mientras sea válido y comunicará
hechos estructurados mediante un protocolo de framing length-prefixed JSON
sobre stdin/stdout.

Los mensajes de protocolo ocupan stdout exclusivamente. Los logs y diagnósticos
operativos van a stderr. El supervisor debe detectar EOF, frames inválidos,
timeouts, salida inesperada y versiones incompatibles; cada caso se clasifica y
puede invalidar el estado del worker sin contaminar el grafo con hechos
incompletos.

La TypeScript Compiler API es la autoridad semántica para símbolos,
referencias, exports, reexports y resolución de módulos. Tree-sitter no puede
sustituirla.

## Alternatives

- **Crear un proceso TypeScript por repositorio o petición:** aislaría fallos,
  pero añade latencia, consumo y trabajo de inicialización repetido.
- **Ejecutar TypeScript dentro del binario Go:** no es compatible con el runtime
  de la Compiler API y mezclaría ciclos de vida incompatibles.
- **Usar un LSP externo:** añade un servicio no controlado y no garantiza la
  semántica ni la versión exacta del proyecto analizado.

## Consequences

- El repositorio contiene un componente TypeScript con su propio gestor,
  lockfile, tests y typecheck.
- El framing y los tipos de mensajes son contratos versionados.
- El supervisor Go debe implementar backpressure, cancelación, reinicio y
  limpieza de procesos.
- La agrupación de proyectos compatibles puede reducir workers, pero no puede
  mezclar versiones incompatibles silenciosamente.
- Los hechos devueltos deben incluir procedencia suficiente para auditoría y
  estados `EXACT`, `CANDIDATE` o `UNRESOLVED`.

## Risks

- Fugas de memoria del Language Service pueden acumularse en workers de larga
  duración; se medirán y se definirán límites de reciclado.
- Un cambio de versión de TypeScript puede cambiar la resolución; la versión
  efectiva forma parte del registro del proyecto.
- Un frame truncado o un worker muerto durante un delta puede dejar hechos
  incompletos; el lote debe ser transaccional desde el punto de vista del
  índice.

## Status

Aceptada. El protocolo inicial y el supervisor se implementarán después de la
fase de aceleración sintáctica.
