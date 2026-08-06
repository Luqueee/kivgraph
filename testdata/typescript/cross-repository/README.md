# Fixture cross-repository TypeScript

Tres repositorios sintéticos usados por LUQUE-0707, LUQUE-0709 y el cierre de
`EXTENDS`/`PACKAGE_DEPENDS_ON`.

- `shared-library`: provider publicado como `@ladygraph-fixture/shared`, con
  barrel, alias de reexport y `declaration maps` hacia sus fuentes.
  `src/inheritance.ts` añade `NamedShape extends Shape, Named` — herencia
  local, con Shape en `src/value.ts` y Named en el mismo fichero, cada base
  su propia arista — y la clase `Widget`, el destino de la herencia
  cross-repository de `consumer-a`.
- `consumer-a`: imports directos de valor y de tipo, más
  `src/derived.ts#LabeledWidget extends Widget`, herencia cross-repository
  contra la fuente real del proveedor gracias al declaration map. Su
  `package.json` también declara `@ladygraph-fixture/unused`, una dependencia
  que nada importa: prueba de que `PACKAGE_DEPENDS_ON` nunca sale de una
  cadena nominal de `package.json`, sólo de un import resuelto por el
  checker.
- `consumer-b`: barrel, alias de import, reexport y namespace.

Los consumidores resuelven el provider mediante `paths`, de modo que no se
instala ni se enlaza `node_modules` dentro del repositorio.

`shared-library/dist/` está compilado de verdad con `tsc` (declaración +
`declarationMap`) y committeado: sin declaration maps reales, la herencia
cross-repository no tiene fuente que resolver, que es justo el caso que
este fixture existe para probar. Tras tocar `shared-library/src/**`,
regenerarlo desde `shared-library/`:

```
../../../ts-worker/node_modules/.bin/tsc -p tsconfig.json
```
