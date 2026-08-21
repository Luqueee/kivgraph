# Instrucciones del worker TypeScript (`ts-worker/`)

Estas reglas se suman a las de `AGENTS.md` en la raíz del repositorio, que se
leen siempre. Una instrucción de este archivo puede añadir restricciones; nunca
puede relajar un contrato de integridad, compatibilidad o verificación
declarado en la raíz.

El orquestador Go que invoca este worker está en `internal/`.

- El worker usa TypeScript estricto y módulos ESM.
- Los límites de proceso, protocolo y adaptadores tienen tipos explícitos; `any`
  requiere una justificación local.
- `stdout` contiene únicamente framing/protocolo. Los logs van a `stderr`.
- Todo recurso persistente se cierra al cancelar o terminar el proceso.
- Las promesas rechazadas se clasifican en el límite adecuado; no se ocultan
  con aserciones.
- No editar `ts-worker/dist` manualmente: regenerarlo con `pnpm build`.

## Identidad y resolución cross-repository

- La identidad de un símbolo de otro repositorio sale siempre del proyecto del
  proveedor. Si su `.d.ts.map` coloca el símbolo, esa es la posición; si no lo
  hay pero el puente nombró la fuente, se le pregunta a su checker qué
  declaración exporta ese módulo bajo el nombre pedido. Las dos aristas son
  exactas y **no valen lo mismo**: la primera es
  `EXACT_TYPECHECKED`/`TYPESCRIPT_CHECKER` y la segunda
  `EXACT_PACKAGE_MAPPED`/`TYPESCRIPT_PROJECT_REFERENCE`, porque el paso de
  artefacto a fuente lo afirma la configuración de compilación del proveedor y
  no un mapa que emitiera. Sin fuente nombrada no se pregunta nada: la
  referencia queda `UNRESOLVED`. Ver ADR 0038.
- Un fixture cross-repository que resuelve al proveedor con `paths` no prueba
  nada sobre la forma que instala un gestor de paquetes. El consumidor llega
  al proveedor por un symlink de `node_modules` y el motor devuelve la ruta
  del destino del enlace, así que `consumer-linked` es el fixture que defiende
  el caso real; exigir `declarationMap` en el proveedor no lo arregla.
- Una copia instalada por un gestor de paquetes es un `File` distinto de la
  fuente que la produjo, y el tarball publicado no trae ni `src` ni
  `.d.ts.map`: ninguna transformada anclada en la raíz del proveedor relaciona
  las dos rutas, porque el artefacto no está bajo esa raíz. La fuente se
  nombra por el `name` del `package.json` más cercano al artefacto, buscado en
  el registro de proveedores; nunca por el nombre que escribió el consumidor,
  que en una dependencia transitiva pertenece a otro repositorio. La arista es
  `EXACT_PACKAGE_MAPPED`/`TYPESCRIPT_PROJECT_REFERENCE`, y si la fuente del
  workspace no exporta el nombre pedido -deriva de versión- **no se cae hacia
  el artefacto**: la referencia queda `UNRESOLVED`. Ver ADR 0051 y el fixture
  `installed-package`.
- El proyecto que resuelve la identidad de un símbolo importado es el que
  **posee el fichero** que nombra el mapa de declaraciones, no el paquete del
  que se importó. Un paquete fachada existe para reexportar los de su
  workspace, así que su mapa apunta al repositorio que los declara; preguntar
  al programa de la fachada por ese fichero no devuelve nada y la referencia se
  abandonaba como `PROVIDER_SOURCE_UNAVAILABLE` aunque la declaración estuviera
  indexada. La identidad acredita al dueño: componerla contra la fachada
  produce una clave que ese repositorio no publica y deja la arista colgando.
  La pertenencia es la raíz registrada más larga que contiene el fichero, y
  nadie posee lo que está bajo un `node_modules` -la copia instalada de un
  paquete cuelga de la raíz de quien lo instaló-.
- El worker que ejecuta una pasada es el del checkout cuando existe
  `ts-worker/dist` y nadie configuró otro: un shim instalado de una release
  anterior ocupa el mismo nombre en el `PATH` y ganaba, de modo que `pnpm
  build` no cambiaba nada observable y la medición salía mal con el aspecto de
  estar bien.

## Verificación

```bash
cd ts-worker
pnpm check
pnpm build
```

`pnpm check` es `format:check`, `lint`, `typecheck` y `test`. `dist/` es
generado: se regenera con `pnpm build` y nunca se edita a mano.
