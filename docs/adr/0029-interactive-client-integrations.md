# ADR 0029: Selección interactiva de clientes para integraciones

- **Estado:** aceptada
- **Fecha:** 2026-08-10
- **Decisión:** hacer interactivos `mcp install` y `skill install` cuando no
  reciben `--target`, detectar los clientes presentes en el ámbito solicitado y
  permitir instalar en uno o varios clientes en una sola invocación.

## Contexto

La matriz de clientes y las rutas nativas están definidas en el ADR 0028, pero
obligar al usuario a conocer el identificador interno de cada cliente antes de
instalar una integración convierte el primer uso en una tarea de documentación.
Además, una sola invocación no cubría el caso habitual de tener varios agentes
instalados.

## Decisión

Los comandos de instalación tienen dos modos:

```text
ladygraph mcp install [--scope user|project]
ladygraph skill install [--scope user|project]
```

Sin `--target`, Ladygraph:

1. inspecciona las raíces de configuración o instalación conocidas para el
   ámbito solicitado;
2. muestra los clientes detectados y el listado completo compatible en un orden
   determinista;
3. selecciona todos los detectados al pulsar Enter;
4. acepta una lista de números separada por comas para seleccionar un subconjunto;
5. si no detecta ninguno, ofrece igualmente todos los clientes compatibles.

`--target TARGET` se conserva para automatización no interactiva y selecciona
un único cliente. `status` y `remove` siguen exigiendo `--target`, porque una
operación de inspección o borrado no debe adivinar el destino.

Las señales de detección son locales y de solo lectura: ficheros de
configuración conocidos, directorios raíz de cada agente y, para Claude
Desktop, su configuración o aplicación instalada. No se ejecutan comandos de
terceros, no se descargan credenciales y no se cambia la lista de aprobaciones.
Claude Desktop continúa excluido de `skill install` y de cualquier ámbito de
proyecto.

Cada destino seleccionado conserva las garantías del ADR 0028: validación del
documento antes de escribir, rechazo de symlinks, backup cuando corresponde,
escritura atómica y `0600`. Si un destino falla, los destinos restantes se
intentan y el proceso termina con código distinto de cero.

## Alternativas descartadas

- **Preguntar un cliente por invocación:** obliga a repetir el comando cuando
  el usuario tiene varios agentes.
- **Seleccionar siempre todos los clientes conocidos:** escribiría en rutas de
  agentes que el usuario no tiene instalados.
- **Detectar ejecutables con `PATH`:** no todos los clientes tienen CLI y el
  resultado dependería del shell desde el que se invoque; las rutas nativas son
  la señal estable que ya usa el adaptador.
- **Añadir una librería TUI:** el selector de líneas cubre la elección múltiple
  sin añadir dependencias ni alterar el protocolo de `stdout` de `serve`.

## Consecuencias

El primer uso de `mcp install` y `skill install` requiere una entrada por
`stdin` salvo que se pase `--target`. Los scripts existentes que usan
`--target` siguen siendo válidos. La selección interactiva no inicializa
Ladygraph ni indexa repositorios.
