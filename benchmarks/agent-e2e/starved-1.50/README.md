# El barrido con tope de `$1,50`, parado a las 4 ejecuciones

Se conserva porque mide una cosa que el barrido definitivo ya no puede mostrar:
con `$1,50` por ejecución, **la tarea Go de 6 archivos ahoga a los tres brazos**.
`cold` escribió 1 archivo de 6 y luego 0; `kivgraph` 0; `graft` 1 y ninguno
correcto. Las cuatro agotaron presupuesto (`terminal_reason: budget_exhausted`).

No se mezcla con el barrido definitivo, que corre a `$3,00` y además lleva tres
arreglos que a estas ejecuciones les faltan: mensaje de commit neutro en el estado
preparado, `.git` denegado a `Read`/`Grep`/`Glob`, y presupuesto agotado
distinguido de fallo real.

La ejecución `go-svc-a-f3a50ad-kivgraph-t1` es la que leyó `.git/logs/HEAD` y
motivó ese cierre; escribió 0 archivos, así que no contaminó ningún resultado.
