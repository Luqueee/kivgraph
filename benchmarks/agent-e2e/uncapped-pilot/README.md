# La tríada sin tope de presupuesto

Tres ejecuciones (`api-db-go-f3a50ad`, trial 1, los tres brazos) corridas **sin**
`--max-budget-usd`. Se conservan porque son la evidencia de por qué el barrido
lleva tope: 54, 55 y 75 turnos por `$4,03`, `$4,47` y `$5,60`, ninguno
convergiendo, y `rate_limit_event` de tipo `five_hour` en las tres. No se mezclan
con el barrido: son otra condición experimental.
