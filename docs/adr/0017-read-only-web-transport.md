# ADR 0017: Transporte HTTP read-only para el visor web

**Estado:** aceptada
**Fecha:** 2026-08-07

## Contexto

Ladygraph expone actualmente sus nueve tools MCP únicamente por STDIO. El
servidor MCP y la configuración aceptan `StdioTransport` y `transport: stdio`,
pero un navegador no puede consumir ese canal directamente. El SDK MCP trae
transportes HTTP, aunque Ladygraph no los cablea.

El visor React necesita consultar el mismo grafo que usa MCP sin duplicar la
carga de LadybugDB ni crear una segunda fuente de hechos. La superficie MCP
está cerrada por contrato y está optimizada para clientes de herramientas, no
para entregar tiles, subgrafos inducidos o buffers binarios de renderizado.

## Decisión

1. Se añadirá un subcomando independiente `ladygraph ui --config <path>`.
   `ladygraph serve` conservará su contrato STDIO y no abrirá un puerto por
   defecto.
2. El visor usará una API HTTP read-only separada de las nueve tools MCP. La
   API leerá el `SnapshotStore` publicado y no podrá indexar, reconstruir,
   registrar repositorios, editar archivos ni mutar generaciones.
3. La dirección por defecto será `127.0.0.1:7777`. El bind a una dirección no
   loopback requerirá una opción explícita y una advertencia operacional: las
   respuestas contienen nombres, rutas y metadatos de código fuente.
   La configuración valida `web.address` como `host:port` y usa
   `127.0.0.1:7777` por defecto; `--addr` permite un override explícito. Un
   bind que no sea una IP loopback emite una advertencia porque el endpoint no
   incorpora autenticación.
4. La primera versión servirá el bundle web y la API desde el mismo proceso.
   CORS permanecerá deshabilitado por defecto; el origen esperado es el propio
   servidor.
5. La API tendrá versionado explícito (`/api/v1`) y respuestas de error con
   códigos estables. `snapshot_id` formará parte de la metadata y de cada
   respuesta que dependa del grafo.
6. El transporte tendrá endpoints específicos para `meta`, búsqueda,
   `symbol`, `neighborhood` y tiles/LOD. No se reutilizará JSON de las tools
   MCP para transportar un millón de aristas: la topología grande usará un
   formato binario versionado con `ArrayBuffer` y tipos densos.
7. El servidor rechazará métodos distintos de `GET`/`HEAD`, no expondrá
   operaciones mutantes y limitará tamaño, profundidad, nodos visitados y
   frecuencia de consultas según la configuración validada.
8. La API no afirmará datos que el `HotSnapshot` no conserva. Los campos
   canónicos ausentes de la proyección deberán obtenerse mediante una decisión
   posterior, no mediante inferencia nominal.

## Alternativas descartadas

- **Añadir tools MCP para la UI:** mezcla dos contratos, rompe la lista cerrada
  de nueve tools y obliga al navegador a implementar un cliente MCP.
- **Usar el transporte HTTP del SDK MCP sin una API específica:** resuelve el
  framing, pero no entrega subgrafos inducidos, tiles ni el formato de buffers
  que exige el render.
- **Proceso Node separado que consulte LadybugDB:** duplica carga, schema,
  lifecycle y memoria, y puede observar una generación distinta al
  `HotSnapshot` servido por Ladygraph.
- **Abrir HTTP dentro de `ladygraph serve`:** cambia el contrato de un proceso
  que actualmente es STDIO puro y complica el aislamiento de sesiones.

## Riesgos

- Un bind no loopback expone identificadores y rutas de código a la red local o
  externa si no existe una capa de autenticación delante.
- Un endpoint de viewport mal limitado podría convertir una consulta local en
  una exportación masiva de memoria.
- El contrato HTTP añade una superficie de compatibilidad que debe versionarse
  y probarse sin modificar el contrato MCP.

## Consecuencias

- Hay que ampliar `internal/config`, el CLI y el lifecycle con una sección
  HTTP/UI validada, manteniendo deshabilitada la UI por defecto.
- Hay que definir y probar límites de payload, timeout, snapshot y memoria.
- El bind loopback no es autenticación. La exposición fuera del host requiere
  una capa externa de autenticación y revisión de seguridad; no se incluirá
  implícitamente.
- El servidor tendrá dos contratos read-only: MCP para agentes y HTTP para el
  visor. Ambos deben leer el mismo `SnapshotStore` y versionarse
  independientemente.
