---
name: publishing-releases
description: Procedimiento seguro y reproducible para decidir, preparar, etiquetar, verificar y cerrar una release de Kivgraph. Usar cuando alguien pida sacar una versión, al tocar internal/version/version.go, una nota de release o un tag vX.Y.Z.
---

# Publicar una release de Kivgraph

Esta skill cubre el cambio completo: decidir si merece la pena publicar,
actualizar las fuentes correctas, pasar los gates, crear el tag y comprobar lo
que realmente llegó a GitHub y al MCP Registry. No publica nada si la persona
no lo ha pedido explícitamente.

## Reglas que no se negocian

- La versión sólo sube. Nunca se baja, se reinicia, se reutiliza ni se reescribe
  un tag que ya se haya empujado.
- Las releases estables usan vX.Y.Z. Las releases de desarrollo usan un sufijo
  SemVer, por ejemplo vX.Y.Z-dev.N; son prereleases y no se marcan como latest.
- El tag debe apuntar al commit exacto que ya está integrado en main. No se
  etiqueta una rama de trabajo ni un árbol sucio.
- La nota de release debe existir antes del tag. Si falta, CI quema el tiempo de
  construir los artefactos y falla justo antes de publicarlos.
- Una release es un evento de distribución, no una marca de progreso del
  backlog. El número de tarea, un PASS en TASKS.md o una suite verde no son
  por sí solos un motivo.

`kivgraph update` selecciona un canal y compara versiones con SemVer. El canal
`stable` consulta `/releases/latest`; el canal `dev` enumera las primeras 100
prereleases publicadas y elige la mayor versión válida. Sin `--channel`, una
instalación estable usa `stable` y una instalación prerelease usa `dev`. También
se puede seleccionar explícitamente con `--channel stable|dev` o
`KIVGRAPH_UPDATE_CHANNEL`.

## 1. Decidir si hay release y qué número usar

Publicar sólo tiene sentido si quien actualiza obtiene algo ejecutable y
observable:

- una corrección que afecta al binario publicado;
- una capacidad nueva del CLI, MCP o de un lenguaje soportado;
- un cambio de compatibilidad, schema o migración;
- una corrección del bundle, instaladores, checksums, analizador o visor web.

No publicar por documentación aislada, TASKS.md, ADRs, benchmarks, tests,
refactors sin cambio observable, ni por completar una tarea LUQUE-####.
Resume en una frase qué gana quien actualiza. Si la frase sólo habla del
trabajo interno, no hay release.

En la serie 0.MINOR.PATCH:

| versión | usarla para |
| --- | --- |
| PATCH | opción por defecto: bugs y cambios que no amplían ni rompen la superficie observable; |
| MINOR | una tool MCP o comando nuevo, un cambio de schema persistente, una compatibilidad nueva o un flag retirado. |

Antes de elegir el número, contrástalo con GitHub y con todos los tags. No uses
un número recordado de una conversación ni una comparación lexicográfica:

~~~bash
git fetch origin --tags
gh api repos/Luqueee/kivgraph/releases/latest --jq .tag_name
git tag --list 'v[0-9]*' --sort=-version:refname | head -1
~~~

El nuevo tag tiene que ser semánticamente mayor que la release estable que
GitHub sirve como latest, mayor que cualquier tag existente y no puede existir
ya como tag ni como release:

~~~bash
TAG=vX.Y.Z
if git show-ref --verify --quiet "refs/tags/$TAG"; then
  echo "tag already exists: $TAG" >&2
  exit 1
fi
if gh release view "$TAG" >/dev/null 2>&1; then
  echo "GitHub release already exists: $TAG" >&2
  exit 1
fi
~~~

Si hay dudas sobre el orden semántico, detenerse y resolverlas; no forzar el
número. kivgraph update compara versiones de forma estricta y una versión
menor deja instalaciones sin camino de actualización.

## 2. Preparar las fuentes de la release

Trabajar desde main actualizado. Si los cambios aún están en una PR, primero
se integran mediante el flujo normal del repositorio; el commit de preparación
no se etiqueta desde la rama de la PR.

Para preparar el commit desde una base conocida:

~~~bash
git switch main
git pull --ff-only origin main
git switch -c chore/release-vX.Y.Z
~~~

La fuente de versión y la nota obligatoria son:

~~~text
internal/version/version.go                 var Value = "X.Y.Z"
landing/src/content/releases/vX.Y.Z.md      nombre de la nota y frontmatter
~~~

La página pública de telemetría también forma parte del contrato de una
release. No lleva una copia manual de «la última versión»: nombra la primera
release que introdujo cada emisor y describe el collector que está desplegado.
Si cambian un campo, un emisor, la deduplicación, el almacenamiento, la
retención o el opt-out, actualizar en el mismo commit:

~~~text
landing/src/content/docs/telemetry.md
docs/development/analytics.md
docs/adr/0083-a-download-is-not-a-person.md o un ADR que lo sustituya
~~~

Una nota nueva no basta si la página sigue describiendo el pipeline anterior.

El resto de comandos fijados con KIVGRAPH_VERSION=v... son documentación
derivada. No hay que mantener una lista manual: el test los descubre en
README.md, docs/, landing/ y los scripts, excluyendo historiales y ledgers.
Actualiza sólo los ejemplos que pretendan enseñar la versión actual; no hagas
un reemplazo global que altere referencias históricas.

~~~bash
go test ./internal/version/ \
  -run 'Test(DocumentedInstallVersionMatchesTheBinary|ReleaseNotes)$' \
  -count=1
~~~

La nota landing/src/content/releases/vX.Y.Z.md debe tener este frontmatter:

~~~yaml
---
version: vX.Y.Z
date: YYYY-MM-DD
requires_reindex: false
---
~~~

Elige requires_reindex por el comportamiento, no por costumbre:

- true si cambian loaders, resolución, identidades, aristas o el schema de
  forma que un grafo existente ya no representa correctamente las fuentes;
- false si el grafo existente sigue siendo válido.

Si cambia el schema persistente o una superficie de compatibilidad, revisa el
ADR y el camino de migración o full rebuild antes de escribir la nota. La nota
debe explicar el impacto para el usuario, los cambios incompatibles y la acción
necesaria para actualizar. Su cuerpo se publica en la landing y como GitHub
Release Note, por lo que se escribe en inglés.

server.json es una plantilla de metadatos del MCP Registry, no otra fuente de
la versión del binario. El workflow genera su version y sus packages a partir
de los artefactos de esa ejecución. No se añade una lista packages ni se
inventan checksums; sólo se edita el template si cambia su metadata estable.

No editar a mano dist/, .tooling/, web/dist, landing/dist, ts-worker/dist,
manifests fijados ni lockfiles.

## 3. Gates antes de crear el tag

El tag dispara el pipeline, pero CI no puede decidir si el número ni la nota
eran correctos. Ejecutar los gates relevantes antes de publicar y conservar el
resultado:

~~~bash
git diff --check
test -z "$(find . -type f -name '*.go' -not -path './.git/*' -exec gofmt -l {} +)"
go vet ./...
go test ./...
make build
make test-ladybug
make lint-ladybug
make coverage
make semantic-coverage
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7
go run honnef.co/go/tools/cmd/staticcheck@2025.1.1 \
  -checks='all,-U1000' ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...
scripts/check-docs.sh origin/main
go test -race \
  ./cmd/kivgraph/... \
  ./internal/hotsnapshot/... \
  ./internal/indexing/... \
  ./internal/daemon/... \
  ./internal/storage/generation/...
~~~

make test-ladybug es el modo soportado para la suite nativa; no sustituirlo por
go test -tags ladybug sin la biblioteca y el CGO_* que prepara el Makefile.
make build produce el binario Go normal, no el bundle nativo que se distribuye.

Repetir los checks de los tres paquetes Node, igual que CI:

~~~bash
(cd ts-worker && pnpm install --frozen-lockfile && pnpm format:check && \
  pnpm lint && pnpm typecheck && pnpm test && pnpm build)
(cd web && pnpm install --frozen-lockfile && pnpm check && pnpm build)
(cd landing && pnpm install --frozen-lockfile && pnpm check && pnpm build)
~~~

make semantic-coverage y los analizadores no deben convertirse en skips por
falta de herramientas. Instalar el toolchain que exige CI y dejar visible el
fallo si no se puede completar.

CI también cruza el código para windows/amd64 y parsea los dos scripts de
PowerShell. Si se modifica código específico de Windows o un instalador,
reproducir esos dos checks con las herramientas del workflow; que Linux compile
no demuestra que el instalador de Windows sea válido.

Si se está en un host de publicación, verificar además el bundle nativo de esa
plataforma y su reproducibilidad:

~~~bash
make build-linux-amd64       # sólo en Linux amd64
make build-darwin-arm64      # sólo en macOS arm64
scripts/check-reproducible-bundle.sh
~~~

El workflow de release es quien construye las tres plataformas —Linux amd64,
macOS arm64 y Windows amd64— en hosts nativos. Nunca tratar un bundle local de
otra plataforma como evidencia de esa plataforma.

## 4. Commit, integración y tag

Después de actualizar Value, la nota y la documentación fijada, comprobar:

~~~bash
go test ./internal/version/ \
  -run 'Test(DocumentedInstallVersionMatchesTheBinary|ReleaseNotes)$' \
  -count=1
git diff --check
git status --short
~~~

Crear un commit de preparación con un mensaje en inglés:

~~~bash
git add internal/version/version.go landing/src/content/releases/vX.Y.Z.md
# Añadir explícitamente, si cambiaron, los ficheros con ejemplos de instalación.
git commit -m "chore(release): prepare vX.Y.Z"
~~~

Integrar ese commit en main mediante la protección habitual del repositorio.
Cuando el commit ya esté en el remoto, crear el tag anotado sobre el SHA exacto
de origin/main:

~~~bash
git fetch origin main --tags
test "$(git rev-parse HEAD)" = "$(git rev-parse origin/main)"
test -z "$(git status --porcelain)"
git tag -a vX.Y.Z -m "Kivgraph vX.Y.Z"
git push origin vX.Y.Z
~~~

Para una release de desarrollo, sustituir el tag y el nombre de la nota por
`vX.Y.Z-dev.N`. El workflow publica la GitHub Release como prerelease y no la
marca como latest; tampoco la publica en el MCP Registry. La nota se llama
exactamente `landing/src/content/releases/vX.Y.Z-dev.N.md`.

No crear manualmente una GitHub Release antes del workflow: el push del tag
dispara .github/workflows/release.yml y gh release create es responsabilidad
del job publish.

## 5. Qué hace realmente el workflow

.github/workflows/release.yml vuelve a ejecutar ci.yml sobre el commit
etiquetado y después:

1. construye y verifica el bundle completo en cada host nativo;
2. comprueba versión, procedencia, checksums, rust-analyzer, web/index.html,
   la ayuda de ui y la ausencia de landing/ en el payload;
3. crea un archivo reproducible y un MCPB (.mcpb) por plataforma;
4. ensambla SHA256SUMS sobre todos los assets y adjunta attestations de
   provenance;
5. lee el cuerpo de la nota correspondiente al tag y crea la GitHub Release;
   una estable se marca como latest y una prerelease conserva `latest` intacto;
6. descarga desde la release recién publicada cada archivo de las tres
   plataformas, comprueba su digest, lo extrae y vuelve a ejecutar
   scripts/verify-bundle.sh;
7. sólo después valida y publica los tres MCPB en el MCP Registry usando OIDC.

La publicación esperada contiene exactamente estos tipos de assets:

~~~text
kivgraph-linux-amd64.tar.gz
kivgraph-darwin-arm64.tar.gz
kivgraph-windows-amd64.zip
kivgraph-linux-amd64.mcpb
kivgraph-darwin-arm64.mcpb
kivgraph-windows-amd64.mcpb
install.sh
install.ps1
uninstall.sh
uninstall.ps1
SHA256SUMS
~~~

Los tar.gz/zip son para instalación manual e instaladores; los mcpb son los
paquetes que entiende el Registry. El server.json que se publica en el
Registry se genera durante el job con los hashes de esos MCPB, no se toma del
fichero commiteado sin packages.

## 6. Comprobar y cerrar la publicación

Esperar el run del tag y revisar todos los jobs, no sólo verify:

~~~bash
gh run list --workflow release.yml --limit 10
RUN_ID="$(gh run list --workflow release.yml --limit 1 --json databaseId --jq '.[0].databaseId')"
test -n "$RUN_ID"
gh run view "$RUN_ID" --log-failed
gh release view vX.Y.Z --json tagName,isDraft,isPrerelease,isLatest,assets,url
~~~

El run sólo está cerrado cuando pasan verify, las tres filas de build, publish y
las tres filas de post-publish-smoke. En una estable también debe pasar registry
y la GitHub Release debe ser pública, no draft, no prerelease, estar marcada
como latest y tener los 11 assets anteriores. En una prerelease no se espera el
job registry ni la marca latest.

Descargar los assets publicados y verificar el manifest completo desde fuera de
la workspace que los construyó:

~~~bash
tmp_dir="$(mktemp -d)"
gh release download vX.Y.Z --repo Luqueee/kivgraph --dir "$tmp_dir"
(
  cd "$tmp_dir"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum -c SHA256SUMS
  else
    shasum -a 256 -c SHA256SUMS
  fi
)
~~~

Si falla post-publish-smoke, registry o la verificación externa, la release no
se da por buena aunque GitHub muestre el tag. Registrar qué asset, job, digest o
metadata falló antes de decidir la reparación.

La publicación tampoco termina en GitHub. Actualizar la landing desplegada
desde el `main` que contiene la release, construirla con `make landing-check` y
`make landing-build`, recargar `kivgraph-landing` y comprobar desde fuera:

~~~bash
curl -fsS https://kivgraph.dev/releases/ | grep -F "vX.Y.Z"
curl -fsS https://kivgraph.dev/telemetry/ | grep -F "/api/telemetry/first-run"
curl -fsSI https://kivgraph.dev/install.sh | grep -i '^location:'
~~~

El collector de primer arranque y el dashboard D1 son parte del smoke de
distribución. Comprobar que la ruta exacta responde `204`, que el Worker
desplegado tiene su binding D1 y su secreto de hash diario, y que la migración
que agrega la versión está aplicada **antes** de publicar el emisor. No enviar
un evento sintético para poner una versión en la gráfica: si el smoke ejecuta
de verdad un instalador o un binario publicado, su fila debe aparecer en el
dashboard; si no ejecuta ninguno, la ausencia todavía es el resultado honesto.
Una instalación real observada que no aparece es una release incompleta, no
algo que se difiere al siguiente cron.

## 7. Fallos y recuperación

- Antes de empujar el tag: corregir en la misma preparación y repetir todos los
  gates.
- Después de empujar el tag pero antes de crear la GitHub Release: el tag ya
  está consumido. Corregir hacia delante con el siguiente número; no borrar ni
  reutilizar el tag fallido.
- Después de crear la GitHub Release: no retaggear, borrar la release ni
  reemplazar artefactos sin una decisión explícita. El workflow usa --clobber;
  repetirlo sobre una release existente puede sustituir assets y reiniciar sus
  contadores de descarga.
- Si sólo falla el MCP Registry, conservar la GitHub Release y separar el
  diagnóstico del artefacto ya publicado. No atribuir al registry un éxito que
  no aparece en el job registry.

La corrección normal es una nueva release con un número mayor. Nunca se arregla
una instalación publicada cambiando silenciosamente el contenido de un tag que
ya vio un usuario.

## Referencias de verdad

Cuando una afirmación de esta skill parezca no coincidir con el comportamiento,
consultar en este orden:

1. .github/workflows/release.yml, que define la entrega real;
2. internal/version/documented_test.go, que descubre las versiones documentadas
   y la nota obligatoria;
3. scripts/verify-bundle.sh, scripts/build-mcpb.sh y
   scripts/build-server-json.sh, que definen los contratos de los artefactos;
4. docs/development/mcp-registry.md y
   docs/release/production-qualification.md, para las restricciones y la
   evidencia de cualificación.
