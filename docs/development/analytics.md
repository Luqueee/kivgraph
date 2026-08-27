# Analítica: Umami para SEO

Este documento describe cómo está integrada la analítica de la landing, qué
registra, qué **no** registra, y cómo usarla para responder preguntas de SEO.
No es documentación de usuario: `docs/` es material interno y no se publica.

El criterio es uno y conviene enunciarlo antes que nada: **no se optimiza para
tener más datos, sino para tener datos accionables**. Un evento que no cambia
una decisión no se añade.

## Qué hay

Un Umami **autoalojado** en `https://analytics.luqueee.dev`, confirmado por su
propio `/api/config`, que devuelve `"cloudMode": false`. El endpoint de
recolección es `/api/send`.

El tracker lo emiten las **dos** mitades del sitio y ninguna otra cosa:

|mitad|componente|
|---|---|
|landing|`landing/src/components/landing/Layout.astro`|
|documentación y `404`|`landing/src/components/starlight/Head.astro`|

Las dos leen `umamiTracker()` de `landing/src/pages/_seo.ts`, así que el tag es
idéntico en las 39 páginas y **no hay forma de que una mitad derive de la
otra**. Medido sobre `dist/client`: 39 de 39 páginas con tracker, y exactamente
**uno** por página.

El tag que se sirve:

```html
<script
  defer
  src="https://analytics.luqueee.dev/script.js"
  data-website-id="<uuid>"
  data-domains="kivgraph.dev"
  data-performance="true"
></script>
```

### Por qué el sitio no duplica pageviews

Porque no puede. La landing es Astro con `output: "server"` y todo
prerenderizado salvo `/raw/[...slug]`, y **no monta `ClientRouter` ni
ViewTransitions**: cada navegación es una carga completa de documento. No hay
enrutado de cliente que pueda disparar un segundo pageview, y `auto-pageview`
queda en su valor por defecto porque no hay nada que desactivar.

## Variables de entorno

|variable|dónde|qué hace|
|---|---|---|
|`KIVGRAPH_UMAMI_SCRIPT_URL`|`landing/.env` del host|URL absoluta del tracker|
|`KIVGRAPH_UMAMI_WEBSITE_ID`|`landing/.env` del host|el UUID que acuñó Umami|

**Fail-closed por diseño:** con una sola de las dos, `umamiTracker()` devuelve
`null` y no se emite nada. Por eso `astro dev`, un build local y CI no pueden
escribir en el dataset de producción.

Las dos se **hornean en tiempo de build**, porque las páginas son
prerenderizadas. Exportarlas y reiniciar `pm2` no emite nada: hay que
reconstruir.

```bash
cd /root/kivgraph && git pull
make landing-build && pm2 restart kivgraph-landing
curl -s https://kivgraph.dev/ | grep -c analytics.luqueee.dev   # espera 1
```

### El guarda que las variables no pueden ser

`data-domains="kivgraph.dev"` sale de `PRODUCTION_HOST` en `landing/src/site.mjs`
y **no** del host de `Astro.site`. La distinción es la que decide si funciona:
un despliegue de staging fija `site` al origen de staging, así que seguir a
`Astro.site` le dejaría casar consigo mismo y escribir en producción. El par de
variables falla cerrado cuando falta una, pero una preview que **hereda las dos**
es exactamente el caso que ese par no puede atrapar.

`PRODUCTION_ORIGIN` es además el valor de reserva de `site` en
`astro.config.mjs`, así que el origen de producción se declara **una vez**.

## Qué se registra

Automáticamente, por el tracker:

- pageview con `url` **incluida la query string**, `referrer`, `title`,
  `hostname`, idioma y resolución de pantalla;
- país y dispositivo, derivados por el servidor a partir de la petición;
- duración de visita y navegación entre páginas, derivadas de la sesión;
- **Core Web Vitals** por `data-performance`: LCP, INP, CLS, FCP y TTFB.

Y los eventos de la tabla de más abajo.

### Qué NO se registra

- **Nada personal.** No hay `identify()`, ni un id de usuario, ni campos libres
  que puedan arrastrar texto escrito por el visitante.
- **Ningún secreto.** El `website-id` es público por construcción — viaja en el
  HTML de cada página — y no autoriza nada.
- **Nada trivial.** Ni `scroll_25`, ni `mouse_move`, ni tiempo en pantalla por
  tramos. Un evento que se dispara sin intención distorsiona el *bounce rate* y
  el engagement, que son justo las dos métricas con las que se decide qué página
  arreglar.

### Las query strings se conservan a propósito

`data-exclude-search` **no** está puesto, y no debe ponerse. Ahí es donde viajan
`utm_source`, `utm_medium`, `utm_campaign`, `utm_content`, `utm_term`, `gclid`,
`fbclid` y `msclkid`. Excluirlas dejaría la atribución de campañas en blanco.

## Eventos

Todos salen de una acción que expresa **intención real**. Cinco de los seis
pasan por `CopyButton.astro`, que es lo que garantiza un nombre consistente y
**un disparo por copia** — se reporta en el `then` del `writeText`, así que una
copia que el portapapeles rechazó no cuenta.

|evento|cuándo se dispara|metadata|
|---|---|---|
|`install_copy`|se copia el one-liner|`where`: `hero`, `final_cta`|
|`prompt_copy`|se copia el prompt del agente|—|
|`client_connect_copy`|se copia `kivgraph mcp install`|—|
|`quickstart_copy`|se copia un comando del Quickstart|`step`: título del paso|
|`mcp_config_copy`|se copia el JSON de MCP|—|
|`github_click`|enlace al repo|`where`: `topbar`, `footer`, `docs_header`|

Dónde se dispara cada uno, medido sobre `dist/client`, que es lo que hay que
saber para probarlos y lo que decide si un goal se puede acotar por ruta:

|evento|sección de la portada|ancla|
|---|---|---|
|`prompt_copy`|hero|`/`|
|`install_copy`|hero y CTA final|`/` y `#install`|
|`client_connect_copy`|06, *works where you already code*|`#agents`|
|`quickstart_copy`|07, *start with your workspace*|`#quickstart`|
|`mcp_config_copy`|07, la caja del JSON|`#quickstart`|
|`github_click`|topbar, footer y cabecera de docs|las 39 páginas|

Los cinco de copia viven **sólo en la portada**. Si algún día se añade un botón
de copiar fuera de ella -- `/install/` es el candidato obvio -- hay que volver a
los goals: uno acotado a una ruta deja de contar en silencio, y una conversión
que baja sin avisar se lee como que la gente dejó de instalar.

`github_click` **no** usa JavaScript nuestro: Umami lee `data-umami-event` del
clic. En `TopBar.astro` el atributo se deriva del propio `href`, así que una
entrada nueva no puede olvidarse de él, y **sólo** lo llevan los enlaces
externos: uno interno ya es un pageview, y contarlo dos veces pondría en el
informe un número que ninguna visita produjo.

### Convención de nombres

- `snake_case`, en minúsculas, sin espacios ni acentos;
- `<objeto>_<acción>`, no al revés: `install_copy`, no `copy_install`;
- la metadata es plana y sus valores son cadenas, porque eso es por lo que un
  informe agrupa;
- un evento nuevo se añade **aquí** antes que en el código.

### Si el tracker no está

No pasa nada. `report()` en `CopyButton.astro` comprueba `window.umami` y
retorna en silencio si no existe — bloqueado, instancia caída, o un build sin
las variables — y envuelve la llamada en un `try`. Una analítica que falla no
es un fallo de la página, y el copiado que el lector pidió ocurre igual.

## Goals recomendados

La conversión de este sitio **no se puede observar**: sucede en una terminal.
Lo más cerca que la página llega es el momento en que el visitante se lleva el
comando o el prompt. Eso son las conversiones primarias.

|nivel|eventos|qué significa|
|---|---|---|
|**primaria**|`install_copy`, `prompt_copy`|se lleva lo necesario para instalar|
|secundaria|`client_connect_copy`, `mcp_config_copy`|está conectando su agente|
|secundaria|`quickstart_copy`|está siguiendo la guía paso a paso|
|micro|`github_click`|interés, no compromiso|

El embudo que interesa medir:

```text
landing SEO -> hero -> install_copy | prompt_copy
```

`quickstart_copy` con `step` distingue quién abandonó y dónde: si el paso 1 se
copia mucho más que el 3, el problema está entre medias.

## Convención UTM

**Los UTM sólo van en enlaces externos que controlamos nosotros.** Nunca en un
enlace interno: reescribiría el `referrer` de la propia sesión y partiría una
visita en dos, que es exactamente lo que arruina un informe de atribución.

Reglas: minúsculas, sin espacios, nombres estables en el tiempo.

```text
?utm_source=reddit&utm_medium=social&utm_campaign=launch
?utm_source=x&utm_medium=social&utm_campaign=launch
?utm_source=youtube&utm_medium=video&utm_campaign=demo
?utm_source=github&utm_medium=referral&utm_campaign=readme
```

|parámetro|vocabulario|
|---|---|
|`utm_source`|`reddit`, `x`, `youtube`, `github`, `hn`, `newsletter`|
|`utm_medium`|`social`, `video`, `referral`, `email`|
|`utm_campaign`|el nombre de la campaña, estable: `launch`, `readme`, `v0-8`|
|`utm_content`|la variante, cuando hay dos creatividades|
|`utm_term`|sólo en campañas de pago con palabra clave|

## Cómo analizar tráfico orgánico

El eje es siempre el mismo: **referrer -> landing -> comportamiento ->
conversión**.

1. En el dashboard, filtra por `Referrer` = `google.com` (o `bing.com`,
   `duckduckgo.com`).
2. Ordena por `Path` para ver **qué páginas reciben el tráfico**, no sólo
   cuántas visitas hay.
3. Cruza con `Bounce rate` y `Visit duration`: mucho tráfico con rebote alto es
   una página que rankea para una intención que no satisface.
4. Añade el goal para ver **qué landings convierten**, no cuáles reciben más.
5. Mira los Web Vitals de esa misma página en la pestaña de rendimiento.

Segmentaciones disponibles: referrer, path, landing page, dispositivo, país,
`utm_source`, `utm_medium`, `utm_campaign`, evento/conversión y Web Vitals.

### Los cuatro casos que hay que saber encontrar

|patrón|qué significa|qué hacer|
|---|---|---|
|tráfico alto, rebote alto|rankea para otra intención|reescribir la entradilla|
|tráfico bajo, conversión alta|buena y nadie la ve|enlaces internos hacia ella|
|tráfico alto, LCP p75 malo|pierde a quien llega|optimizar esa ruta|
|posición buena, CTR bajo|el snippet no vende|reescribir `title` y `description`|

El último **no se ve en Umami**: sale de Search Console. De ahí la sección
siguiente.

## Umami y Google Search Console son complementarios

Umami no sustituye a Search Console y no debe intentarlo. Cada uno ve una mitad
y la unión es lo accionable:

|Search Console|Umami|
|---|---|
|query, impresiones, CTR, posición|visitantes, visitas, rebote, duración|
|landing URL|landing URL, referrer, navegación|
|país, dispositivo|país, dispositivo, eventos, conversiones, Web Vitals|

**La columna que las une es la landing URL.** El cruce se hace exportando el
informe de páginas de Search Console y el de páginas de Umami, y uniendo por
esa columna:

```text
Search Console: /docs/cli/  ->  12.400 impresiones, CTR 1,2 %, posición 8,4
Umami:          /docs/cli/  ->  148 visitas, rebote 71 %, LCP p75 3,9 s
```

Esa fila dice tres cosas a la vez: rankea, el snippet no convence, y quien
entra se encuentra una página lenta. Ninguna de las dos herramientas la dice
sola.

No hace falta más infraestructura para esto. Una integración automática entre
las dos exigiría un job, credenciales de la API de Search Console y un
almacén — desproporcionado para un sitio de 39 páginas y un cruce que se hace
con dos CSV.

## Core Web Vitals

`data-performance="true"` es lo que los enciende. Umami los recoge del propio
navegador del visitante, así que son **datos de campo**, no de laboratorio: son
lo que de verdad experimenta quien llega desde Google, y por eso valen más que
un Lighthouse local para decidir qué arreglar.

Lee siempre el **percentil 75**, que es el umbral con el que Google evalúa, y
no la media: una media buena esconde perfectamente un cuarto de visitantes con
una experiencia mala.

Prioridad: **LCP**, **INP**, **CLS**. `FCP` y `TTFB` son diagnóstico, no
objetivo.

La medida de laboratorio del hero vive en otro sitio y responde otra pregunta:
`landing/AGENTS.md`, sección *Movimiento*.

## Agentes de IA: la segunda propiedad

Dos fenómenos distintos, dos datasets. La propiedad principal describe
**personas**; los crawlers y agentes de IA van a una segunda propiedad y no
tocan visitantes, rebote, duración, journeys ni conversión de la primera. El
filtro de bots de Umami se queda **encendido** en las dos.

### Por qué no es un middleware de Astro

Porque no ve nada. Medido sobre el build: un middleware en `src/middleware.ts`
recibió **una** de siete rutas -- `/raw/docs/cli.md`, la única no
prerenderizada -- y se perdió `/`, `/docs/cli/`, `/install/`, `/robots.txt`,
`/sitemap-0.xml` y el 404. El handler estático del adaptador contesta esas
antes de que exista el pipeline SSR, y son justo las que pide un crawler.

Lo que sí ve todo es `landing/server.mjs`, que es lo que arranca pm2 en lugar
del entry del adaptador. No reimplementa nada: importa el `handler` que el
propio adaptador exporta -- `createStandaloneHandler`, con estáticos, el `301`
de barra final y el 404 -- y lo envuelve en un `http.createServer`. Verificado a
través del wrapper: `/`, `/docs/cli/`, `/robots.txt` y `/sitemap-0.xml` en
`200`, y `/docs/cli` en `301` a la canónica.

### El registro de agentes

`landing/src/ai-agents.mjs`. Plain ESM por la misma razón que `site.mjs`: lo
consume un proceso Node pelado que Vite nunca transforma.

**Cada identificador salió de la documentación del operador**, con la fuente y
la fecha al lado. Añadir un agente es una fila ahí y un caso en
`ai-agents.test.mjs`; ningún otro sitio del repositorio nombra un agente.

|proveedor|agentes|categoría|verificación publicada|
|---|---|---|---|
|OpenAI|`OAI-SearchBot`|`search`|rangos IP (`searchbot.json`)|
|OpenAI|`ChatGPT-User`|`user_fetch`|rangos IP (`chatgpt-user.json`)|
|OpenAI|`GPTBot`|`training`|rangos IP (`gptbot.json`)|
|Anthropic|`Claude-SearchBot`|`search`|ninguna publicada|
|Anthropic|`Claude-User`|`user_fetch`|ninguna publicada|
|Anthropic|`ClaudeBot`|`training`|ninguna publicada|
|Perplexity|`PerplexityBot`|`search`|rangos IP (`perplexitybot.json`)|
|Perplexity|`Perplexity-User`|`user_fetch`|rangos IP (`perplexity-user.json`)|

Tres cosas que el registro hace a propósito y conviene no deshacer:

- **Los UA de OpenAI llevan un Chrome completo dentro.** Su cadena real empieza
  por `Mozilla/5.0 (Macintosh; ...) Chrome/131.0.0.0 Safari/537.36` y el token
  va al final. Un matcher que comprobase «¿es Chrome?» antes que el token
  archivaría los tres como humanos.
- **Un bot genérico no es IA.** `Googlebot`, `Bingbot`, `Discordbot`, `curl` y
  un navegador caen a `null`. Convertir cualquier bot en bot de IA vaciaría de
  sentido el segundo dataset entero.
- **`Google-Extended` no está, y su ausencia es el punto.** Es un token de
  `robots.txt` que controla si Google puede usar contenido ya rastreado para
  entrenar Gemini; la documentación de crawlers de Google no le asigna ningún
  user agent HTTP, así que nunca llega en una petición. Inventar tráfico de
  Gemini a partir de él sería fabricar una cifra.

`unknown_ai` es la cuarta categoría: un operador que este fichero conoce
enviando un agente que no. Es un hallazgo, no una clasificación, y la señal de
que hace falta una fila nueva.

### Verificación

Hoy es `user_agent` y el evento lo dice: `verified: false`. Nadie resuelve DNS
ni compara rangos en el camino de la petición, porque eso sí costaría latencia.

OpenAI y Perplexity **publican ficheros CIDR** -- están en el registro -- así que
subir esto a `ip_range` es posible más adelante y fuera de banda: descargar los
JSON con caché y comparar. Anthropic no publica ninguno, así que una fila de
Claude no puede pasar de ser una afirmación. Mentir sobre eso sería peor que no
verificar.

### Qué se envía, y qué no

Un evento `ai_crawler_request` a la propiedad de crawlers, con
`provider`, `agent`, `category`, `path`, `method`, `status` y `verified`.

**No se envía** la IP, ninguna cookie, ninguna cabecera de autorización, la
query string ni el cuerpo. El user agent completo se queda **sólo** en el log
local: es lo que permite convertir un `unknown_ai` en una fila del registro, y
describe un robot y no a una persona.

El sender se identifica como `kivgraph-landing/1.0`, neutro y honesto. Umami
descarta lo que parece un bot, y lo que reportamos **es** un bot: falsear la
cabecera sería spoofing, y apagar el filtro globalmente expondría la propiedad
principal justo a la contaminación que todo esto evita.

### Ruido

- **Assets fuera.** `/_astro/`, `/pagefind/`, el favicon, el manifest y todo lo
  que termina en extensión de asset no generan fila.
- **Dentro a propósito:** `robots.txt`, `llms.txt`, `llms-full.txt`, los
  sitemaps y `/raw/**.md`. No son páginas, pero son **cómo** un agente descubre
  y lee este sitio, y un crawler que empieza por `llms.txt` se comporta distinto
  de uno que recorre el sitemap.
- **Deduplicación de 15 minutos** por par (agente, ruta). Medido: cuatro
  peticiones a la misma ruta producen **un** evento. No es sampling -- que
  tiraría filas al azar y haría insignificante el recuento de un agente poco
  frecuente -- y el log local sigue teniendo las cuatro.

### Nada de esto bloquea una respuesta

`fire-and-forget` con timeout de 2 s, dentro de `response.on("finish")`, y todo
fallo se traga. Medido con Umami apuntando a un puerto muerto: `/docs/cli/`
contesta en **0,6-0,9 ms**. Si Umami se cae, `kivgraph.dev` sirve igual.

### Log local

Una línea JSON por detección en `stdout`, que es donde pm2 ya recoge:

```json
{"ai_agent":true,"ai_provider":"openai","ai_agent_name":"OAI-SearchBot",
 "ai_category":"search","ai_verification":"user_agent","verified":false,
 "method":"GET","path":"/docs/cli/","status":200,"at":"..."}
```

Es la fuente de verdad que sobrevive a que Umami esté caído o a que un evento
se deduplique, y no añade un segundo sistema de logging.

## MANUAL SETUP REQUIRED

Lo que sigue **no se puede hacer desde el repositorio**. Son cambios en el
dashboard de Umami y en el host.

### 1. Goals en el dashboard

Umami no define goals en el cliente. En el sitio, *Settings -> Websites ->
kivgraph -> Goals*, crear uno por evento:

|nombre|acción|valor|
|---|---|---|
|`install_copy`|Triggered event|`install_copy`|
|`prompt_copy`|Triggered event|`prompt_copy`|
|`client_connect_copy`|Triggered event|`client_connect_copy`|
|`quickstart_copy`|Triggered event|`quickstart_copy`|
|`mcp_config_copy`|Triggered event|`mcp_config_copy`|

`github_click` se deja **fuera** de los goals a propósito: es interés, y
contarlo como conversión inflaría la tasa con clics que no llevan a instalar
nada. Se ve igual en *Events* sin ser goal.

**Hay que crear los goals después de que llegue el primer evento, no antes**, y
la razón cuesta una tarde si se descubre sola. Con *Triggered event* el
formulario pide un segundo valor, y ese valor es el **nombre del evento** -- no
la ruta, aunque lo parezca. Umami llena ese desplegable con los eventos que el
sitio ya registró, así que en un sitio sin ninguno ofrece sólo `/`, que es lo
único que hay en la tabla. Elegirlo crea seis goals que buscan un evento
llamado `/` y **nunca cuentan**, mientras *Events* sí suma: el campo `Name` es
sólo la etiqueta de la tarjeta, así que los goals se ven bien nombrados y a
cero. Ya pasó una vez.

Y el mismo desplegable vacío revienta la página con
`TypeError: Cannot read properties of null (reading 'value')`. Es un bug de la
UI, no de la configuración: el select hace
`items?.map(e => typeof e === "object" ? e : {label: e, value: e})` y después
`.map(e => e.value)`, y como `typeof null === "object"` el `null` pasa el
guardia intacto y revienta en el segundo `map`.

Así que el orden es: desplegar, pulsar una vez cada botón de copiar en
`https://kivgraph.dev/`, comprobar en *Events* que aparecen los nombres, y
entonces crear los goals eligiéndolos de la lista.

### 1b. La propiedad de crawlers de IA y sus variables

*Settings -> Websites -> Add website*, con nombre
`kivgraph.dev - AI Crawlers` y dominio `kivgraph.dev`. Copiar el **Website ID**
y ponerlo en `landing/.env` **del host**, junto a las dos que ya hay:

```env
KIVGRAPH_UMAMI_URL=https://analytics.luqueee.dev
KIVGRAPH_UMAMI_AI_WEBSITE_ID=<el id de la propiedad de crawlers>
```

Nunca el id de la propiedad principal: mezclarlos es justo lo que esta
separación existe para impedir. Como el par de la web, **falla cerrado**: con
una de las dos ausentes no se envía nada, así que en desarrollo está apagado.
`KIVGRAPH_UMAMI_AI_TRACKING=off` lo desactiva sin borrar nada.

Estas dos las lee `server.mjs` en el arranque; **no** son variables de build, así
que basta con reiniciar:

```bash
pm2 restart kivgraph-landing
pm2 logs kivgraph-landing --lines 5   # la primera línea dice ai_tracking: true
```

### 1c. Los informes de la propiedad de crawlers

Todo sale de un solo evento, `ai_crawler_request`, filtrando por sus campos.

|informe|filtro|qué contesta|
|---|---|---|
|**AI Crawlers Overview**|ninguno|desglose por proveedor, agente, categoría y ruta|
|**Search AI Crawlers**|`category = search`|quién te está indexando para citarte|
|**AI User Fetches**|`category = user_fetch`|**qué pide alguien a su asistente**|
|**Training Crawlers**|`category = training`|quién recoge para entrenar|
|**Provider x page**|`provider = openai` + `path`|qué lee cada proveedor|

`user_fetch` es el que más dice: es intención humana llegando por una máquina, y
es lo que se compara con los referrals de IA de la propiedad principal.

### 1d. Referrals de IA, en la propiedad principal

No hace falta nada nuevo: el referrer ya se recoge y las query strings también.
En *Referrers*, filtrar por `chatgpt.com`, `perplexity.ai`, `claude.ai`,
`gemini.google.com` y `copilot.microsoft.com`; y en los parámetros, por
`utm_source=chatgpt.com`, que es lo que ChatGPT añade a los enlaces que muestra.

La correlación que se busca no la calcula Umami, y no hace falta que lo haga:

```text
OAI-SearchBot  ->  /docs/tools/find-by-intent/     (propiedad de crawlers)
        ...semanas después...
referrer chatgpt.com  ->  /docs/tools/find-by-intent/   (propiedad principal)
```

Las dos mitades comparten la **ruta**, que es la columna por la que se cruzan a
mano, igual que con Search Console.

### 2. El dominio del sitio en Umami

La entrada del sitio sigue apuntando a `kivgraph.luqueee.dev`. Cambiarla a
`kivgraph.dev` en *Settings -> Websites*. No corta la recogida — el tracker no
filtra por ese campo — pero el panel muestra un host que hoy sólo redirige.

### 3. Search Console

Crear la propiedad de **dominio** de `kivgraph.dev` con el registro `TXT`, y
enviar `https://kivgraph.dev/sitemap-index.xml`.

### 4. First-party tracking, si se quiere

Hoy el tracker se sirve desde `analytics.luqueee.dev`. Un bloqueador que
reconozca ese patrón se lo come, y el dato se pierde entero. Servirlo desde el
propio dominio lo evita:

```text
https://kivgraph.dev/u.js      en vez de  https://analytics.luqueee.dev/script.js
https://kivgraph.dev/api/u     en vez de  https://analytics.luqueee.dev/api/send
```

Umami lo soporta de forma nativa, sin parches, con dos variables **en la
instancia**:

```env
TRACKER_SCRIPT_NAME=u.js
COLLECT_API_ENDPOINT=/api/u
```

y un proxy inverso en el host de la landing que reenvíe esas dos rutas a la
instancia. Con Cloudflare delante, una regla de origen o un Worker hacen lo
mismo sin tocar el host.

Hecho eso, el repositorio no cambia: basta con apuntar
`KIVGRAPH_UMAMI_SCRIPT_URL` a `https://kivgraph.dev/u.js`. Esa es la razón de
que la URL sea una variable de entorno y no un literal.

**No está hecho** porque exige tocar la instancia y el proxy, y porque un
proxy a medias es peor que ninguno: si `/api/u` no llega, se pierde el 100 %
del dato en vez del porcentaje que hoy bloquea un adblocker.

## Verificación

```bash
# el tracker se emite, una vez por página, con las dos opciones
curl -s https://kivgraph.dev/ | grep -o '<script[^>]*analytics[^>]*>'

# los eventos están en el HTML servido
curl -s https://kivgraph.dev/ | grep -o 'data-copy-event="[a-z_]*"' | sort -u
curl -s https://kivgraph.dev/ | grep -o 'data-umami-event="[a-z_]*"' | sort -u
```

En el navegador, con la consola abierta: copiar el comando de instalación debe
producir **una** petición a `/api/send`, no dos. Y en `localhost` no debe
producir ninguna, porque `data-domains` no casa.
