# Instrucciones del CLI (`cmd/kivgraph/`)

Estas reglas se suman a las de `AGENTS.md` en la raíz del repositorio, que se
leen siempre. Una instrucción de este archivo puede añadir restricciones; nunca
puede relajar un contrato de integridad, compatibilidad o verificación
declarado en la raíz.

Lo que estos comandos ejecutan vive en `internal/`; aquí sólo está su
superficie observable.

## Ayuda, salidas y registro

- La ayuda no es un error: `--help`, `-h` y `help` escriben en `stdout` y
  terminan con código `0`; una invocación sin comando o con uno desconocido
  escribe una línea en `stderr` y apunta a la ayuda, nunca vuelca la superficie
  entera. Un comando puntual informa en texto plano cuando `stderr` es una
  terminal y como registro JSON cuando no lo es; `serve` y `ui` registran
  siempre en JSON, porque un cliente lee su `stderr`.
- La ayuda marca el comando que esta build no puede ejecutar. El bundle MCP
  publicado no lleva `webassets`, así que anunciar `ui` sin decirlo describe
  una capacidad que no existe.
- Un comando puntual clasifica lo que escribe en `stderr`: el progreso es
  `INFO` y sólo un fallo es `ERROR`, y el texto de la línea es el `msg` del
  registro, nunca un campo dentro de un mensaje fijo. Un registro que un
  lector no puede filtrar por nivel ni encontrar por texto no informa de nada.
- `doctor` informa del techo de versión con el que este binario comprueba
  tipos, no solo del `go` del PATH: son números distintos y el que decide si un
  repositorio se puede indexar es el primero.

## Protocolo de `index --full --json`

- El flujo de `index --full --json` es un protocolo, no una bitácora: `stdout`
  lleva sólo eventos JSON por línea -`progress`, y un único `result` al final- y
  el informe que leería una persona no se escribe en ese modo. Los contadores de
  los tres lenguajes viajan en el `result`; derivarlos del `msg` de un registro
  convertiría texto para humanos en una API. Un lector ignora una clase de
  evento que no conoce.

## Comandos que destruyen o terminan estado

- `kivgraph clean` retira generaciones publicadas: enumera y no toca nada sin
  `--yes`, porque no hay deshacer -- también se lleva el backup del que vive
  `rollback`. Sin flags deja el store vacío y libera la reserva de espacio;
  con `--keep-active` conserva exactamente la generación publicada. Nunca toca
  la configuración ni el registro de repositorios.
- `kivgraph stop` termina los procesos largos de este usuario -- `serve` y
  `ui` -- y nada más. Selecciona por invocación, no por ejecutable: una
  indexación en curso son minutos de análisis y no se tira, y el propio `stop`
  no se mata a sí mismo. Manda `SIGTERM`, espera el cierre graceful acotado y
  sólo entonces `SIGKILL`, y antes de escalar vuelve a comprobar que el pid
  sigue siendo la misma invocación: un pid liberado durante la espera puede ya
  pertenecer a otro proceso. `--dry-run` enumera sin señalar.

## `kivgraph ui`

- `kivgraph ui` registra la dirección que ha enlazado, incluida la que
  resuelve un puerto `0`, y se niega a arrancar cuando el binario no lleva el
  tag `webassets`: el bundle MCP publicado no lo lleva, así que solo podría
  servir la página de «bundle no disponible».

## Verificación

Un cambio de la superficie del CLI es un cambio de compatibilidad: revisar
`cmd/kivgraph/help.go`, la documentación de `landing/` y `scripts/install.sh`.

```bash
go test ./cmd/...
make build
```
