# ADR 0039: Frontera del repositorio Rust y monikers duplicados

- **Estado:** aceptada
- **Fecha:** 2026-08-12
- **Revisa:** ADR 0033, ADR 0035

## Contexto

Dos repositorios Rust reales -`api-music-nodo` y `kenalink-rs`- se indexaban sin
error y después impedían publicar ninguna generación. Cada uno fallaba por un
camino distinto, y los dos son la forma normal de un crate, no una rareza:

```text
invalid fact set: edge REFERENCES has unknown target "CD2O…"
snapshot build failed: unresolved reference "…src/main.rs:CRATE_PROVIDER_NOT_FOUND:
  tracing-subscriber:crate:70" source/file mismatch
```

### El crate vendorizado

`kenalink-rs` sustituye `songbird` por un fork en `vendor/songbird` con
`[patch.crates-io]`. Cargo lo compila y `rust-analyzer` lo indexa; el
descubrimiento Cargo de Kivgraph **nunca camina `vendor`**, así que ningún
manifest de ese repositorio declara ese crate. La normalización descartaba sus
1 523 definiciones -no puede inventar un paquete- y publicaba igualmente las
referencias hacia ellas: 74 destinos colgantes y una pasada que no valida. Los
5 464 hechos restantes de ese crate se tiraban sin decirlo.

### Los targets de un paquete

Un paquete Cargo compila varios crates: la biblioteca, cada binario, el build
script y un crate por test de integración. El moniker SCIP nombra el **paquete**
-`rust-analyzer cargo api-music-nodo 1.0.0 crate/`-, así que los módulos raíz de
todos ellos llegan como un mismo símbolo definido en varios documentos; el
propio `rust-analyzer` lo reporta como bug suyo. Solo uno puede ser el nodo, y
el resto de documentos seguían atribuyendo sus usos a ese nodo: un `use` de
`src/main.rs` acreditado a una declaración de `tests/redis_parity.rs`. El
snapshot exige que un no resuelto y su símbolo fuente vivan en el mismo archivo,
y con razón: lo contrario sitúa una observación en un archivo donde no ocurrió.

## Decisión

### 1. La frontera del repositorio es una sola decisión

`workspace.CargoExcludes` responde, sobre una ruta, la misma pregunta que el
descubrimiento resuelve caminando: `.git`, `target`, `vendor`, `node_modules` y
las `exclusions` configuradas. El análisis Rust descarta todo documento que
caiga ahí, igual que ya descartaba los de fuera de la raíz del repositorio.

Un crate vendorizado deja entonces de tener nodos y sus usos se declaran
`CRATE_PROVIDER_NOT_FOUND`, que es exactamente lo que es: código de un
proveedor que nadie registró.

### 2. Un moniker es un nodo, y lo publica el target más alcanzable

Cuando varios documentos definen el mismo símbolo, publica el que Cargo compila
en el target más alcanzable -biblioteca, binario, build script, test, bench,
ejemplo- y la ruta desempata. La clasificación sigue las convenciones de
directorio de Cargo y **no cambia ninguna clave**: decide dónde vive el nodo,
nunca cómo se nombra, así que un consumidor de otro repositorio sigue componiendo
la clave que su proveedor publica.

Solo la biblioteca es enlazable, así que es la única que otro repositorio puede
nombrar. Ordenar por ruta habría publicado `crate/` y `main()` desde `build.rs`,
que es lo que hacía perder 328 referencias de `src/main.rs` en `kenalink-rs`.

### 3. Un uso pertenece a una declaración de su propio documento

`enclosing` solo devuelve una declaración que el grafo publique **en ese
archivo**; si la más interna vive en otro, sube al siguiente contenedor -un uso
dentro de un módulo está dentro de todo lo que lo contiene- y si no hay ninguno,
el uso no tiene fuente.

Un fallo de resolución no la necesita: se declara igual, con su archivo y su
posición y sin `SourceSymbolKey`. Perderlo habría convertido 299 agujeros
declarados de `api-music-nodo` en silencio.

### 4. Ninguna arista se publica hacia un destino propio que no se publica

`NormalizeRust` descarta la arista cuyo destino es de este repositorio y no está
en el conjunto, y lo cuenta en `EdgesWithoutTarget`. Un destino de otro
repositorio sigue ausente a propósito: lo cierra el merge con el proveedor.

## Alternativas descartadas

- **Indexar el crate vendorizado como paquete del repositorio.** Publica como
  propio el código de un tercero y contradice al descubrimiento, que es quien
  decide qué crates existen.
- **Distinguir los targets en la clave estable.** Rompe la composición
  cross-repository: el consumidor compone el moniker del proveedor y no puede
  saber qué discriminador añadió. Además exige migración y ADR de claves.
- **Relajar la invariante del snapshot.** El hecho seguiría siendo falso; solo
  dejaría de verse.
- **Publicar el símbolo duplicado desde el primer documento que emita el
  analizador.** Hace depender el grafo del orden de emisión de `rust-analyzer`.

## Consecuencias

- Los dos repositorios publican generación. Sobre ellos: `api-music-nodo`
  1 665 símbolos y 5 713 referencias, `kenalink-rs` 726 y 2 773.
- Los usos de nivel superior de un `main.rs`, un `build.rs` o un test de
  integración no producen arista cuando su módulo raíz no es el publicado: 184 y
  7 en esos repositorios. Se cuentan y se nombran en los diagnósticos.
- Un símbolo definido en varios documentos se nombra en los diagnósticos, con el
  documento que lo publica y los que no.

## Verificación

`testdata/rust/targets` es un paquete con biblioteca, binario, build script,
test de integración y un crate en `vendor/`. Sobre él,
`TestFullKeepsARustGraphInsideItsOwnRepository` exige un conjunto válido, ningún
archivo bajo `vendor/`, el crate vendorizado declarado, `crate/` publicado desde
`src/lib.rs`, `main` desde `src/main.rs` -no desde `build.rs`-, la llamada del
binario a la biblioteca, y que ningún no resuelto se acredite a una declaración
de otro archivo. Sin los arreglos, ese test falla con los dos errores del
contexto.
