# ADR 0047: La identidad de un campo se enraíza en un tipo con nombre

- **Estado:** aceptado e implementado; medido sobre `kena` y con regresión en
  `internal/goloader`
- **Fecha:** 2026-08-19
- **Revisa:** la identidad canónica de los campos de structs anónimos, y por qué
  cambiarla no exige migración

## Contexto

`kivgraph index --full` fallaba al publicar sobre un repositorio real:

```text
invalid fact set: symbol "Errors.Message" is defined by two files,
"file:api-db-go:internal/application/handlers/bots_mock_test.go" and
"file:api-db-go:internal/application/handlers/command_mock_test.go",
so two declarations share one identity
```

Los dos archivos son tests del mismo paquete y ninguno declara un nombre de nivel
superior en común. Lo que comparten es una forma, escrita dentro de una función
para deserializar una respuesta:

```go
var env struct {
	Errors []struct {
		Message string `json:"message"`
	}
}
```

`fieldOwner` construía la ruta de un campo con los nombres de campo intermedios
que encontraba en la pila y la devolvía **siempre que no estuviera vacía**, sin
exigir que un tipo con nombre la enraizara. Para ese campo devolvía `Errors`, que
no está vacío, así que `ownerFor` lo tomaba y nunca llegaba a preguntar a
`localContainer` por la función y la variable — que son justo lo que distingue una
ocurrencia de otra.

Resultado: **todo archivo de un paquete que deserialice esa forma declara un único
`Errors.Message`**. Un `Symbol` con dos `File` que lo declaran es lo que prohíbe la
multiplicidad de `DEFINES`, así que el índice no fallaba al extraer, fallaba al
publicar, con una clave base32 y dos rutas por todo diagnóstico.

El caso plano ya estaba resuelto y con test: `var raw struct{ GuildID string }`
dentro de `ParseFirst` es `ParseFirst.raw.GuildID`, porque ahí `fieldOwner` no
encuentra ningún campo intermedio con nombre, devuelve vacío y cede el turno a
`localContainer`. El defecto aparecía sólo con **un nivel de anidamiento**, que es
lo que introduce un campo intermedio con nombre y hace que la ruta parcial parezca
una respuesta.

Coste observado: `api-db-go` no se podía indexar con `go.include_tests`. En el
benchmark `benchmarks/agent-e2e/` eso obligó a excluir los tests Go de un lado y no
del otro, una asimetría declarada en su informe.

## Decisión

Una ruta de campo sólo es una respuesta si está **enraizada en un tipo con
nombre**. `fieldOwner` rastrea si vio un `*ast.TypeSpec`; si no lo vio, devuelve
vacío y `localContainer` construye la identidad con la función y los contenedores
con nombre que llevan hasta el campo.

La identidad de ese campo pasa a ser:

```text
antes:   Errors.Message
después: TestBotsGetAll_RefineNoQuery.env.Errors.Message
```

Se descartó la alternativa de **no emitir** esos campos. Un campo de struct
anónimo local no es direccionable desde fuera, así que quitarlo también cerraba la
colisión y era menos código; pero `localContainer` existe precisamente para
nombrarlos, su comentario documenta la identidad `ParseTicketsCreate.raw`, y hay un
test que la defiende. La opción coherente con lo que el proyecto ya decidió es
completar esa identidad, no retirar el símbolo.

## Consecuencias

**Qué cambia de identidad.** Sólo los campos cuya ruta no estaba enraizada en un
tipo con nombre: campos de structs anónimos declarados dentro de una función, con
al menos un campo intermedio con nombre. Un campo de un tipo declarado —el caso
normal— no se toca, porque su ruta ya nacía en el `TypeSpec`.

**Por qué no hay migración.** Se reparte en dos:

- Las identidades **en colisión** no pueden estar en ninguna generación publicada:
  un fact set que las contiene no valida, así que nunca llegó a publicarse. No hay
  nada que migrar.
- Las identidades **sin colisión** —una sola ocurrencia de la forma en el
  paquete— sí pudieron publicarse como `Errors.Message`, y en la próxima
  reconstrucción pasan a llevar la ruta completa. Es un cambio de clave para esa
  clase de símbolo, y se absorbe donde ya se absorben todos: `index --full`
  reconstruye la generación entera y el `HotSnapshot` es una proyección derivada,
  no una fuente alternativa de hechos. Ninguna clave sobrevive parcialmente a una
  reconstrucción completa.

El namespace histórico `luque-stable-key` no cambia, ni el algoritmo de la clave:
lo que cambia es el `qualified_name` que entra en él, y sólo para esa clase.

**Diagnóstico.** El error de fact set inválido ahora nombra la declaración además
de la clave. Antes decía `symbol "KY2XF76JIM4A…"`, cincuenta y dos caracteres de
base32 que no aparecen en ningún archivo del usuario; encontrar `Errors.Message`
exigió instrumentar el validador. Un mensaje que nombra la identidad y no la
declaración no es actuable.

**Lo que sigue abierto.** El barrido de `benchmarks/agent-e2e/` se midió antes de
este arreglo, con los tests Go fuera del índice de Kivgraph y dentro del de graft.
Rehacerlo con simetría queda pendiente y sus números lo declaran.
