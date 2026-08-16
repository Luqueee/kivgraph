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
actual. Es el precio de haberlo hecho una vez. No se hace dos.

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

La versión vive en **tres sitios** y los tres tienen que coincidir. No hay script
que lo haga: se editan a mano.

```bash
grep -n 'Value = ' internal/version/version.go          # var Value = "X.Y.Z"
grep -rn 'KIVGRAPH_VERSION=v' README.md docs/installation.md
```

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
v0.1.0 … v0.4.0      numerado actual
```

Once tags publicados entre el 11 y el 12 de agosto de 2026. Veintiún commits
`chore(release)` en total. Ninguna de las dos cosas se repite.
