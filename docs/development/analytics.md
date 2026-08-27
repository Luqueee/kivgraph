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

## MANUAL SETUP REQUIRED

Lo que sigue **no se puede hacer desde el repositorio**. Son cambios en el
dashboard de Umami y en el host.

### 1. Goals en el dashboard

Umami no define goals en el cliente. En el sitio, *Settings -> Websites ->
kivgraph -> Goals*, crear:

|nombre|tipo|valor|
|---|---|---|
|`install_copy`|event|`install_copy`|
|`prompt_copy`|event|`prompt_copy`|
|`client_connect_copy`|event|`client_connect_copy`|
|`quickstart_copy`|event|`quickstart_copy`|
|`mcp_config_copy`|event|`mcp_config_copy`|

`github_click` se deja **fuera** de los goals a propósito: es interés, y
contarlo como conversión inflaría la tasa con clics que no llevan a instalar
nada.

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
