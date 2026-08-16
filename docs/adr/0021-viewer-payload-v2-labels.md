# ADR 0021: Payload `LGVB` v2 con etiquetas y layout equilibrado

**Estado:** aceptada · **Fecha:** 2026-08-07 · **Revisa:** ADR 0017, ADR 0018, ADR 0019

## Contexto

El visor consumía el payload binario `LGVB` v1: cabecera, nodos y aristas con
IDs densos. Un ID denso no dice nada a quien mira el grafo, así que la web
mostraba `file 1738` o `package 40` en cada nodo. Resolver esos nombres con una
segunda llamada JSON exigiría enviar hasta 2.000 identificadores por vista en la
URL de un `GET`.

Además, las aristas de tile solo cubrían símbolos: un tile de repositorios o de
paquetes llegaba sin ninguna relación, aunque el snapshot conserva 295
dependencias entre paquetes fuera del CSR de símbolos.

Por último, el layout publicado empaquetaba cada contenedor en una rejilla de
`4` columnas fijas. Con 34 repositorios eso produce un mundo de `29.752` por
`349.824`: una tira doce veces más alta que ancha, ilegible a cualquier zoom.

## Decisión

1. El payload sube a `LGVB` v2. Tras la sección de aristas se añade una sección
   de etiquetas: por cada nodo, en orden de nodo, un `uint16` con la longitud y
   sus bytes UTF-8. La cabecera declara su offset y su tamaño en `56`–`64`.
2. La etiqueta es el nombre que el snapshot ya conoce: nombre del repositorio,
   nombre del paquete, ruta del archivo o nombre cualificado del símbolo. Un ID
   que el snapshot no resuelve se etiqueta `unknown <id>`, nunca en blanco.
3. Las etiquetas se truncan a `512` bytes sin partir un rune. El límite de
   `32 MiB` por payload sigue siendo el mismo y ahora también cubre la sección
   de etiquetas.
4. `format_version` sólo acepta `2` (o vacío). Un cliente fijado a v1 recibe
   `UNSUPPORTED_VERSION` en vez de un payload que no sabe leer.
5. Un tile de paquetes incluye las relaciones `PACKAGE_DEPENDS_ON` entre los
   paquetes visibles, marcadas con el flag `1` de arista. El cliente resuelve
   los extremos por `(tipo, id denso)`, porque un ID denso sólo es único dentro
   de su tipo.
6. `/api/v1/meta` publica el viewport raíz del layout (`min_x`, `min_y`,
   `max_x`, `max_y`, `max_lod`, `max_nodes`). Sin él un cliente no puede pedir
   su primer tile: `/api/v1/tiles` exige límites explícitos.
7. `layout.Config.Columns` pasa a admitir `0`, que es el valor por defecto y
   significa «equilibrado»: el ancho de cada rejilla se deriva del número de
   hijos y de su relación de aspecto, de forma que el mundo publicado queda
   aproximadamente cuadrado.
8. El visor renderiza en tres dimensiones y la cámara rota por defecto. Cada
   tipo de nodo ocupa su propio plano de profundidad — repositorios delante,
   símbolos detrás — porque el layout anida un contenedor alrededor de sus
   hijos y en una proyección plana caen sobre los mismos píxeles. Un botón
   `3D`/`2D` conmuta entre rotar y desplazar.
9. La etiqueta dibujada en el lienzo es el nombre acortado a sus dos últimos
   segmentos de ruta (máximo `32` caracteres). El nombre completo viaja en el
   nodo y se muestra al pasar el cursor: nada se pierde, pero un módulo como
   `kena.bot/api-db-go/internal/domain/errors` deja de tapar a sus vecinos.
10. El visor no dibuja las coordenadas absolutas del layout: proyecta cada eje
    por rango. Las posiciones distintas se ordenan y se reparten a intervalos
    iguales, conservando el orden y las columnas del layout. Con la proyección
    lineal, la celda más densa del mundo de `~/kena` concentraba `11` de `128`
    paquetes mientras tres cuartas partes del lienzo quedaban vacías; por
    rango, ninguna celda pasa de `2`.
11. El visor dibuja además la contención que cada nodo ya declara en el
    payload (`parent_kind`, `parent_id`), sólo cuando el contenedor viaja en
    la misma tile. Sin ella un repositorio aparece junto a sus paquetes sin
    nada que los una, afirmando una desconexión que el grafo no tiene.
    Se dibuja como línea fina sólida, nunca discontinua: Reagraph construye
    una curva Catmull-Rom y un tubo **por guion**, y una arista de contención
    por nodo costaba más que todas las dependencias juntas.
12. Una leyenda fija nombra cada color: repositorio, paquete, archivo, símbolo,
    y los tres trazos — dependencia exacta, dependencia y contiene. La paleta
    vive en un único sitio del adaptador para que leyenda y lienzo no puedan
    divergir.
13. Cada nivel tiene un presupuesto de nodos configurable entre `100` y
    `10.000`, con `10.000` como techo del endpoint. Reagraph construye un
    objeto por nodo, así que el nivel completo no cabe necesariamente en un
    frame razonable. Cuando el servidor recorta la tile, la vista lo dice: es
    una muestra acotada, no el nivel entero.
14. El fetch, la validación y la adaptación corren en un Web Worker, con un
    `AbortController` por petición para que cambiar de nivel cancele el
    anterior. El render se queda en el hilo principal: WebGL no es accesible
    desde el worker en esta integración.
15. El presupuesto por nivel es un valor por defecto, no un techo fijo: un
    deslizador de `100` a `10.000` nodos deja al lector elegir cuánto grafo
    quiere ver, con `250` ms de espera para no pedir una tile por píxel
    arrastrado. Un contador de FPS muestra el coste de esa elección.

## Alternativas descartadas

- **Endpoint JSON de etiquetas:** una segunda petición por tile con miles de
  IDs en la URL; `GET` tiene límite de tamaño y el servidor ya rechaza URIs
  largas.
- **Nombres en el nodo de tamaño fijo:** un registro de 48 bytes no admite una
  ruta de archivo, y reservar espacio fijo desperdicia la mayoría del payload.
- **Etiquetar en el cliente desde `/api/v1/symbol`:** una llamada por nodo.
- **Esperar que el worker arregle la lentitud:** medido, no lo hace: de los
  `11,8` s de una tile de `2.000` nodos, `27` ms son red, decodificación y
  adaptación juntas. El resto es construir la escena.
- **Agregar aristas de símbolo a nivel de paquete:** sería una arista derivada
  que el grafo canónico no afirma; se usan las relaciones de paquete reales.
- **Dejar las `4` columnas y arreglarlo en el cliente:** el visor sólo puede
  reescalar; la relación de aspecto del mundo se decide en el layout.
- **Force layout en el navegador para descongestionar:** ADR 0019 lo rechaza:
  no es determinista y las posiciones dejarían de ser las del snapshot.
- **Escalar el zoom y ya:** el zoom amplía por igual el hueco vacío y el
  amontonamiento; la densidad desigual no se corrige con la cámara.

## Consecuencias

- El payload crece con los nombres. Un tile de 2.000 nodos añade decenas de
  kilobytes, muy por debajo del límite.
- Cambiar el layout cambia las posiciones publicadas y el fingerprint del
  layout: es una proyección derivada, no un hecho canónico, y se reconstruye
  con `kivgraph index --full` o al reabrir el snapshot.
- Resolver posiciones por mapa en vez de recorrer el array de nodos elimina un
  coste cuadrático: con `2.000` nodos eran cuatro millones de comparaciones
  antes del primer frame.
- El perfil de CPU del nivel `files` mostró un `27 %` del tiempo en
  `getPoint`, `initNonuniformCatmullRom` y `getLengths`: geometría de guiones.
  Con la línea sólida, `files` pasó de `2.833 ms` a `809 ms` con `600` nodos,
  lo que permitió doblar el presupuesto a `1.200` y aun así bajar a
  `1.231 ms`.
- El render sigue bloqueando el hilo principal mientras construye la escena;
  el presupuesto por nivel acota cuánto.
- El visor muestra nombres reales en todos los niveles y oculta las etiquetas
  por encima de `200` nodos, donde se solaparían; el nombre sigue disponible al
  pasar el cursor.
- Cualquier cliente del binario debe migrar a v2; no hay compatibilidad con v1
  porque el servidor no la ofrece.
- La proyección por rango conserva el orden y las columnas del layout, pero no
  sus distancias: dos nodos vecinos en pantalla no están necesariamente a la
  misma distancia en el mundo publicado. El grafo se lee como topología, no
  como mapa a escala.
