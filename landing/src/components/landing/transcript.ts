/**
 * The transcript figures on this page render traffic captured from
 * `ladygraph serve` over stdio. Field names, values and error strings are
 * verbatim; only the line wrapping is ours, and every line that is not part of
 * the capture is prefixed with `#`.
 *
 * The tones are the viewer palette declared in src/styles/global.css, doing the
 * same job here that it does in the graph: one colour per node kind, plus one
 * for an exact, type-checked edge.
 */

export type Tone = "dim" | "tool" | "file" | "symbol" | "exact";

export interface Piece {
  text: string;
  tone?: Tone;
}

export type Line = Piece[];

export const TONE_CLASS: Record<Tone, string> = {
  dim: "text-gray-400",
  tool: "text-accent-200",
  file: "text-graph-file",
  symbol: "text-graph-symbol",
  exact: "text-graph-exact",
};
