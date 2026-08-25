# ADR 0070: un proyecto JavaScript se declara con `jsconfig`

- **Estado:** aceptada
- **Fecha:** 2026-08-25
- **Cambia el protocolo MCP:** no
- **Cambia el schema persistente:** no
- **Obliga a reconstruir:** no -- la huella de la caché de hechos incluye el
  binario, así que la pasada siguiente ya no reutiliza nada
- **Cambia la superficie del CLI:** no
- **Relaja un contrato de la raíz:** no -- las aristas las sigue resolviendo el
  checker, sobre un programa que el motor construye igual

## Contexto: había un repositorio que no podía declarar su proyecto

Un repositorio registrado como TypeScript sólo aporta hechos si alguno de sus
paquetes tiene un **proyecto**, y hasta aquí un proyecto era un fichero cuyo
nombre empieza por `tsconfig.` y acaba en `.json`. Un paquete que envía
JavaScript no tiene ninguno, y no lo tiene por decisión de su dueño: es un CLI
de `.mjs` sueltos, no una migración a medias.

Lo medido sobre un workspace de `52` repositorios registrados: uno de ellos
-- `~145 KB` de `.mjs`, con `package.json` propio-- aportaba `0` símbolos, y el
conjunto de hechos validaba `51`. La única salida documentada era añadirle un
`tsconfig.json`, o sea pedirle a un proyecto JavaScript que se declare
TypeScript para poder ser leído.

Y el motor no necesitaba eso. Probado con el `facts-cli` del worker sobre un
`jsconfig.json` y dos `.mjs`, sin escribir `allowJs` en ninguna parte:

```
8 symbols, 4 references, 0 imports, 3 exports, 0 extends, 0 dependencies
files: ["other.mjs", "tool.mjs"]
```

Funciones, parámetros, exports y referencias reales. Lo que rechazaba el
fichero era el descubrimiento del lado Go, no el compilador.

## La decisión

Un `jsconfig` declara un proyecto igual que un `tsconfig`, y en él **`allowJs`
se implica**. Tres reglas, y ninguna es nueva: son las del compilador.

1. `isTypeScriptConfigName` acepta el prefijo `jsconfig.` además de
   `tsconfig.`.
2. `resolveTypeScriptConfig` pone `allowJs: true` cuando el proyecto es un
   `jsconfig` y el fichero no declara la opción. **Un valor declarado gana,
   `false` incluido.** Sin esto, la resolución de fuentes del lado Go no
   reclamaría un solo `.mjs` mientras el motor que carga ese mismo proyecto
   reclama todos: dos respuestas distintas a la misma pregunta.
3. Donde dos configs comparten directorio, el orden de preferencia es
   `tsconfig.json`, luego `jsconfig.json`, luego cualquier variante. Un
   paquete a medio migrar conserva su `jsconfig` para el editor y su proyecto
   sigue siendo el `tsconfig`.

## Lo que se descartó

- **Sintetizar el proyecto de un paquete que no declara ninguno.** Es la
  opción sin ficheros, y el worker ya sabe hacerlo -- es el proyecto inferido
  del ADR 0050. Pero un proyecto inferido no tiene configuración de proveedor,
  así que por su propio contrato sólo produce `symbols` y `references`: ni
  imports, ni exports, ni `extends`, ni dependencias de paquete. Un `jsconfig`
  cuesta un fichero de cuatro líneas y da los hechos completos.
- **`typescript.include_unclaimed_sources` como respuesta.** Hoy no lo es por
  dos motivos independientes: el paquete se descarta antes de que exista una
  unidad que encierre esos ficheros, y el barrido de huérfanos no mira la
  familia JavaScript a propósito -- si un `.mjs` es fuente lo decide el
  `allowJs` del proyecto, y ese barrido no resuelve ninguno.
- **Escribir un `tsconfig` dentro del repositorio indexado.** Kivgraph no
  escribe en el código que indexa. El ADR 0050 ya lo descartó.

## Consecuencias

Un repositorio de JavaScript entra en el grafo declarando un `jsconfig.json`,
que es el fichero que ya usan `tsserver` y los editores para exactamente eso.
El aviso `index.typescript.no_package` sigue nombrando al que no declara
ninguno de los dos.

Verificado de punta a punta sobre un repositorio de `.mjs` sueltos, con el
binario y el worker reales:

|proyecto del repositorio|resultado de la pasada|
|---|---|
|ninguno|`symbols=2`, 1 repositorio validado, aviso `no_package`|
|`jsconfig.json`|`symbols=4`, 2 repositorios / 2 paquetes / 2 ficheros|
