# ADR 0061: Un fichero publicado se prueba contra su grafo, no contra sus contadores

- **Estado:** aceptada
- **Fecha:** 2026-08-22
- **Completa:** el formato publicado por el [ADR 0045](0045-mapping-the-published-snapshot.md)
- **Cambia el layout de la generación:** añade `snapshot.content.sha256`
- **Cambia el schema:** no

## Contexto

`LUQUE-2014`. Una generación publica su grafo en `graph.db` y, desde el ADR
0045, también el `HotSnapshot` ya construido en `snapshot.kvsnap`. Ese fichero
se mapea y se sirve: `loadPublishedSnapshot` lo mapea y el snapshot lee sus
cadenas de esas páginas en vez de copiarlas.

Un fichero que se sirve necesita una prueba de que contiene el grafo de **esta**
generación. La cabecera lleva dos digests: uno del payload, que prueba que los
bytes están íntegros, y uno llamado `contentDigest`, que es el que responde a la
pregunta de pertenencia.

Lo que se escribía en ese campo era el valor de `snapshot.sha256`, y eso lo
produce `writeSnapshotDigest(candidatePath, result.Tables)`: **un digest de los
contadores por tabla** que reportó el cargador.

### La medición

Los contadores no distinguen dos grafos de la misma forma. El mismo corpus
`kena` indexado en dos `HOME` distintos produjo:

|magnitud|valor|
|---|---|
|`snapshot.sha256`|`e80c6d46d3a6956c3f4c5c87321ccad67a1d8072b7ec6d22e12f3bd09a96f5fe`, **idéntico**|
|filas de no resueltos con la misma clave y `detail` distinto|`288`|
|arena de cadenas|`63.914.142` contra `63.914.190` bytes|

Un fichero derivado de uno de esos grafos se aceptaba como perteneciente al
otro. La causa de esa diferencia concreta era otra -- `LUQUE-2015`, ya cerrada--,
pero la colisión de contadores no depende de ella: es una propiedad del digest.

### Por qué no lo vio ningún test

Porque el fixture no modelaba producción. `seedPublishedGeneration` escribía
`report.Digest` -- el digest **de contenido**, que el proyecto ya calcula en cada
build-- en `snapshot.sha256`. Producción escribía el de contadores. Todas las
pruebas del fichero ejercitaban un emparejamiento **más fuerte** que el real, así
que ninguna podía ver el hueco.

Es exactamente lo que `AGENTS.md` exige de un fixture: «demuestra el caso real o
no demuestra nada: comprobar qué forma instala de verdad la herramienta que se
está imitando».

## Decisión

Separar las dos preguntas, porque son distintas y cuestan distinto.

* **`snapshot.sha256` se queda como está**: el digest de los contadores. Su
  trabajo es la comprobación del `Rollback` -- «¿sigue esta base de datos
  teniendo lo que registró?»-- que se resuelve con unos `COUNT(*)` junto al
  escaneo de invariantes que ya corre. No se toca ni una línea de ese camino.
* **`snapshot.content.sha256` es nuevo**: registra `snapshotContentDigest(rows)`,
  el digest del grafo que el fichero contiene. Es lo que la cabecera repite y lo
  que `loadPublishedSnapshot` compara.

El digest de contenido **ya se calculaba en cada build** (`snapshot.go:112`,
hacia `SnapshotReport.Digest`) y se descartaba para este uso, así que la
decisión no añade coste de cómputo.

### Por qué un fichero nuevo y no cambiar el que hay

Se consideró que `snapshot.sha256` pasara a llevar el digest de contenido, con
el `Rollback` recomputándolo. Se descarta: un digest de contadores y uno de
contenido son los dos 64 caracteres hexadecimales e **indistinguibles**, así que
una generación publicada por un binario anterior fallaría el rollback con
«digest mismatch» -- un mensaje que dice corrupción para describir una
actualización. Es la misma clase de defecto que el `doctor` en rojo que se
corrigió al subir la versión del formato, y no se repite.

Un fichero nuevo, en cambio, tiene una ausencia con significado definido.

## Consecuencias

* Una generación publicada antes de este ADR lleva `snapshot.kvsnap` y no lleva
  el registro. Eso **no es un fallo**: `ErrNoRecordedGraphDigest` es de la misma
  clase que la ausencia, el lector no puede probar el fichero y deriva el grafo
  del store canónico, que es lo que siempre se hizo. `doctor` lo informa como
  `PASS` con su explicación, junto a `ErrSnapshotFileVersion`.
* El siguiente `index --full` escribe el registro y el fichero vuelve a
  cargarse. No hay migración que ejecutar.
* `Prune` borra el registro con su generación, así que no puede quedar huérfano:
  vive dentro del directorio de la generación, como el resto.
* El registro se escribe **después** del fichero, así que su presencia implica el
  fichero. El orden inverso dejaría que un lector encontrara una prueba de un
  snapshot que todavía no está.
* Un fichero cuyo registro lleve el digest de contadores -- la forma que tenía
  producción-- se rechaza, porque la cabecera ahora lleva el del grafo.

## Verificación

* `TestTableCountsCannotProveWhichGraphAFileHolds`: dos grafos de la misma forma
  a una firma de distancia dan contadores idénticos y digests de grafo
  distintos, y el fichero del primero colocado en la generación del segundo se
  rechaza nombrando el `content digest`.
* `TestAGenerationWithoutTheRecordIsAnUpgradeNotAFailure`: clasifica sobre el
  centinela y no sobre el mensaje, comprueba que no se confunde con la ausencia,
  y que el grafo se sigue sirviendo derivado.
* `TestLoadOrBuildSnapshotFallsBackAndSaysWhy`: seis formas de no poder probar
  el fichero, cada una declarada.
* `seedPublishedGeneration` escribe ahora lo que escribe producción, con un
  `snapshot.sha256` deliberadamente ajeno al grafo para que cualquier lectura
  accidental de él falle.
