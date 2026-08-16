# Instrucciones del visor (`web/`)

Estas reglas se suman a las de `AGENTS.md` en la raíz del repositorio, que se
leen siempre. Una instrucción de este archivo puede añadir restricciones; nunca
puede relajar un contrato de integridad, compatibilidad o verificación
declarado en la raíz.

El servidor que publica las tiles y el payload `LGVB` está en `internal/`.

## Alcance y estilo

- La aplicación web en `web/` mantiene TypeScript estricto, ESM, Biome y
  Vitest; los payloads binarios grandes permanecen fuera del estado React y
  `web/dist` se regenera con el build de Vite.
- `web/` es solo el previsualizador del grafo: `App` monta el visor a pantalla
  completa y no contiene landing, cabecera ni secciones de presentación. El
  visor es oscuro por construcción (`class="dark"` y `darkTheme` de Reagraph).
- Los componentes UI que se necesiten deben añadirse con
  `pnpm dlx shadcn@latest init`/`add` y Tailwind CSS, no a mano ni con una
  segunda librería de estilos; el paquete no vendoriza primitives sin uso.
- `web/src/renderer` recibe el payload `LGVB` versionado como `ArrayBuffer`,
  conserva sus vistas fuera de React y solo materializa el límite visible que
  consume `reagraph`; el adaptador rechaza payloads que excedan el límite en
  vez de truncar silenciosamente la topología.

## Layout del mundo

- El visor no dibuja las coordenadas publicadas: deriva su propio layout 3D de
  la estructura del tile - contención, dependencias, comunidades y profundidad
  jerárquica - y lo calcula en el worker, una vez por tile. Una tile es una
  muestra del mundo, y la rejilla que empaquetó el servidor no dice nada sobre
  qué paquetes van juntos cuando falta la mayoría.
- Primero estructura y después física: los clusters se colocan y se relajan
  entre sí, cada nodo cuelga de su contenedor y sólo al final una relajación
  local corrige lo que la estructura no decidió. No hay simulación por frame.
- El layout es determinista: las direcciones salen del hash de la identidad
  del nodo, no de su posición en el tile ni de un generador aleatorio. El mismo
  tile dibuja siempre el mismo mundo.
- El radio de una concha es la suma cuadrática de los radios de sus hijos,
  nunca el mayor por la raíz del número: eso manda los hijos pequeños a la
  órbita del grande y deja el volumen intermedio vacío.
- Todas las distancias del layout son proporciones del radio con el que se
  dibuja un nodo. Reservar espacio en unidades ajenas a lo que se pinta produce
  o una maraña o un campo de puntos invisibles.
- Ningún eje puede colapsar: si uno queda más estrecho que la mitad del más
  ancho se reescala en torno al centroide. La profundidad que sólo existe en
  los números no vale nada.

## Qué se dibuja y cómo

- El visor no muestra IDs densos: cada nodo se rotula con el nombre que el
  snapshot conoce, acortado en el lienzo y completo al pasar el cursor. Un ID
  denso solo es único dentro de su tipo de nodo, así que los extremos de una
  arista se resuelven por `(tipo, id)`.
- El visor dibuja la contención declarada por `parent_kind`/`parent_id` cuando
  el contenedor viaja en la misma tile, y una leyenda nombra cada color y cada
  trazo. La paleta se declara una sola vez en el adaptador.
- Ninguna arista del visor es discontinua: Reagraph construye una curva y un
  tubo por guion, y con una arista por nodo eso domina el frame. La distinción
  se hace con color y grosor; las aristas entre clusters se curvan para no
  atravesar el centro en línea recta.
- Sólo los repositorios y los hubs llevan rótulo permanente; el resto sale al
  pasar el cursor. Reagraph dibuja cada etiqueta a tamaño fijo en unidades de
  mundo, así que rotularlo todo sólo produce una mancha gris.
- Al posar el cursor sobre un nodo se ilumina su vecindario y se atenúa el
  resto; al salir se apaga. Reagraph sólo atenúa cuando hay selección, así que
  el nodo apuntado va en `selections` y lo que toca en `actives`. Encender y
  apagar esperan a que el cursor se pose: cada cambio del conjunto activo
  reconstruye las mallas de arista.
- La cámara encuadra la extensión proyectada sobre sus propios ejes, no la
  esfera envolvente, y abre fuera de eje. Un layout estructural nunca es una
  bola, y encuadrar una deja el grafo en un tercio de la pantalla.
- La jerarquía se lee por tamaño: el rango dibujado va de `4` a `22` unidades y
  el lienzo recibe esos mismos límites como `minNodeSize`/`maxNodeSize`, porque
  Reagraph reescala los tamaños que recibe. El extremo pequeño no baja de `4`:
  por debajo un símbolo deja de ser un punto y no es nada.
- La contención se atenúa según lo que sostiene y la separación entre hermanos
  la fija el nodo típico del tile, no el mayor. Un contenedor con todos sus
  trazos al mismo brillo es un erizo, y espaciar símbolos con la medida de un
  repositorio infla el mundo hasta hacerlos invisibles.
- Los nodos se dibujan con una geometría de esfera compartida y materiales
  compartidos por color y opacidad. Una esfera de `25 × 25` por nodo son mil
  quinientos triángulos para un punto de cinco píxeles.
- La contención no es una arista: viaja como pares de índices y se dibuja como
  una única malla de segmentos con color por vértice. Hay una por nodo, nada
  la selecciona, y como arista obligaría a reconstruir toda la geometría del
  grafo cada vez que se mueve el resaltado.
- El visor elige el nivel de detalle según los píxeles proyectados de la
  cámara, conserva histéresis entre `1.1` y `1` píxeles y eleva dependencias
  hacia contenedores visibles; cambiar de nivel sólo reproyecta durante un
  frame de interacción y nunca modifica el tile.

## Coste por fotograma

- El visor dibuja bajo demanda: el bucle de render se detiene con el grafo
  quieto y despierta con los eventos de puntero del lienzo. Un grafo publicado
  no se mueve, y redibujarlo sesenta veces por segundo cuesta un núcleo a
  cambio de nada. Cualquier componente que refresque por su cuenta -el
  contador de FPS- vive fuera del lienzo: cada commit reaplica su `frameloop`.
- El visor pide las tiles desde un Web Worker con `AbortController` por
  petición; el render permanece en el hilo principal. El número de nodos por
  vista es ajustable desde la interfaz, una tile recortada se declara como tal
  y un contador de FPS expone el coste de la elección.
- El visor consulta `navigator.gpu` antes de montar WebGPU; sin un adaptador
  utilizable muestra el motivo y conserva WebGL. La ruta WebGPU usa materiales
  nodo y nunca se declara activa por una mera propiedad de configuración.
- La fábrica WebGL del visor desactiva antialiasing; `FrameGovernor` limita el
  DPR a `1` mientras el puntero se mueve - también al pasar por encima, no sólo
  al arrastrar - y durante un gesto sostenido pausa además el picking y oculta
  las etiquetas con `visible = false`. Todo se restaura al quedar inactivo, y
  cada cambio del drawing buffer se repinta sincrónicamente antes de exponerse
  al navegador, para no mostrar un frame negro.
- Las etiquetas no se desmontan durante un gesto: `labelType` permanece fijo
  porque conmutarlo reconstruye todas las mallas de glifos y detiene el
  arrastre más de `130 ms` en cada extremo del gesto.
- El rótulo del nodo apuntado no vive en el estado del visor: viaja por un
  canal propio (`createStatusChannel`) porque un `setState` por nodo rozado
  reconstruye el árbol de elementos de todo el grafo. La selección y su
  vecindario son un único estado y se aplican dentro de `startTransition`.
- Las geometrías instanciadas de Troika parten con `instanceCount: 0` hasta
  que llega el atlas de glifos asíncrono y no llaman a `dispose()` al sustituir
  un atributo: `troika-three-text@0.52.5` permanece fijado y parcheado porque
  WebGPU rechaza el valor por defecto `Infinity` y porque liberar los buffers
  de una geometría viva deja al backend enlazando un índice inexistente.
- Antes de construir el `WebGPURenderer`, el visor resuelve `instanceCount` en
  `InstancedBufferGeometry` con la misma regla que WebGL - el mínimo de
  `meshPerAttribute * count` entre los atributos instanciados, `0` sin
  ninguno - para que ninguna librería pueda llevar un `Infinity` a
  `drawIndexed`.

## Verificación

```bash
cd web
pnpm check
pnpm build
```

`pnpm check` es `format:check`, `lint`, `typecheck` y `test`. `dist/` es
generado con Vite y lo sirve `internal/webassets` sólo en builds con el tag
`webassets`.
El benchmark end-to-end está en `benchmarks/web-viewer/`.
