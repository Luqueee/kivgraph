# Fixture cross-repository negativo

Casos que nunca deben producir una arista exacta, usados por LUQUE-0708 y
LUQUE-0709:

- homónimo local con el mismo nombre que un export del provider;
- paquete duplicado en dos repositorios (`AMBIGUOUS_PACKAGE_PROVIDER`);
- export ausente (`EXPORT_NOT_FOUND`);
- versión incompatible (`VERSION_MISMATCH`);
- `.d.ts` sin source map (`DECLARATION_SOURCE_NOT_MAPPED`);
- otro paquete que exporta un símbolo con el mismo nombre.

El consumidor `consumer/` resuelve cada provider mediante `paths`, que apunta
al `dist/` dentro del propio repositorio proveedor.

`consumer-linked/` existe porque eso no es lo que instala un gestor de
paquetes: resuelve `@ladygraph-fixture/nomap` por un symlink de
`node_modules`, la forma real, y el motor devuelve la ruta del destino del
enlace. Es el caso en el que un proveedor sin declaration map se coloca por
el checker de su propio proyecto (`EXACT_PACKAGE_MAPPED`, ADR 0038); con
`paths` esa diferencia no se observa.
