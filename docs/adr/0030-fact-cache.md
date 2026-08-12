# ADR 0030: Caché de hechos por unidad de análisis

- **Estado:** aceptada
- **Fecha:** 2026-08-11
- **Revisa:** ADR 0012

## Contexto

Medido sobre un workspace de 33 repositorios (`85 165` símbolos, `222 154`
aristas), una pasada completa tarda `17 s`, de los cuales `8.5 s` son análisis:
cada módulo Go se vuelve a cargar y cada paquete TypeScript vuelve a arrancar
su worker aunque no haya cambiado un byte. El caso corriente no es indexar un
corpus nuevo: es reindexar tras tocar un fichero, o añadir un repositorio a los
treinta y tres que ya estaban.

El análisis ya está paralelizado y ordenado por peso (ADR 0012 y las notas de
concurrencia); lo que queda no es un problema de planificación, sino de volver
a hacer trabajo cuyo resultado no ha cambiado.

Una caché de hechos es la pieza más peligrosa que se puede añadir a este
proyecto. Los demás errores de indexación fallan de forma ruidosa: la pasada
aborta, o el repositorio sale visiblemente vacío. Una entrada mal invalidada
publica un grafo que **parece correcto y miente**, y lo que Ladygraph vende es
justamente que una arista `EXACT` es de fiar.

## Decisión

La unidad de caché es la unidad de análisis: un módulo Go o un paquete
TypeScript. Una entrada guarda los hechos que la unidad produjo **y la lista de
todo lo que leyó, con su huella**. Servir una entrada exige revalidar esa lista
entera contra el mundo actual.

No se calcula una clave y se confía en ella: se registra la dependencia
observada y se comprueba. Esa diferencia es la que permite invalidar por cosas
que no son ficheros del propio repositorio.

### Qué entra en una entrada

| Entrada | Cubre |
| --- | --- |
| `tree` | Todo fichero de código bajo un directorio, por ruta y contenido |
| `file` | Un fichero concreto: manifest, `tsconfig`, `go.work`, lockfile |
| `provider` | Quién responde hoy a un nombre de paquete, y su contenido |
| `registry` | Qué repositorio provee qué módulo Go |

Una unidad Go depende de su propio árbol, del de **todos los módulos de su
grupo de workspace** -dos módulos comparten workspace exactamente cuando uno
alcanza al otro, así que el código del hermano es la información de tipos de
este-, de su `go.work` y del registro de módulos.

Una unidad TypeScript depende de sus raíces de fuentes, de su manifest y su
`tsconfig`, del lockfile del repositorio, y de **cada paquete que pidió**,
resuelto o no. Un nombre que hoy no provee nadie tiene huella `absent`: si
mañana aparece el paquete que lo provee, la entrada deja de valer. Sin eso, un
`UNRESOLVED` se serviría para siempre y nunca se convertiría en la arista
`EXACT` que le corresponde.

El árbol se recorre en cada validación, no se lee la lista de ficheros que la
última pasada abrió: un fichero **añadido** a un paquete no está en esa lista y
sí cambia los hechos.

### La identidad del analizador

Una entrada la escribe un analizador concreto y solo ese la puede leer:

- el contenido del propio ejecutable, no su número de versión -un build de
  desarrollo cambia el normalizador sin cambiar una versión-;
- la respuesta de `go env` (`GOVERSION`, `GOROOT`, `GOFLAGS`, `GOMODCACHE`,
  `GOPATH`, `GOPRIVATE`): `go/types` viaja enlazado en el binario, pero la
  biblioteca estándar contra la que comprueba es código bajo `GOROOT` y la
  lista de build la decide el comando `go`;
- el contenido del worker TypeScript resuelto, porque la misma línea de
  comandos ejecuta lo que dejó el último `pnpm build`;
- los build tags, `include_tests` y `go.allow_network`.

Cualquier actualización de Ladygraph o del toolchain arranca en frío. Es el
precio de no tener que acordarse de subir un número de versión.

### Lo que nunca se guarda

Un módulo que el cargador no pudo leer (`MODULE_NOT_LOADED`). Que un módulo
cargue depende del caché de módulos, que ninguna huella del código describe:
guardar el fallo convertiría «descarga las dependencias y vuelve a indexar» en
una operación sin efecto.

### El modo `verify`

`indexing.fact_cache` acepta `off`, `on` y `verify`. En `verify` se analiza
todo, se compara cada entrada servible contra lo que el análisis acaba de
producir y **una divergencia aborta la pasada** nombrando la unidad. Los hechos
publicados son siempre los del análisis, así que verificar no puede publicar
una mentira: cuesta la pasada entera y compra la prueba.

Es la respuesta para cualquiera que dude de la caché en su propio corpus, y es
como se verificó esta.

## Consecuencias

Medido con el mismo binario, comparando siempre contra el digest del snapshot:

| Corpus | Fría | Caliente | Digest |
| --- | --- | --- | --- |
| 33 repositorios (`85 165` símbolos) | `17.7 s` | `8.3 s` | idéntico |
| 2 repositorios | `6.8 s` | `1.7 s` | idéntico |

`verify` sobre los 33: `33/33` verificadas, dos pasadas, cero divergencias. El
digest coincide además con el de las pasadas anteriores a que la caché
existiera.

Sobre una copia del workspace de 33 repositorios, comparando en cada paso la
pasada con caché contra una reconstrucción limpia del mismo estado:

| Mutación | Reanalizadas | Grafo |
| --- | --- | --- |
| ninguna | 0 de 33 | idéntico |
| añadir un export | 3 | idéntico |
| tocar un proveedor | 1 | idéntico |
| renombrar el export | 3 | idéntico |
| borrar el export | 3 | idéntico |
| registrar un repositorio nuevo | 1 | idéntico |
| consumir el proveedor nuevo | 3 | idéntico |
| desregistrar el proveedor | 1 | idéntico |

Añadir un repositorio a treinta y tres reanaliza uno, que era el caso que
motivó todo esto: la versión conservadora -invalidar todo cuando cambia el mapa
de proveedores- es trivial de escribir y no sirve para nada.

### Limitaciones declaradas

- `node_modules` es una entrada, pero también son cientos de miles de ficheros
  que son función del lockfile, y es el lockfile lo que se registra. Editar
  `node_modules` a mano no invalida una entrada.

  El lockfile se busca desde la raíz del repositorio registrado **hacia
  arriba**, y se registra la cadena entera, exista o no cada candidato. Un
  workspace guarda ese fichero por encima de sus paquetes -pnpm escribe un
  único `pnpm-lock.yaml` en la raíz del workspace y cada repositorio
  registrado cuelga de él-, así que mirar sólo en la raíz del repositorio no
  encontraba nada: los tres candidatos daban huella `absent`, invariantes bajo
  cualquier instalación, y el único control sobre las dependencias instaladas
  quedaba inerte justo en la disposición para la que se escribió. Se registra
  la cadena completa y no sólo el primer acierto porque una entrada registra
  el nombre de lo que hay que volver a medir: un lockfile que aparezca más
  cerca del repositorio también tiene que invalidarla.
- Un `tsconfig` que hereda de una ruta fuera del repositorio registrado no está
  en ninguna huella. Heredar de un paquete instalado sí lo está, por el
  lockfile.
- La procedencia -commit, rama y estado sucio- no viaja en el conjunto de
  hechos de una unidad y por tanto tampoco en la entrada. La estampa la pasada
  al fusionar, desde el registro que se le dio. Guardarla dentro de la entrada
  hacía que un acierto reprodujese el commit de la pasada que la escribió: el
  grafo declaraba movido un repositorio cuyo contenido era el actual, el valor
  publicado dependía de qué unidad acertara primero, y `verify` -que compara
  conjuntos enteros- abortaba la pasada por un commit que no cambió ningún
  fichero. La versión de entrada pasa a `2`; una entrada de la versión `1` se
  descarta.
- El código bajo `GOROOT` se identifica por `GOVERSION`, no por su contenido:
  un toolchain parcheado a mano conserva su identidad.
- El recorrido de un árbol sobreaproxima a propósito: un fichero que el
  cargador excluiría por build tag se registra igual. Equivocarse en esa
  dirección cuesta un análisis; en la contraria, sirve hechos que nadie puede
  reproducir.

Las entradas viven en `indexing.fact_cache_path`, fuera de todo repositorio
indexado, y se retiran cuando nada las ha usado en treinta días: dos workspaces
indexados desde el mismo `HOME` comparten directorio y no deben desalojarse
mutuamente.
