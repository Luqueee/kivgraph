# ADR 0008: Carga y resolución de configuración

- Estado: aceptada
- Fecha: 2026-08-05

## Contexto

Ladygraph necesita cargar un documento principal `config.yaml` y el registro
independiente `repositories.yaml` antes de iniciar el indexador. La
configuración debe ser reproducible entre invocaciones y no puede aceptar
campos desconocidos silenciosamente.

## Decisión

El paquete `internal/config` usa `gopkg.in/yaml.v3` con `KnownFields(true)` y
exige el campo `version`. La versión soportada actualmente es `1`. `Load`
valida ambos documentos y devuelve la configuración junto con el registro de
repositorios ya resuelto.

Los defaults siguen el contrato de `PLAN.md`. Las rutas se normalizan a rutas
absolutas expandiendo variables de entorno y `~`; una ruta relativa del archivo
principal se resuelve respecto a la carpeta de `config.yaml`, y una ruta
relativa de un repositorio respecto a la carpeta de `repositories.yaml`.

La carga no comprueba todavía existencia, permisos, symlinks ni anidamiento de
repositorios. Esas comprobaciones pertenecen a LUQUE-0403 y no deben ocultarse
como parte de la resolución sintáctica de configuración.

## Consecuencias

- Un typo en una clave YAML produce un error antes de arrancar el servicio.
- Los consumidores reciben paths absolutos y no necesitan repetir la lógica de
  expansión.
- Un cambio incompatible exige incrementar la versión del schema.
- El registro puede estar explícitamente vacío (`repositories: []`), pero debe
  declarar la lista y la versión del schema.


## Inicialización local

`config.Initialize` materializa la configuración y el registro vacío con
creación exclusiva por defecto (`Force` permite un reemplazo explícito). La
operación crea los directorios de estado con permisos `0700` y los archivos
con permisos `0600`. `RegisterRepositories` añade registros mediante una
escritura atómica y valida nombres, paths y lenguajes antes de reemplazar el
registro.

La CLI expone este contrato mediante `ladygraph init`; el diagnóstico posterior
pertenece a `ladygraph doctor`, que además valida la accesibilidad Git y el
estado publicado sin modificar repositorios fuente.