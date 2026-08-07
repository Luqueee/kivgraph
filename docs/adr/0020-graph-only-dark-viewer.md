# ADR 0020: Superficie del visor limitada al grafo y tema oscuro

**Estado:** aceptada · **Fecha:** 2026-08-07 · **Revisa:** ADR 0018, ADR 0019

## Contexto

La primera iteración de `web/` incluía una landing: cabecera, secciones de
presentación, textos de contrato y un primitive `Button` generado por la CLI de
shadcn. Ese contenido no participa en la exploración del grafo, obliga a
mantener dependencias de UI (`radix-ui`, `lucide-react`,
`class-variance-authority`, `clsx`, `tailwind-merge`) y desplaza al visor a una
sección dentro de una página de marketing.

El uso real del paquete es un previsualizador: abrir la URL y explorar el grafo
publicado. Además, la lectura de un grafo con nodos y aristas de color es más
estable sobre fondo oscuro.

## Decisión

1. `web/` renderiza exclusivamente el visor. `App` monta `GraphPreview` a
   pantalla completa (`h-svh`); no hay cabecera, secciones, textos de
   presentación ni navegación por anclas.
2. Se retiran los artefactos que solo sostenían esa landing:
   `web/src/components/ui/button.tsx`, `web/src/lib/utils.ts` y las
   dependencias `radix-ui`, `lucide-react`, `class-variance-authority`, `clsx`
   y `tailwind-merge`.
3. Se retiran también las dependencias directas `three` y `@types/three`: tras
   ADR 0019 ningún módulo del paquete importa Three.js; Reagraph resuelve su
   propia copia.
4. El visor es oscuro por construcción: `index.html` fija `class="dark"` y
   `color-scheme: dark`, y `GraphCanvas` usa el `darkTheme` de Reagraph. No hay
   selector de tema ni palabra clave de tema claro en la UI.
5. La regla de shadcn sigue vigente para futuros componentes: si el chrome de
   `LUQUE-1711` necesita primitives, se añaden con la CLI y no a mano. Hoy el
   paquete no vendoriza ninguno.

## Alternativas descartadas

- **Conservar la landing y anclar el visor debajo:** mantiene dependencias y
  texto que nadie consume y esconde la superficie útil.
- **Ofrecer conmutador claro/oscuro:** añade estado y una segunda paleta que
  probar sin necesidad demostrada; el visor es una sola vista.
- **Conservar `Button` y `cn` «por si acaso»:** código muerto que la CLI vuelve
  a generar en cuanto haga falta.

## Consecuencias

- El bundle baja de `1.596,12 kB` a `1.555,01 kB` minificado y el CSS de
  `29,43 kB` a `13,54 kB`; el warning de chunk de Reagraph sigue visible.
- Los tests del shell comprueban que el markup contiene el canvas del visor y
  que no reaparecen `header`/`section` de landing.
- El gate `WEB_VIEWER_PERFORMANCE_PASS` mide esta superficie, no la anterior.
