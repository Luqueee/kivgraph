# ADR 0024: Backend WebGPU opcional del visor

- **Estado:** aceptada con límites
- **Fecha:** 2026-08-08
- **Revisa:** ADR 0018, ADR 0021, ADR 0023

## Contexto

El visor ya usa WebGL mediante React Three Fiber. Ese renderer sí usa la GPU
cuando el navegador expone una implementación acelerada; sin embargo, Three.js
ofrece un backend WebGPU con una ruta de materiales basada en nodos. Reagraph
4.32 no expone una fábrica de renderer en `glOptions`: copia las opciones como
objeto y siempre deja que React Three Fiber cree `WebGLRenderer`. Además, el
renderer WebGPU no acepta los materiales clásicos que Reagraph crea para
esferas, aristas, líneas y etiquetas.

No se puede activar WebGPU por una propiedad aislada ni afirmar que está activo
si el navegador no devuelve un adaptador utilizable.

## Decisión

1. El visor consulta `navigator.gpu.requestAdapter()` antes de montar
   `GraphCanvas`. Si no existe la API, no hay adaptador o la consulta falla,
   monta WebGL y muestra la razón del fallback junto al contador de FPS.
2. Se mantiene un parche mínimo y versionado de `reagraph@4.32.0` que permite
   que `glOptions` reciba una fábrica o una instancia de renderer. La fábrica
   crea `WebGPURenderer` con el canvas de React Three Fiber, espera a
   `renderer.init()` y comprueba que el backend efectivo siga siendo WebGPU;
   si Three.js cae a su backend WebGL interno, el visor remonta la ruta WebGL
   normal y muestra la causa.
3. En la rama WebGPU el catálogo de React Three Fiber conserva los nombres JSX
   que Reagraph ya usa, pero los resuelve a `MeshBasicNodeMaterial`,
   `MeshPhongNodeMaterial`, `LineBasicNodeMaterial` y `SpriteNodeMaterial`.
   El `renderNode` propio del visor usa también `MeshBasicNodeMaterial`; la
   rama WebGL conserva los materiales clásicos.
4. La elección es automática y no se mezcla con el layout, el payload `LGVB`,
   el worker ni el `HotSnapshot`. Una nueva selección de backend remonta el
   canvas para que no sobrevivan materiales o recursos del backend anterior.
5. El backend WebGL continúa siendo la ruta de compatibilidad y la única ruta
   verificada en el smoke test automatizado actual. WebGPU se considera una
   optimización oportunista, no un requisito de instalación ni una garantía de
   rendimiento.

6. La fábrica de fallback crea `WebGLRenderer` sin antialiasing. Durante una
   interacción de cámara, `FrameGovernor` limita el DPR a `1`, oculta
   temporalmente las etiquetas de Reagraph y pausa el picking de objetos; al
   terminar el gesto restaura el DPR y las etiquetas. La reducción es
   transitoria y no cambia el payload ni el layout publicado. Sólo se redibuja
   cuando el DPR realmente cambia y cada reconstrucción del drawing buffer
   fuerza un frame inmediato, evitando exponer un frame negro durante el gesto
   o su restauración.

7. El visor fija `troika-three-text@0.52.5` mediante un patch versionado. Su
   `GlyphsGeometry` empieza con `instanceCount = 0` y sólo habilita los glifos
   cuando el worker termina el atlas. El valor heredado `Infinity` es válido
   para WebGL, pero `drawIndexed` de WebGPU exige un entero finito y fallaba
   durante esa ventana asíncrona.

## Alternativas descartadas

- **Cambiar sólo `glOptions` desde `GraphPreview`:** Reagraph lo extiende como
  un objeto y descarta una fábrica; nunca llega un `WebGPURenderer` a R3F.
- **Usar `WebGPURenderer` con los materiales de Reagraph:** Three.js documenta
  que esa ruta requiere materiales nodo; las instancias clásicas pueden fallar
  al compilar o renderizar.
- **Copiar GraphScene y el store internos de Reagraph:** duplicaría una escena
  completa y rompería la actualización futura del paquete. El parche conserva
  el store y sólo cambia el contrato de `glOptions`.
- **Forzar WebGPU sin consultar un adaptador:** haría que navegadores sin
  soporte fallen en vez de conservar el visor WebGL.

## Consecuencias

- En un navegador con adaptador WebGPU, la escena se inicializa con el backend
  WebGPU y materiales compatibles con nodos.

- En el fallback WebGL, el smoke test de rotación mostró el indicador `58 fps`,
  `p50=16,7 ms`, `p95=16,8 ms` y `1` intervalo superior a `25 ms` en
  `1.500 ms`; la cifra depende del navegador, GPU, DPR y tamaño de la tile,
  por lo que no es un SLO universal.
- En navegadores sin soporte, el visor sigue funcionando y el usuario ve
  `WebGL fallback` con la causa concreta; no se emite un PASS de WebGPU.
- El bundle incluye `three/webgpu`, los parches de Reagraph y
  `troika-three-text`. El coste de carga y la mejora de FPS en una GPU
  discreta todavía requieren una medición independiente con Chromium
  no-headless o un navegador que exponga un adaptador real.
- Los tests cubren la decisión de capacidad, sus fallos y el conteo finito de
  instancias de Troika antes de que termine la carga asíncrona. El smoke test
  de CI cubre WebGL; la ruta WebGPU queda pendiente de un entorno con WebGPU
  real y de una métrica comparativa contra WebGL.
