# Fixture cross-repository Go negativo

Casos que nunca deben producir una arista exacta, usados por LUQUE-0812 y
LUQUE-0813:

- homónimo local con el mismo nombre que un export del provider;
- módulo duplicado en dos repositorios (`AMBIGUOUS_MODULE_PROVIDER`);
- método homónimo declarado por otro receptor;
- callback local con el mismo nombre que uno del provider;
- `replace` conflictivo entre dos módulos (`REPLACE_CONFLICT`).

El `go.work` sintético incluye sólo uno de los módulos duplicados —`go` no
admite dos directorios sirviendo el mismo módulo—, mientras que el registro de
módulos ve ambos repositorios.
