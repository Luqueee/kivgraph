# ADR 0022: Layout estructural 3D del visor

- **Estado:** aceptada
- **Fecha:** 2026-08-07
- **Revisa:** ADR 0018, ADR 0019, ADR 0021

## Contexto

El visor colocaba cada nodo proyectando por rango las coordenadas `x`/`y` que
publica el layout del servidor y asignando la `z` según el tipo de nodo. Eso
produce exactamente cuatro profundidades distintas, así que en el nivel `files`
-donde casi todos los nodos son del mismo tipo- el grafo era literalmente un
plano dentro de una escena 3D: rotarlo no enseñaba nada.

La proyección por rango tenía un segundo efecto, más grave que la falta de
profundidad. Normaliza cada eje por separado, de modo que la caja compacta de un
repositorio se estira hasta ocupar todo el ancho del mundo y se solapa con la de
otro. El agrupamiento que el servidor sí había calculado se perdía en la
proyección, los clusters se entremezclaban y las aristas -todas rectas-
cruzaban el centro de lado a lado.

La causa de fondo es que una tile es una **muestra** del mundo: `1.200` de
`4.212` archivos. La rejilla que el servidor empaquetó describe el mundo
completo y no dice nada sobre qué paquetes van juntos cuando falta la mayoría de
sus vecinos. En cambio la tile sí trae la estructura: el árbol de contención en
`parent_kind`/`parent_id` y el grafo de dependencias con su confianza.

## Decisión

1. El visor deja de dibujar las coordenadas publicadas y deriva su propio
   layout 3D de la estructura del tile. Se calcula en el Web Worker, una vez
   por tile, y viaja como posiciones ya resueltas.
2. **Primero estructura, después física.** Se colocan y se relajan los clusters
   entre sí, cada nodo cuelga de una concha alrededor de su contenedor y sólo
   al final una relajación local corrige lo que la estructura no decidió. No
   hay simulación por frame: el mundo llega quieto.
3. Un cluster es el repositorio del que desciende el nodo dentro de la tile.
   Los huérfanos -cuyo repositorio no entró- se agrupan por componentes
   conexas sobre sus dependencias en vez de quedar sueltos uno a uno.
4. Dentro de un repositorio, sus hijos directos se agrupan en comunidades con
   Louvain sobre el peso de dependencia entre subárboles; cada descendiente
   hereda la comunidad de la rama de la que cuelga, así que un archivo aterriza
   con su paquete. Cada comunidad ocupa su propio lóbulo de la bola.
5. La profundidad jerárquica se calcula sobre el DAG condensado: componentes
   fuertemente conexas por Tarjan, camino más largo sobre la condensación. Un
   ciclo no tiene profundidad propia -sus miembros comparten capa- y la capa
   sesga la altura de clusters, lóbulos y nodos con pesos decrecientes. Es una
   orientación, no una cuadrícula.
6. La centralidad es PageRank sobre las dependencias de la tile. Decide el
   tamaño dibujado, el espacio reservado, el orden dentro del lóbulo -el hub
   ocupa la cabeza de la espiral- y qué nodos llevan rótulo.
7. Los contenedores se dimensionan de abajo arriba antes de colocar nada, y el
   radio de una concha es la **suma cuadrática** de los radios de sus hijos,
   `R = √(Σr²)`. Para hijos iguales es exactamente el sitio que necesitan
   -`n` puntos en una esfera quedan a `R · 2/√n`, que debe cubrir dos radios-,
   y para hijos desiguales evita el mayor desperdicio de un layout anidado:
   tomar el más grande y multiplicarlo por `√n` manda treinta paquetes
   pequeños a la órbita del único grande y deja vacía la esfera entre ellos.
   Se añade el suelo de holgura contra el propio contenedor. Con eso el
   solapamiento es imposible por construcción y no algo que la relajación tenga
   que reparar.
8. Las bolas de cluster se siembran con la espiral de Fibonacci indexada por
   una permutación determinista de la identidad, y el radio lo da el tamaño.
   Tomar ambas cosas del mismo orden enrollaría los clusters en un cono. Luego
   se relajan: colisión, atracción por dependencia con suelo en el contacto,
   gravedad débil y la jerarquía en la vertical.
9. La relajación local sólo actúa dentro de un cluster y con un muelle hacia el
   objetivo estructural, para que ninguna fuerza local deshaga la organización
   global.
10. Si un eje queda más estrecho que la mitad del más ancho se reescala en
    torno al centroide. Estirar sólo aumenta distancias, así que no puede crear
    los solapamientos que la colocación acaba de evitar.
11. Todo es determinista: las direcciones salen del hash de la identidad del
    nodo -no de su índice en la tile- y no hay generador aleatorio. El mismo
    repositorio cae en la misma zona del mundo aunque cambien el nivel o el
    presupuesto.
12. Todas las distancias son proporciones del radio con el que se dibuja un
    nodo. Reservar espacio en unidades ajenas a lo que se pinta produce o una
    maraña o un campo de puntos invisibles.
13. Las aristas se clasifican: contención en trazo fino, dependencia local
    tenue, dependencia entre clusters más clara y curvada -para que las que
    comparten dirección se lean como un canal y no atraviesen el centro en
    recta-, y dependencia exacta en verde.
14. Sólo los repositorios y los hubs llevan rótulo permanente. Reagraph dibuja
    cada etiqueta a tamaño fijo en unidades de mundo, así que rotular mil nodos
    sólo produce una mancha; el nombre completo sigue a un cursor de distancia.
15. La cámara encuadra la extensión proyectada sobre sus propios ejes -no la
    esfera envolvente- y abre fuera de eje en azimut y elevación.
16. Al posar el cursor sobre un nodo se iluminan sus aristas y sus vecinos y se
    atenúa el resto; al salir se apaga. Reagraph sólo atenúa cuando hay
    selección, de modo que el nodo apuntado viaja en `selections` y su
    vecindario en `actives`. Ambas transiciones esperan `120` ms a que el
    cursor se pose: sin esa espera, cruzar el grafo encadena una
    reconstrucción de mallas por cada nodo rozado.

## Alternativas descartadas

- **Force-directed libre en el navegador:** ADR 0019 ya lo rechazó por coste y
  por no ser determinista. Además decide la arquitectura global por accidente:
  la estructura que el grafo sí conoce aparecería sólo si la simulación
  converge, y cambiaría en cada recarga.
- **Calcular el layout 3D en el servidor:** obligaría a un `LGVB` v3 con una
  coordenada más y, sobre todo, el servidor no sabe qué entra en la tile. Un
  layout global no puede agrupar lo que el recorte deja fuera.
- **`z` aleatoria:** añade profundidad visual sin aportar orden. Rotar seguiría
  sin explicar nada.
- **Clustering propio de Reagraph (`clusterAttribute`):** empaqueta círculos en
  2D alrededor de un atributo; ni usa la profundidad ni conoce comunidades.
- **Etiquetas por nivel de detalle de Reagraph (`labelType="auto"`):** su
  visibilidad se evalúa una vez al montar y depende de `camera.position.z`, que
  cambia fuera de React. Con una cámara que se mueve, las etiquetas nunca
  vuelven.
- **Apagar el resaltado sólo al salir del lienzo:** deja encendido un nodo que
  el cursor ya abandonó. Las dos transiciones pasan por el mismo temporizador
  de reposo y un `pointer-out` de un nodo que ya no es el apuntado se descarta,
  que es lo que hacía falta para que cruzar entre vecinos no parpadee.

## Consecuencias

- El layout cuesta `11` ms con `34` nodos, `19` ms con `138`, `40` ms con
  `5.000` y `58` ms con `10.000`, dentro del worker y antes del primer frame.
- El presupuesto por vista llega hasta `10.000` nodos, el mismo techo que
  impone el endpoint de tiles. Con el viewport raíz el presupuesto se gasta
  primero en los ancestros -`34` repositorios, `104` paquetes y `4.212`
  archivos-, así que el primer símbolo es el nodo `4.351` y como mucho caben
  `5.650` de los `82.443`. Ver más símbolos exige acotar el viewport, no subir
  el presupuesto.
- El mundo ya no corresponde a las coordenadas publicadas: `ladygraph index
  --full` no reproduce esta imagen, se reconstruye desde la estructura.
- Un tile con presupuesto bajo no llega a los símbolos: la vista lo declara
  como muestra del nivel pedido en vez de mentir sobre lo que dibuja.
- Resaltar un vecindario obliga a Reagraph a reconstruir sus mallas de arista.
  Medido en el nivel `files` con `1.461` aristas y WebGL por software, el
  resaltado tardó `2,2` s en aparecer; por eso ambas transiciones esperan
  `120` ms a que el cursor se pose, para no encadenar una reconstrucción por
  cada nodo rozado.
- Por encima de unos cientos de nodos los rótulos son ilegibles a la distancia
  de encuadre y hay que acercarse. Es geometría, no un defecto: mil nodos
  aireados en una pantalla dan puntos de pocos píxeles.
