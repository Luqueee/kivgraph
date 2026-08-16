# ADR 0032: El visor escucha en todas las interfaces por defecto

- **Estado:** aceptada
- **Fecha:** 2026-08-11
- **Revisa:** ADR 0017

## Contexto

ADR 0017 fijó `127.0.0.1:7777` como bind por defecto de `kivgraph ui` y exigió
«una opción explícita y una advertencia operacional» para cualquier bind que no
fuera loopback. Era la decisión conservadora y describía mal el uso real.

El grafo se indexa donde están los repositorios -una máquina de desarrollo
remota, un servidor de build- y se mira desde otra. Con el default loopback el
caso normal empieza siempre con una edición: `--addr` en cada arranque, o
`web.address` en la configuración. Un default que hay que cambiar la primera
vez que se usa el comando no está protegiendo a nadie: está estorbando y
enseñando a saltárselo.

## Decisión

El bind por defecto es `0.0.0.0:7777`.

La guarda pasa a ser, enteramente, la advertencia. Por eso deja de ser
decorativa: todo bind que no sea loopback registra qué viaja en una respuesta
-rutas de repositorio y de fichero, nombres de símbolo y firmas- y con qué se
cierra -`--addr` o `web.address`-. Una advertencia que sólo dice
«unauthenticated» es una que nadie acciona.

Lo que no cambia:

- El transporte sigue siendo read-only y sirve sólo el `HotSnapshot` publicado.
  No indexa, no reconstruye, no registra repositorios y no muta generaciones.
- `kivgraph serve` sigue siendo STDIO y no abre ningún puerto.
- `ui` sigue siendo opt-in: nada lo arranca solo.
- Una configuración ya escrita conserva su `web.address`. El default sólo
  aplica a una configuración nueva; cambiar una existente es editar el campo.

## Consecuencias

El visor es alcanzable desde la red en el momento en que alguien lo arranca con
una configuración nueva. **No hay autenticación**, así que quien alcance el
puerto ve la topología del código indexado: nombres de repositorio, rutas de
fichero, nombres de símbolo y firmas. No ve el contenido de los ficheros -el
snapshot no lo guarda-, pero eso no lo convierte en público.

Es el intercambio que se acepta aquí, y conviene decirlo sin adornos: se cambia
un default seguro por uno usable. Quien indexe código que no debe salir de la
máquina tiene que restringir el bind, y la advertencia se lo dice en cada
arranque.

Un test fija que la advertencia se emite para `0.0.0.0` y para una IP de red, y
que no se emite para las dos formas de loopback: es lo único que queda entre el
visor y la red en la que está.
