# Fixture cross-repository TypeScript

Tres repositorios sintéticos usados por LUQUE-0707 y LUQUE-0709.

- `shared-library`: provider publicado como `@luque-fixture/shared`, con
  barrel, alias de reexport y `declaration maps` hacia sus fuentes.
- `consumer-a`: imports directos de valor y de tipo.
- `consumer-b`: barrel, alias de import, reexport y namespace.

Los consumidores resuelven el provider mediante `paths`, de modo que no se
instala ni se enlaza `node_modules` dentro del repositorio.
