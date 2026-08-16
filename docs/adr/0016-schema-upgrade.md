# ADR 0016: Upgrade de schema mediante reconstrucción

**Estado:** aceptada
**Fecha:** 2026-08-07

## Contexto

El schema canónico de LadybugDB está versionado y una generación publicada puede
contener una versión anterior. Cambiar columnas o relaciones en esa base no
ofrece una conversión segura: una interpretación incompleta podría convertir
hechos en aristas inválidas y dejar `CURRENT` apuntando a datos no verificables.

## Decisión

`kivgraph upgrade` sigue este flujo:

1. Detecta la generación activa y la versión declarada en `GraphMetadata`.
2. Si la versión ya es la actual, informa un no-op; no reindexa ni publica.
3. Si es anterior, copia la generación completa a un backup determinista,
   escribiendo un manifiesto con tamaños y SHA-256 y publicándolo mediante
   rename atómico.
4. Reindexa todos los repositorios registrados desde sus fuentes, sin usar la
   base antigua como fuente de hechos, y construye una generación candidata con
   el pipeline normal de `rebuild.Run`.
5. Exige los gates de integridad, snapshot, digest y probes del rebuild antes
   de aceptar la publicación.
6. Ante un fallo de extracción o antes de cambiar `CURRENT`, conserva la
   generación anterior. Si falla la validación posterior a la publicación,
   restaura la anterior únicamente después de verificarla contra el manifiesto
   del backup.

No se modifica una base existente en el sitio ni se intenta inferir el
significado de columnas de un schema no reconocido. La publicación sigue siendo
la única transición de autoridad y el backup conserva una ruta explícita de
recuperación.

## Consecuencias

- Un upgrade requiere espacio para el backup y una generación nueva.
- La reconstrucción puede tardar tanto como una indexación full.
- El backup es portable y auditable, y una modificación o archivo inesperado en
  la generación retenida impide el rollback en lugar de reactivarla a ciegas.
- Un schema sintético o una versión posterior se rechaza explícitamente; no se
  convierte silenciosamente.
