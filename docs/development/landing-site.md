# El sitio público: landing, documentación y SEO

**Fecha de verificación:** 2026-08-15
**Alcance:** `landing/`, más el asset de skill
`internal/integrations/assets/kivgraph/SKILL.md`
**Gate:** ninguno. `TASKS.md` no define un gate para esta superficie y no se
inventa uno; lo que sigue son comprobaciones medidas, no un token emitido.

## Qué es

`landing/` es la landing pública y la documentación de usuario: un proyecto pnpm
propio, como `ts-worker/` y `web/`, en Astro con Starlight. No entra en ningún
bundle, y el workflow de release lo comprueba por dos vías -- que el directorio
no está en el payload y que ninguna línea de `SHA256SUMS` lo nombra.

Se verifica con `make landing-check` y `make landing-build`, y se sirve con
`pm2 start landing/ecosystem.config.cjs` en el puerto `6767`.

El directorio se llamaba `site/`. El cutover fue completo: `Makefile`, los dos
workflows, `.gitignore`, `README.md`, el nombre del paquete, la unidad de pm2 y
la variable de entorno. La guarda del release es la razón de que no pueda quedar
a medias: renombrar el directorio sin renombrar su assertion la deja
comprobando un path que ya no existe, es decir, nada.

## Lo que se publica

| Superficie | Tamaño medido |
| --- | --- |
| Páginas HTML | 27 |
| Páginas de documentación | 26 (4.388 líneas de markdown) |
| Referencia por tool | 14 páginas |
| Sección MCP | 4 páginas: clientes, skill, uso, troubleshooting |
| Componentes de la landing | 16 (1.612 líneas) |
| Piel de Starlight | `docs.css`, 742 líneas |
| Markdown crudo | 25 rutas bajo `/raw/` |
| `llms.txt` | 5.772 B |
| `llms-full.txt` | 199.108 B, 4.464 líneas, las 26 páginas |
| `sitemap-0.xml` | 26 URLs |
| `robots.txt` | 15 tokens de rastreador |
| Iconos | 7 PNG más el manifest |

## La documentación del MCP sale de una captura, no de la memoria

Todo ejemplo de las catorce páginas de tool es una captura literal. Se
construyó el binario de HEAD con el tag `ladybug` y se condujo
`kivgraph serve` por stdio contra la generación publicada `30` -- 2 repositorios,
311 ficheros, 57 paquetes,
10.957 símbolos, 40.125 aristas, 1.642 referencias no resueltas -- registrando
`tools/list` completo, una llamada real por tool y los caminos de error.

Eso es lo que hizo visibles los tres primeros defectos de la lista siguiente: la
página vieja describía una superficie que el servidor no publica.

La referencia documenta lo que `internal/mcp/server.go` registra, no el paquete
`internal/mcp/tools`. Son catorce tools. `get_source` es una de ellas y
`get_unresolved_references` no está publicada.

## SEO, con un agente como segundo lector

Lo que existe para una IA: `/llms.txt` como índice, `/llms-full.txt` con toda la
documentación en una sola petición, y el markdown de cada página en
`/raw/<ruta>.md`, que es lo que anuncia su `<link rel="alternate">`. Los tres se
generan con `getCollection`, así que una página nueva no puede dejarlos rancios.

`robots.txt` permite explícitamente a los rastreadores de IA y **cada token sale
de la documentación de su operador**, con la URL y la fecha impresas en el propio
fichero. Un token que no se pudo verificar no entró. Tres de ellos --
`ChatGPT-User`, `Perplexity-User`, `Meta-ExternalFetcher` -- los publican sus
operadores advirtiendo que pueden ignorar `robots.txt`: se nombran porque un
permiso ignorado no cuesta nada, y el comentario lo dice en vez de sugerir un
control que quizá no obliga.

El JSON-LD del sitio es **un** grafo: la landing publica `SoftwareApplication` en
`<site>/#software` y `WebSite` en `<site>/#website`, y cada página de
documentación emite un `TechArticle` que los referencia por `@id` en vez de
duplicarlos. El `BreadcrumbList` va como nodo hermano porque schema.org pone
`breadcrumb` sólo en `WebPage`.

Toda URL absoluta se deriva de `Astro.site`. Los hechos del proyecto viven una
sola vez en `landing/src/pages/_seo.ts`.

## Defectos encontrados y corregidos

Sólo el defecto 2 cambia el comportamiento de lo que se instala; los otros diez
eran del sitio o de su documentación.

| # | Defecto | Dónde |
| --- | --- | --- |
| 1 | La referencia documentaba `get_unresolved_references`, que `serve` no registra, y omitía `get_source`, que sí | `reference/mcp-tools.md` |
| 2 | La skill instalada enrutaba al agente a esa tool inexistente y no mencionaba `get_source` ni `get_file_outline` | `assets/kivgraph/SKILL.md` |
| 3 | El quickstart afirmaba que sin generación publicada «toda consulta falla»; en realidad no hay superficie de consulta | `quickstart.md` |
| 4 | Una regla de componente sin capa anulaba en silencio cada `mt-*`: `mt-8` computaba `0px` | cascada de Tailwind |
| 5 | Un grid sin columna base lo dimensionaba su hijo más ancho: 172 px de desbordamiento en móvil | 5 componentes |
| 6 | Once enlaces muertos con slug de guion bajo (`/reference/tools/find_references/`) | `mcp/skills.md` |
| 7 | Los rótulos de columna del footer eran `h2` con estilo de subtítulo | `Footer.astro` |
| 8 | Filas de puntos suspensivos solapadas: clave y valor `shrink-0` excedían la pista de grid | `Snapshot.astro` |
| 9 | El relleno de los iconos usaba el color de página y no el de la imagen: recuadro visible | `icons.sh` |
| 10 | La marca ocupaba el 60,9 % del lienzo; a 16 px son diez píxeles de marca | iconos |
| 11 | `favicon.svg` era la marca antigua y **ganaba**, porque el navegador prefiere el vector | `<head>` |

El defecto 2 es el más grave de la lista y el menos visible: una skill es lo que
un agente lee antes de decidir, así que una tool que el servidor no publica no es
una imprecisión de documentación, enruta la pregunta a una llamada que falla.

## Verificación

| Comprobación | Resultado |
| --- | --- |
| `make landing-check` | 31 ficheros, 0 errores, 0 warnings, 0 hints |
| `make landing-build` | 27 páginas, 25 rutas markdown, 3 endpoints |
| `biome lint` | 0 warnings (se corrigieron 8 de `noDescendingSpecificity` reordenando, sin `ignore` y sin desactivar la regla) |
| Enlaces internos | 1.109 referencias del mismo origen, 0 sin resolver |
| Duplicados de `<head>` | 0 en canonical, description, `og:*`, `twitter:*`, robots, `theme-color`, manifest y `ld+json` |
| Filas de lectura solapadas | 0 a 1440, 1024, 390 y 320 px, con el timestamp de 20 caracteres intacto |
| Desbordamiento horizontal | 0 a las cuatro anchuras |
| `prefers-reduced-motion: reduce` | 0 llamadas a `requestAnimationFrame` |
| Lienzo del hero | caja 780², buffer 975², `pointer-events: none`, sin listeners |
| Iconos | 7 ficheros, cada uno sirviendo `200` con su tamaño declarado |
| Esquema de encabezados | 1 `h1`, 6 `h2`, 14 `h3`, 0 `h4` |
| Tema | `data-theme="dark"` en las 27 páginas, 0 selectores de tema |
| Go | `gofmt` limpio, `go vet` limpio, `go test ./...` ok |

Las tablas de argumentos de las doce páginas se compararon propiedad por
propiedad contra el esquema de entrada capturado: ninguna ausente, ninguna
inventada. El corpus de páginas se puede volver a contar con `wc -l
landing/src/content/docs/docs/tools/*.md`; el esquema corresponde a
`internal/mcp/tools/*.go` en `891d245` y su cobertura se revalida con `go test
./internal/mcp -run TestEveryPublishedArgumentDescribesItself`.

## Límites residuales

Las mediciones de arriba son las del 2026-08-15 y se dejan como se tomaron,
salvo el recuento actual de páginas de referencia y su total, actualizado al
incorporar la duodécima página.
Tres de los límites que este informe registraba ya no existen, y se corrigen
aquí en vez de dejarlos contradiciendo el código:

- **El dominio existe y el `site` por defecto es el de producción.** El sitio se
  publica en `https://kivgraph.dev` -- era `https://kivgraph.luqueee.dev` cuando
  se tomó esta medición--, y ése es ahora el valor de reserva
  de `site` en `astro.config.mjs`. La dirección del fallback está invertida a
  propósito: `site` se hornea en tiempo de build y CI construye sin `.env`, así
  que un build sin variable tiene que emitir el canonical correcto. Lo que se
  fija en `landing/.env` es el caso de desarrollo,
  `KIVGRAPH_LANDING_URL=http://localhost:6767`. Antes era al contrario, y eso
  fue exactamente lo que publicó `http://localhost:6767` como canonical de todas
  las páginas.
- **`og.png` ya no lo genera `icons.sh`.** La tarjeta dejó de ser la marca
  centrada en el lienzo: lleva el wordmark, el titular y el lema en la Geist que
  sirve el sitio, y componer tipografía es justo lo que ese script no hace. Se
  renderiza desde `landing/scripts/social-card.html` en un navegador y se
  commitea como los demás assets. El resto de la nota sigue vigente para los
  iconos.
- **No hay marca vectorial.** La fuente es un ráster, así que no se publica
  `favicon.svg` y no hay `.ico`: los enlaces son PNG, más el `rel="shortcut icon"`
  que emite Starlight.
- **El fondo de los iconos es `#0f1117`, no `--color-shell`.** Es el color de la
  propia imagen; rellenar con el de la página dejaba un recuadro. `background_color`
  del manifest lo iguala, `theme_color` sigue siendo el del sitio, y la tarjeta
  social usa ese mismo `#0f1117` por la misma razón.
- **El rename ya llegó a la ruta.** Las URLs de la referencia son `/docs/...` y
  `/reference/[...slug]` queda como redirección. El contenido vive en
  `src/content/docs/docs/`, que es el anidamiento que este informe daba por
  evitado; el coste real fue otro: `_seo.ts` siguió agrupando por el prefijo
  `reference/` durante todo ese tiempo, y como sus matchers son predicados que
  devuelven `false` en vez de búsquedas que fallen, `llms.txt` perdió dos
  secciones enteras sin un solo error de build.
- **`make test-ladybug` no cubre esta superficie.** El sitio no enlaza la
  biblioteca nativa; lo que se verificó bajo ese tag fue el binario con el que se
  capturó la superficie MCP.
