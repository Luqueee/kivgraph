# ADR 0023: Coste de render del visor

- **Estado:** aceptada
- **Fecha:** 2026-08-08
- **Revisa:** ADR 0018, ADR 0021, ADR 0022

## Contexto

Con el layout estructural en su sitio, el visor dibujaba un tile de `1.200`
nodos a `1` fotograma por segundo y seguía dibujándolo con el grafo quieto y
nadie tocando nada. Medido sobre el índice de `~/workspace` con WebGL por software:

```text
draw calls con la imagen quieta, 5 s   34.164
fotogramas por segundo en reposo            1
resaltar un nodo                        2.254 ms
```

El perfil de CPU atribuía el `82 %` a `(program)` -rasterizado, fuera de
JavaScript-, así que el problema no era el trabajo del adaptador sino lo que
se le pedía a la GPU en cada fotograma. Tres causas, todas medibles:

1. Reagraph dibuja cada nodo con una esfera de `25 × 25` segmentos: `1.250`
   triángulos para un punto de cinco píxeles, `1,5` millones por tile, con un
   material Phong translúcido propio por nodo.
2. El bucle de render está siempre activo. Un grafo publicado no se mueve, y
   aun así se redibujaba entero sesenta veces por segundo.
3. La contención es una arista por nodo -`1.200` de las `1.460` de un tile de
   archivos- y Reagraph reconstruye y fusiona la geometría de todas sus
   aristas cada vez que cambia el conjunto resaltado.

## Decisión

1. El visor aporta su propio `renderNode`: una geometría de esfera de
   `10 × 8` compartida por todos los nodos y materiales básicos compartidos por
   color y opacidad. El material del tema es un Phong iluminado sólo por una
   luz ambiental con `0.7` de emisivo del propio color, que al tamaño al que se
   dibuja es un disco plano de ese color; calcularlo por fragmento no compra
   nada. Un tile de mil nodos pasa de `1,5` millones de triángulos y mil
   materiales a `192.000` y una decena.
2. El bucle de render pasa a `demand`: se dibuja cuando el árbol de React hace
   commit -tile nueva, resaltado- y mientras el puntero trabaja sobre el
   lienzo, y se detiene `700` ms después del último evento. Reagraph no expone
   `frameloop`, así que un hijo del lienzo lo fija sobre el store de React
   Three Fiber y lo vuelve a reclamar cuando un commit lo devuelve a `always`.
3. El contador de FPS vive en su propio componente. Refrescarlo dos veces por
   segundo desde el visor re-renderizaba el lienzo entero, y cada commit del
   lienzo reaplicaba su `frameloop`, que es justo lo que impedía que el modo
   bajo demanda se sostuviera.
4. La contención deja de ser una arista y viaja como pares de índices. El
   visor la dibuja como una única malla de segmentos con color por vértice:
   nada la selecciona, y atenuarla al resaltar es una escritura en el atributo
   de color en vez de una reconstrucción de geometría.
5. `three` y `@react-three/fiber` pasan a ser dependencias directas, fijadas a
   la misma versión que resuelve Reagraph. Dos copias de `three` en el árbol
   romperían las comprobaciones de identidad de sus clases.

## Alternativas descartadas

- **Instanciar los nodos en un `InstancedMesh`:** es el paso que llevaría las
  `1.200` draw calls por fotograma a una, pero Reagraph monta un componente
  React por nodo y no hay forma de agruparlos sin sustituir su escena, que es
  precisamente lo que ADR 0020 decidió dejar de mantener.
- **Bajar el `devicePixelRatio` del lienzo:** Reagraph no expone `dpr` ni la
  prop `frameloop` del `Canvas`; sólo `glOptions`.
- **Apagar el bucle sólo cuando la cámara descansa:** depende de los eventos
  internos de `camera-controls`, que Reagraph conduce desde su propio
  `useFrame`. Escuchar los eventos DOM del lienzo es equivalente y no depende
  de detalles ajenos.
- **Dibujar la contención con `LineSegments2` (líneas gruesas):** exige un
  material de tiras y triplica los vértices. Una línea de un píxel basta para
  un trazo que sólo aporta contexto.

## Consecuencias

Medido sobre el mismo índice, mismo equipo y WebGL por software, en el nivel
`files` con `1.200` nodos:

```text
                              antes    después
draw calls en reposo (5 s)   34.164          0
fps interactuando                 1      26-42
cargar el nivel              402-879 ms   263 ms
resaltar un nodo              2.254 ms    354 ms
aristas que reconstruye       1.460         295
```

- En reposo el visor no consume GPU ni hilo principal. Es la diferencia entre
  quemar un núcleo mientras la pestaña está abierta y costar sólo cuando el
  lector hace algo.
- Quedan unas `1.200` draw calls por fotograma mientras se interactúa, una por
  nodo. Es el techo de esta integración y no se puede bajar sin instanciar.
- El contador de FPS mide llamadas a `requestAnimationFrame`, que el navegador
  sigue entregando aunque no se dibuje; en reposo el número que muestra ya no
  describe el coste, y por eso el coste se mide en draw calls.
