---
name: publishing-releases
description: Cómo y sobre todo cuándo se publica una release de Kivgraph - la versión sólo sube, los tres sitios donde vive, qué verifica CI, y la lista de cambios que NO merecen release. Usar al proponer o preparar una release, al tocar `internal/version/version.go`, al crear un tag `vX.Y.Z`, o cuando alguien pida "saca una versión".
---

# Publicar una release de Kivgraph

## La regla que más cuesta descubrir

**La versión sólo sube. Nunca baja, nunca se reinicia, nunca se reutiliza.**

`kivgraph update` pregunta a GitHub por `/releases/latest` y decide con una sola
comparación (`internal/update/update.go:134`):

```go
result.UpdateAvailable = semver.Compare(currentSemver, release.TagName) < 0
```

Es `<`, estricto. Una release con número menor que el instalado **no existe** para
esa instalación.

El 11 de agosto de 2026 el versionado se reinició: el proyecto había llegado a
`v0.10.1` y un commit `chore(release): restart versioning at v0.1.0` volvió a
empezar desde abajo. Toda instalación entre `v0.5.0` y `v0.10.1` quedó
**huérfana para siempre**: `update` les responde que están al día y nunca les
ofrecerá nada, porque `v0.4.0` no es mayor que `v0.10.1`.

No tiene arreglo salvo saltar por encima de `v0.10.1`, que tiraría el numerado
actual. Ese fue el precio de hacerlo sin motivo.

El 16 de agosto de 2026 se reinició otra vez, y esta vez la regla no protegía a
nadie: el proyecto se renombró a Kivgraph y el nombre del asset que pide una
instalación va compilado en el binario
(`bundleDirName = "kivgraph-" + GOOS + GOARCH`, `internal/update/update.go:34`).
Un `ladygraph` instalado pide `ladygraph-<os>-<arch>.tar.gz` y toda release
futura publica `kivgraph-*`: `update` le falla por asset ausente antes de
comparar un solo número. El rename ya cortó la línea de actualización de las 14
releases anteriores -23 descargas de bundle en total, dos máquinas propias-, así
que se borraron todas, se borraron sus tags y el numerado empezó en `v0.1.0`.

De ahí sale la condición, que es la regla de verdad: **un reinicio sólo es
legítimo cuando ninguna instalación podría haber actualizado de todos modos, y
eso hay que demostrarlo con el asset que pide, no con el número que tiene.**
Fuera de ese caso, la versión sólo sube.

## Cuándo NO se publica

Esta sección existe porque el historial dice lo contrario: **21 commits
`chore(release)` en dos días**, uno cada pocas horas.

Una release es un **evento de distribución**, no un punto de control. Sólo tiene
sentido si alguien que ejecute `kivgraph update` va a recibir algo que necesita.

No se publica por:

- documentación, `TASKS.md`, ADRs, benchmarks o `report.md`;
- refactors sin cambio observable;
- tests nuevos, por muchos que sean;
- **terminar una tarea `LUQUE-XXXX` o cerrar una fase**;
- dejar el trabajo «etiquetado» o «guardado»;
- que el árbol esté limpio y la suite en verde.

El backlog y la versión publicada son dos cosas distintas. Un `PASS` en `TASKS.md`
no es un motivo para publicar; es un motivo para pasar a la siguiente tarea.

**Si nadie ha pedido una release, no se propone una release.**

## Cuándo sí

- Un fallo que alguien está sufriendo **hoy en el binario publicado**.
- Una capacidad que el usuario puede ejecutar: un comando, una tool MCP, un
  lenguaje soportado.
- Un cambio de esquema o de compatibilidad que necesita su camino de upgrade.
- Una corrección en lo que se distribuye: el bundle, el instalador, los
  checksums, el analizador fijado, los assets web.

Prueba de una frase: **qué gana quien actualiza**. Si no se puede escribir sin
mencionar el número de tarea, no hay release.

## Qué número toca

Proyecto en `0.x`, esquema `0.MINOR.PATCH`:

| | Cuándo |
| --- | --- |
| **PATCH** | Por defecto. Correcciones y todo lo que no cambia la superficie observable. |
| **MINOR** | Sólo si la superficie crece o rompe: una tool MCP nueva, un comando nuevo, un cambio de esquema persistente, un flag retirado. |

Ante la duda, **patch**. Subir un minor es gratis hoy y caro después, porque el
número no baja.

## El procedimiento

Verificar primero. Un tag dispara CI, pero **CI no sabe si la release debía
existir**.

```bash
gofmt -l <archivos-go-modificados>
go vet ./...
go test ./...
make test-ladybug
make build
```

La versión vive en **cuatro sitios** y los cuatro tienen que coincidir. Se
editan a mano; lo que ya no hace falta recordar es cuáles son.

```bash
grep -n 'Value = ' internal/version/version.go   # var Value = "X.Y.Z"
go test ./internal/version/ -run DocumentedInstallVersion
```

Ese test descubre por sí mismo cada comando de instalación fijado del
repositorio y falla nombrando archivo y línea del que no coincida. Esta sección
decía «tres sitios» y su `grep` sólo miraba `README.md` y
`docs/installation.md`; mientras los dos iban en `v0.5.0`,
`landing/src/content/docs/install.md` llevaba **dos minors** de retraso
diciéndole a un lector que fijara `v0.3.0`. Una lista escrita a mano vuelve a
quedarse corta en cuanto una página nueva lleve el comando, así que el test la
descubre en vez de enumerarla.

Y hay un quinto archivo que no lleva la versión dentro sino en su **nombre**:

```bash
ls landing/src/content/releases/vX.Y.Z.md   # tiene que existir antes del tag
```

`release.yml` toma el cuerpo de esa página como las notas de la release, y si no
existe **corta la publicación** -- después de reejecutar `ci.yml` y de construir
los dos bundles en sus hosts nativos, porque el paso que la lee es el último. El
frontmatter es `version`, `date` y `requires_reindex`, validado por el esquema de
`landing/src/content.config.ts`; ese último campo es la única cosa que le dice a
quien actualiza si su grafo publicado le sirve tal cual, así que se responde
mirando si algún loader cambió lo que ve, no por costumbre.

Un tag sin esa página es exactamente el fallo que se paga más caro: el número no
se reutiliza, así que se quema un `vX.Y.Z` entero por un archivo de treinta
líneas que se escribe en un minuto.

Un commit propio, un tag **anotado** -como todos los anteriores-, y el tag
después del commit:

```bash
git commit -am "chore(release): prepare vX.Y.Z"
git tag -a vX.Y.Z -m "Kivgraph vX.Y.Z"
git push origin main
git push origin vX.Y.Z
```

El árbol tiene que estar limpio antes del tag: el workflow hace checkout del
commit exacto al que apunta, así que un tag sobre trabajo sin commitear publica
algo que no existe en `main`.

## Qué verifica CI, y qué no verifica nadie

`.github/workflows/release.yml` dispara con `v*.*.*` y, antes de construir nada,
**vuelve a ejecutar `ci.yml` sobre el commit etiquetado**. Después, por
plataforma y siempre en un host nativo -`linux/amd64` y `darwin/arm64`, no hay
cross-compilation porque cgo enlaza la biblioteca nativa-:

- que `kivgraph version` y `version --json` digan exactamente la versión del tag;
- que `bin/rust-analyzer` sea el release fijado en `tools/manifest.json`;
- que `web/index.html` esté en el payload **y** que la ayuda no diga
  `unavailable: this build carries no web bundle` — un bundle con assets enlazado
  sin el tag `webassets` serviría la página de error en todas las rutas;
- los checksums externos e internos.

Y sólo al final, `gh release create --verify-tag --generate-notes --latest`.

Lo que no comprueba nadie es **si la release tenía que existir**. Eso es esta
skill.

## Si algo sale mal

Un tag publicado no se retira ni se reescribe: hay instalaciones que ya lo vieron
y `update` compara números, no contenidos. Se arregla **hacia delante**, con el
patch siguiente.

Si CI falla después de empujar el tag, no hay release en GitHub: `gh release
create` es el último paso. Se corrige y el siguiente intento lleva un número
nuevo; el que falló no se reutiliza.

## El historial que explica estas reglas

```text
v0.5.0 … v0.10.1     numerado original, abandonado
d6b9b61              chore(release): restart versioning at v0.1.0
v0.1.0 … v0.6.0      catorce releases bajo el nombre Ladygraph
58f018b              refactor: rename the project to kivgraph
                     las catorce releases y sus tags, borrados
v0.1.0 …             numerado actual, bajo el nombre Kivgraph
```

Catorce tags publicados entre el 11 y el 14 de agosto de 2026 -más los del
numerado original, que ya no están en ninguna parte-, veintitrés commits
`chore(release)` y dos reinicios de numerado. Sólo el segundo tiene una razón
que se puede comprobar. El ritmo no se repite.

El mapa de lo que se borró, por si alguien busca a qué commit apuntaba un tag:

```text
v0.1.0 d6b9b61   v0.3.1 5c3ef12   v0.3.5 13ab818   v0.6.0 d67bc0e
v0.1.1 efe4bc3   v0.3.2 e13b9ad   v0.4.0 a07dc7b
v0.1.2 bca77c4   v0.3.3 056d85b   v0.5.0 28b9e2d
v0.2.0 515e101   v0.3.4 b1cf7b0   v0.5.1 a0372d4
v0.3.0 ba091fe
```
