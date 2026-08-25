# ADR 0074: el presupuesto de superficie tiene dos formas

- **Estado:** aceptada
- **Fecha:** 2026-08-25
- **Cambia el protocolo MCP:** sí -- dos descripciones de tool se acortan; ningún
  nombre, esquema ni comportamiento cambia
- **Cambia el schema persistente:** no
- **Obliga a reconstruir:** no
- **Cambia la superficie del CLI:** no
- **Relaja un contrato de la raíz:** no -- lo aprieta, porque añade una superficie
  que hasta hoy no estaba en ninguna balanza

## Contexto: el guardia medía un servidor que nadie sirve

`TestServerSurfaceStaysCheapToKeepResident` mide `nombre * 2 + descripción` sobre
las tools que un cliente mantiene residentes, con un techo de `1900` bytes. Lo
medía sobre `publishedServer`, construido con `NewServerWithSnapshotStore` -- que
**no registra `index_project`**.

`kivgraph serve` sí lo registra. Medido contra el binario, la superficie real eran
`2110` bytes: `210` por encima de un techo que el test declaraba cumplido con
`1897`. El hallazgo salió de medir el coste de una tool nueva, no de un fallo.

Y había una segunda cosa: la misma afirmación se pagaba dos veces. Las
`instructions` del handshake -- también residentes-- ya dicen «Where it loses: a
rare name in a single small repository is cheaper to grep», y `find_references` y
`find_symbol` la repetían cada una en su descripción.

## Decisión

**Una: se retira la duplicación.** La frase sobrevive exactamente una vez, en las
`instructions`, que es el único sitio donde vale para las doce tools a la vez.
`find_references` pasa de `364` a `314` bytes y `find_symbol` de `187` a `135`.
Las dos páginas de la landing que citaban la descripción literal se sincronizan.

**Dos: se guardan las dos formas del servidor**, con una línea de presupuesto cada
una:

|superficie|tools|bytes|techo|
|---|---|---|---|
|consulta, la que todo cliente mantiene|`12`|`1795`|`MaximumResidentSurfaceBytes` `1900`|
|con indexado, la que un cliente configurado recibe|`13`|`2008`|`MaximumIndexingSurfaceBytes` `2100`|

Dos líneas y no una, porque un cliente que nunca configura el indexado nunca paga
la tool mutante. Y el test nuevo comprueba además que **las dos formas difieren en
exactamente una tool**: sin eso, el techo de consulta se podría cumplir moviendo
una descripción a la tool que sólo algunos clientes ven, que es trucar la métrica
en vez de cumplirla.

## Consecuencias

- El presupuesto de consulta queda con `105` bytes de holgura y el de indexado con
  `92`. Antes la holgura declarada era de `3` bytes sobre una cuenta incompleta.
- El guardia nuevo se vio fallar antes de aceptarlo: a techo `1990` dice
  `indexing surface = 2008 bytes over 12 tools`.
- La cifra en **tokens** que el comentario del guardia cita sigue siendo la de
  `11` tools, y no se puede refrescar con una corrida: el arnés falla cerrado
  porque sus capturas nativas son de la generación `000001`. Queda en
  `LUQUE-2227`, y el presupuesto vive en bytes mientras tanto.

## Lo que este ADR no cierra

Que `index_project` cuesta `213` bytes y cada frase suya es una restricción que el
llamante necesita -- una llamada para todos los proyectos, consentimiento
explícito, nunca escribe dentro de las fuentes--. No hay grasa ahí. Si llega una
tool decimocuarta, algo se retira: el techo no se sube por llegar.
