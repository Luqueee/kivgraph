# De qué son los no resueltos

`LUQUE-2007`. El índice de `kena` publica `9.581` referencias Go, `5.998`
TypeScript y `1.969` Rust sin resolver, y ningún conjunto de preguntas había
preguntado nunca **qué son**. Un contador
agregado no distingue una limitación declarada de un defecto escondido, y ésa era
toda la información disponible.

La pregunta se contesta sin leer código, porque el contrato de la raíz lo
garantiza: cada `UNRESOLVED` conserva su motivo, su repositorio y su lenguaje, y
cuando hay una ocurrencia concreta conserva su archivo y su posición. Esto lee
exactamente esos campos del grafo canónico publicado y los agrupa.

Métricas crudas en `results.json`.

## Provenance

|dato|valor|
|---|---|
|fecha|2026-08-21|
|commit|`9cad2d3`|
|corpus|`kena`, 37 repositorios, con `cargo` en el `PATH`|
|máquina|`Mac17,2` (Apple M5), macOS `26.6`|

## El panorama

|lenguaje|no resueltos|símbolos publicados|
|---|---|---|
|`go`|`6.059`|`11.731`|
|`typescript`|`5.991`|`108.730`|
|`rust`|`1.969`|`3.063`|

Contra **referencias** -- que es el denominador honesto, porque un no resuelto es
una referencia que no resolvió-- Rust es el más alto: `14,9 %` de `13.223`, contra
`8,4 %` de Go y `2,2 %` de TypeScript.

## La respuesta: ninguna es un defecto de resolución

|grupo|cuenta|parte|qué es|
|---|---|---|---|
|`CRATE_PROVIDER_NOT_FOUND`|`1.857`|`94,3 %`|el sysroot y las dependencias externas|
|`DEFINITION_NOT_INDEXED`|`112`|`5,7 %`|definiciones sin ocurrencia en fuente|
|**llamadas del workspace que fallaron al resolver**|**`0`**|`0 %`|--|

El `94,3 %` son `alloc::boxed::Box`, `alloc::macros::vec`,
`alloc::collections::vec_deque::VecDeque` y compañía: la biblioteca estándar y lo
que el caché local no trajo. El indexado de Rust es hermético por decisión, así
que eso es una **limitación declarada** funcionando como se documentó.

## El `5,7 %`, etiqueta por etiqueta

Las `112` ocurrencias tienen `112` etiquetas distintas, así que se pudieron leer
una a una:

|forma|cuenta|veredicto|
|---|---|---|
|bloques `impl` -- `impl::Type` y `impl::Type::Trait`|`56`|**hallazgo**, abajo|
|miembros generados por `derive` -- `Default::default`, `Clone::clone`, `PartialEq<Self>::eq`|`53`|ausencia inherente: no hay fuente que indexar|
|dos campos de tupla `::0` y un nombre de test|`3`|cola|

Los `53` son código que el compilador escribe: no existe ocurrencia en fuente, y
un no resuelto es el registro honesto de haber visto la referencia sin poder
resolverla.

## El hallazgo: una rama que nunca se ejecuta

Los `56` restantes son bloques `impl`, inherentes y de trait. **El cargador nunca
los publica como símbolo.** La evidencia es directa:

```
find_symbol { name: "impl", kind: "implementation", repo: "api-music-nodo" }
  -> total: 0
```

Y sin embargo `internal/rustloader/kinds.go` tiene código para ellos:
`PublishedKind` devuelve `"implementation"` cuando `isImplementationBlock`
acierta, y `PublishedName` renderiza `impl X for Y` porque «un bloque de
implementación no tiene nombre propio». Ninguna de las dos ramas se ejecuta jamás
en este corpus.

Los **miembros** de esos mismos bloques sí están indexados -- `get_file_outline`
sobre `src/error.rs` los lista, `error::impl::ApiError::with_context_header@174-177`
entre ellos. Así que no es que el cargador ignore los `impl`: indexa su contenido
y no su cabecera, y cada referencia que `rust-analyzer` emite hacia esa cabecera
queda **permanentemente sin resolver**. `56` en este corpus.

Es la misma enfermedad que el camino incremental retirado en el ADR 0057: código
que existe para un caso que no ocurre. Aquí es más pequeño y tiene una segunda
mitad -- referencias que no pueden resolver nunca-- que inflan una métrica que
alguien lee para decidir si confiar en el grafo.

## Go: `95 %` terceros, y ni una llamada del workspace

|grupo|cuenta|parte|qué es|
|---|---|---|---|
|`MODULE_PROVIDER_NOT_FOUND`|`5.768`|`95,2 %`|módulos de terceros: `fiber/v3::Ctx`, `mongo-driver/v2/bson::M`, `zerolog::Event.Msg`|
|`DECLARATION_OUTSIDE_REPOSITORY`|`288`|`4,8 %`|declaraciones que `go test` sintetiza **dentro del caché de build**|
|`PACKAGE_NOT_BUILDABLE`|`3`|`0,05 %`|directorios cuyos ficheros están todos detrás de build tags|
|**llamadas del workspace que fallaron al resolver**|**`0`**|`0 %`|--|

Los `288` se identifican por su propio `detail`, que es una ruta dentro de
`Library/Caches/go-build/`: son los paquetes `*.test` que el toolchain genera --
`::main`, `::init`, `::tests`, `::benchmarks`, `::fuzzTargets` -- y que no existen
en ningún fichero del repositorio. Desaparecen con `include_tests: false`.

Y los `3` no son código roto. Su `detail` lo dice: `build constraints exclude all
Go files in ...`, sobre la raíz de `api-db-go` y dos directorios `testutil`. Un
directorio sin ficheros Go compilables para la plataforma actual no es un fallo,
y el cargador lo registra en vez de callarlo.

## Un contador cuenta observaciones, no hechos

`go_unresolved` dice `9.581`. El grafo guarda `6.059` filas. La clave de un no
resuelto incluye el **offset**, así que sólo colapsan dos observaciones de la
**misma** posición -- y con `include_tests: true` eso pasa a propósito: `go/packages`
carga `pkg` y `pkg.test`, y las dos observan el mismo punto del mismo fichero.

Medido, no supuesto:

|`include_tests`|el índice declara|el grafo guarda|
|---|---|---|
|`true`|`9.581`|`6.059`|
|`false`|`4.397`|`4.397`|

Sin tests las dos cifras **coinciden exactamente**. Con tests, el número que un
usuario lee sobreestima los hechos distintos en `1,58x`. Ni una fila se pierde:
lo que sobra son observaciones repetidas. Queda en `LUQUE-2009`.

## TypeScript: un cuarto apunta al propio código de `kena`

|grupo|cuenta|parte|qué es|
|---|---|---|---|
|`PACKAGE_PROVIDER_NOT_FOUND`|`4.389`|`73,3 %`|paquetes externos: `react`, `vitest`, `zod`, `fastify`|
|`PROVIDER_SOURCE_UNAVAILABLE`|`1.220`|`20,4 %`|**paquetes propios sin salida construida**|
|`DECLARATION_SOURCE_NOT_MAPPED`|`272`|`4,5 %`|declaración sin `declarationMap`|
|`DECLARATION_NOT_RESOLVED`|`109`|`1,8 %`|nombre que el proveedor no declara|
|`MODULE_NOT_RESOLVED`|`1`|`0,0 %`|un paquete que no resuelve|

**`1.492` ocurrencias -- el `24,9 %` -- piden código de `kena`**, no de terceros:
`@kena/sdk::CommandContext`, `@kena/sdk::SlashCommandBuilder`,
`@kena/web::Translations`. La causa es el estado del corpus, verificado:
**ninguno** de los tres paquetes internos tiene `dist/`.

Y aquí la parte accionable, que no es la que este informe dijo primero.

**Corrección.** La primera versión de esta sección leyó `declarationMap` en los
tres `tsconfig.json` y concluyó que `@kena/web` no lo emitía. Es falso:
`library-web` no compila con `tsc` sino con `tsdown`, y su `tsdown.config.ts`
lleva `dts: { sourcemap: true }` con un comentario que explica el motivo en los
términos de este grafo -- «sin ellos nada conecta el `.d.ts` publicado con `src/`,
y los consumidores cross-repo se quedan en dependencia de paquete en lugar de
arista de símbolo». Buscar una clave en el fichero equivocado es la misma clase de
error que la verdad construida por patrón del conjunto `reach`.

Se construyó la librería y se volvió a medir. **No cambió nada**: `5.998` no
resueltos antes, `5.997` después. Y la causa no es el mapa ni el build.

**Segunda corrección, del mismo tipo que la primera.** Esta sección dijo después
que `library-web` no estaba en `pnpm-workspace.yaml`. También falso: está, en la
línea `24` de `43`, y se afirmó lo contrario tras mirar el fichero con un `head`.
Dos conclusiones seguidas sacadas de una salida truncada.

Lo que el fichero completo y el `readlink` dicen juntos es una causa **única** para
todo el cuarto interno:

|hecho|evidencia|
|---|---|
|los paquetes internos **son** miembros del workspace|`pnpm-workspace.yaml`, 43 entradas, `library-web` incluido|
|sus consumidores los piden por rango de registro|`105` declaraciones `@kena/*` en `28` `package.json`, **todas** `0.0.1`, ninguna `workspace:*`|
|`pnpm 11` no enlaza por defecto|`link-workspace-packages` sin configurar; el default es `false` desde `pnpm 10`|
|así que se resuelven desde el store|`node_modules/@kena/shared -> .pnpm/@kena+shared@0.0.1_.../node_modules/@kena/shared`|
|y esa copia no tiene con qué mapear|`0` `.d.ts.map`, sin `src/`|

Y `PROVIDER_SOURCE_UNAVAILABLE` salta exactamente donde el worker dice: cuando el
dueño de la declaración **no declara proyecto propio**. Una copia bajo
`node_modules` no pertenece a ningún repositorio registrado, y el cargador las
excluye por diseño.

De modo que el monorepo consumía **tarballs publicados de su propio código**, y
ninguna cantidad de `pnpm build` lo cambiaba.

## Se arregló, y se midió

Se puso `linkWorkspacePackages: true` en `pnpm-workspace.yaml` -- **no** en el
`.npmrc`, donde no surte efecto: `pnpm 10` movió sus propios ajustes al fichero
del workspace y los escribe en camelCase, como sus vecinos `allowBuilds` y
`minimumReleaseAgeExclude`. Después se reinstaló y se construyeron los cinco
paquetes internos.

|medida|antes|después|delta|
|---|---|---|---|
|no resueltos TypeScript|`5.998`|**`4.969`**|`-1.029`|
|`PROVIDER_SOURCE_UNAVAILABLE`|`1.220`|`226`|`-994`|
|`DECLARATION_SOURCE_NOT_MAPPED`|`272`|`237`|`-35`|
|símbolos TypeScript|`123.829`|`124.371`|`+542`|
|**aristas del grafo**|`493.544`|**`495.814`**|**`+2.270`**|

Las aristas **subieron** más de lo que bajaron los no resueltos: cada referencia
que resolvió se convirtió en arista de símbolo, y algunas en más de una. El cuarto
interno pasó de `1.492` a `463`.

## Y los `463` que quedan tampoco son un defecto

Siguen nombrando `@kena/sdk`: `SlashCommandBuilder`, `ContainerBuilder`,
`Collection`. Pero `src/discord.ts` del sdk dice qué son:

```ts
export { SlashCommandBuilder } from "@discordjs/builders";
export { Collection } from "@discordjs/collection";
```

Son reexportaciones de **terceros**. La cadena llega del consumidor al sdk -- que
ahora sí mapea a su `src/`-- y de ahí sale del corpus hacia `@discordjs/*`, que no
tiene fuente en el grafo. Es la misma clase que el sysroot de Rust: el dueño de la
declaración está fuera, y ninguna construcción propia lo trae dentro.

Nada de esto es un defecto de Kivgraph: es lo que un grafo puede saber de un
paquete que llega como artefacto sin fuente. Pero decir «construye el workspace y
se arregla» era falso, y sólo medirlo lo demostró.

## Reproducir

```bash
export PATH="$HOME/.cargo/bin:$PATH"   # sin cargo el corpus sale sin Rust
kivgraph index --full --json
go run -tags ladybug ./benchmarks/unresolved-shape \
  -database "$HOME/.local/state/kivgraph/generations/$(cat "$HOME/.local/state/kivgraph/CURRENT")/graph.db" \
  -language rust -examples 200
```

## Limitaciones

- Un corpus, una máquina, un toolchain. Otro sysroot, o una dependencia que el
  caché sí traiga, mueve el `94,3 %`.
- La clasificación de las `112` es **por forma del símbolo**, leída etiqueta a
  etiqueta. Las formas están citadas para que quien lea pueda discrepar de la
  lectura.
- Los tres lenguajes están clasificados, pero **cada uno sobre un corpus y un
  toolchain**. Otro sysroot, otras dependencias en caché o un workspace
  construido mueven los tres repartos.
- El sondeo lee el grafo canónico, así que sus símbolos por lenguaje son los del
  grafo y no coinciden con `go_definitions` del evento de índice: uno cuenta
  símbolos publicados y el otro definiciones que el cargador vio.
