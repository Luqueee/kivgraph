# ADR 0075: `version --json` describe el proceso, no el directorio

- **Estado:** aceptada
- **Fecha:** 2026-08-26
- **Cambia el protocolo MCP:** no
- **Cambia el schema persistente:** no
- **Obliga a reconstruir:** no
- **Cambia una salida del CLI:** sí -- campos que a veces se tomaban prestados
  de un bundle ajeno pasan a ser `null`, y la versión pasa a ser siempre la
  compilada

## Lo que pasaba

Preparando la `v0.8.0`, en la raíz del checkout:

```
$ kivgraph version
0.8.0
$ kivgraph version --json | jq -r .kivgraph
0.3.6
```

El mismo comando, dos respuestas. La de `--json` era la de un `dist/` de cuatro
días antes, con la versión anterior al rename del proyecto.

`findBundleManifest` probaba dos candidatos: el manifest del bundle en el que
vive el ejecutable -`dir(dir(exe))/manifest.json`, correcto- y, si ese no
existía, `$PWD/dist/kivgraph-<os>-<arch>/manifest.json`. El segundo no tiene
ninguna relación con el proceso que corre: depende de dónde se invocó. Y
`loadBundleProvenance` tomaba de él **el campo de versión**
(`Kivgraph: manifest.Release`), no sólo la procedencia.

En producción el ejecutable siempre se conoce -- `os.Executable()` falla y el
comando aborta--, así que el candidato del `cwd` sólo se alcanzaba en un binario
de desarrollo: exactamente el caso en el que hay un `dist/` al lado.

## La decisión

**`version --json` describe el proceso.** De ahí salen tres cosas:

1. La versión es siempre `version.Value`, la constante compilada. Un manifest
   puede enriquecer la procedencia; no puede renombrar el binario.
2. El manifest se busca **sólo** relativo al ejecutable. Un bundle que esté en
   el directorio de trabajo describe otro build.
3. Un manifest cuyo `release` no coincide con `Value` se **rechaza nombrando los
   dos**, igual que ya se rechazaba uno construido para otra plataforma. Es el
   mismo razonamiento -- no describe a este ejecutable-- aplicado al otro campo
   de identidad.

`workingDir` sigue siendo parámetro de `Collect`, y ahora sólo hace lo que le
queda: localizar el manifest de gramáticas de un checkout de desarrollo.

## Por qué no es una regresión de compatibilidad

La forma de la salida no cambia: mismos campos, mismos tipos. Lo que cambia es
qué gana cuando dos fuentes discrepan, y unos campos que a veces venían
prestados ahora son `null`. Eso es lo que el propio contrato del tipo ya decía
antes de este cambio:

> Fields unavailable outside a distribution bundle are represented as null
> rather than guessed values.

Un manifest leído del directorio de trabajo **es** un valor adivinado. La regla
existía; el código la infringía.

Los tres consumidores del comando ejecutan el binario **del bundle**
-- `scripts/build-bundle.sh:351`, `release.yml:155`, `ci.yml:265`--, así que
resuelven por el primer candidato y no cambian. Y el guardia nuevo no puede
saltar en un bundle legítimo: `build-bundle.sh:347` escribe en el manifest el
`version` del binario que acaba de construir, de modo que los dos valores son el
mismo por construcción. Sólo salta si alguien empareja un manifest con otro
ejecutable, que es la única cosa que se quiere detectar.

## Lo que se retiró

`TestCollectReadsDistBundleForTheRunningPlatform` fijaba el comportamiento
retirado, y llamaba `Collect("", workingDir)` -- con ruta de ejecutable vacía, que
en producción no ocurre. Su nombre decía «lee el bundle de dist»; lo que
demostraba era que un binario sin identidad se cree lo que hay al lado.

El fixture de los tests escribía `"release": "0.1.0"` mientras el binario que los
corre lleva otra versión: nunca fue un bundle que pudiera existir. Ahora escribe
`Value`, y por eso el caso bueno se puede seguir midiendo -- un fixture demuestra
el caso real o no demuestra nada.

## Verificación

Dos tests negativos, cada uno visto fallar antes del arreglo:

- `TestCollectIgnoresABundleInTheWorkingDirectory` -- devolvía
  `resolver-v9`, prestado del bundle del directorio de trabajo.
- `TestCollectRejectsABundleManifestForAnotherRelease` -- aceptaba un manifest
  de otra versión en silencio.

Y la reproducción de la ficha, en la raíz del checkout con su `dist/` viejo:
antes `0.3.6`, después `0.8.0` con el commit real del build info.

Cierra `LUQUE-2228`.
