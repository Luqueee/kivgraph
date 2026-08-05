# ADR 0010: Motor semántico de TypeScript

- Estado: aceptada
- Fecha: 2026-08-05

## Contexto

El ADR 0005 asumió que el análisis semántico de TypeScript se ejecutaría con la
Compiler API JavaScript dentro de un worker Node.js persistente. Esa suposición
dejó de ser válida.

`typescript@7.0.2` es la versión `latest` publicada y ya no contiene el
compilador en JavaScript. El compilador es un binario nativo por plataforma
distribuido en `optionalDependencies`, y el paquete npm expone únicamente un
cliente RPC sobre `tsgo --api`, con framing msgpack, bajo los subpaths
`typescript/unstable/sync` y `typescript/unstable/async`. `ts.createProgram` y
el `TypeChecker` en proceso ya no existen.

El benchmark versionado en `benchmarks/typescript-engine` compara el motor
nativo `typescript@7.0.2` contra la Compiler API `typescript@5.9.3` sobre un
corpus generado idéntico. Ambos motores producen el mismo número de
diagnósticos semánticos, lo que confirma que analizan programas equivalentes.

El resultado no es que el motor nativo sea uniformemente más rápido, sino que
cambia el modelo de coste:

- carga en frío entre `4.4x` y `4.9x` más rápida, constante con el tamaño;
- chequeo semántico completo entre `1.45x` y `4.85x` más rápido;
- residente entre `1.4x` y `2.3x` menor, contando el proceso servidor;
- referencias con alcance de archivo entre `21x` y `462x` más rápidas, con la
  ventaja creciendo con el corpus;
- re-resolución tras una edición entre `20x` y `37x` más rápida;
- resolución de un símbolo suelto entre `3x` y `7x` más lenta, por un
  round-trip fijo de 70 a 140 microsegundos;
- lectura completa de los exports de un barrel hasta `130x` más lenta, porque
  cada símbolo transferido paga su propio coste;
- resolución por lotes de 50 posiciones de un archivo entre `1.34x` y `1.48x`
  más rápida, es decir, unos 5,6 microsegundos por símbolo frente a 7,5.

## Decisión

El motor semántico de TypeScript es el compilador nativo de TypeScript 7,
consumido mediante la API asíncrona del paquete oficial. Es el único motor.

La versión se fija de forma exacta. La superficie consumida está marcada
`unstable` por el propio paquete, por lo que un test de contrato debe fallar
cuando esa superficie cambie, en lugar de degradarse en silencio.

El protocolo del worker es **por lotes y con granularidad de archivo**. Una
petición identifica un archivo y un conjunto de posiciones o de símbolos.
Queda prohibida una petición por símbolo: esa forma convierte la ventaja
medida en una pérdida medida. Tampoco se materializan conjuntos completos de
exports de un módulo cuando existe una consulta más estrecha.

Se usa la API asíncrona y no la síncrona. El supervisor exige cancelación y
backpressure, y la variante síncrona bloquea el bucle de eventos del worker
mientras espera la respuesta del servidor nativo, impidiendo atender una
cancelación en curso.

No se implementa un segundo motor para proyectos fijados a versiones antiguas
de TypeScript. Cuando la versión declarada por el proyecto queda fuera de la
ventana soportada, el análisis se realiza igualmente con el motor nativo y los
hechos resultantes se emiten como `CANDIDATE`, registrando el motivo y la
versión efectiva. La divergencia de semántica es una limitación declarada, no
un hecho exacto no verificado.

El worker sigue siendo un proceso Node.js supervisado por Go. No se
reimplementa el protocolo msgpack del servidor nativo en Go: es un contrato
generado y no documentado para terceros, y `github.com/microsoft/typescript-go`
no publica ninguna versión etiquetada. El trabajo pesado ya ocurre en el
binario nativo, por lo que el cliente JavaScript no es el cuello de botella.

El runtime del worker es Node.js. Bun queda descartado: la API síncrona de
TypeScript 7 falla en Bun 1.3.14 al acceder a `stdout._handle.fd`, y el runtime
JavaScript no es el componente que domina el coste.

## Alternativas

- **Mantener la Compiler API de TypeScript 5 como motor único.** Conserva
  fidelidad con proyectos antiguos, pero renuncia a toda la ventaja medida y
  carece de una primitiva de referencias con alcance de archivo.
- **Implementar ambos motores desde el principio.** Duplica el mapeo de
  símbolos, las referencias, la ruta incremental, los fixtures y los tests, y
  retrasa la fase completa para cubrir un caso que todavía no se ha observado
  en repositorios reales.
- **Hablar el protocolo nativo directamente desde Go.** Eliminaría el worker
  Node.js, pero exige mantener un protocolo generado e inestable sin versiones
  publicadas.

## Consecuencias

- El artefacto distribuible incorpora los binarios nativos por plataforma del
  compilador, junto a LadybugDB y las grammars, y el manifiesto reproducible
  debe registrarlos.
- El diseño del protocolo del worker queda restringido: lotes por archivo,
  cancelación y ausencia de consultas por símbolo.
- Un proyecto fuera de la ventana de versiones soportadas nunca produce una
  arista `EXACT`; produce `CANDIDATE` con motivo registrado.
- Una actualización del compilador puede romper la superficie `unstable`; el
  test de contrato convierte esa rotura en un fallo de CI.
- Añadir más adelante la ruta de la Compiler API es un cambio aditivo y exige
  un ADR nuevo.

## Riesgos

- La API está marcada `unstable` y puede cambiar entre versiones menores.
- La equivalencia semántica entre TypeScript 5 y 7 no se ha medido sobre
  repositorios reales; por eso la degradación a `CANDIDATE` es obligatoria y no
  opcional.
- El servidor nativo es un proceso adicional que el supervisor debe vigilar,
  reiniciar y limpiar junto al worker.
