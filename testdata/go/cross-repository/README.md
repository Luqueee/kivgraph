# Fixture cross-repository Go

Tres repositorios sintéticos usados por LUQUE-0811 y LUQUE-0813.

- `shared-library`: provider `example.com/ladygraph-fixture/shared`, con función,
  método, campo, constante y una función que recibe callbacks.
- `consumer-a`: llamada directa, llamada a método y callback.
- `consumer-b`: import con alias de paquete y un `replace` local válido hacia
  un módulo anidado del propio repositorio.

Los módulos se resuelven mediante el `go.work` sintético de LUQUE-0801; no se
instala nada ni se toca ningún repositorio real.
