# ADR 0015: Bundle Linux amd64

**Estado:** aceptada

**Fecha:** 2026-08-07

## Contexto

LUQUE-1501 necesita un artefacto instalable que contenga el ejecutable Go,
el worker TypeScript, la biblioteca nativa fijada de LadybugDB, las grammars y
los avisos de licencia. Ladygraph usa CGO para LadybugDB; por tanto, un
binario compilado sin su biblioteca nativa no es un producto ejecutable.

La procedencia debe ser auditable sin introducir timestamps de build ni copiar
repositorios indexados o artefactos de entrada al bundle.

## Decisión

`scripts/build-linux-amd64.sh` genera por defecto
`dist/ladygraph-linux-amd64/` con este layout:

```text
bin/ladygraph
bin/ladygraph-ts-worker
lib/liblbug.so
worker/dist/**
worker/node_modules/typescript/**
worker/package.json
worker/pnpm-lock.yaml
grammars/manifest.json
licenses/LICENSE
licenses/THIRD_PARTY_NOTICES.md
licenses/third-party/**
manifest.json
```

El ejecutable Go se compila con `-tags ladybug`, `-trimpath`, `CGO_ENABLED=1`
y un `RUNPATH` relativo (`$ORIGIN/../lib`). El bundle sigue dependiendo de las
bibliotecas estándar del sistema Linux amd64; no se copian glibc, libstdc++ ni
otras bibliotecas del sistema.

El script siempre descarga y verifica el asset nativo mediante
`scripts/fetch-ladybug.sh`, instala el worker con `pnpm install --frozen-lockfile`,
ejecuta `pnpm build` y copia el paquete `typescript` requerido en runtime. El
manifest no contiene la hora actual. Registra:

- versión del manifest y del producto;
- objetivo `linux/amd64`;
- versiones de core/binding de LadybugDB, SHA-256 del asset descargado y
  SHA-256 de la biblioteca que entra al payload;
- versiones de Go, Node, pnpm y TypeScript;
- versión del esquema canónico y del formato de filas del snapshot;
- hash SHA-256 y tamaño de cada archivo del payload.

`resolver_version` es `null` en este bundle porque pertenece a los metadatos del
grafo/snapshot generado, no al toolchain de distribución. El valor efectivo
se publica con el snapshot que el servidor cargue.

El binario expone `ladygraph version --json` como el contrato de provenance
auditable del producto. En un bundle lee el `manifest.json` adyacente y
comprueba el digest del inventario de grammars antes de emitirlo; en desarrollo
usa la información de build/runtime disponible y deja `null` donde no puede
afirmar un valor. El campo `resolver` describe el grafo/snapshot cargado, por
lo que el bundle lo mantiene `null`; `serverInfo.version` conserva la misma
versión de release que `ladygraph version`.

`dist/` es un directorio generado e ignorado por Git. Un build limpio se
obtiene ejecutando `make build-linux-amd64` desde un checkout sin cambios; un
build desde un árbol modificado no falla, pero `manifest.json` marca
`source.dirty: true`.

## Consecuencias

- El bundle es autocontenido para el código y LadybugDB, pero requiere Linux
  amd64 y las bibliotecas estándar del sistema.
- El `RUNPATH` permite ejecutar `bin/ladygraph` directamente desde el bundle,
  sin `LD_LIBRARY_PATH`.
- Los hashes de payload permiten detectar alteraciones después de la
  generación.
- Las licencias de módulos Go con archivo `LICENSE` se incluyen bajo
  `licenses/third-party/`; la procedencia y los enlaces de la licencia del
  core nativo se mantienen en `THIRD_PARTY_NOTICES.md`.
- La separación entre manifest del bundle y metadatos del snapshot evita
  atribuir al ejecutable una versión de resolver que no generó el grafo.
