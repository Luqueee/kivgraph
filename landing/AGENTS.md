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
- El corolario tiene su propia víctima medida: un `margin: 0` puesto en el
  `<style>` de un componente para resetear el `<fieldset>` de las pestañas de la
  portada se comió el `mt-10` que lo separaba de su propio texto. Un reset de
  margen **sólo** puede vivir en el `@layer base` de `Layout.astro`; lo que se
  queda con el componente es lo que ninguna utility cubre, y en ese caso es
  `min-width: 0`, porque el UA da al `<fieldset>` un `min-width: min-content`
  que rompe cualquier grid en el que entre.
- Las dos mitades del sitio llevan la misma piel y **una sola declaración de
  paleta**: los tokens viven en el `@theme` de `global.css` y nadie escribe un
  literal de color en otro sitio. Las superficies son `--color-shell`,
  `--color-panel`, `--color-raise`, `--color-rule` y `--color-rule-strong`, y
  sus nombres evitan un prefijo `t-` a propósito: `border-t-<color>` es la
  utility de `border-top-color` de Tailwind, así que un `--color-t-line` haría
  que `border-t-line` significase dos cosas. La prosa es `--color-gray-300`, las
  etiquetas `--color-gray-400`, y nada más tenue que eso lleva texto. No es una
  preferencia: `--color-gray-500` sobre `--color-panel` mide `3,96:1` y AA pide
  `4,5:1` para texto normal, así que un rótulo en `gray-500` es un fallo de
  contraste. La portada tenía 34 cuando se midió por primera vez, todos de ese
  mismo par; con las etiquetas en `gray-400` la cuenta es `0` en la portada, en
  `/comparison/`, en `/install/` y en `/kivgraph-faq/`.
- Y **una sola regla de tipografía**, por la misma razón. `docs.css` ya la
  enunciaba para su mitad: mono para lo que es un titular, una etiqueta, un
  identificador, un comando, una cabecera de tabla o una cifra; sans para la
  prosa. La landing llevaba `font-mono` en el `<body>`, así que arrastraba cada
  párrafo a la cara de terminal, dejaba sin usar la Geist Sans que la página
  descarga igual, y hacía que el `h1` rompiese en tres líneas. Ahora el `<body>`
  es `font-sans` y `font-mono` se pone donde significa algo. Un componente
  nuevo que escriba prosa no declara fuente; uno que escriba una etiqueta o un
  readout declara `font-mono`.
- El titular de la portada es el caso que esa regla obliga a medir: una cara
  monoespaciada compone mucho más ancho que una proporcional al mismo cuerpo, y
  las 52 letras de la frase sólo caben en dos líneas cuando la columna pasa de
  `52rem`. De ahí `max-w-4xl` en vez del `max-w-3xl` de la prosa, y un cuerpo
  que escalona dos veces -- `text-3xl sm:text-4xl lg:text-5xl`-- porque a los
  `720px` que deja una tablet los 48px volvían a partirlo en tres con `it.`
  solo en la última. Medido a `1728`, `1440`, `1024`, `768` y `390`: dos líneas
  en las cuatro primeras, tres sin huérfana en la última, y `0` de
  desbordamiento horizontal en todas.
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
- Toda página publicada se alcanza desde un enlace interno, no sólo desde el
  sitemap. Un sitemap es un descubrimiento, no una recomendación: una URL que
  sólo vive ahí es lo que Search Console informa como «descubierta, actualmente
  sin indexar» y nunca llega a rastrear. Medido sobre `dist/client`: las 37
  URLs del sitemap y los enlaces internos que apuntan a cada una. Cada página
  de la documentación recibe 37 o más desde la barra lateral de Starlight;
  `/releases/` recibía `0`, porque no está en la barra lateral y el footer de
  la landing no la nombraba. Por eso está en el grupo `start` del footer. Al
  añadir una página, contar sus enlaces entrantes antes de cerrar.
- El namespace de la documentación se renombró `reference` -> `docs` y el
  redirect de `astro.config.mjs` apunta a la forma **con** barra final, que es
  la única canónica del sitio. Sin ella el `301` aterrizaba en
  `/docs/<ruta>` -- que responde `200` y declara un canonical distinto del
  suyo--, o sea un salto más del que un rastreador necesita. Verificado:
  `num_redirects=1` y la URL final es la canónica.
- `trailingSlash: "always"`. El valor por defecto de Astro es `ignore`, que
  servía las dos formas: `/docs/cli` y `/docs/cli/` respondían `200` con el
  mismo HTML y el mismo canonical, o sea una segunda URL por página que un
  rastreador busca y descarta -- 37 de ellas. Astro advierte que en las páginas
  prerenderizadas la barra final es cosa del host y puede ignorar la
  configuración, así que se midió en vez de suponerlo: con el adaptador
  `node` en `standalone` **sí** la respeta y la forma sin barra devuelve `301`
  a la canónica. Comprobado además que no alcanza lo que lleva extensión:
  `/llms.txt`, `/robots.txt`, `/sitemap-0.xml`, `/raw/<ruta>.md` y `/og.png`
  siguen en `200`.
- Un `ListItem` de `BreadcrumbList` lleva `item` **siempre menos en el último**,
  y eso es de Google, no de schema.org, que lo marca opcional. Las dos cosas
  son verdad a la vez y por eso el error era fácil: este sitio emitía un
  eslabón por segmento de URL y dejaba sin `item` los intermedios -- `Docs`,
  `Tools`, `MCP server`, `Guides`--, que no son páginas: las cuatro rutas
  responden `404`. Markup válido para schema.org e inválido para Google, que es
  la diferencia entre que parsee y que aparezca. Se emiten sólo los eslabones
  direccionables, que además es lo que pide la guía -- «breadcrumbs that
  represent a typical user path to a page, instead of mirroring the URL
  structure»-- y cumple el mínimo de dos.
  https://developers.google.com/search/docs/appearance/structured-data/breadcrumb
- El sitemap lleva `lastmod` y sale de **git**, no del `mtime` del build: la
  fecha del build es la del build, y marcaría las 37 URLs como cambiadas en
  cada despliegue. `src/lastmod.mjs` hace **una** pasada de
  `git log --format=%cI --name-only --no-merges -- landing/src` -- 37 páginas
  tocan bastante más de 37 ficheros-- y `--no-merges` está porque un merge
  reporta su diff entero y estamparía media rama con su propia fecha.
- Google usa `lastmod` para programar el recrawl y **descarta el campo en todo
  el sitemap cuando no se lo cree**, así que las dos únicas respuestas
  aceptables son una fecha exacta y ninguna. Por eso todo camino de fallo
  devuelve `undefined`: sin `git`, fuera de un checkout, en un clon
  **shallow** -- donde cada fichero colapsa sobre el commit de punta-- o para
  una URL cuyo fichero fuente el módulo no sabe nombrar. Medido en un clon de
  prueba: `--depth 1` da `undefined`, y el mismo código tras
  `git fetch --unshallow` da `2026-08-23T13:49:53+02:00`.
- El caso shallow no es hipotético: `ci.yml` clonaba a la profundidad por
  defecto, así que su build de la landing habría pasado con un sitemap sin una
  sola fecha -- verificando algo que no es lo que se sirve. Por eso el job
  `verify` lleva `fetch-depth: 0`; `release.yml` ya lo llevaba.
- Dos páginas no son una entrada de la colección y se fechan a mano: `/`, que
  no tiene contenido propio y toma la más reciente de los componentes que
  **importa** -- leer los imports y no el directorio evita que un componente
  que la página ya no usa siga fechándola--, y `/releases/`, que es una página
  que renderiza una colección y toma las dos cosas. Las hojas de estilo y la
  capa de movimiento quedan fuera a propósito: cambian cómo se ve la página, no
  lo que dice, y `lastmod` contesta la segunda pregunta.
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
  `process.env` pelado sólo ve lo que exportó la shell. El valor de reserva de
  `site` es el origen de producción, `https://kivgraph.luqueee.dev`, y la
  dirección importa: `site` se hornea en tiempo de build y CI construye sin
  `.env`, así que la reserva tiene que ser la respuesta correcta. Lo que se fija
  en el fichero es el caso de desarrollo,
  `KIVGRAPH_LANDING_URL=http://localhost:6767`. Al contrario -- reserva
  `localhost`, variable en el despliegue-- es lo que publicó
  `http://localhost:6767` como canonical y en el sitemap de todas las páginas.
  Una variable ya exportada gana sobre el fichero -medido-, y `.env` está en
  `.gitignore`.
- Los hechos de identidad -- nombre, lema, resumen, repositorio, licencia, el
  alt de la tarjeta social-- viven en `landing/src/site.mjs`, ESM pelado y sin
  un solo `import` de `astro:*`, porque `astro.config.mjs` tiene que poder
  importarlo. `_seo.ts` los reexporta, así que sigue siendo el módulo del que
  todo componente y endpoint importa. La única copia a mano es
  `landing/public/site.webmanifest`, que es JSON estático.
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
  propiedad del artículo: schema.org pone `breadcrumb` sólo en `WebPage`. El
  `FAQPage` de `Faq.astro` es un segundo `<script>` -- lo exige ser un tipo de
  página distinto -- pero no es huérfano: lleva `@id` en `<site>/#faq` y apunta a
  los otros dos por `isPartOf` y `about`.
- El override `Head` añade únicamente lo que Starlight no emite. Starlight ya
  escribe el título, la descripción, el canonical y `twitter:card`, así que
  repetir cualquiera de los cuatro es un defecto, no una redundancia inocua. Lo
  que **no** emite y por tanto va ahí: la imagen de preview con sus dimensiones,
  su tipo y su `alt`. El `alt` describe la imagen, no repite el lema que ya está
  en el `og:description` de al lado, y sale de `PREVIEW_ALT` para que las dos
  mitades no describan la misma tarjeta de dos formas.
- El `<link rel="alternate" type="text/markdown">` va condicionado a que la
  página sea miembro de la colección `docs`. `/releases/` es un `StarlightPage`,
  así que hereda el override con el id sintético `releases`, y `raw/[...slug]`
  sólo sirve rutas de la colección: anunciarlo sin condición publicaba un
  `/raw/releases.md` que **devolvía 500**, no 404 -- lo que este archivo decía
  antes era una suposición y la medida la desmiente. Y la causa no era el
  anuncio: `raw/[...slug]` estaba prerenderizada bajo `output: "server"`, así
  que el router seguía **casando** cualquier `/raw/**.md` contra una ruta sin
  instancia de componente para los caminos que `getStaticPaths` nunca emitió, y
  Astro lanzaba dentro de su propio pipeline antes de que ningún handler
  nuestro pudiese contestar. Un `if` dentro del `GET` ahí es código muerto:
  comprobado. Por eso la ruta es `prerender = false` y resuelve la entrada en
  la petición -- una lectura de colección y un `join`--, que es lo que le
  permite devolver `404` con `text/plain` para lo que no publica y
  `text/markdown` para lo que sí. Verificado: `/raw/releases.md`,
  `/raw/nonsense.md` y un slug anidado inventado dan `404`; los 37 `alternate`
  del sitio y los 73 enlaces de `llms.txt` y `llms-full.txt` dan `200`.

## Iconos

- Los iconos son assets commiteados, no un paso de build:
  `landing/scripts/icons.sh` los genera desde una imagen cuadrada con
  ImageMagick o, en su defecto, con `sips`, y falla cerrado sin ninguno de los
  dos. CI corre en Linux y nunca lo ejecuta. El maskable deja la marca dentro
  del 80 % central porque el lanzador recorta a un círculo.
- `og.png` **no** sale de `icons.sh`. La tarjeta social lleva el wordmark, el
  titular y el lema compuestos en la Geist que sirve el sitio, y componer
  tipografía es lo que ese script no puede hacer. La fuente es
  `landing/scripts/social-card.html`: se abre en un navegador, se captura el
  elemento `#card` -- que mide exactamente 1200x630, así que ningún viewport lo
  escala-- y el PNG se commitea. Sus `@font-face` apuntan a `node_modules`, que
  es donde están las fuentes exactas del sitio. La malla del fondo se aparta a
  la derecha a propósito: la marca es un ráster opaco y encima del patrón se lee
  como un cuadrado recortado en él.
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

## La portada

- El orden de las bandas **es el argumento** y va en una dirección: tengo un
  símbolo que quiero cambiar -> esto dice de él un grafo resuelto -> ésta es la
  medición -> por eso una búsqueda de texto no puede decir lo mismo -> qué es en
  realidad un nombre -> las relaciones cruzan repositorios -> se enchufa al
  agente que ya usas -> instálalo. `index.astro` no compone nada más.
- Lo que **no** está en la portada es la lista de las once tools. El visitante
  pregunta qué va a poder entender su agente, no qué funciones exporta el
  servidor; la referencia de tools es una página y cada banda enlaza a ella donde
  la pregunta aparece. Un componente que enumeraba las once desapareció por eso,
  junto con la banda de tres tarjetas que sólo servía para enlazar tres páginas
  SEO: esos enlaces viven ahora dentro del texto de la sección a la que
  pertenecen, que es además el ancla contextual que un buscador premia.
- **Toda cifra publicada sale de una sola pasada, y la pasada se nombra.** La
  portada cita `benchmarks/graph-tools-comparison/results-all.json`, commit
  `954b9eb`, `o200k_base`, kivgraph `0.5.0`, 29 preguntas sobre 37 repositorios:
  `35.961` tokens contra `267.980` de `grep` más lectura, 28 de 29 exactas en
  los dos brazos. Mezclar pasadas es el defecto concreto que había: un total de
  tokens de una, una exactitud de otra y unos tiempos de indexado de una tercera
  produjeron un `6.200 / 7 de 7` que **no aparecía en ningún fichero
  commiteado**, y un bloque de multiplicadores -`28.3x`, `2.1x`, `0.2x` sobre un
  monorepo de `41` repositorios y `100.118` símbolos-- que no existía en el
  harness al que se atribuía. Si una tabla nueva necesita brazos rivales, van en
  su propia tabla con su propio JSON: la única pasada donde los seis brazos
  midieron de verdad es `results.json`, y ahí kivgraph es `0.3.2`.
- El contrapeso es parte de la evidencia, no un descargo debajo. `grep` sale más
  barato en 5 de las 29 preguntas y la portada las cuenta; la propia sección de
  producto publica el caso `I1_go_depth2`, donde perdemos.
- La demostración de producto **no es ilustrativa**. Cada ruta, línea, salto y
  cifra sale de una captura verbatim de `raw-all/`, y el componente nombra el
  fichero. Un diagrama con nombres verosímiles argumentaría contra la única
  propiedad que esta página vende, igual que el campo de nodos sintético que se
  retiró del hero.
- Las tres pestañas de esa sección son un grupo de radios y **cero JavaScript**:
  un grupo de radios ya se navega con el teclado y lo anuncia un lector de
  pantalla, los tres paneles van en el HTML servido -- así que un rastreador ve
  los tres-- y una página cuyo CSS no llega muestra los tres en vez de ninguno.
  Los `<input>` van antes de las pestañas y de los paneles porque todo el control
  es un selector de hermano; están `sr-only` y nunca `display: none`, que es lo
  que le quitaría el foco.
- El CTA final es su propia banda y no una `Section`: su encabezado **es** la
  llamada a la acción, y meterlo dentro pondría dos `h2` en una banda. Repite la
  geometría de `Section` -- `border-t`, `max-w-6xl`, `py-14`, `data-reveal`-- para
  que el ritmo sea idéntico desde fuera.

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
  argumenta contra lo que va a demostrar. La tarjeta ya no vive en el hero: está
  en la banda de la medición, junto a las tres cifras grandes que resume, y las
  dos leen los mismos dos totales de la misma pasada.
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
  recibe. Son cuatro piezas: la entrada del hero, el encendido del fondo, el
  halo que sigue al puntero, los dos CTA magnéticos, más los reveals al hacer
  scroll y el parallax del fondo.
- El marcado del hero declara los anclajes y la capa no busca por estructura:
  `data-hero` en la banda, `data-hero-item` en los siete bloques en orden de
  lectura, `data-hero-field` en el plano y `data-hero-halo` en el halo. El
  primer nombre de `HERO_ORDER` es `title` y no el bloque que va primero en la
  página: ese nombre recibe un tween propio con retardo cero, y el `h1` es el
  candidato a LCP. Poner el `eyebrow` delante encola el `h1`, que es el caso de
  `1112 ms` de la tabla de abajo.
- El halo vive en el marcado, no lo crea el JS: un elemento añadido en runtime
  no lleva el atributo de scope de Astro y el CSS scoped del componente no lo
  alcanzaría. Reposa en `opacity: 0` porque no contiene nada -- si el módulo no
  corre, no hay nada que revelar.
- El brillo es un `::after` con un bucle CSS y **ningún tween alcanza un
  pseudo-elemento**. El hero declara su duración como `--sheen-duration` y la
  entrada la acorta para una pasada y luego **elimina** la propiedad, que es lo
  que devuelve el elemento al valor de la hoja de estilo en vez de fijar una
  copia suya.
- Medido sobre la portada servida, cinco cargas: LCP entre `64` y `72 ms` con
  mediana `72 ms`, y el elemento es el `H1` en las cinco -- igual que sin
  animación, y sin moverse al añadir el `eyebrow` ni al pasar el cuerpo a la
  Geist Sans, que son las dos comprobaciones que esas ediciones exigían. Los
  siete bloques del hero terminan en `opacity: 1` y los 21 bloques con
  `data-reveal` también; con `prefers-reduced-motion: reduce` los siete salen a
  `1`, el halo a `0`, el brillo con `animation-name: none` y las barras a su
  ancho final.
- El `h1` es el elemento LCP de la portada, y lo que penaliza esa métrica es el
  **retardo**, no la duración ni el fundido. Medido inyectando cada regla antes
  del primer pintado, con `PerformanceObserver` sobre
  `largest-contentful-paint`:

  |regla sobre el `h1`|LCP|elemento|
  |---|---|---|
  |ninguna|`76 ms`|`H1`|
  |sólo `transform`, `.6s`|`48 ms`|`H1`|
  |`opacity` + `transform`, `.6s`, sin retardo|`88 ms`|`H1`|
  |`opacity`, `.6s`, con `.6s` de retardo|`1.112 ms`|`H1`|
  |`opacity: 0` permanente|`40 ms`|`P`|

  Así que el `h1` **puede** entrar animado si su retardo es cero; lo que no
  puede es esperar su turno en una cascada. La última fila es la trampa: un
  `h1` oculto deja de ser candidato, Chrome mide un párrafo más pequeño y la
  métrica *mejora* mientras la página no se lee.
- Cualquier animación nueva sobre el hero se mide igual antes de darla por
  gratis. La sonda que inyecta el CSS tiene que insertar el `<style>` en cuanto
  `document.head` exista: en `evaluateOnNewDocument` ni `head` ni
  `documentElement` existen todavía, y un `MutationObserver` registrado ahí no
  llega a observar nada -- devuelve el baseline disfrazado de resultado.
- El estado inicial lo pone GSAP con `gsap.from`, **nunca el CSS**. Ningún
  elemento reposa en `opacity: 0` -- la única declaración así en las hojas de
  estilo es el keyframe que parpadea el cursor del transcript--, así que un
  bundle bloqueado deja la página entera visible en vez de esconderla detrás de
  un script.
- GSAP core más `ScrollTrigger` pesan `112,1 KB` sin comprimir y `43,5 KB` con
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

## Enlaces externos

- Todo enlace que sale del sitio abre en una pestaña nueva, con
  `target="_blank"` y `rel="noopener noreferrer"`. `noopener` es el motivo del
  `rel`: sin él la pestaña abierta obtiene un handle sobre `window.opener` y
  puede navegar esta.
- Son tres superficies y ninguna cubre a las otras dos:
  - El markdown de la documentación lo resuelve
    `rehype-external-links` en `astro.config.mjs`. Un autor no puede recordar
    los atributos en cada enlace.
  - Las anclas literales de la landing los llevan escritos.
  - Las navegaciones generadas -- `TopBar.astro`, `Footer.astro`-- lo derivan
    del propio `href` con `/^https?:\/\//`, así que una entrada nueva no puede
    olvidar la regla.
  - El icono social de Starlight es un componente y ningún paso de rehype lo
    alcanza: vive sobreescrito en `src/components/starlight/SocialIcons.astro`,
    que conserva el `rel="me"` original -- es una afirmación de identidad y
    quitarlo rompería la verificación rel-me.
- Se comprueba sobre el HTML generado, no leyendo plantillas: `dist/client`
  tiene `79` anclas externas y `2.436` internas, y la cuenta correcta es `0`
  externas sin la regla y `0` internas con `target`.

## Verificación

```bash
make landing-check
make landing-build
```

`pnpm --dir landing check` es `format:check`, `lint` y `typecheck`
(`astro check`).
Se sirve con `pm2 start landing/ecosystem.config.cjs` en el puerto `6767`.
