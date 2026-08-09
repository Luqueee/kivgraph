# ADR 0008: `index_project` como mutación MCP con consentimiento

**Estado:** aceptada  
**Fecha:** 2026-08-09

## Contexto

Ladygraph expone consultas MCP sobre el `HotSnapshot` publicado. Registrar un
proyecto y reconstruir el grafo cambia el registro persistente y puede publicar
una nueva generación, por lo que no debe estar disponible como efecto lateral
de una consulta ni ejecutarse sin autorización explícita del cliente.

El comando `serve` ya carga la configuración y el snapshot publicado. La misma
instancia debe ser la dueña del ciclo de vida del indexador para que las
mutaciones y las consultas usen el mismo registro y almacén de snapshots.

## Decisión

Se añade la herramienta `index_project` únicamente a los servidores MCP
construidos con un `indexing.ProjectIndexer`. Los constructores de servidor sin
indexador conservan una superficie de solo lectura.

La entrada es:

```json
{
  "name": "nombre-del-proyecto",
  "path": "/ruta/al/proyecto",
  "languages": ["go"],
  "confirmed": true
}
```

`name`, `path` y `languages` se validan antes de modificar el registro. El
nombre no puede contener separadores de ruta, la ruta se resuelve a absoluta y
debe ser un directorio existente, y los lenguajes admitidos son `go`,
`typescript`, `javascript`, `ts` y `js`, sin duplicados.

`confirmed` es opcional para permitir la negociación de consentimiento MCP.
El flujo es:

1. Si el cliente anuncia `elicitation`, el servidor solicita aprobación con el
   nombre y la ruta del proyecto. Solo `accept` con `confirmed: true` permite
   continuar; `decline`, `cancel` o un error son rechazos.
2. Sin `elicitation`, el cliente debe enviar `confirmed: true` explícitamente.
3. Cualquier otra entrada termina con `PERMISSION_REQUIRED` o
   `PERMISSION_DENIED` y el indexador no se invoca.

La herramienta está anotada como no solo lectura y destructiva, con una
confirmación de usuario requerida. La configuración persistente de Oh My Pi
refuerza la aprobación con `tools.approval.ladygraph_1mcp_index_project: prompt`.

El servicio `internal/indexing.Service` serializa las mutaciones, agrega el
proyecto a una copia del registro, persiste la candidata, ejecuta el flujo de
indexación y rebuild completo, y publica el `HotSnapshot` resultante. Si el
indexado o el rebuild fallan, restaura el registro anterior y deja activa la
generación anterior. La publicación del snapshot ya validado se realiza después
del rebuild. Un error de publicación conserva la candidata para permitir una
reintentación sin reindexar.

`cmd/ladygraph serve` construye este servicio con la configuración cargada y lo
inyecta en el servidor MCP. El transporte continúa siendo STDIO y el cierre
graceful sigue perteneciendo al contexto compartido del comando.

## Alternativas descartadas

- Exponer `index_project` en todos los servidores: permitiría registrar
  proyectos sin una configuración persistente ni un almacén de snapshot válido.
- Ejecutar el indexado desde el handler MCP sin servicio: duplicaría la
  orquestación del CLI y dificultaría serialización, rollback y pruebas.
- Aceptar coincidencias nominales como aristas exactas: contradice las
  invariantes canónicas del grafo; el rebuild sigue siendo la única fuente de
  hechos.

## Consecuencias

- Los clientes MCP que no soporten elicitation deben mostrar su propia
  confirmación y enviar `confirmed: true` solo después de obtenerla.
- Cada operación aceptada puede tardar lo mismo que un indexado y rebuild
  completo; no se añade una ruta incremental incompleta.
- Los servidores creados con `NewServer` siguen siendo compatibles y no
  anuncian la mutación.
