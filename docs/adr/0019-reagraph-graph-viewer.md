# ADR 0019: Reagraph para el visor de grafos

**Estado:** aceptada  
**Fecha:** 2026-08-07  
**Revisa:** ADR 0018

## Contexto

El visor web ya valida el payload binario `LGVB` y necesita una superficie de
interacción mantenible para explorar nodos y aristas. El renderer Three.js
propio de la primera implementación resolvía el caso de buffers grandes con
`Points`, `LineSegments` y picking por color, pero duplicaba controles, layouts,
etiquetas y eventos que una librería especializada ya mantiene.

Reagraph `4.32.0` ofrece `GraphCanvas`, controles de cámara, layout, etiquetas y
eventos de nodos/aristas sobre WebGL. Su contrato público recibe arrays de
`GraphNode` y `GraphEdge`, por lo que no puede consumir directamente un
`ArrayBuffer` denso ni debe recibir sin límite el corpus completo de
`HotSnapshot`.

## Decisión

1. `web/` fija `reagraph` en `4.32.0`. Three.js, React Three Fiber y las
   dependencias de layout quedan encapsuladas por esa integración; la
   aplicación no mantiene otra escena Three.js propia.
2. El contrato `LGVB` no cambia. `decodeGraphPayload` conserva el
   `ArrayBuffer`, valida cabecera, secciones y snapshot, y
   `web/src/renderer/reagraph.ts` adapta únicamente un payload acotado al
   formato público de Reagraph.
3. El adaptador admite como máximo `2.000` nodos y `8.000` aristas por vista.
   Si el payload excede esos límites devuelve `REAGRAPH_NODE_LIMIT` o
   `REAGRAPH_EDGE_LIMIT`; nunca trunca nodos o aristas en silencio. Un endpoint
   de tiles o neighborhood debe entregar la vista acotada antes de invocar al
   adaptador.
4. `GraphCanvas` usa `layoutType="custom"` y posiciones calculadas desde las
   coordenadas enteras del payload. Se desactiva la animación global, se
   conservan pan/zoom y el hover expone el índice y tipo del nodo. No se
   ejecuta una simulación force-directed global en el navegador.
5. Las aristas se entregan con geometría homogénea (`dashed: false` y
   `arrowPlacement: "none"`); `confidence` sigue expresándose mediante el color
   de relleno. Así se evita que la agregación de geometrías de Reagraph falle
   por atributos incompatibles.
6. El visor deja de prometer draw calls constantes o picking GPU por color-ID.
   Reagraph gestiona el render y el picking de interacción; el gate
   `WEB_VIEWER_PERFORMANCE_PASS` debe medir esta implementación antes de
   declarar rendimiento a escala de producción.
7. La topología de `100.000` símbolos y `1.000.000` de aristas sigue siendo
   responsabilidad del servidor: se sirve por tiles o vecindades y no se
   materializa completa en objetos React/Reagraph.

## Alternativas descartadas

- **Mantener el renderer Three.js propio:** escala mejor con buffers densos,
  pero duplica la superficie de interacción y contradice la decisión de usar
  una librería especializada para el grafo.
- **Pasar todo el snapshot a Reagraph:** crea un objeto de librería por entidad,
  aumenta presión de memoria y no ofrece una ruta segura para hubs grandes.
- **Truncar el payload en el cliente:** ocultaría hechos y aristas sin una
  semántica de viewport explícita; se rechaza con un código estable.
- **Ejecutar el layout force-directed global:** no es determinista y no escala
  al corpus de aceptación.

## Consecuencias

- El bundle web incorpora el coste de Reagraph y sus dependencias; Vite puede
  emitir avisos de chunks grandes y deben permanecer visibles.
- Las vistas pequeñas conservan el layout determinista del snapshot, y la
  interacción queda en callbacks de Reagraph sin serializar el payload.
- Las aristas no usan flechas ni trazos discontinuos en esta integración; la
  semántica de `confidence` queda visible por color y el detalle semántico
  completo sigue perteneciendo al snapshot y a sus APIs.
- La capacidad de una vista está explícitamente limitada. El chrome posterior
  debe manejar el error estable y pedir otra tile o neighborhood, no intentar
  recuperarse con un fallback silencioso.
- Los tests del adaptador cubren IDs únicos, referencias, coordenadas,
  confianza de aristas y rechazo por límite. El smoke visual debe comprobar
  canvas WebGL, pan, zoom y hover sobre `GraphCanvas`.
