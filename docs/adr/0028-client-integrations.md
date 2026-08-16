# ADR 0028: Integraciones locales con clientes MCP y Agent Skills

- **Estado:** aceptada
- **Fecha:** 2026-08-10
- **Decisión:** añadir subcomandos explícitos para registrar el servidor MCP y
  copiar la skill de Kivgraph en los formatos nativos de cada cliente
  soportado, sin convertir la configuración externa en una nueva fuente de
  hechos ni alterar el ciclo de vida del indexador.

## Contexto

El bundle ya instala el binario y `kivgraph update` lo mantiene actualizado,
pero el usuario todavía debe registrar manualmente `kivgraph serve` en cada
cliente. Claude Code, Claude Desktop, Codex, OpenCode y Oh My Pi no comparten el
mismo fichero ni el mismo esquema. Las skills sí comparten el formato
`SKILL.md` de Agent Skills en parte de la matriz, pero cada cliente descubre
rutas distintas.

Una instalación automática que reescriba un JSON o TOML completo puede borrar
configuración ajena, comentarios, permisos o políticas de aprobación. Además,
Oh My Pi debe conservar la aprobación explícita de la herramienta mutante
`index_project`.

## Decisión

La CLI expone dos familias independientes:

```text
kivgraph mcp install --target TARGET [--scope user|project]
kivgraph mcp status --target TARGET [--scope user|project]
kivgraph mcp remove --target TARGET [--scope user|project]

kivgraph skill install --target TARGET [--scope user|project]
kivgraph skill status --target TARGET [--scope user|project]
kivgraph skill remove --target TARGET [--scope user|project]
```

`--target` es explícito y admite un cliente por invocación. El comando no
inicializa Kivgraph, no indexa repositorios y no descarga credenciales. La
configuración que registra el MCP usa el ejecutable absoluto que está
atendiendo la invocación y los argumentos `serve`; el launcher estable de la
instalación hace que una actualización posterior no requiera reescribir los
clientes.

La selección interactiva y la instalación en varios clientes sin
`--target` se añadieron posteriormente en el ADR 0029. Las operaciones
`status` y `remove` conservan el target explícito.

Los adaptadores de cliente viven detrás de un módulo interno común. Ese módulo
calcula un plan, valida el estado existente y aplica una escritura atómica. No
se expone a la CLI el esquema específico de ningún cliente.

### Targets MCP

| Target | Ámbito de usuario | Ámbito de proyecto | Formato |
| --- | --- | --- | --- |
| `claude-code` | `~/.claude.json`, clave `mcpServers` | `.mcp.json` | JSON |
| `claude-desktop` | macOS `~/Library/Application Support/Claude/claude_desktop_config.json`; Linux `~/.config/Claude/claude_desktop_config.json` | no soportado | JSON |
| `codex` | `~/.codex/config.toml` | `.codex/config.toml` | TOML, tabla `[mcp_servers.<name>]` |
| `opencode` | `~/.config/opencode/opencode.json` | `opencode.json` | JSON, mapa `mcp` |
| `oh-my-pi` | `~/.omp/agent/mcp.json` | `.omp/mcp.json` | JSON, mapa `mcpServers` |

La matriz se limita inicialmente a los sistemas publicados por Kivgraph
(`darwin/arm64` y `linux/amd64`). Un target sin ámbito solicitado devuelve un
error explícito; no se elige silenciosamente otro fichero.

### Targets de skill

La skill canónica se distribuye como `SKILL.md` con frontmatter estándar:

| Target | Ámbito de usuario | Ámbito de proyecto |
| --- | --- | --- |
| `claude-code` | `~/.claude/skills/kivgraph/SKILL.md` | `.claude/skills/kivgraph/SKILL.md` |
| `codex` | `~/.agents/skills/kivgraph/SKILL.md` | `.agents/skills/kivgraph/SKILL.md` |
| `opencode` | `~/.config/opencode/skills/kivgraph/SKILL.md` | `.opencode/skills/kivgraph/SKILL.md` |
| `oh-my-pi` | `~/.omp/agent/skills/kivgraph/SKILL.md` | `.omp/skills/kivgraph/SKILL.md` |

`claude-desktop` no se ofrece como target de skill local: Claude Desktop
administra las skills de la cuenta o del producto y no comparte la ruta local
de Claude Code.

### Escritura segura

- Se crean directorios nuevos con `0700` y archivos con `0600`.
- No se siguen symlinks en el fichero destino.
- Una configuración inválida o una entrada `kivgraph` incompatible detiene la
  operación; no se repara ni se sobrescribe automáticamente.
- Una entrada ya equivalente es una operación idempotente y no toca el fichero.
- Una modificación conserva las claves desconocidas de JSON y, en Codex,
  añade una tabla TOML sin regenerar el documento completo.
- Antes de reemplazar un fichero existente se conserva una copia
  `*.kivgraph.bak` y luego se hace `fsync` más `rename` en el mismo directorio.
- `--dry-run` muestra el plan sin escribir. `--force` es obligatorio para
  reemplazar o retirar una entrada que no coincide con la configuración que
  Kivgraph gestionaría.
- El instalador no modifica listas de aprobación. En Oh My Pi conserva
  `tools.approval.kivgraph_1mcp_index_project: prompt` si ya existe.

La skill se embebe en el ejecutable y también se copia al bundle generado para
que `skill install` funcione sin una checkout del repositorio. El bundle incluye
la skill en `skills/kivgraph/SKILL.md` dentro de sus checksums.

## Alternativas descartadas

- **Un único JSON universal:** Codex usa TOML y OpenCode usa el mapa `mcp` con
  `type: local`; no describe correctamente los cinco clientes.
- **Editar siempre ficheros globales:** impediría instalaciones por proyecto y
  escribiría fuera del ámbito que el usuario eligió.
- **Llamar siempre a `claude mcp add` o `codex mcp add`:** depende de que el
  cliente esté instalado y oculta qué fichero se modifica; el adaptador directo
  es verificable en un `HOME` temporal.
- **Instalar la skill en una carpeta compartida por todos:** cambia la
  precedencia de skills existentes y no respeta los proveedores nativos de
  cada cliente.
- **Empaquetar primero un `.mcpb` para Claude Desktop:** es una distribución
  distinta, con manifest y ciclo de configuración propios; queda fuera de
  esta primera integración de configuración local.

## Referencias

- [Claude Code MCP quickstart](https://code.claude.com/docs/en/mcp-quickstart)
- [Claude Code skills](https://code.claude.com/docs/en/skills)
- [Codex MCP](https://developers.openai.com/codex/mcp)
- [Codex skills](https://learn.chatgpt.com/docs/build-skills)
- [OpenCode MCP servers](https://opencode.ai/docs/mcp-servers/)
- [OpenCode skills](https://opencode.ai/docs/skills/)
- [Oh My Pi MCP config](https://github.com/can1357/oh-my-pi/blob/main/docs/mcp-config.md)
- [Oh My Pi skills](https://github.com/can1357/oh-my-pi/blob/main/docs/skills.md)

## Consecuencias

La integración añade una superficie de compatibilidad con productos externos y
requiere fixtures por target, pruebas de idempotencia y un smoke test con
`HOME` temporal. Si un cliente cambia su esquema, el adaptador debe fallar
cerrado hasta actualizar este ADR, sus tests y su documentación. La skill
instalada no se actualiza automáticamente al actualizar el bundle: el usuario
debe ejecutar `kivgraph skill install` de nuevo o usar el futuro comando de
actualización explícita.
