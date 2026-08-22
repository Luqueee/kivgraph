# ADR 0062: Dos claves de configuración se retiran, y se siguen aceptando

- **Estado:** aceptada
- **Fecha:** 2026-08-22
- **Cambia la configuración:** retira `storage.snapshots_path` y
  `storage.retain_snapshots`
- **Cambia el schema:** no -- `config.version` sigue en `1`

## Contexto

`LUQUE-2013`. Dos claves prometían cosas que el código no hacía, y se descubrió
al cerrar `LUQUE-2004`, comprobando criterio por criterio en vez de darlos por
buenos.

`storage.snapshots_path` se declaraba, tenía valor por defecto, `Initialize` le
escribía una ruta, `Initialize` **creaba el directorio** y `doctor` comprobaba
que se podía escribir en él. **Nadie escribía nunca nada ahí.** El snapshot
publicado vive dentro del directorio de su generación, junto a `graph.db`.

`storage.retain_snapshots` se declaraba, valía `3` y se validaba como «must be
positive». **Ningún consumidor la leía.** `Prune` conserva la generación actual y
su respaldo, que es lo que el rollback necesita.

Y la documentación de referencia decía que `snapshots_path` contenía «Published
generations», que era simplemente falso.

## Decisión

Retirarlas. De las tres salidas que la tarea dejaba abiertas, es la única que
mejora el producto:

* **Darles significado** es *activamente peor* para `snapshots_path`. Que el
  fichero viva dentro de la generación es lo que hace que `Prune` lo borre con
  ella y que **no pueda quedar huérfano**. Sacarlo a un directorio compartido
  crearía el problema que la clave parecía resolver, y además habría que
  resolver la atomicidad: hoy una generación se publica con un `rename` de su
  propio directorio. Para `retain_snapshots` habría que inventar una política de
  retención sin ningún consumidor que la pida, cuando el rollback usa exactamente
  dos generaciones.
* **Documentarlas como reservadas** es lo que se hace cuando hay un plan
  concreto. El plan al que servirían -- un fichero compartido fuera de la
  generación-- es el que el diseño rechazó.

## La migración: se aceptan, no se rechazan

El decodificador usa `KnownFields(true)`, que es lo que impide que un typo pase
en silencio. Por eso borrar los campos del struct sin más **convertiría en fallo
de carga duro cada `config.yaml` escrito por un `kivgraph init` anterior** --
porque `Initialize` escribía las dos-- y por una clave que nunca hizo nada.
Castigar al usuario por nuestro error no es una migración. Es la misma forma que
el `doctor` que se ponía en rojo al subir la versión del formato de fichero, y es
incorrecta por el mismo motivo.

Así que hay una lista explícita de claves retiradas, `retiredConfigKeys`, y el
documento se reescribe sin ellas **antes** del decodificado estricto:

* La clave se acepta, se ignora y se **nombra**: `Loaded.RetiredKeys` la lleva y
  `kivgraph doctor` la informa como `config.retired` con un `PASS` que dice
  «accepted and ignored, safe to delete».
* La estrictez no se relaja. Reescribir el documento no es lo mismo que aflojar
  el decodificador: una clave que no es conocida **ni retirada** sigue fallando,
  incluida dentro de la misma sección. Eso tiene su propio caso de test, porque
  es lo que se podría haber perdido sin darse cuenta.
* El valor no se honra. Un `retain_snapshots: 7` no hace nada, y la clave viva
  que está a su lado sigue aplicándose.

## Consecuencias

* `Initialize` deja de crear un directorio que nadie usa, y `doctor` deja de
  comprobar que se puede escribir en él. Un directorio `state/snapshots` que ya
  exista se queda donde está: vacío, y el usuario puede borrarlo.
* La validación «must be positive» desaparece con la clave. Nada la echa de
  menos porque nada leía el valor.
* La referencia de configuración documenta las dos como retiradas y corrige la
  afirmación falsa sobre dónde viven las generaciones publicadas.
* `config.version` no sube. Un cambio de versión de schema obliga a migrar a
  todo el mundo; aquí no hay nada que migrar, y una versión nueva haría que un
  binario anterior rechazara ficheros que puede leer perfectamente.

## Verificación

* `TestARetiredKeyLoadsAndIsReported`: un documento con las dos claves carga,
  las reporta en orden, la clave viva de al lado sigue aplicándose, y un
  documento sin ellas no reporta ninguna -- para que el informe no pueda ser una
  constante.
* `TestLoadConfigRejectsInvalidDocuments/unknown_field_inside_a_section_that_has_retired_keys`:
  `storage.snapshot_path` -- un typo de la clave retirada-- sigue siendo un error.
* La referencia de configuración y el fixture de `benchmarks/mcp-stdio` describen
  lo que `init` escribe hoy.
