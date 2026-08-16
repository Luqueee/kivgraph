# ADR 0043: Renombrar el proyecto a Kivgraph

- **Estado:** aceptada
- **Fecha:** 2026-08-16
- **Revisa:** la identidad publicada del proyecto y las superficies que la llevan

## Contexto

El proyecto se llamaba `Ladygraph`. El nombre se descartó por su registro: no
describe lo que hace la herramienta y no envejece bien en una integración que un
tercero instala. El nombre nuevo es `Kivgraph`.

El nombre no vive en un sitio: estaba en 2.285 ocurrencias de 466 archivos
versionados, repartidas por superficies de distinta naturaleza.

```text
la ruta del módulo Go            github.com/Luqueee/ladygraph
el ejecutable y su directorio    cmd/ladygraph
las variables de entorno         29 nombres LADYGRAPH_*
la configuración                 ~/.config/ladygraph/{config,repositories}.yaml
el estado                        ~/.local/state/ladygraph/{graph.lbdb,snapshots,backups,factcache}
la entrada en el cliente MCP     [mcp_servers.ladygraph]
la skill publicada               skills/ladygraph/SKILL.md
los paquetes pnpm                @ladygraph/{ts-worker,web,landing}
el worker por defecto            ladygraph-ts-worker
el bundle publicado              ladygraph-<os>-<arch>
```

El repositorio de GitHub ya estaba renombrado a `Luqueee/kivgraph` cuando se
hizo el cambio, con el mismo `HEAD`: la ruta antigua redirige, así que la ruta
del módulo se puede mover sin partir el historial ni las referencias externas.

## Decisión

El nombre se sustituye conservando el caso -`Ladygraph`, `ladygraph`,
`LADYGRAPH`- en todo archivo versionado, y se renombran las tres rutas que lo
llevaban: `cmd/ladygraph`, `internal/integrations/assets/ladygraph` y el
paquete de fixture `@ladygraph-fixture`.

Cuatro cosas **no** se renombran, y no por descuido:

- El vocabulario de LadybugDB: `ladybug`, `lbug`, el tag de build, `.tooling/ladybug`,
  `make test-ladybug`. Es el motor de almacenamiento de un tercero, no este
  proyecto, y comparte con el nombre antiguo tres letras y ninguna relación.
- El namespace histórico de las stable keys, `luque-stable-key`. Cambiarlo exige
  migración de datos y su propio ADR; el nombre del proyecto no lo obliga. Es
  además lo que hace que un grafo ya publicado siga siendo válido tras esta
  operación: cambia dónde vive, no lo que dice.
- Los identificadores `LUQUE-####` del backlog.
- `benchmarks/mcp-client-32/profiles/trace.out`, una traza binaria de ejecución
  grabada bajo el nombre antiguo. Es una medición histórica: no se edita.

## Consecuencias

Es un cambio incompatible en todas las superficies que la raíz de `AGENTS.md`
enumera menos dos: el schema LadybugDB y el payload `LGVB` no cambian, así que
una generación publicada no se invalida.

La migración de una instalación existente es mover dos directorios y reinstalar
la integración:

```bash
kivgraph stop
mv ~/.config/ladygraph        ~/.config/kivgraph
mv ~/.local/state/ladygraph   ~/.local/state/kivgraph
kivgraph mcp install
kivgraph skill install
kivgraph doctor
```

Sin ese movimiento, `kivgraph` no ve la configuración anterior: `serve` y `ui`
escriben la configuración por defecto cuando no existe, así que el efecto no es
un error sino un grafo vacío y una reindexación completa.

La entrada del cliente MCP cambia de nombre: `[mcp_servers.ladygraph]` queda
huérfana apuntando a un ejecutable que ya no existe, y hay que retirarla. Los
adaptadores exigen `--force` para sustituir una entrada incompatible, no para
retirar la de otro nombre.

Un bundle publicado antes del cambio conserva su nombre y su `SHA256SUMS`;
`kivgraph update` valida contra la plataforma en ejecución y contra el manifest,
así que no puede actualizar sobre un bundle del nombre antiguo. La ruta soportada
es reinstalar con `scripts/install.sh`.

## Verificación

```text
gofmt -l                       limpio
go vet ./...                   limpio
go test ./...                  34 paquetes ok
make test-ladybug              39 paquetes ok
go test ./internal/rustloader/... ./internal/indexer/ -run Rust   ok
make build                     kivgraph 0.6.0
ts-worker pnpm check           93 tests
web pnpm check                 69 tests
make landing-check             31 archivos, 0 errores
kivgraph init con HOME temporal   ~/.config/kivgraph, ~/.local/state/kivgraph
```

El sustituto de un nombre más corto reflota líneas: `gofmt` realineó dos
archivos Go y Biome recompuso un literal que ya cabía en una línea. Los tres
`pnpm check` de Biome fallan si eso no se aplica, que es cómo se detectó.
