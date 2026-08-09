# ADR 0027: Rutas del motor TypeScript en un filesystem que pliega mayúsculas

- **Estado:** aceptada
- **Fecha:** 2026-08-09
- **Revisa:** ADR 0025

## Contexto

APFS es insensible a mayúsculas por defecto. Medido en macOS `26.6` con el
motor TypeScript nativo fijado (`typescript` `7.0.2`), un módulo que el motor
resuelve por su cuenta se reporta con la grafía canónica del volumen, en
minúsculas:

```text
esperado  /Users/adria/.../nomap/dist/index.d.ts
observado /users/adria/.../nomap/dist/index.d.ts
```

Las rutas del propio proyecto no se ven afectadas: llegan como las declara el
`tsconfig`. El daño estaba en las que cruzan a un proveedor, y era doble:

1. Esas rutas entran en las stable keys y en la evidencia, así que el mismo
   repositorio produciría un grafo canónico distinto según la máquina que lo
   indexó.
2. Dejan de casar con los índices que el worker construye a partir de rutas
   reales -el de declaration maps, los prefijos de raíces de proveedor-, así
   que las declaration maps se perdían en silencio. En el fixture negativo eso
   añadía un `UNRESOLVED` (`DECLARATION_SOURCE_NOT_MAPPED`) y **ninguna arista
   `EXACT` falsa**: la degradación caía del lado seguro, pero seguía siendo una
   divergencia por plataforma.

La primera respuesta fue rechazar el indexado TypeScript en un volumen que
pliega mayúsculas. Preservaba el contrato de las stable keys, pero dejaba a
macOS sin TypeScript en su configuración por defecto.

## Decisión

`ts-worker/src/engine-path.ts` corrige el plegado en la frontera: recorre la
ruta componente a componente y devuelve la grafía que el disco usa.

- **No usa `realpath`.** Resolvería los enlaces de `node_modules` hacia el
  almacén `.pnpm` y cambiaría los hechos. La corrección es sólo de mayúsculas
  y minúsculas; un componente que es un enlace se conserva tal cual.
- **Un componente que no existe termina el recorrido** y el resto se conserva
  literal: una ruta virtual o borrada se devuelve intacta, nunca adivinada.
- **Las listas de directorio se memorizan**, así que el coste es un `readdir`
  por directorio que el motor menciona; en un volumen sensible a mayúsculas
  cada componente acierta a la primera.
- **Un fallo sólo se acepta tras releer el directorio.** Un índice memorizado
  es anterior a lo que escribe una indexación en curso, y un negativo obsoleto
  reintroduciría exactamente el error que este módulo corrige.

Se aplica donde una ruta del motor entra por primera vez en los datos del
worker: las declaraciones de un símbolo importado y de un export de proveedor,
el mapa de posiciones de declaración y las posiciones de proveedor sin
declaration map. El rechazo del indexador se retira.

## Consecuencias

- `pnpm check` pasa entero en macOS: `87` pruebas, ninguna saltada. Antes de
  esto fallaban `10` en `7` archivos.
- Un índice TypeScript completo en macOS sobre dos repositorios del corpus
  publica generación con `29` símbolos, `0` referencias no resueltas, `1`
  arista de paquete entre repositorios, integridad sin violaciones y `0` rutas
  plegadas en los hechos emitidos.
- Los tests del worker construyen su raíz temporal con
  `ts-worker/src/temporary-root.ts`, que resuelve el realpath. El motor reporta
  los archivos por su ruta real y `/var` es un enlace en macOS, así que un
  fixture creado desde `tmpdir()` nunca compararía igual. Producción no lo
  sufre: la capa de workspace rechaza una ruta de repositorio con componentes
  enlazados y entrega una raíz ya resuelta.
- Queda una diferencia sin cubrir por pruebas automáticas: nadie verifica en CI
  que un mismo repositorio produzca digests de snapshot idénticos en Linux y en
  macOS. Es una comparación entre plataformas, no una prueba de paquete.
