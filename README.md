# Luque

Luque será un servidor MCP autónomo y local para inteligencia de código cross-repository en TypeScript y Go.

## Estado

El repositorio contiene la base inicial del proyecto. La funcionalidad de indexación, almacenamiento y consultas MCP se incorporará siguiendo el orden definido en [`TASKS.md`](TASKS.md).

## Requisitos

- Go 1.24 o posterior.

## Desarrollo

```bash
make build
make test
make version
```

El comando provisional de versión también puede ejecutarse directamente:

```bash
go build ./cmd/luque
./luque version
```

### Corpus sintético de LadybugDB

El generador crea un corpus JSON Lines reproducible para los benchmarks de
almacenamiento:

```bash
go run ./cmd/luque benchmark generate-graph \
  --symbols 100000 \
  --edges 1000000 \
  --seed 42
```

Por defecto genera 40 repositorios, 100.000 archivos, 100.000 símbolos y
1.000.000 de aristas en `testdata/generated/synthetic`. `--repositories`,
`--files` y `--output` permiten sustituir esos valores. El directorio contiene
`repositories.jsonl`, `files.jsonl`, `symbols.jsonl`, `edges.jsonl` y un
`manifest.json` con los recuentos y las estructuras controladas del grafo.

La carga individual de referencia requiere la biblioteca nativa de LadybugDB y
ejecuta una sentencia preparada por nodo o arista:

```bash
go run -tags ladybug ./benchmarks/ladybug-individual \
  --corpus testdata/generated/synthetic \
  --database /tmp/luque-individual.db \
  --transaction-size 1000
```

El tamaño de transacción solo controla los commits; no agrupa registros en una
misma sentencia. Los resultados se escriben en
`benchmarks/ladybug-individual`.

## Estructura

```text
cmd/luque/   Ejecutable principal.
internal/    Paquetes internos de Luque.
ts-worker/   Worker TypeScript.
testdata/    Fixtures y corpus de pruebas.
benchmarks/  Resultados de benchmarks.
docs/        Documentación y ADR.
scripts/     Automatización auxiliar.
```

## Licencia

Luque se distribuye bajo [Apache License 2.0](LICENSE).

## Licencias de terceros

Los avisos y las licencias de las dependencias distribuidas con Luque se
registran en [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md). La lista se
actualiza al incorporar cada dependencia al producto distribuible.
