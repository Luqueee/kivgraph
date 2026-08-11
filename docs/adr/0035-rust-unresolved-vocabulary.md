# ADR 0035: Vocabulario de no resueltos y límites de exactitud en Rust

- **Estado:** aceptada
- **Fecha:** 2026-08-11
- **Revisa:** ADR 0033

## Contexto

En Go y en TypeScript, el motor que resuelve también informa de lo que no pudo
resolver: el cargador Go entrega diagnósticos por paquete y el worker
TypeScript clasifica cada import fallido. Por eso `UNRESOLVED` es un resultado
del análisis y no una deducción.

`rust-analyzer scip` no ofrece eso. Cuando un token no tiene moniker, el
exportador simplemente lo salta —`// token did not have a moniker, so there is
no reasonable occurrence to emit`—. Un índice SCIP es la lista de lo que sí se
resolvió; el silencio no distingue entre «no hay nada aquí» y «no se pudo».

Sin una respuesta a esto, Rust entraría al grafo con una cobertura que nadie
puede auditar, que es la forma más cara de mentir: no hay error, solo faltan
aristas.

## Decisión

Los no resueltos de Rust se **derivan de tres fuentes observadas**, nunca de
una suposición, y cada uno conserva motivo, repositorio y lenguaje; con
ocurrencia concreta, además archivo y posición.

### 1. El registro de crates

| Motivo | Cuándo |
| --- | --- |
| `CRATE_PROVIDER_NOT_FOUND` | ningún repositorio registrado provee el crate del destino |
| `AMBIGUOUS_CRATE_PROVIDER` | dos o más lo declaran; ninguno lo provee |
| `CRATE_VERSION_UNKNOWN` | el símbolo llega con versión `"."` |
| `CRATE_SYMBOL_NOT_MATCHED` | la clave calculada del destino no existe en el conjunto |

`CRATE_SYMBOL_NOT_MATCHED` es el caso que impide una arista colgante: si el
consumidor compiló contra la copia del registro y el proveedor indexado es otro
código, las firmas difieren y la clave no aparece. Se declara; no se emite.

### 2. El diff entre la sintaxis y el índice

Tree-sitter recorre el archivo y enumera sus declaraciones. Una declaración sin
definición SCIP correspondiente es `DEFINITION_NOT_INDEXED`. Es la única señal
disponible de que el analizador no vio parte del archivo, y es evidencia
observada en dos fuentes, no una inferencia.

De ahí se separan dos casos con causa conocida:

- `TARGET_NOT_BUILDABLE`: la configuración de `cfg` y features no selecciona
  ese código. No es un fallo del índice, es el equivalente Rust de
  `PACKAGE_NOT_BUILDABLE`.
- `MACRO_EXPANSION_DISABLED`: la expansión estaba apagada por configuración.

### 3. El propio repositorio

`DEFINITION_NOT_INDEXED` cubre también el caso simétrico: un símbolo cuyo crate
es de este repositorio y que el índice no define. El bloque `impl` es el
ejemplo cotidiano -`-> Self` lo menciona y SCIP no lo define nunca-, y componer
su clave para emitir la arista dejaba una arista colgante que abortaba la
pasada. Una clave que nadie publica no es un destino.

### 4. La carga del workspace

`WORKSPACE_NOT_LOADED` y `ANALYZER_UNAVAILABLE` son fallos a nivel de
repositorio. Pueden no tener archivo, y no se les fabrica evidencia ni una
arista.

### Lo que no se emite

Una relación cuya forma la gramática no ve -una implementación generada por una
macro- no se emite ni se sustituye por un `CANDIDATE` que sugiera cobertura:
`IMPLEMENTS`, `EXTENDS` y `OVERRIDES` existen exactamente donde el `impl` o el
bound están escritos y el analizador resolvió sus dos extremos. El destino de un
`OVERRIDES` se compone desde el símbolo del trait y se descarta si el índice no
lo observó.

## Alternativas descartadas

- **Confiar en que el índice está completo.** Es la opción que no se puede
  auditar.
- **Emitir un `CANDIDATE` por coincidencia de nombre para cada declaración no
  indexada.** Convierte un hueco medible en ruido con apariencia de dato.
- **Parchear rust-analyzer para que emita los tokens sin moniker.** Nos ataría
  a un fork y no cambia el hecho de que sin moniker no hay identidad.

## Consecuencias

- `get_unresolved_references` responde para Rust con el mismo detalle que para
  los otros dos lenguajes; la tool no cambia de esquema, porque el vocabulario
  de motivos ya es por lenguaje.
- La cobertura de Rust es medible: declaraciones vistas por la sintaxis frente
  a definiciones indexadas.
- La auditoría de exactitud puede separar `false exact edges` de aristas
  colgantes, porque ninguna de las dos categorías se disimula como la otra.

## Riesgos

- **Sobreconteo de `DEFINITION_NOT_INDEXED`.** La sintaxis ve declaraciones que
  el analizador descarta legítimamente. Equivocarse en esa dirección infla un
  contador visible; en la contraria, oculta un hueco. Se elige la dirección
  ruidosa y se afina con fixtures.
- **Vocabulario creciente.** Cada motivo nuevo debe tener una causa observable
  distinta; un motivo que no se puede provocar en un test no entra.

## Verificación

Sobre los fixtures, los tres casos declaran lo que no pudieron resolver:
`core` en los dos que compilan contra la biblioteca estándar, y `support` en el
que indexa al consumidor sin registrar a su proveedor. La auditoría exige que
cada motivo esperado aparezca y que ninguna arista exacta sobre; ambos
contadores están en `benchmarks/rust-semantic/results.json`.

El vocabulario implementado añadió `CRATE_VERSION_MISMATCH` al listado inicial:
un crate registrado a otra versión no es un proveedor ausente, y confundirlos
habría escondido el caso que más se parece a una arista correcta.
