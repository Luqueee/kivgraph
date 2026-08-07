# ADR 0018: Visor React, Vite y Three.js con payload binario

**Estado:** aceptada, revisada por ADR 0019
**Fecha:** 2026-08-07


## Contexto

El repositorio contiene un worker TypeScript estricto, pero no una aplicación
web. `ts-worker` usa pnpm 11.5.1, Node 22, Biome, Vitest y TypeScript 7.0.2;
no existe un workspace pnpm de monorepo ni un bundler web propio.

El `HotSnapshot` probado contiene 100.000 símbolos y 1.000.000 de aristas. Una
UI que cree un objeto Three.js por nodo o arista, ejecute force layout en el
hilo principal o coloque buffers grandes en estado React no cumple un objetivo
de interacción rápida.

## Decisión

1. La aplicación vivirá en `web/` como paquete TypeScript independiente. En
   la primera iteración conservará su lockfile y lifecycle separado de
   `ts-worker`; la CI tendrá pasos explícitos para ambos paquetes.
2. El stack será React + Vite + Reagraph + Tailwind CSS. Reagraph encapsula
   React Three Fiber, Three.js y los controles WebGL; los primitives de UI se
   inicializarán y añadirán mediante `pnpm dlx shadcn@latest init`, con el
   preset Radix Nova y variables CSS. La aplicación no mantiene una segunda
   escena Three.js propia.
3. El paquete mantendrá `strict: true`, ESM, Biome y Vitest, fijará pnpm
   11.5.1 y Node 22, y usará un `check` que cubra formato, lint, typecheck y
   tests. `dist/` será generado y nunca editado manualmente.
4. El contrato `LGVB` seguirá viviendo en `ArrayBuffer` fuera del estado React.
   El decoder valida el payload y el adaptador crea solo la vista acotada que
   recibe Reagraph; la transferencia desde un Web Worker queda separada del
   componente de presentación.
5. `GraphCanvas` gestiona la cámara, el layout, las etiquetas y los eventos de
   interacción. El picking de color-ID de la primera implementación queda
   sustituido por el picking de eventos de Reagraph.
6. El layout inicial seguirá siendo jerárquico y determinista
   (`repository`, `package`, `file`, `symbol`) con niveles de detalle. El
   adaptador entrega `layoutType="custom"` y el servidor debe devolver solo el
   nivel y viewport necesarios; no se ejecuta force layout global.
7. La topología grande se entregará mediante un formato binario versionado,
   con cabecera, `snapshot_id`, conteos, offsets y buffers densos para IDs,
   posiciones, tipos y confianza. JSON quedará para metadata y detalles
   pequeños.
8. El bundle de Vite se copiará reproduciblemente desde `web/dist` al
   directorio `web/` del bundle Linux amd64. El binario de distribución se
   compilará con el tag `webassets`; los builds normales no requieren Node y
   devuelven un fallback HTTP `503` explícito cuando no hay bundle. Cada asset
   copiado se registra con hash SHA-256 y tamaño en `manifest.json` y
   `SHA256SUMS`.

## Contrato binario v1 (superado por ADR 0021)

Los endpoints `/api/v1/tiles` y las respuestas binarias de
`/api/v1/neighborhood` usan `application/octet-stream` con un blob único:

- cabecera fija de 64 bytes, magic `LGVB`, versión little-endian `uint16` en
  el offset `4` y `snapshot_id` `uint64` en el offset `8`;
- conteos y offsets `uint32` para nodos y aristas en los offsets `16`–`40`;
- el flag `1` en el offset `7` indica truncamiento por presupuesto;
- cada nodo ocupa 48 bytes: IDs densos, tipos, nivel y cuatro coordenadas
  `int64` half-open;
- cada arista ocupa 16 bytes: source, target, evidence, kind, confidence,
  provenance y flags;
- el payload completo no puede superar `32 MiB`.

Los IDs densos solo son válidos junto con el `snapshot_id` y el tipo de nodo.
Un `format_version` desconocido, un `snapshot_id` divergente o un payload que
supere el límite se rechazan con códigos HTTP estables; nunca se interpreta un
buffer de otra versión.

ADR 0021 sube este contrato a v2: añade una sección de etiquetas tras las
aristas y declara su offset y tamaño en los bytes `56`–`64` de la cabecera.


## Alternativas descartadas

- **JSON completo para la topología:** multiplica tamaño, parseo y presión de
  memoria en el navegador.
- **Force-directed global en el navegador:** no escala a 100.000 símbolos y
  1.000.000 de aristas; además produce layout no determinista.
- **Estado React para buffers:** fuerza reconciliaciones y copias innecesarias.
- **Un paquete web dentro de `ts-worker`:** mezcla runtime de análisis con
  runtime de navegador y amplía el riesgo del bundle existente.
- **Renderer Three.js propio como superficie final:** duplica controles y
  eventos; la implementación quedó sustituida por Reagraph en ADR 0019.
- **Pasar el snapshot completo a Reagraph:** materializa objetos por entidad y
  no ofrece una ruta segura para hubs grandes; el adaptador usa límites.

## Riesgos

- Un corpus real con hubs puede superar los límites de una vista Reagraph;
  el servidor debe entregar tiles o neighborhoods acotados.
- La memoria de GPU, el picking por raycast y el rendimiento de líneas varían
  mucho entre hardware; el gate debe registrar el adaptador y no prometer
  rendimiento universal.
- Un decoder binario defectuoso podría provocar lecturas fuera de rango o
  aceptar datos de otro snapshot; la validación debe ocurrir antes de adaptar
  buffers al renderer.

## Consecuencias

- Se necesitan benchmarks de payload, decodificación, primer frame, pan/zoom,
  hover, memoria y subgrafos antes de declarar el gate del visor.
- Los niveles de detalle y los límites del adaptador son parte del contrato:
  rechazar una vista demasiado grande es preferible a ocultar aristas sin una
  semántica de viewport explícita.
- El formato binario requiere versión, validación de longitudes y rechazo
  seguro de buffers truncados o de otro `snapshot_id`.
- React/Vite/Reagraph/Three.js introducen dependencias nuevas y deben fijarse
  en el lockfile del paquete. Su compatibilidad real con TypeScript 7 debe
  mantenerse cubierta por el typecheck del paquete.
- La aplicación seguirá siendo un cliente read-only. No incluirá edición,
  indexación ni publicación de snapshots.
