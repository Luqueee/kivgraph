# Fixture type-relations Go

Módulo único usado por los tests de IMPLEMENTS, EMBEDS y OVERRIDES.

- `Shape`: interfaz de un método (`Area`), satisfecha por `Circle` (por
  valor) y por `*Square` (sólo por puntero); `Triangle` no la implementa,
  le falta el método.
- `Solid`: interfaz que embebe `Shape` y añade `Volume`; no tiene
  implementador en este fixture, sólo demuestra EMBEDS interfaz-interfaz.
- `Anything`: la interfaz vacía bajo un nombre propio; nunca debe aparecer
  como destino de IMPLEMENTS, la satisface cualquier tipo y no informa nada.
- `Base`: struct con el método `ID`; `Circle` lo embebe por valor y `Square`
  lo embebe por puntero (EMBEDS struct-struct, distinguiendo ambas formas).
- `Circle.ID`: declarado directamente, oculta el `ID` que llegaría
  promovido desde `Base` (OVERRIDES).
- `Circle.String`: satisface `fmt.Stringer`, interfaz de una dependencia sin
  repositorio propio en este workspace: la relación existe pero su destino
  no tiene clave derivable en este paso y debe descartarse.

No usa el `go.work` sintético: es un único módulo autocontenido, cargado
directamente con `goloader.Load`.
