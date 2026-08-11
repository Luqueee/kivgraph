# ADR 0031: Lo que un proceso no usó ni midió no se informa

- **Estado:** aceptada
- **Fecha:** 2026-08-11
- **Revisa:** ADR 0013

## Contexto

Un agente indexó un monorepo de cuarenta repositorios y escribió un reporte de
fricciones. Ninguna era una respuesta equivocada sobre el grafo: todas eran el
producto describiéndose mal, que cuesta lo mismo porque también hace perder
horas.

Tres tocaban superficie observable por un cliente MCP o por quien lee la
salida de un comando:

1. `graph_status` respondía `status: "ready"` con el snapshot servido y, en la
   misma respuesta, `storage: {state: "not_configured"}`,
   `worker: {state: "not_configured"}` y todo el bloque `metrics` a cero
   -`index.duration: 0`, `index.files: 0`, `snapshot.created_at:
   "0001-01-01T00:00:00Z"`-. Los conteos reales sí venían en `results`. La
   conclusión del reporte es exacta: esos campos **no sirven como semáforo**,
   porque dicen lo mismo con el grafo vacío y con el grafo publicado.
2. Todo el progreso de un comando salía en `stderr` con `level: "ERROR"`,
   incluso cuando el resultado era `PASS`, y el texto de un error real viajaba
   como atributo de un mensaje fijo `"command stderr"`.
3. Un diagnóstico del cargador Go que no tumbaba la pasada se contaba
   (`diagnostics=3`) y se tiraba, y un repositorio registrado como TypeScript
   que no declara ningún paquete no producía nada sin decirlo.

ADR 0013 ya había decidido lo correcto para el primer caso -«no aparece un
objeto `metrics` vacío que pudiera confundirse con observabilidad activa»-
pero sólo lo aplicaba al campo entero: un registro configurado seguía
serializando secciones que ese proceso nunca observó.

## Decisión

Un proceso informa de lo que usa y de lo que midió. Nada más.

- `serve` responde desde el `HotSnapshot` publicado: no abre LadybugDB y no
  ejecuta el worker TypeScript. Ambos se declaran `not_applicable` **con el
  motivo**, en lugar de `not_configured`, que sugiere un cableado pendiente
  donde no falta ninguno. El testigo de lo que se sirve sigue siendo `status`,
  `snapshot_id` y `snapshot_age_ms`.
- `metrics.Report` omite la sección que este proceso nunca observó. `serve`
  registra consultas y el snapshot que cargó; no indexa nada, así que informar
  de un índice de cero segundos sobre cero ficheros describiría trabajo hecho
  en otro proceso -- y se lee exactamente igual que un grafo vacío.
- El nivel de un registro viaja con la escritura. El progreso es `INFO`, sólo
  un fallo es `ERROR`, y el texto de la línea es el `msg`: un registro que no
  se puede filtrar por nivel ni encontrar por texto no informa de nada.
- Un diagnóstico que no tumba la pasada se imprime junto al informe, y viaja
  en la entrada de la caché de hechos para que una pasada caliente lo repita.
  Un repositorio TypeScript que no declara paquete se nombra.

`HealthNotConfigured` se conserva para un host que sí cablea una sonda y esa
sonda no responde. Es una afirmación sobre el servidor; `not_applicable` es
una afirmación sobre este proceso.

## Consecuencias

Cambia la forma de la respuesta de `graph_status`:

| Campo | Antes | Ahora |
| --- | --- | --- |
| `storage.state`, `worker.state` en `serve` | `not_configured` | `not_applicable` con `detail` |
| `metrics.index`, `.snapshot`, `.worker`, `.ladybug` | siempre presentes, a cero | ausentes si el proceso no los observó |

Un cliente que leyera `metrics.index.files` en `serve` recibía `0`; ahora
recibe la ausencia del campo, que es la respuesta correcta a una pregunta
sobre trabajo que ocurrió en otro proceso. `metrics.queries` sigue presente
siempre: son las consultas de este servidor.

El resto de la superficie cambia en la misma dirección y sin romper nada:
la ayuda marca el comando que esta build no puede ejecutar, `init` valida el
vocabulario de lenguajes donde se escribe el valor, y una configuración fuera
de la ubicación por defecto mantiene su estado en su propio directorio.

## Limitaciones

`graph_status` sigue sin poder informar de la indexación: ocurrió en otro
proceso y ningún registro compartido la observa. Un host que coordine
indexación y servidor en el mismo proceso -- `index_project` sobre un `serve`
vivo -- sí llena esas secciones, y entonces aparecen.
