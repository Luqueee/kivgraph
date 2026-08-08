# ADR 0024: Nivel de detalle según cámara

- **Estado:** aceptada
- **Fecha:** 2026-08-08
- **Revisa:** ADR 0018, ADR 0022, ADR 0023

## Contexto

El tile publicado conserva hasta `10.000` nodos para que el visor pueda mostrar
símbolos cuando el lector se acerca. Esa misma cantidad no es legible cuando la
cámara encuadra todo el grafo: los símbolos se convierten en textura de menos
de un píxel y las aristas internas dominan la escena. El servidor no debe
recortar el tile para resolver una limitación de una cámara concreta, porque
una tile es una muestra del mundo y el visor puede cambiar de escala sin otra
petición.

## Decisión

1. El visor calcula un nivel de detalle a partir de la distancia de la cámara,
   su campo de visión y la altura del viewport. La proyección usa el tamaño
   base de cada tipo de nodo, en las mismas unidades que `GraphCanvas` dibuja.
2. Se conservan cuatro niveles jerárquicos: `repositories`, `packages`,
   `files` y `symbols`. La selección empieza en el nivel solicitado por la
   tile y nunca muestra un nivel más profundo que el cargado.
3. El cambio usa histéresis: un nodo necesita `1.1` píxeles proyectados para
   entrar en un nivel y el nivel se conserva hasta bajar de `1` píxel. En el
   límite de cámara de Reagraph (`50.000` unidades, `fov=10` y viewport de
   `900` píxeles), el nivel `symbols` baja a `files`; la prueba del adaptador
   fija ese contrato.
4. `CameraLodObserver` vive dentro del `Canvas` y toma muestras desde el
   `useFrame` de React Three Fiber. Sólo cuando cambia el nivel se proyectan
   de nuevo los nodos, la contención y las rutas agregadas; el lienzo en reposo
   no recibe trabajo adicional.
5. La proyección reutiliza la jerarquía de contención existente. Los nodos
   ocultos conservan sus ancestros visibles y las dependencias se elevan a sus
   contenedores cuando corresponde. Una ruta agregada sigue siendo una
   representación visual marcada como `lodAggregate`, no una arista semántica
   nueva.

## Alternativas descartadas

- **Pedir una tile nueva en cada zoom:** aumenta latencia y tráfico, y hace que
  el servidor decida una vista que sólo conoce el cliente.
- **Escalar sólo la opacidad de los símbolos:** mantiene el coste de geometría
  y deja las aristas internas dominando la imagen.
- **Eliminar símbolos sin elevar las dependencias:** pierde la estructura que
  explica por qué dos contenedores están conectados.
- **Ejecutar una simulación o proyección en cada frame:** el layout publicado
  es determinista y estático; recalcularlo durante el movimiento desperdicia
  CPU y puede producir cambios visuales no reproducibles.

## Consecuencias

- Alejarse muestra primero la arquitectura de repositorios, paquetes y
  archivos, en vez de una nube ilegible de símbolos.
- Acercarse conserva el detalle disponible en la tile y permite volver a
  `symbols` sin recargar datos.
- La lectura visible cambia durante la interacción de cámara y el resumen HUD
  declara el nivel efectivo (`full detail`, `files LOD`, etc.).
- El nivel es una decisión de presentación: no modifica el `HotSnapshot`, el
  payload `LGVB` ni la semántica de sus aristas.
