# Instrucciones de la landing y la documentación (`landing/`)

Estas reglas se suman a las de `AGENTS.md` en la raíz del repositorio, que se
leen siempre. Una instrucción de este archivo puede añadir restricciones; nunca
puede relajar un contrato de integridad, compatibilidad o verificación
declarado en la raíz.

No entra en ningún bundle publicado; la lista blanca del payload vive en
`scripts/build-bundle.sh`.

## Alcance y proyecto

- `landing/` es la landing pública y la documentación de usuario, en Astro con
  Starlight, y es su propio proyecto pnpm como `ts-worker/` y `web/`. No entra
  en ningún bundle: el payload de `scripts/build-bundle.sh` es una lista blanca
  y el workflow de release comprueba que ni el directorio ni una línea de
  `SHA256SUMS` lo nombran. Se verifica con `make landing-check` y `make
  landing-build`, y se sirve con `pm2 start landing/ecosystem.config.cjs` en el
  puerto `6767`.
- La landing no es una página de Starlight: su shell es
  `landing/src/components/landing/Layout.astro` y sus componentes viven en ese
  mismo directorio; sólo la documentación lleva el chrome de Starlight
  -sidebar, Pagefind, tabla de contenidos-. Es oscura por construcción, como el
  visor: los overrides de `landing/src/components/starlight/` fijan
  `data-theme="dark"` y no montan selector, porque la paleta clara de Starlight
  vive entera bajo `[data-theme='light']` y basta con no seleccionarla nunca.

## Capas, paleta y ritmo

- `global.css` importa las utilities de Tailwind dentro de la capa `utilities` y
  no importa preflight, así que una página fuera de Starlight no tiene reset y
  **una regla de componente sin capa gana a cualquier utility**, tenga la
  especificidad que tenga. El reset de la landing es por eso un `@layer base`
  con `:global()` dentro del shell: sin la capa, una regla como `p { margin: 0 }`
  anulaba en silencio cada `mt-*` de la página. Un grid que deba encogerse
  declara además su columna base -`grid-cols-1`-, porque la columna implícita
  la dimensiona el hijo más ancho y un `<pre>` desbordaba el viewport.
- Las dos mitades del sitio llevan la misma piel y **una sola declaración de
  paleta**: los tokens viven en el `@theme` de `global.css` y nadie escribe un
  literal de color en otro sitio. Las superficies son `--color-shell`,
  `--color-panel`, `--color-raise`, `--color-rule` y `--color-rule-strong`, y
  sus nombres evitan un prefijo `t-` a propósito: `border-t-<color>` es la
  utility de `border-top-color` de Tailwind, así que un `--color-t-line` haría
  que `border-t-line` significase dos cosas. La prosa es `--color-gray-300`, las
  etiquetas `--color-gray-400`, y nada más tenue que eso lleva texto.
- `landing/src/styles/docs.css` es la piel de Starlight y **sólo** entra por
  `customCss` después de `global.css`: la landing no lo carga. Reasigna las
  propiedades `--sl-*` -- que existen todas en
  `node_modules/@astrojs/starlight/style/props.css`, nunca se inventa un
  nombre -- y las `--ec-*` de Expressive Code a esos tokens. Todo radio es `0`
  y no hay sombras.
- La jerarquía de encabezados es la misma en las dos mitades y la lleva un
  marcador monoespaciado, no el tamaño: la landing pone un índice tenue (`01`,
  `#`) delante de un título `--color-gray-100`, y los docs ponen `##` en `h2` y
  `#` en `h3`. Un `h2` de sección manda sobre todo subtítulo. Lo que es una
  etiqueta -- cabecera de columna, pie de figura, clave de una fila -- se queda
  en `--color-gray-400` y **fuera** de la jerarquía: los rótulos de columna del
  footer eran `h2` con estilo de subtítulo y ahora el footer no aporta ningún
  encabezado.
- El filete sobre un `h2` de la documentación va en `.sl-heading-wrapper`, no en
  el `h2`: Starlight lo deja `display: inline` para que el ancla quepa al final
  de la última línea, así que un borde en el propio `h2` abrazaría las palabras
  en vez de cruzar la columna. Medir `main h2` devuelve `0px` y no significa que
  la regla esté muerta.
- El ritmo vertical de la landing se declara en `Section.astro` y se aplica sin
  excepciones: `py-14` por banda, `mt-10` de cabecera a contenido y entre
  bloques, `mt-5` de subtítulo a su bloque, `mt-3` de bloque a su nota. Dos
  bandas contiguas quedan a `7rem` y dos bloques de una banda a `5rem`, que es
  lo que evita que se lean como una sola.
- Una fila de puntos suspensivos (`LeaderRow.astro`) no puede solapar a su
  vecina: clave y valor son `min-w-0` y la fila envuelve, porque con los dos
  extremos `shrink-0` el ancho mínimo de la fila excedía su pista de grid y
  `snapshot_built_at 2026-08-15T11:14:10Z` se metía encima de `schema_version`.
  Las rejillas de lectura usan `auto-fit` con `minmax(min(20rem,100%),1fr)`, así
  que sólo se añade columna cuando cabe entera.

## SEO y lectores agente

- El SEO del sitio está escrito para dos lectores y el segundo es un agente. Lo
  que existe para él: `/llms.txt` con el índice y `/llms-full.txt` con toda la
  documentación en una sola petición, más el markdown de cada página en
  `/raw/<ruta>.md`, que es lo que anuncia su `<link rel="alternate">`. Los tres
  se generan con `getCollection`, nunca desde una lista escrita a mano: una
  página nueva no puede dejarlos rancios.
- `robots.txt` permite explícitamente a los rastreadores de IA y **cada token
  sale de la documentación de su operador**, con la URL y la fecha impresas en
  el propio fichero. Un token que no se pueda verificar no entra. Tres de ellos
  -`ChatGPT-User`, `Perplexity-User`, `Meta-ExternalFetcher`- los publican sus
  operadores advirtiendo que pueden ignorar `robots.txt`: se nombran porque un
  permiso ignorado no cuesta nada, y el comentario lo dice en vez de sugerir un
  control que quizá no obliga.
- Toda URL absoluta se deriva de `Astro.site`, nunca de un literal: el
  despliegue fija `LADYGRAPH_LANDING_URL` y un host escrito a mano publicaría el
  canonical de otra máquina. Los hechos del proyecto -nombre, lema, repositorio,
  licencia, la lista de tools- viven una sola vez en `landing/src/pages/_seo.ts`,
  que consumen el shell de la landing, el override de `Head` y los tres
  endpoints.
- El JSON-LD del sitio es **un** grafo: la landing publica
  `SoftwareApplication` en `<site>/#software` y `WebSite` en `<site>/#website`, y
  cada página de documentación emite un `TechArticle` que los referencia por
  `@id` en vez de duplicarlos. El `BreadcrumbList` es un nodo hermano, no una
  propiedad del artículo: schema.org pone `breadcrumb` sólo en `WebPage`.
- El override `Head` añade únicamente lo que Starlight no emite. Starlight ya
  escribe el título, la descripción, el canonical y `twitter:card`, así que
  repetir cualquiera de los cuatro es un defecto, no una redundancia inocua.

## Iconos

- Los iconos son assets commiteados, no un paso de build:
  `landing/scripts/icons.sh` los genera desde una imagen cuadrada con
  ImageMagick o, en su defecto, con `sips`, y falla cerrado sin ninguno de los
  dos. CI corre en Linux y nunca lo ejecuta. El maskable deja la marca dentro
  del 80 % central porque el lanzador recorta a un círculo, y `og.png` es la
  marca centrada en 1200x630.
- El lienzo de un icono rellenado es `--bg`, y tiene que ser **el color de fondo
  de la propia imagen fuente**, no el de la página: la marca actual trae
  `#0f1117` y rellenar con `#0a0b0d` dejaba un recuadro visible dentro de
  `og.png` y del maskable. Por eso `background_color` del manifest es `#0f1117`
  -- es el campo que el lanzador pinta detrás del icono -- mientras `theme_color`
  sigue siendo el `#0a0b0d` del sitio.
- `--zoom` recorta la fuente a su centro antes de escalar, porque un icono de
  app quiere la marca al ~80 % del lienzo y ésta ocupaba el 60,9 % medido: a
  16 px eso son diez píxeles de marca en una pestaña. Se mide, no se estima.
- No hay marca vectorial: la fuente es un ráster. Un `favicon.svg` rancio no es
  inofensivo, **gana**, porque el navegador prefiere el vector -- el de la marca
  antigua seguía sirviéndose y era lo que se veía en la pestaña. `icons.sh
  --drop-svg` lo retira, y entonces `favicon` de Starlight apunta al PNG de
  32 px, que emite como `rel="shortcut icon"`.

## Contenido

- La documentación de `landing/` está escrita para quien usa Ladygraph y sale de
  las fuentes del repositorio -`cmd/ladygraph/help.go`, `internal/config`,
  `internal/mcp/tools`, `README.md`-, copiadas literalmente cuando son un
  contrato. `docs/` sigue siendo material interno de ingeniería -ADRs,
  informes de cualificación- y no se publica.
- La referencia de tools documenta la superficie que `internal/mcp/server.go`
  registra, no el paquete `internal/mcp/tools`: son once tools, `get_source`
  entre ellas, y `get_unresolved_references` no está publicada -- esa pregunta
  la contesta el CLI. Sin generación publicada sólo se registra
  `index_project`, y la documentación lo dice donde se instala.

## El hero

- El lienzo del hero (`landing/src/lib/hero-graph.ts`) es un grafo sintético en
  2D, sin three.js ni reagraph, y no importa nada de `web/`. Toma la paleta de
  las propiedades `--color-graph-*` de `global.css`, que son los literales de
  `web/src/renderer/reagraph.ts`, y dibuja bajo demanda: un
  `IntersectionObserver` detiene el bucle fuera de pantalla y
  `prefers-reduced-motion: reduce` pinta un solo fotograma sin instalar bucle
  alguno. No acepta entrada de puntero: es una figura, no un control, y no
  instala ningún listener. El elemento declara ancho **y** alto en CSS; dejar
  uno al tamaño intrínseco hace que el drawing buffer realimente la caja y el
  canvas crece sin límite.

## Verificación

```bash
make landing-check
make landing-build
```

`pnpm --dir landing check` es `format:check`, `lint` y `typecheck`
(`astro check`).
Se sirve con `pm2 start landing/ecosystem.config.cjs` en el puerto `6767`.
