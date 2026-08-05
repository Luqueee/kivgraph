# Fixture cross-repository negativo

Casos que nunca deben producir una arista exacta, usados por LUQUE-0708 y
LUQUE-0709:

- homónimo local con el mismo nombre que un export del provider;
- paquete duplicado en dos repositorios (`AMBIGUOUS_PACKAGE_PROVIDER`);
- export ausente (`EXPORT_NOT_FOUND`);
- versión incompatible (`VERSION_MISMATCH`);
- `.d.ts` sin source map (`DECLARATION_SOURCE_NOT_MAPPED`);
- otro paquete que exporta un símbolo con el mismo nombre.

El consumidor resuelve cada provider mediante `paths`.
