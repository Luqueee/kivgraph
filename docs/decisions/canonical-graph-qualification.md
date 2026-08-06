# Calificación del grafo canónico

**Estado:** `CANONICAL_GRAPH_PASS`

**Fecha de verificación:** 2026-08-06

Este documento emite el gate de la fase 9. Certifica que Luque construye el
grafo canónico definitivo desde código real, lo verifica, lo publica de forma
atómica y deriva de él un HotSnapshot **consultable**.

## Qué se califica

| Elemento | Valor |
| --- | --- |
| Esquema canónico | versión `002` |
| Numeración de códigos | `facts.CodeFormatVersion` = 1 |
| Formato de clave durable | `hotsnapshot.StableKeyFormatVersion` = 1 |
| LadybugDB | core y binding `v0.13.1` |
| Corpus | `testdata/go/cross-repository`, tres repositorios y cuatro módulos Go |
| Plataforma | `linux/amd64`, Go 1.24, CGO habilitado |

## Cadena verificada de extremo a extremo

Los hechos **no** se escribieron a mano: se derivaron del fixture con
`goloader` y `facts.NormalizeGo`, cargando cada módulo de cada repositorio,
incluido el módulo anidado `internal/legacy` de `consumer-b`.

```text
código Go real
→ goloader + facts.NormalizeGo   3 repos, 4 paquetes, 4 archivos, 9 símbolos,
                                 15 evidencias, 32 aristas, 0 no resueltas
→ Set.Validate                   0 aristas colgantes
→ luque rebuild                  las ocho etapas en PASS
→ generación 000001 publicada
→ luque snapshot                 9 símbolos, 15 aristas en el CSR
```

Las ocho etapas del rebuild, con biblioteca nativa:

```text
[PASS] facts          validated 3 repositories, 4 packages, 4 files, 9 symbols, 32 edges
[PASS] staging        staged 14 canonical table(s)
[PASS] graph.next     generations/000001.tmp
[PASS] bulk load      copied 39 node(s) and 47 edge(s)
[PASS] integrity      27 of 27 canonical table(s) matched; 0 invariant violation(s)
[PASS] snapshot       hot snapshot built (3 repositories, 4 packages, 4 files,
                      9 symbols, 15 edges, 17 edge(s) not represented in the CSR)
[PASS] golden probes  3 golden probe(s) passed
[PASS] publish        published generation 000001
```

## El snapshot se deriva del grafo, no de los hechos

`luque snapshot --root …` lee la generación **publicada** con `ScanCanonical` y
construye el HotSnapshot desde esas filas. Ni el escáner ni el adaptador ven un
`facts.Set`. Es la diferencia que hace significativa la calificación: lo que se
consulta es lo que de verdad quedó almacenado.

Dos construcciones consecutivas del mismo grafo emiten el mismo digest de
contenido, `0c8ce3bf…`, que no depende del reloj ni de rutas de máquina.

## El snapshot es consultable, no sólo construible

Interrogado sobre la generación publicada:

```text
snapshot: 9 símbolos, 15 aristas en el CSR

  Answer         out=0 in=4        Shape          out=0 in=2
  Compute        out=1 in=3        Shape.Area     out=2 in=1
  Legacy         out=0 in=1        Shape.Width    out=0 in=3
  Register       out=1 in=1        main           out=8 in=0
                                   main           out=3 in=0
```

Las nueve claves durables hacen ida y vuelta por `SymbolByStableKey`. Y la
consulta que justifica el proyecto —**travesía cross-repository**— funciona:

```text
travesía desde "main" (consumer-a): 7 nodos alcanzados en 2 repositorios
  depth 0  main
  depth 1  Shape, Shape.Width, Shape.Area, Answer, Register, Compute
```

`main` vive en `consumer-a`; los seis destinos están declarados en
`shared-library`. La arista cruza el límite de repositorio con confianza
`EXACT_TYPECHECKED`, y la clave del consumidor coincide con la que el proveedor
asigna a su propia declaración.

## Aristas que no entran en el CSR

De las 32 aristas del grafo, 15 entran en el CSR y **17 no**: las de contención
(`CONTAINS_PACKAGE`, `CONTAINS_FILE`, `DEFINES`) y las de paquete a paquete. El
CSR del HotSnapshot indexa símbolo a símbolo; el resto no cabe por diseño.

No es pérdida de información y se declara en cada informe:

* la contención ya está en los propios nodos, en `FileKey`, `PackageKey` y
  `RepositoryKey`;
* la dependencia entre paquetes vive en la base canónica, que sigue siendo la
  fuente de verdad.

## Dos defectos que sólo apareció al usar datos reales

Ambos estaban en `internal/hotsnapshot`, escrito y probado contra fixtures
hechas a mano. Con el grafo real, la construcción fallaba.

**1. El builder exigía riqueza descriptiva, no identidad.** Rechazaba un
repositorio sin `commit`, un paquete sin `module_path` y un símbolo sin
`signature`. Los tres son legítimamente vacíos: un checkout sin metadatos de
git no tiene commit, un paquete npm no tiene ruta de módulo Go, y una constante
o un campo no tienen firma. La validación se separó: identidad e integridad
referencial se exigen; los campos descriptivos son opcionales.

**2. `EdgeRow` no podía representar dos ocurrencias de la misma relación.** El
modelo canónico declara las relaciones semánticas `MANY_MANY` justamente porque
el mismo símbolo puede alcanzar el mismo destino desde varios sitios, y cada
ocurrencia lleva su evidencia. La fila del snapshot tiraba la clave de
evidencia, así que dos usos distintos colapsaban en filas idénticas y el
detector de duplicados rechazaba el grafo entero. En el corpus real ocurre:

```text
REFERENCES  consumer-a:main -> shared-library:Shape.Width
  evidence:file:consumer-a:main.go:158:163
  evidence:file:consumer-a:main.go:206:211
```

`EdgeRow` ganó `EvidenceKey`, presente en la ordenación, en la igualdad y en el
digest. Las dos ocurrencias sobreviven como aristas distintas.

Además, `ErrInvalidSnapshotRows` era un centinela sin detalle: rechazaba el
grafo sin decir qué fila ni por qué. Ahora cada rechazo nombra la fila, su
clave y el motivo, conservando `errors.Is`.

## Reproducción

```bash
scripts/fetch-ladybug.sh
make test-ladybug
luque rebuild --facts FACTS.json --root ROOT --generation 000001 \
  --resolver-version luque-0906 --snapshot-id 1
luque snapshot --root ROOT
```

## Límites de esta calificación

* El corpus es el fixture Go de tres repositorios. La escala grande pertenece a
  LUQUE-1602 y las cifras de rendimiento a la fase de rendimiento.
* `IMPLEMENTS`, `EMBEDS` y `OVERRIDES` tienen tabla, se cargan, se verifican y
  se codifican, pero `facts.NormalizeGo` todavía no las produce: no aparecen en
  este grafo.
* Los dos lenguajes están calificados por separado, no combinados en un solo
  grafo. Ver la ampliación de abajo.
* El snapshot vive en memoria. Su persistencia y su publicación a los lectores
  MCP pertenecen a fases posteriores.

## Ampliación del 2026-08-06: TypeScript

LUQUE-0907 cerró el hueco que este documento registraba como límite. El grafo
TypeScript recorre ahora la misma cadena completa, con sus propias aristas
cross-repository:

```text
shared-library   4 símbolos    consumer-a  3 IMPORTS_SYMBOL
consumer-b       2 IMPORTS_SYMBOL, uno de ellos a través de un alias

[PASS] facts      3 repositories, 3 packages, 4 files, 12 symbols, 26 edges
[PASS] integrity  27 of 27 canonical table(s) matched; 0 invariant violation(s)
[PASS] snapshot   12 symbols, 7 edges in the CSR
[PASS] publish    published generation 000001

luque doctor graph   PASS, los seis invariantes a cero
```

Las cinco aristas `IMPORTS_SYMBOL` son `EXACT_TYPECHECKED` y su destino coincide
exactamente con la clave que el proveedor asigna a su propia declaración,
incluidos los casos con alias (`value as republished`,
`aliasedHelper as helper`), donde el nombre local del binding no coincide con el
del proveedor. Que `doctor graph` pase lo confirma de forma independiente:
`exact_edge_without_source` y `exact_edge_without_target` exigen que ambos
extremos estén declarados, y una clave mal derivada los violaría.

Sigue pendiente combinar Go y TypeScript en **una misma generación**: cada
lenguaje se calificó en su propio grafo. La convergencia de ambos en un solo
`Set` está probada en `internal/facts` desde LUQUE-0901, pero no se ha
publicado todavía un grafo mixto.
