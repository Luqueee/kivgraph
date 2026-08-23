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
  despliegue fija `KIVGRAPH_LANDING_URL` y un host escrito a mano publicaría el
  canonical de otra máquina. Los hechos del proyecto -nombre, lema, repositorio,
  licencia, la lista de tools- viven una sola vez en `landing/src/pages/_seo.ts`,
  que consumen el shell de la landing, el override de `Head` y los tres
  endpoints.
- El entorno del despliegue se declara en `landing/.env`, y `astro.config.mjs`
  lo lee con `process.loadEnvFile()` antes de que Vite mire: el config **no** es
  un módulo que Vite transforme, así que `import.meta.env` está vacío ahí y un
  `process.env` pelado sólo ve lo que exportó la shell. Eso es exactamente lo
  que publicó `http://localhost:6767` como canonical y en el sitemap de todas
  las páginas del despliegue. Una variable ya exportada gana sobre el fichero
  -medido-, y `.env` está en `.gitignore`.
- Las analíticas son un Umami autoalojado y **el tracker sale de ese par de
  variables o no sale**: `KIVGRAPH_UMAMI_SCRIPT_URL` y
  `KIVGRAPH_UMAMI_WEBSITE_ID`, que lee `umamiTracker()` en `_seo.ts`. Con una
  sola de las dos no se emite nada, así que `astro dev`, un build local y CI no
  pueden escribir en el dataset de producción. El `id` es un UUID que acuña la
  instancia en tiempo de ejecución, no un hecho del repositorio, y por eso no
  vive en `_seo.ts` como el resto. El `<script>` va `is:inline` -- el fichero lo
  sirve la instancia, no este bundle -- y lo emiten **las dos** mitades del
  sitio, incluido el 404, para que un visitante que pasa de la landing a la
  documentación siga siendo una sesión.
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

- La documentación de `landing/` está escrita para quien usa Kivgraph y sale de
  las fuentes del repositorio -`cmd/kivgraph/help.go`, `internal/config`,
  `internal/mcp/tools`, `README.md`-, copiadas literalmente cuando son un
  contrato. `docs/` sigue siendo material interno de ingeniería -ADRs,
  informes de cualificación- y no se publica.
- La referencia de tools documenta la superficie que `internal/mcp/server.go`
  registra, no el paquete `internal/mcp/tools`: son once tools, `get_source`
  entre ellas, y `get_unresolved_references` no está publicada -- esa pregunta
  la contesta el CLI. Sin generación publicada sólo se registra
  `index_project`, y la documentación lo dice donde se instala.

## El hero

- El hero es centrado y su fondo es CSS: una malla estática de `72px` en
  `--color-rule-strong` y un único brillo diagonal que la recorre, declarados
  en el `<style>` de `Hero.astro`. No hay canvas, ni `requestAnimationFrame`,
  ni observers, ni lectura de paleta desde la cascada: el lienzo sintético que
  había antes costaba 415 líneas y dibujaba un campo cuyas posiciones no
  codificaban ninguna relación del grafo.
- El brillo es el `::after` de la malla, no un hermano, para que la máscara de
  la malla lo recorte: un hermano iluminaría los bordes de la banda que la
  máscara existe para esconder. Va y viene con `alternate` en vez de dar la
  vuelta, porque un barrido de un solo sentido salta a su posición inicial en
  cada ciclo. `prefers-reduced-motion: reduce` lo detiene.
- El velo es una costura, no un scrim. Sólo cierra el patrón en los bordes de
  la banda; un lavado lo bastante opaco para «proteger» el texto borra la malla
  en su lugar, que es exactamente cómo un fondo acaba invisible.
- La banda no lleva `border-bottom`: la primera `Section` dibuja su propio
  `border-top`, y dos filetes contiguos leen como una regla de 2px que ninguna
  otra costura de la página tiene.
- Las barras de `TokenSaving.astro` se derivan de sus propias cifras y nunca se
  dimensionan a mano: una gráfica cuya geometría contradice sus números
  argumenta contra lo que va a demostrar.
- `global.css` omite el preflight de Tailwind a propósito, porque Starlight
  trae su propio reset. El de la landing vive en el `@layer base` de
  `Layout.astro` y es el que quita el chrome nativo de `<button>`
  (`appearance`, `background`, `font`). Una página que no use `Layout` -- una
  ruta de preview suelta, por ejemplo -- dibuja controles del sistema: caja
  `ButtonFace` clara con la tipografía del sistema. Ningún componente lo
  duplica.

## Movimiento

- `landing/src/lib/motion.ts` es la única capa de animación con JavaScript, y
  se carga sólo desde `Layout.astro`: la documentación de Starlight no la
  recibe. Anima reveals al hacer scroll y un parallax sobre el fondo del hero.
- **Nada por encima del pliegue se desvanece.** El `h1` es el candidato a LCP y
  un elemento en `opacity: 0` no es una pintura: fundirlo mueve la métrica
  tanto como dure la animación más lo que tarde el módulo en cargar.
- El estado inicial lo pone GSAP con `gsap.from`, **nunca el CSS**. Ningún
  elemento reposa en `opacity: 0` -- la única declaración así en las hojas de
  estilo es el keyframe que parpadea el cursor del transcript--, así que un
  bundle bloqueado deja la página entera visible en vez de esconderla detrás de
  un script.
- GSAP core más `ScrollTrigger` pesan `110,7 KB` sin comprimir y `43,5 KB` con
  gzip en el bundle de la portada, medidos sobre `dist/client/_astro`. No afecta
  a la indexación -- el contenido va en el HTML servido-- pero sí es JavaScript
  sin usar a ojos de Lighthouse. Cambiar el número al tocar la capa.
- Sólo se animan `opacity` y `transform`. Ninguno realimenta el layout, así que
  un reveal no puede contribuir a CLS.
- `prefers-reduced-motion: reduce` retorna antes de registrar el plugin: no se
  instrumenta nada, en vez de instrumentarlo y luego saltarlo.
- El contrato del marcado es `data-reveal` en un contenedor, cuyos hijos
  directos entran escalonados, y `data-hero-field` en el plano del hero. Los
  declara `Section.astro`, así que una sección nueva lo hereda.
- Una animación de carga no va en JavaScript diferido: el módulo puede
  ejecutarse después del primer pintado, y entonces la barra de `TokenSaving`
  se vería completa antes de encogerse para crecer. Esa crece en CSS.

## Verificación

```bash
make landing-check
make landing-build
```

`pnpm --dir landing check` es `format:check`, `lint` y `typecheck`
(`astro check`).
Se sirve con `pm2 start landing/ecosystem.config.cjs` en el puerto `6767`.
