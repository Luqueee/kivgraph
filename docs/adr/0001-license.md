# ADR 0001: Licencia del proyecto

- **Estado:** aceptada
- **Fecha:** 2026-08-04

## Context

Kivgraph es un servidor MCP distribuible como software libre. El proyecto debe
permitir su uso, modificación y redistribución, y debe mantener una política
clara para contribuciones y dependencias nativas. La integración prevista con
LadybugDB puede incorporar componentes con avisos de copyright y obligaciones
de redistribución que deben poder documentarse de forma explícita.

## Decision

Kivgraph se distribuye bajo **Apache License 2.0** (`Apache-2.0`).

La licencia completa se encuentra en `LICENSE`. Los avisos y las licencias de
terceros se registrarán en `THIRD_PARTY_NOTICES.md` a medida que se incorporen
dependencias al producto distribuible.

## Alternatives

### MIT

MIT es breve, permisiva y sencilla de aplicar. Sin embargo, no incluye una
concesión expresa de patentes. Esa omisión deja menos claro el tratamiento de
contribuciones que puedan estar cubiertas por derechos de patente.

### Apache-2.0

Apache-2.0 mantiene una licencia permisiva y añade una concesión expresa de
patentes, reglas para conservar avisos y condiciones claras para redistribuir
archivos modificados. Su texto es más largo y exige mantener una disciplina
documental mayor que MIT.

## Consequences

- Los usuarios pueden usar, modificar y redistribuir Kivgraph bajo los términos
  de Apache-2.0.
- Las redistribuciones deben conservar la licencia y los avisos aplicables.
- Los archivos modificados deben indicar los cambios cuando corresponda.
- La concesión de patentes de Apache-2.0 reduce la ambigüedad jurídica para
  contribuciones cubiertas por patentes.
- Cada nueva dependencia distribuida debe añadirse al inventario de terceros y
  revisarse antes de incorporarla.

## Risks

- La licencia de LadybugDB y de sus dependencias nativas puede imponer avisos o
  condiciones adicionales; se revisará al fijar esas versiones en LUQUE-0201.
- No se debe asumir que la licencia de una dependencia es compatible sin
  verificar su texto y sus obligaciones de redistribución.
- El inventario de terceros puede quedar incompleto si se incorpora una
  dependencia sin actualizar `THIRD_PARTY_NOTICES.md`.

## Status

Aceptada para el repositorio base. La validación concreta de las dependencias
nativas queda pendiente de LUQUE-0201.
